package proxy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"testing"
)

type nopFlusher struct{}

func (nopFlusher) Flush() {}

func TestWriteClaudeDeltaMatchesMapEncoding(t *testing.T) {
	for i, s := range jsonStringCases() {
		for _, kind := range []string{"text", "thinking"} {
			data, _ := json.Marshal(map[string]interface{}{
				"type":  "content_block_delta",
				"index": i,
				"delta": map[string]string{"type": kind + "_delta", kind: s},
			})
			want := fmt.Sprintf("event: %s\ndata: %s\n\n", "content_block_delta", string(data))
			var got bytes.Buffer
			writeClaudeDelta(&got, nopFlusher{}, i, kind, s)
			if got.String() != want {
				t.Fatalf("delta for %q:\n got %q\nwant %q", s, got.String(), want)
			}
		}
	}
}

func TestWriteOpenAIDeltaChunkMatchesMapEncoding(t *testing.T) {
	for i, s := range jsonStringCases() {
		for _, field := range []string{"content", "reasoning_content"} {
			data, _ := json.Marshal(map[string]interface{}{
				"id":      "chatcmpl-" + s,
				"object":  "chat.completion.chunk",
				"created": int64(1700000000 + i),
				"model":   "model<" + s,
				"choices": []map[string]interface{}{{
					"index":         0,
					"delta":         map[string]string{field: s},
					"logprobs":      nil,
					"finish_reason": nil,
				}},
			})
			want := fmt.Sprintf("data: %s\n\n", data)
			var got bytes.Buffer
			writeOpenAIDeltaChunk(&got, "chatcmpl-"+s, int64(1700000000+i), "model<"+s, field, s)
			if got.String() != want {
				t.Fatalf("chunk for %q:\n got %q\nwant %q", s, got.String(), want)
			}
		}
	}
}

func TestSendSSEMatchesFprintf(t *testing.T) {
	h := &Handler{}
	payload := map[string]interface{}{"type": "message_stop", "x": "<&>"}
	data, _ := json.Marshal(payload)
	want := fmt.Sprintf("event: %s\ndata: %s\n\n", "message_stop", string(data))
	rec := httptest.NewRecorder()
	h.sendSSE(rec, rec, "message_stop", payload)
	if rec.Body.String() != want {
		t.Fatalf("got %q want %q", rec.Body.String(), want)
	}
}

func TestSplitRunesMatchesRuneSlices(t *testing.T) {
	for _, s := range jsonStringCases() {
		r := []rune(s)
		for _, n := range []int{0, 1, len(r) / 2, len(r)} {
			if n > len(r) {
				continue
			}
			head, tail := splitRunes(s, n)
			if head != string(r[:n]) || tail != string(r[n:]) {
				t.Fatalf("splitRunes(%q, %d) = %q, %q", s, n, head, tail)
			}
		}
	}
}
