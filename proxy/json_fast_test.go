package proxy

import (
	"encoding/json"
	"math"
	"math/rand"
	"strings"
	"testing"
)

func jsonStringCases() []string {
	cases := []string{
		"", "plain", `quote " and \ backslash`, "<script>&amp;</script>",
		"\b\f\n\r\t\x00\x01\x1f\x7f", "emoji 😀 and accents éè", "line sep para",
		"invalid \xff\xfe utf8 \xc3", "\xed\xa0\x80 surrogate", strings.Repeat("long text ", 500),
	}
	r := rand.New(rand.NewSource(1))
	for i := 0; i < 2000; i++ {
		b := make([]byte, r.Intn(40))
		for j := range b {
			b[j] = byte(r.Intn(256))
		}
		cases = append(cases, string(b))
	}
	return cases
}

func TestAppendJSONStringMatchesEncodingJSON(t *testing.T) {
	for _, s := range jsonStringCases() {
		want, _ := json.Marshal(s)
		if got := appendJSONString(nil, s); string(got) != string(want) {
			t.Fatalf("appendJSONString(%q) = %s, want %s", s, got, want)
		}
	}
}

// referenceApproxTokens is the original rune-slice implementation of
// estimateApproxTokens.
func referenceApproxTokens(text string) int {
	runes := []rune(text)
	length := len(runes)
	if length == 0 {
		return 0
	}
	if length < 5 {
		return max(1, int(math.Ceil(float64(length)/3.0)))
	}
	var regularAscii, digits, symbols, nonASCII int
	for _, r := range runes {
		switch {
		case r >= 0x80:
			nonASCII++
		case r >= '0' && r <= '9':
			digits++
		case (r >= '!' && r <= '/') || (r >= ':' && r <= '@') || (r >= '[' && r <= '`') || (r >= '{' && r <= '~'):
			symbols++
		default:
			regularAscii++
		}
	}
	estimated := int(math.Ceil(float64(regularAscii)/4.5 + float64(digits)/2.0 + float64(symbols)/1.5 + float64(nonASCII)/1.5))
	if estimated < 1 {
		return 1
	}
	return estimated
}

func TestEstimateApproxTokensMatchesReference(t *testing.T) {
	for _, s := range jsonStringCases() {
		want := referenceApproxTokens(s)
		if got := estimateApproxTokens(s); got != want {
			t.Fatalf("estimateApproxTokens(%q) = %d, want %d", s, got, want)
		}
		if got := estimateApproxTokensBytes([]byte(s)); got != want {
			t.Fatalf("estimateApproxTokensBytes(%q) = %d, want %d", s, got, want)
		}
	}
}

func TestJSONSizeMatchesMarshal(t *testing.T) {
	payload := ClaudeToKiro(perfClaudeRequest(), false)
	want, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	got, err := jsonSize(payload)
	if err != nil || got != len(want) {
		t.Fatalf("jsonSize = %d, %v; want %d", got, err, len(want))
	}
}
