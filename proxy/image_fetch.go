package proxy

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"github.com/springmusk026/Kiro-Gateway/config"
	"io"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"
)

// Remote media fetching.
//
// Kiro only accepts inline (base64) images and documents, so image and
// document blocks that reference an http(s) URL are downloaded by the gateway
// before translation. The download is the request path's only outbound call to
// an arbitrary host, so it is locked down against server-side request forgery:
//
//   - https only (plain http only with KIRO_FETCH_ALLOW_HTTP=1);
//   - the host is resolved first and every address must be public: loopback,
//     private, link-local (incl. cloud metadata 169.254.169.254), multicast,
//     CGNAT, unspecified and reserved ranges are refused;
//   - on a direct connection the dialer re-checks the address it actually
//     connects to, so a DNS answer that changes between check and dial (DNS
//     rebinding) is still refused;
//   - through an outbound proxy the gateway cannot see the final address, so
//     the hostname is resolved and checked before the request is handed over;
//   - every redirect hop is re-validated the same way, at most 3 redirects;
//   - 10 s timeout, a size cap, and the content type must be in the allowed
//     set both by header and by the leading bytes.
//
// Nothing is cached, on disk or in memory. KIRO_FETCH_IMAGE_URLS=0 disables
// fetching; such requests are then rejected with a clear 400.

const (
	mediaFetchTimeout      = 10 * time.Second
	maxMediaFetchRedirects = 3
	// maxFetchedImageBytes matches Kiro's per-image base64 budget for Claude
	// models (5 MB); larger images are rejected upstream anyway.
	maxFetchedImageBytes = 5 << 20
	// maxRemoteMediaPerRequest bounds how many URLs one request may make the
	// gateway download, so a single request cannot fan out into many fetches.
	maxRemoteMediaPerRequest = 20
)

// mediaRequestError is a client error found while preparing images or
// documents; handlers answer it with 400 invalid_request_error.
type mediaRequestError struct{ msg string }

func (e *mediaRequestError) Error() string { return e.msg }

func newMediaError(format string, args ...interface{}) error {
	return &mediaRequestError{msg: fmt.Sprintf(format, args...)}
}

// asMediaError reports whether err is a client-facing media error.
func asMediaError(err error) (*mediaRequestError, bool) {
	var me *mediaRequestError
	if errors.As(err, &me) {
		return me, true
	}
	return nil, false
}

func envDisabled(name string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(name))) {
	case "0", "false", "off", "no":
		return true
	}
	return false
}

func envEnabled(name string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(name))) {
	case "1", "true", "on", "yes":
		return true
	}
	return false
}

// mediaURLFetchEnabled reports whether http(s) media URLs are downloaded.
// On by default; KIRO_FETCH_IMAGE_URLS=0 turns it off.
func mediaURLFetchEnabled() bool { return !envDisabled("KIRO_FETCH_IMAGE_URLS") }

// mediaFetchAllowHTTP allows plain-http URLs (off by default).
func mediaFetchAllowHTTP() bool { return envEnabled("KIRO_FETCH_ALLOW_HTTP") }

// mediaFetchAllowLoopbackForTest lets tests fetch from httptest servers. It
// only relaxes the loopback check; every other refusal still applies.
var mediaFetchAllowLoopbackForTest = false

var (
	cgnatPrefix     = netip.MustParsePrefix("100.64.0.0/10")
	thisNetPrefix   = netip.MustParsePrefix("0.0.0.0/8")
	ietfPrefix      = netip.MustParsePrefix("192.0.0.0/24")
	benchmarkPrefix = netip.MustParsePrefix("198.18.0.0/15")
	reservedPrefix  = netip.MustParsePrefix("240.0.0.0/4")
	nat64Prefix     = netip.MustParsePrefix("64:ff9b::/96")
	testNetPrefixes = []netip.Prefix{
		netip.MustParsePrefix("192.0.2.0/24"),
		netip.MustParsePrefix("198.51.100.0/24"),
		netip.MustParsePrefix("203.0.113.0/24"),
		netip.MustParsePrefix("2001:db8::/32"),
	}
)

// disallowedFetchAddr returns why addr may not be fetched from, or "".
func disallowedFetchAddr(addr netip.Addr) string {
	addr = addr.Unmap()
	if nat64Prefix.Contains(addr) {
		// NAT64 embeds an IPv4 address in the last 32 bits.
		b := addr.As16()
		addr = netip.AddrFrom4([4]byte{b[12], b[13], b[14], b[15]})
	}
	switch {
	case !addr.IsValid():
		return "invalid address"
	case addr.IsLoopback():
		if mediaFetchAllowLoopbackForTest {
			return ""
		}
		return "loopback address"
	case addr.IsUnspecified():
		return "unspecified address"
	case addr.IsPrivate():
		return "private address"
	case addr.IsLinkLocalUnicast(), addr.IsLinkLocalMulticast():
		return "link-local address"
	case addr.IsMulticast(), addr.IsInterfaceLocalMulticast():
		return "multicast address"
	case cgnatPrefix.Contains(addr):
		return "carrier-grade NAT address"
	case thisNetPrefix.Contains(addr), ietfPrefix.Contains(addr), benchmarkPrefix.Contains(addr), reservedPrefix.Contains(addr):
		return "reserved address"
	case addr.Is4() && addr == netip.AddrFrom4([4]byte{255, 255, 255, 255}):
		return "broadcast address"
	}
	for _, p := range testNetPrefixes {
		if p.Contains(addr) {
			return "reserved address"
		}
	}
	return ""
}

