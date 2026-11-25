package proxy

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"math"
	"reflect"
	"sort"
	"strconv"
	"testing"
	"time"
)

// referenceCanonicalJSON is the original json.Marshal-based canonicalizer.
func referenceCanonicalJSON(buf *bytes.Buffer, value interface{}) {
	switch v := value.(type) {
	case nil:
		buf.WriteString("null")
	case string:
		encoded, _ := json.Marshal(v)
		buf.Write(encoded)
	case bool:
		if v {
			buf.WriteString("true")
		} else {
			buf.WriteString("false")
		}
	case []interface{}:
		buf.WriteByte('[')
		for i, item := range v {
			if i > 0 {
				buf.WriteByte(',')
			}
			referenceCanonicalJSON(buf, item)
		}
		buf.WriteByte(']')
	case map[string]interface{}:
		buf.WriteByte('{')
		keys := make([]string, 0, len(v))
		for key := range v {
			if key != "cache_control" {
				keys = append(keys, key)
			}
		}
		sort.Strings(keys)
		for i, key := range keys {
			if i > 0 {
				buf.WriteByte(',')
			}
			encoded, _ := json.Marshal(key)
			buf.Write(encoded)
			buf.WriteByte(':')
			referenceCanonicalJSON(buf, v[key])
		}
		buf.WriteByte('}')
	default:
		encoded, _ := json.Marshal(v)
		buf.Write(encoded)
	}
}

func TestCanonicalJSONMatchesReference(t *testing.T) {
	values := []interface{}{
		nil, true, false, 0, -7, 3.5, 1e21, 1e-7, -2.5e-9, 123456789.125, 0.1, math.Inf(1), math.NaN(), float32(1.5), int64(9),
		json.Number("12.50"), "x<y>&z", []interface{}{"a", 1.0, map[string]interface{}{"b": nil}},
		map[string]interface{}{"z": 1.0, "a": " ", "cache_control": map[string]interface{}{"type": "ephemeral"}},
	}
	for _, s := range jsonStringCases() {
		values = append(values, s, map[string]interface{}{s: s})
	}
	var req map[string]interface{}
	if err := json.Unmarshal(perfClaudeRequestJSON(false), &req); err != nil {
		t.Fatal(err)
	}
	values = append(values, req)
	for _, v := range values {
		var want bytes.Buffer
		referenceCanonicalJSON(&want, v)
		if got := appendCanonicalJSON(nil, v); !bytes.Equal(got, want.Bytes()) {
			t.Fatalf("canonical(%#v)\n got %s\nwant %s", v, got, want.Bytes())
		}
	}
}

// referenceProfile recomputes a profile from flattened blocks the original
// way (canonical string per block, hashed in four writes).
func referenceProfile(blocks []cacheablePromptBlock, claude bool) []promptCacheBreakpoint {
	hasher := sha256.New()
	var out []promptCacheBreakpoint
	cumulative := 0
	var active time.Duration
	for _, block := range blocks {
		var buf bytes.Buffer
		referenceCanonicalJSON(&buf, block.Value)
		chunk := buf.String()
		hasher.Write([]byte(strconv.Itoa(len(chunk))))
		hasher.Write([]byte{0})
		hasher.Write([]byte(chunk))
		hasher.Write([]byte{0})
		cumulative += referenceApproxTokens(chunk)
		ttl := block.TTL
		if claude {
			if block.TTL > 0 {
				active = block.TTL
			} else if block.IsMessageEnd && active > 0 {
				ttl = active
			}
		}
		if ttl <= 0 {
			continue
		}
		var fp [32]byte
		copy(fp[:], hasher.Sum(nil))
		out = append(out, promptCacheBreakpoint{Fingerprint: fp, CumulativeTokens: cumulative, TTL: ttl})
	}
	return out
}

func TestPromptCacheProfilesMatchReference(t *testing.T) {
	tracker := newPromptCacheTracker(defaultPromptCacheTTL)

	claudeReq := perfClaudeRequest()
	got := tracker.BuildClaudeProfile(claudeReq, 1)
	want := referenceProfile(flattenClaudeCacheBlocks(claudeReq), true)
	if got == nil || len(want) == 0 || !reflect.DeepEqual(got.Breakpoints, want) {
		t.Fatalf("claude breakpoints differ:\n got %+v\nwant %+v", got, want)
	}

	openaiReq := perfOpenAIRequest()
	gotOpenAI := tracker.BuildOpenAIProfile(openaiReq, 1)
	wantOpenAI := referenceProfile(flattenOpenAICacheBlocks(openaiReq), false)
	if gotOpenAI == nil || !reflect.DeepEqual(gotOpenAI.Breakpoints, wantOpenAI) {
		t.Fatal("openai breakpoints differ")
	}

	// Without any cache_control marker there is no Claude profile.
	unmarked := perfClaudeRequest()
	unmarked.System = perfSystemPrompt()
	last := &unmarked.Messages[len(unmarked.Messages)-1]
	last.Content = "Now summarise what changed."
	if p := tracker.BuildClaudeProfile(unmarked, 1); p != nil {
		t.Fatalf("unmarked request has a profile: %+v", p)
	}
}
