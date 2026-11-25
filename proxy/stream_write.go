package proxy

import (
	"io"
	"net/http"
	"strconv"
	"sync"
	"unicode/utf8"
)

// Streaming write helpers. Each SSE event is still written with one Write
// call and flushed (sse_heartbeat.go relies on whole-event writes), but the
// frame is assembled in a pooled buffer instead of through fmt and string
// copies, and the hottest event, a Claude text or thinking delta, is encoded
// by hand. The bytes are identical to the map-and-json.Marshal form
// (stream_write_test.go).

var sseFramePool = sync.Pool{New: func() interface{} {
	b := make([]byte, 0, 1024)
	return &b
}}

// writeSSEFrame writes one assembled frame, flushes, and recycles its buffer.
func writeSSEFrame(w io.Writer, flusher http.Flusher, bp *[]byte, frame []byte) {
	_, _ = w.Write(frame)
	recycleSSEFrame(bp, frame)
	flusher.Flush()
}

// writeSSEEvent writes "event: <event>\ndata: <data>\n\n" and flushes.
func writeSSEEvent(w io.Writer, flusher http.Flusher, event string, data []byte) {
	bp := sseFramePool.Get().(*[]byte)
	b := append((*bp)[:0], "event: "...)
	b = append(b, event...)
	b = append(b, "\ndata: "...)
	b = append(b, data...)
	b = append(b, "\n\n"...)
	writeSSEFrame(w, flusher, bp, b)
}

// writeClaudeDelta writes a content_block_delta event carrying a text
// ("text" → text_delta) or thinking ("thinking" → thinking_delta) delta.
// The object is laid out in encoding/json's sorted key order.
func writeClaudeDelta(w io.Writer, flusher http.Flusher, index int, field, text string) {
	bp := sseFramePool.Get().(*[]byte)
	b := append((*bp)[:0], "event: content_block_delta\ndata: {\"delta\":{\""...)
	b = append(b, field...)
	b = append(b, "\":"...)
	b = appendJSONString(b, text)
	b = append(b, ",\"type\":\""...)
	b = append(b, field...)
	b = append(b, "_delta\"},\"index\":"...)
	b = strconv.AppendInt(b, int64(index), 10)
	b = append(b, ",\"type\":\"content_block_delta\"}\n\n"...)
	writeSSEFrame(w, flusher, bp, b)
}

// writeSSEData writes "data: <data>\n\n" in one Write, without flushing.
func writeSSEData(w io.Writer, data []byte) {
	bp := sseFramePool.Get().(*[]byte)
	b := append((*bp)[:0], "data: "...)
	b = append(b, data...)
	b = append(b, "\n\n"...)
	_, _ = w.Write(b)
	recycleSSEFrame(bp, b)
}

// writeOpenAIDeltaChunk writes a chat.completion.chunk whose delta has one
// field (content or reasoning_content), in encoding/json's sorted key order,
// without flushing.
func writeOpenAIDeltaChunk(w io.Writer, id string, created int64, model, field, text string) {
	bp := sseFramePool.Get().(*[]byte)
	b := append((*bp)[:0], "data: {\"choices\":[{\"delta\":{"...)
	b = appendJSONString(b, field)
	b = append(b, ':')
	b = appendJSONString(b, text)
	b = append(b, "},\"finish_reason\":null,\"index\":0,\"logprobs\":null}],\"created\":"...)
	b = strconv.AppendInt(b, created, 10)
	b = append(b, ",\"id\":"...)
	b = appendJSONString(b, id)
	b = append(b, ",\"model\":"...)
	b = appendJSONString(b, model)
	b = append(b, ",\"object\":\"chat.completion.chunk\"}\n\n"...)
	_, _ = w.Write(b)
	recycleSSEFrame(bp, b)
}

func recycleSSEFrame(bp *[]byte, frame []byte) {
	if cap(frame) <= 1<<20 {
		*bp = frame[:0]
		sseFramePool.Put(bp)
	}
}

// splitRunes splits s after its first n runes: the same strings as
// string([]rune(s)[:n]) and string([]rune(s)[n:]), without the rune slice.
func splitRunes(s string, n int) (string, string) {
	if !utf8.ValidString(s) {
		// Conversion through []rune replaces invalid bytes; keep that.
		r := []rune(s)
		return string(r[:n]), string(r[n:])
	}
	i := 0
	for k := 0; k < n && i < len(s); k++ {
		if s[i] < utf8.RuneSelf {
			i++
			continue
		}
		_, size := utf8.DecodeRuneInString(s[i:])
		i += size
	}
	return s[:i], s[i:]
}