// mediaResolver is swapped in tests.
var mediaResolver = func(ctx context.Context, host string) ([]netip.Addr, error) {
	ips, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return nil, err
	}
	return ips, nil
}

// validateFetchURL checks a URL (initial or redirect target) before any
// connection is made: scheme, credentials, and that every address the host
// resolves to is public.
func validateFetchURL(ctx context.Context, u *url.URL) error {
	switch strings.ToLower(u.Scheme) {
	case "https":
	case "http":
		if !mediaFetchAllowHTTP() {
			return newMediaError("media URL %q must use https", redactURL(u))
		}
	default:
		return newMediaError("media URL scheme %q is not supported; use https or base64 data", u.Scheme)
	}
	if u.User != nil {
		return newMediaError("media URL must not contain credentials")
	}
	host := u.Hostname()
	if host == "" {
		return newMediaError("media URL has no host")
	}
	var addrs []netip.Addr
	if ip, err := netip.ParseAddr(strings.Trim(host, "[]")); err == nil {
		addrs = []netip.Addr{ip}
	} else {
		lower := strings.ToLower(strings.TrimSuffix(host, "."))
		if lower == "localhost" || strings.HasSuffix(lower, ".localhost") {
			if !mediaFetchAllowLoopbackForTest {
				return newMediaError("media URL host %q is not allowed (loopback address)", host)
			}
		}
		rctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		resolved, err := mediaResolver(rctx, host)
		if err != nil || len(resolved) == 0 {
			return newMediaError("media URL host %q could not be resolved", host)
		}
		addrs = resolved
	}
	for _, a := range addrs {
		if why := disallowedFetchAddr(a); why != "" {
			return newMediaError("media URL host %q is not allowed (%s)", host, why)
		}
	}
	return nil
}

// redactURL drops query strings (often signed tokens) from error messages.
func redactURL(u *url.URL) string {
	c := *u
	c.RawQuery = ""
	c.Fragment = ""
	c.User = nil
	return c.String()
}

// safeDialControl refuses connections to disallowed addresses at connect
// time. It runs for direct connections only (not through a proxy).
func safeDialControl(network, address string, _ syscall.RawConn) error {
	ap, err := netip.ParseAddrPort(address)
	if err != nil {
		return fmt.Errorf("refusing to dial %q: %v", address, err)
	}
	if why := disallowedFetchAddr(ap.Addr()); why != "" {
		return &mediaRequestError{msg: fmt.Sprintf("media URL resolved to a disallowed %s", why)}
	}
	return nil
}

// mediaFetchProxy returns the outbound proxy for target, following the same
// rule as the upstream client: the configured proxy, else the environment.
func mediaFetchProxy(target *url.URL) (*url.URL, error) {
	if p := strings.TrimSpace(config.GetProxyURL()); p != "" {
		return url.Parse(p)
	}
	return mediaEnvProxy(&http.Request{URL: target})
}

// mediaEnvProxy is the environment proxy lookup. Tests replace it:
// http.ProxyFromEnvironment caches the environment on first use, which would
// leak the test host's proxy settings into unrelated tests.
var mediaEnvProxy = http.ProxyFromEnvironment

// newMediaFetchClient builds a one-off client for a fetch. With a proxy the
// connection goes to the proxy, so the address check happens in
// validateFetchURL; without one the dialer checks the real address too.
func newMediaFetchClient(ctx context.Context, proxyURL *url.URL) *http.Client {
	dialer := &net.Dialer{Timeout: mediaFetchTimeout}
	tr := &http.Transport{
		TLSHandshakeTimeout:   mediaFetchTimeout,
		ResponseHeaderTimeout: mediaFetchTimeout,
		MaxIdleConns:          1,
		IdleConnTimeout:       time.Second,
		DisableKeepAlives:     true,
	}
	if proxyURL != nil {
		tr.Proxy = http.ProxyURL(proxyURL)
	} else {
		dialer.Control = safeDialControl
	}
	tr.DialContext = dialer.DialContext
	return &http.Client{
		Timeout:   mediaFetchTimeout,
		Transport: tr,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) > maxMediaFetchRedirects {
				return newMediaError("media URL redirected more than %d times", maxMediaFetchRedirects)
			}
			return validateFetchURL(ctx, req.URL)
		},
	}
}

// remoteMediaKind selects which content types a fetch accepts.
type remoteMediaKind int

const (
	remoteImage remoteMediaKind = iota
	remoteDocument
)

