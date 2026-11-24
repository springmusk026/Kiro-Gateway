package proxy

import (
	"context"
	"strings"
)

// Kiro has no stop-sequence parameter, so Anthropic stop_sequences and OpenAI
// stop are enforced here: output is cut at the first match and the upstream
// request is cancelled.

const maxStopSequences = 16

type stopSequencesKey struct{}

// withStopSequences attaches the request's stop sequences to ctx, so the
// response paths can enforce them without widening their signatures.
func withStopSequences(ctx context.Context, seqs []string) context.Context {
	clean := normalizeStopSequences(seqs)
	if len(clean) == 0 {
		return ctx
	}
	return context.WithValue(ctx, stopSequencesKey{}, clean)
}

func stopSequencesFrom(ctx context.Context) []string {
	if ctx == nil {
		return nil
	}
	seqs, _ := ctx.Value(stopSequencesKey{}).([]string)
	return seqs
}

func normalizeStopSequences(seqs []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, s := range seqs {
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
		if len(out) == maxStopSequences {
			break
		}
	}
	return out
}

// openAIStopSequences decodes the OpenAI "stop" parameter: a string or an
// array of strings.
func openAIStopSequences(stop interface{}) []string {
	switch v := stop.(type) {
	case string:
		return []string{v}
	case []interface{}:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	case []string:
		return v
	}
	return nil
}

// stopSequenceMatcher finds stop sequences across streamed chunks. It holds
// back only the trailing text that could still begin a stop sequence (at most
// the longest sequence minus one byte), so ordinary text streams through
// without delay.
type stopSequenceMatcher struct {
	seqs    []string
	buf     string
	matched string
	done    bool
}

// newStopSequenceMatcher returns nil when there is nothing to match; every
// method is safe on a nil matcher and then passes text through unchanged.
func newStopSequenceMatcher(seqs []string) *stopSequenceMatcher {
	seqs = normalizeStopSequences(seqs)
	if len(seqs) == 0 {
		return nil
	}
	return &stopSequenceMatcher{seqs: seqs}
}

// Push adds streamed text and returns the part that is safe to emit. On a
// match it returns the text before the match and every later call returns "".
func (m *stopSequenceMatcher) Push(text string) string {
	if m == nil {
		return text
	}
	if m.done {
		return ""
	}
	m.buf += text
	if idx, seq := earliestStopMatch(m.buf, m.seqs); idx >= 0 {
		out := m.buf[:idx]
		m.buf = ""
		m.done = true
		m.matched = seq
		return out
	}
	hold := m.pendingPrefixLen()
	out := m.buf[:len(m.buf)-hold]
	m.buf = m.buf[len(m.buf)-hold:]
	return out
}

// Flush releases held-back text at the end of a text run.
func (m *stopSequenceMatcher) Flush() string {
	if m == nil || m.done {
		return ""
	}
	out := m.buf
	m.buf = ""
	return out
}

// Done reports whether a stop sequence matched.
func (m *stopSequenceMatcher) Done() bool { return m != nil && m.done }

// Matched returns the stop sequence that ended the output.
func (m *stopSequenceMatcher) Matched() string {
	if m == nil {
		return ""
	}
	return m.matched
}

// pendingPrefixLen is the length of the longest suffix of buf that is a proper
// prefix of some stop sequence.
func (m *stopSequenceMatcher) pendingPrefixLen() int {
	best := 0
	for _, seq := range m.seqs {
		max := len(seq) - 1
		if max > len(m.buf) {
			max = len(m.buf)
		}
		for n := max; n > best; n-- {
			if strings.HasSuffix(m.buf, seq[:n]) {
				best = n
				break
			}
		}
	}
	return best
}

func earliestStopMatch(text string, seqs []string) (int, string) {
	idx, which := -1, ""
	for _, seq := range seqs {
		if i := strings.Index(text, seq); i >= 0 && (idx < 0 || i < idx || (i == idx && len(seq) > len(which))) {
			idx, which = i, seq
		}
	}
	return idx, which
}

// cutAtStopSequence truncates fully buffered output at the first stop
// sequence.
func cutAtStopSequence(text string, seqs []string) (string, string, bool) {
	if idx, seq := earliestStopMatch(text, seqs); idx >= 0 {
		return text[:idx], seq, true
	}
	return text, "", false
}

// bufferedStopWatch enforces stop sequences on the fully buffered (non-stream)
// paths: it watches the raw text as it arrives to cancel upstream early, and
// cuts the final text at the first match.
type bufferedStopWatch struct {
	seqs   []string
	maxLen int
	// early allows cancelling upstream on a raw-text match. It is off when
	// tag-based thinking may put a stop sequence inside reasoning that is
	// stripped from the final text.
	early  bool
	hit    bool
	seq    string
	cancel func()
}

func newBufferedStopWatch(seqs []string, early bool, cancel func()) *bufferedStopWatch {
	seqs = normalizeStopSequences(seqs)
	if len(seqs) == 0 {
		return nil
	}
	b := &bufferedStopWatch{seqs: seqs, early: early, cancel: cancel}
	for _, s := range seqs {
		if len(s) > b.maxLen {
			b.maxLen = len(s)
		}
	}
	return b
}

// Hit reports whether a stop sequence already ended the output.
func (b *bufferedStopWatch) Hit() bool { return b != nil && b.hit }

// Observe checks the text accumulated so far, of which the last added bytes
// are new.
func (b *bufferedStopWatch) Observe(content string, added int) {
	if b == nil || b.hit || !b.early {
		return
	}
	start := len(content) - added - b.maxLen
	if start < 0 {
		start = 0
	}
	if idx, seq := earliestStopMatch(content[start:], b.seqs); idx >= 0 {
		b.hit = true
		b.seq = seq
		if b.cancel != nil {
			b.cancel()
		}
	}
}

// Reset clears a watch for a fresh upstream attempt.
func (b *bufferedStopWatch) Reset() {
	if b != nil {
		b.hit = false
		b.seq = ""
	}
}

// Apply cuts the final text at the first stop sequence. It returns the text,
// the tool uses to keep (none once text was cut, since they came after it) and
// the matched sequence ("" when no stop sequence ended the output).
func (b *bufferedStopWatch) Apply(text string, toolUses []KiroToolUse) (string, []KiroToolUse, string) {
	if b == nil {
		return text, toolUses, ""
	}
	if cut, seq, ok := cutAtStopSequence(text, b.seqs); ok {
		return cut, nil, seq
	}
	if b.hit {
		return text, nil, b.seq
	}
	return text, toolUses, ""
}
