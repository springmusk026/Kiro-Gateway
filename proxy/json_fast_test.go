package proxy

import (
	"encoding/json"
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