// sniffImageType returns png/jpeg/gif/webp from the leading bytes, or "".
func sniffImageType(b []byte) string {
	switch {
	case bytes.HasPrefix(b, []byte("\x89PNG\r\n\x1a\n")):
		return "png"
	case bytes.HasPrefix(b, []byte{0xFF, 0xD8, 0xFF}):
		return "jpeg"
	case bytes.HasPrefix(b, []byte("GIF87a")), bytes.HasPrefix(b, []byte("GIF89a")):
		return "gif"
	case len(b) >= 12 && bytes.Equal(b[0:4], []byte("RIFF")) && bytes.Equal(b[8:12], []byte("WEBP")):
		return "webp"
	}
	return ""
}

// fetchedMedia is a downloaded image or document.
type fetchedMedia struct {
	Data      []byte
	MediaType string // e.g. image/png, application/pdf, text/plain
	FileName  string // last path segment of the final URL
}

// fetchRemoteMedia downloads rawURL under the rules above.
func fetchRemoteMedia(ctx context.Context, rawURL string, kind remoteMediaKind, maxBytes int64) (*fetchedMedia, error) {
	if !mediaURLFetchEnabled() {
		return nil, newMediaError("fetching media from URLs is disabled on this gateway (KIRO_FETCH_IMAGE_URLS=0); send the content as base64 data instead")
	}
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || u.Host == "" {
		return nil, newMediaError("invalid media URL")
	}
	if err := validateFetchURL(ctx, u); err != nil {
		return nil, err
	}
	proxyURL, err := mediaFetchProxy(u)
	if err != nil {
		return nil, fmt.Errorf("media fetch proxy: %w", err)
	}

	fctx, cancel := context.WithTimeout(ctx, mediaFetchTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(fctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, newMediaError("invalid media URL")
	}
	if kind == remoteImage {
		req.Header.Set("Accept", "image/png, image/jpeg, image/gif, image/webp")
	} else {
		req.Header.Set("Accept", "application/pdf, text/plain, text/markdown, text/csv, text/html")
	}
	resp, err := newMediaFetchClient(ctx, proxyURL).Do(req)
	if err != nil {
		if me, ok := asMediaError(err); ok {
			return nil, me
		}
		if fctx.Err() != nil && ctx.Err() == nil {
			return nil, newMediaError("fetching media URL %q timed out", redactURL(u))
		}
		return nil, newMediaError("fetching media URL %q failed", redactURL(u))
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, newMediaError("fetching media URL %q returned HTTP %d", redactURL(u), resp.StatusCode)
	}
	if resp.ContentLength > maxBytes {
		return nil, newMediaError("media at %q is %d bytes, over the %d byte limit", redactURL(u), resp.ContentLength, maxBytes)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return nil, newMediaError("reading media URL %q failed", redactURL(u))
	}
	if int64(len(data)) > maxBytes {
		return nil, newMediaError("media at %q is over the %d byte limit", redactURL(u), maxBytes)
	}

	mediaType, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	mediaType = strings.ToLower(mediaType)
	final := resp.Request.URL
	name := ""
	if final != nil {
		if i := strings.LastIndex(final.Path, "/"); i >= 0 {
			name = final.Path[i+1:]
		}
	}

	switch kind {
	case remoteImage:
		headerFormat := strings.TrimPrefix(mediaType, "image/")
		if headerFormat == "jpg" {
			headerFormat = "jpeg"
		}
		if !strings.HasPrefix(mediaType, "image/") || !isSupportedImageFormat(headerFormat) {
			return nil, newMediaError("media URL %q has content type %q; only image/png, image/jpeg, image/gif and image/webp are supported", redactURL(u), mediaType)
		}
		sniffed := sniffImageType(data)
		if sniffed == "" {
			return nil, newMediaError("media URL %q did not return a png, jpeg, gif or webp image", redactURL(u))
		}
		// The bytes decide the format: a mislabeled png is still a png.
		return &fetchedMedia{Data: data, MediaType: "image/" + sniffed, FileName: name}, nil
	default:
		switch mediaType {
		case "application/pdf":
			if !bytes.HasPrefix(data, []byte("%PDF-")) {
				return nil, newMediaError("media URL %q is labeled application/pdf but is not a PDF", redactURL(u))
			}
		case "text/plain", "text/markdown", "text/csv", "text/html":
			if !utf8.Valid(data) {
				return nil, newMediaError("media URL %q returned text that is not valid UTF-8", redactURL(u))
			}
		default:
			return nil, newMediaError("document URL %q has content type %q; supported: application/pdf, text/plain, text/markdown, text/csv, text/html", redactURL(u), mediaType)
		}
		return &fetchedMedia{Data: data, MediaType: mediaType, FileName: name}, nil
	}
}

func isSupportedImageFormat(format string) bool {
	switch format {
	case "png", "jpeg", "gif", "webp":
		return true
	}
	return false
}

// isRemoteURL reports whether s is an http(s) URL (as opposed to data: or
// raw base64).
func isRemoteURL(s string) bool {
	lower := strings.ToLower(strings.TrimSpace(s))
	return strings.HasPrefix(lower, "https://") || strings.HasPrefix(lower, "http://")
}
