package proxy

import (
	"bytes"
	"encoding/json"
	"sync"
	"unicode/utf8"
)

// Allocation-light JSON helpers for the request path. Every helper here
// produces exactly the bytes encoding/json produces for the same value
// (json_fast_test.go checks this), so wire output does not change.

const jsonHex = "0123456789abcdef"

// jsonReplacementEscape is how encoding/json writes an invalid UTF-8 byte.
const jsonReplacementEscape = "\\ufffd"

// appendJSONString appends s as a JSON string, escaped like json.Marshal
// (HTML-safe: <, > and & become <, > and &; invalid UTF-8
// becomes �; U+2028 and U+2029 are escaped).
func appendJSONString(dst []byte, s string) []byte {
	dst = append(dst, '"')
	start := 0
	for i := 0; i < len(s); {
		if b := s[i]; b < utf8.RuneSelf {
			if b >= 0x20 && b != '"' && b != '\\' && b != '<' && b != '>' && b != '&' {
				i++
				continue
			}
			dst = append(dst, s[start:i]...)
			switch b {
			case '\\', '"':
				dst = append(dst, '\\', b)
			case '\b':
				dst = append(dst, '\\', 'b')
			case '\f':
				dst = append(dst, '\\', 'f')
			case '\n':
				dst = append(dst, '\\', 'n')
			case '\r':
				dst = append(dst, '\\', 'r')
			case '\t':
				dst = append(dst, '\\', 't')
			default:
				dst = append(dst, '\\', 'u', '0', '0', jsonHex[b>>4], jsonHex[b&0xF])
			}
			i++
			start = i
			continue
		}
		c, size := utf8.DecodeRuneInString(s[i:])
		if c == utf8.RuneError && size == 1 {
			dst = append(dst, s[start:i]...)
			dst = append(dst, jsonReplacementEscape...)
			i += size
			start = i
			continue
		}
		if c == ' ' || c == ' ' {
			dst = append(dst, s[start:i]...)
			dst = append(dst, '\\', 'u', '2', '0', '2', jsonHex[c&0xF])
			i += size
			start = i
			continue
		}
		i += size
	}
	dst = append(dst, s[start:]...)
	return append(dst, '"')
}

// jsonBufPool holds scratch buffers for encoding.
var jsonBufPool = sync.Pool{New: func() interface{} { return new(bytes.Buffer) }}

func getJSONBuf() *bytes.Buffer {
	b := jsonBufPool.Get().(*bytes.Buffer)
	b.Reset()
	return b
}

func putJSONBuf(b *bytes.Buffer) {
	if b.Cap() > 1<<20 {
		return // let very large buffers go
	}
	jsonBufPool.Put(b)
}

// countingWriter counts bytes written to it.
type countingWriter struct{ n int }

func (c *countingWriter) Write(p []byte) (int, error) {
	c.n += len(p)
	return len(p), nil
}

// jsonSize reports len(json.Marshal(v)) without keeping the encoded bytes.
func jsonSize(v interface{}) (int, error) {
	var cw countingWriter
	if err := json.NewEncoder(&cw).Encode(v); err != nil {
		return 0, err
	}
	return cw.n - 1, nil // Encode adds a trailing newline
}
