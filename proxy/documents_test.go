package proxy

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

var testPDF = base64.StdEncoding.EncodeToString([]byte("%PDF-1.4 minimal test document"))

func mustClaudeRequest(t *testing.T, body string) *ClaudeRequest {
	t.Helper()
	var req ClaudeRequest
	if err := json.Unmarshal([]byte(body), &req); err != nil {
		t.Fatalf("bad request json: %v", err)
	}
	return &req
}

func userContentParts(t *testing.T, items []map[string]interface{}) []map[string]interface{} {
	t.Helper()
	for i := len(items) - 1; i >= 0; i-- {
		if items[i]["role"] == "user" {
			parts, ok := items[i]["content"].([]map[string]interface{})
			if !ok {
				t.Fatalf("last user item has no content array: %#v", items[i])
			}
			return parts
		}
	}
	t.Fatal("no user item")
	return nil
}

func TestClaudePDFDocumentBothTransports(t *testing.T) {
	req := mustClaudeRequest(t, `{"model":"claude-sonnet-4.5","max_tokens":100,"messages":[{"role":"user","content":[
		{"type":"document","title":"Q3 report.pdf","source":{"type":"base64","media_type":"application/pdf","data":"`+testPDF+`"}},
		{"type":"text","text":"Summarize it"}
	]}]}`)
	if err := prepareClaudeMedia(context.Background(), req); err != nil {
		t.Fatalf("prepare: %v", err)
	}
	payload := ClaudeToKiro(req, false)
	cur := payload.ConversationState.CurrentMessage.UserInputMessage
	if len(cur.Documents) != 1 {
		t.Fatalf("documents = %+v", cur.Documents)
	}
	d := cur.Documents[0]
	if d.Name != "Q3 report" || d.Format != "pdf" || d.Source.Bytes != testPDF {
		t.Fatalf("document = %+v", d)
	}
	if cur.Content != "Summarize it" {
		t.Fatalf("content = %q", cur.Content)
	}

	// Legacy generateAssistantResponse wire shape.
	raw, _ := json.Marshal(payload)
	if !strings.Contains(string(raw), `"documents":[{"name":"Q3 report","format":"pdf","source":{"bytes":"`+testPDF+`"}}]`) {
		t.Fatalf("legacy body missing documents: %s", raw)
	}

	// /v1/responses wire shape.
	parts := userContentParts(t, buildResponsesInput(payload))
	last := parts[len(parts)-1]
	if last["type"] != "input_file" || last["filename"] != "Q3 report.pdf" || last["file_data"] != "data:application/pdf;base64,"+testPDF {
		t.Fatalf("input_file part = %#v", last)
	}
	if parts[0]["type"] != "input_text" || parts[0]["text"] != "Summarize it" {
		t.Fatalf("first part = %#v", parts[0])
	}
}

func TestClaudeTextAndContentDocumentsAreInlined(t *testing.T) {
	img := base64.StdEncoding.EncodeToString(testPNG)
	req := mustClaudeRequest(t, `{"model":"claude-sonnet-4.5","max_tokens":100,"messages":[{"role":"user","content":[
		{"type":"document","title":"notes <v2>","source":{"type":"text","media_type":"text/plain","data":"alpha beta"}},
		{"type":"document","source":{"type":"content","content":[{"type":"text","text":"gamma"},{"type":"image","source":{"type":"base64","media_type":"image/png","data":"`+img+`"}}]}},
		{"type":"text","text":"Compare"}
	]}]}`)
	if err := prepareClaudeMedia(context.Background(), req); err != nil {
		t.Fatalf("prepare: %v", err)
	}
	payload := ClaudeToKiro(req, false)
	cur := payload.ConversationState.CurrentMessage.UserInputMessage
	if len(cur.Documents) != 0 {
		t.Fatalf("text documents must be inlined, got %+v", cur.Documents)
	}
	for _, want := range []string{`<document name="notes &lt;v2&gt;">`, "alpha beta", `<document name="document">`, "gamma", "Compare"} {
		if !strings.Contains(cur.Content, want) {
			t.Fatalf("content %q missing %q", cur.Content, want)
		}
	}
	if len(cur.Images) != 1 {
		t.Fatalf("content-source image lost: %+v", cur.Images)
	}
}

func TestClaudeDocumentInsideToolResult(t *testing.T) {
	req := mustClaudeRequest(t, `{"model":"claude-sonnet-4.5","max_tokens":100,"messages":[
		{"role":"user","content":"read the file"},
		{"role":"assistant","content":[{"type":"tool_use","id":"t1","name":"read","input":{"path":"a.pdf"}}]},
		{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":[
			{"type":"text","text":"file contents:"},
			{"type":"document","title":"a.pdf","source":{"type":"base64","media_type":"application/pdf","data":"`+testPDF+`"}}
		]}]}
	]}`)
	if err := prepareClaudeMedia(context.Background(), req); err != nil {
		t.Fatalf("prepare: %v", err)
	}
	payload := ClaudeToKiro(req, false)
	cur := payload.ConversationState.CurrentMessage.UserInputMessage
	if len(cur.Documents) != 1 || cur.Documents[0].Name != "a" {
		t.Fatalf("documents = %+v", cur.Documents)
	}
	if !strings.Contains(cur.Content, `[Tool returned the document "a.pdf"`) {
		t.Fatalf("tool result text does not mention the document: %q", cur.Content)
	}
}

func TestOpenAIChatFilePart(t *testing.T) {
	var req OpenAIRequest
	if err := json.Unmarshal([]byte(`{"model":"claude-sonnet-4.5","messages":[{"role":"user","content":[
		{"type":"text","text":"What is in this file?"},
		{"type":"file","file":{"filename":"budget.xlsx","file_data":"data:application/vnd.openxmlformats-officedocument.spreadsheetml.sheet;base64,`+testPDF+`"}}
	]}]}`), &req); err != nil {
		t.Fatal(err)
	}
	if msg := validateOpenAIRequestShape(&req); msg != "" {
		t.Fatalf("validate: %s", msg)
	}
	if err := prepareOpenAIMedia(context.Background(), req.Messages); err != nil {
		t.Fatalf("prepare: %v", err)
	}
	payload := OpenAIToKiro(&req, false)
	docs := payload.ConversationState.CurrentMessage.UserInputMessage.Documents
	if len(docs) != 1 || docs[0].Name != "budget" || docs[0].Format != "xlsx" {
		t.Fatalf("documents = %+v", docs)
	}
	parts := userContentParts(t, buildResponsesInput(payload))
	if f := parts[len(parts)-1]; f["filename"] != "budget.xlsx" ||
		!strings.HasPrefix(f["file_data"].(string), "data:application/vnd.openxmlformats-officedocument.spreadsheetml.sheet;base64,") {
		t.Fatalf("input_file = %#v", f)
	}
}

func TestResponsesInputFileAndReasoningItems(t *testing.T) {
	msgs, err := parseResponsesInput(json.RawMessage(`[
		{"type":"reasoning","id":"rs_1","summary":[{"type":"summary_text","text":"thought"}],"encrypted_content":"opaque"},
		{"role":"user","content":[{"type":"input_text","text":"Read this"},{"type":"input_file","filename":"spec.md","file_data":"data:text/markdown;base64,` + base64.StdEncoding.EncodeToString([]byte("# Spec")) + `"}]},
		{"type":"input_file","filename":"extra.pdf","file_data":"data:application/pdf;base64,` + testPDF + `"}
	]`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if err := prepareOpenAIMedia(context.Background(), msgs); err != nil {
		t.Fatalf("prepare: %v", err)
	}
	payload := OpenAIToKiro(&OpenAIRequest{Model: "claude-sonnet-4.5", Messages: msgs}, false)
	var all []KiroDocument
	for _, h := range payload.ConversationState.History {
		if h.UserInputMessage != nil {
			all = append(all, h.UserInputMessage.Documents...)
		}
	}
	all = append(all, payload.ConversationState.CurrentMessage.UserInputMessage.Documents...)
	if len(all) != 2 || all[0].Format != "md" || all[1].Format != "pdf" {
		t.Fatalf("documents = %+v", all)
	}
}

func TestDocumentValidationErrors(t *testing.T) {
	doc := func(mt, data string) string {
		return `{"type":"document","source":{"type":"base64","media_type":"` + mt + `","data":"` + data + `"}}`
	}
	big := base64.StdEncoding.EncodeToString(make([]byte, maxDocumentBytes+1))
	six := strings.TrimSuffix(strings.Repeat(doc("application/pdf", testPDF)+",", 6), ",")
	cases := map[string]string{
		doc("application/zip", testPDF):            "unsupported type",
		doc("application/pdf", big):                "limit is",
		doc("application/pdf", "!!!not base64!!!"): "not valid base64",
		six: "too many documents",
		`{"type":"document","source":{"type":"file","file_id":"file_1"}}`: "Files API is not supported",
	}
	for blocks, want := range cases {
		req := mustClaudeRequest(t, `{"model":"claude-sonnet-4.5","max_tokens":10,"messages":[{"role":"user","content":[`+blocks+`]}]}`)
		err := prepareClaudeMedia(context.Background(), req)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("want %q, got %v", want, err)
			continue
		}
		if _, ok := asMediaError(err); !ok {
			t.Errorf("%q is not a client error", err)
		}
	}

	// OpenAI file_id and unsupported types.
	for content, want := range map[string]string{
		`{"type":"file","file":{"file_id":"file-abc","filename":"a.pdf"}}`:                                               "file_id",
		`{"type":"file","file":{"filename":"a.exe","file_data":"data:application/x-msdownload;base64,` + testPDF + `"}}`: "unsupported type",
	} {
		var msgs []OpenAIMessage
		if err := json.Unmarshal([]byte(`[{"role":"user","content":[{"type":"text","text":"x"},`+content+`]}]`), &msgs); err != nil {
			t.Fatal(err)
		}
		err := prepareOpenAIMedia(context.Background(), msgs)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("want %q, got %v", want, err)
		}
	}
}

func TestDocumentNamesAreSanitizedAndUnique(t *testing.T) {
	req := mustClaudeRequest(t, `{"model":"claude-sonnet-4.5","max_tokens":10,"messages":[{"role":"user","content":[
		{"type":"document","title":"report_v1.pdf","source":{"type":"base64","media_type":"application/pdf","data":"`+testPDF+`"}},
		{"type":"document","title":"report v1","source":{"type":"base64","media_type":"application/pdf","data":"`+testPDF+`"}},
		{"type":"document","source":{"type":"base64","media_type":"application/pdf","data":"`+testPDF+`"}}
	]}]}`)
	payload := ClaudeToKiro(req, false)
	docs := payload.ConversationState.CurrentMessage.UserInputMessage.Documents
	got := []string{docs[0].Name, docs[1].Name, docs[2].Name}
	want := []string{"report-v1", "report v1", "document"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("names = %q, want %q", got, want)
		}
	}
	dup := ClaudeToKiro(mustClaudeRequest(t, `{"model":"claude-sonnet-4.5","max_tokens":10,"messages":[{"role":"user","content":[
		{"type":"document","title":"a","source":{"type":"base64","media_type":"application/pdf","data":"`+testPDF+`"}},
		{"type":"document","title":"a","source":{"type":"base64","media_type":"application/pdf","data":"`+testPDF+`"}}
	]}]}`), false).ConversationState.CurrentMessage.UserInputMessage.Documents
	if dup[0].Name != "a" || dup[1].Name != "a (2)" {
		t.Fatalf("duplicate names = %q, %q", dup[0].Name, dup[1].Name)
	}
}

// A large document must not count against the text payload budget, or the
// whole conversation would be truncated away.
func TestLargeDocumentDoesNotTruncateHistory(t *testing.T) {
	big := base64.StdEncoding.EncodeToString(append([]byte("%PDF-1.4"), make([]byte, 3<<20)...))
	req := mustClaudeRequest(t, `{"model":"claude-sonnet-4.5","max_tokens":10,"messages":[
		{"role":"user","content":"first question"},
		{"role":"assistant","content":"first answer"},
		{"role":"user","content":[{"type":"document","title":"big","source":{"type":"base64","media_type":"application/pdf","data":"`+big+`"}},{"type":"text","text":"and this?"}]}
	]}`)
	payload := ClaudeToKiro(req, false)
	raw, _ := json.Marshal(payload)
	if !strings.Contains(string(raw), "first question") || strings.Contains(string(raw), truncationPlaceholder) {
		t.Fatal("history was truncated because of the document size")
	}
	if payload.ConversationState.CurrentMessage.UserInputMessage.Content != "and this?" {
		t.Fatalf("current content = %q", payload.ConversationState.CurrentMessage.UserInputMessage.Content)
	}
}

func TestDocumentURLSourceIsFetched(t *testing.T) {
	allowTestFetch(t)
	srv := imageServer(t)
	defer srv.Close()
	req := mustClaudeRequest(t, `{"model":"claude-sonnet-4.5","max_tokens":10,"messages":[{"role":"user","content":[
		{"type":"document","source":{"type":"url","url":"`+srv.URL+`/doc.pdf"}},{"type":"text","text":"x"}]}]}`)
	if err := prepareClaudeMedia(context.Background(), req); err != nil {
		t.Fatalf("prepare: %v", err)
	}
	docs := ClaudeToKiro(req, false).ConversationState.CurrentMessage.UserInputMessage.Documents
	if len(docs) != 1 || docs[0].Name != "doc" || docs[0].Format != "pdf" {
		t.Fatalf("documents = %+v", docs)
	}
}

func TestHandlerRejectsUnsupportedDocument(t *testing.T) {
	server := textUpstream(t, "hello")
	defer server.Close()
	h := setupIntegrityPathTest(t, server)

	rec := httptest.NewRecorder()
	h.handleClaudeMessages(rec, httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{
		"model":"claude-sonnet-4.5","max_tokens":10,
		"messages":[{"role":"user","content":[{"type":"document","title":"x","source":{"type":"base64","media_type":"application/zip","data":"`+testPDF+`"}}]}]}`)))
	if rec.Code != 400 || !strings.Contains(rec.Body.String(), "invalid_request_error") || !strings.Contains(rec.Body.String(), "unsupported type") {
		t.Fatalf("got %d %s", rec.Code, rec.Body.String())
	}

	// A document-only message is valid user content and reaches upstream.
	rec = httptest.NewRecorder()
	h.handleClaudeMessages(rec, httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{
		"model":"claude-sonnet-4.5","max_tokens":10,
		"messages":[{"role":"user","content":[{"type":"document","source":{"type":"base64","media_type":"application/pdf","data":"`+testPDF+`"}}]}]}`)))
	if rec.Code != 200 {
		t.Fatalf("document-only request: %d %s", rec.Code, rec.Body.String())
	}
}

// History may replay thinking blocks (with or without signature) and
// redacted_thinking blocks; they are accepted and not forwarded.
func TestClaudeAcceptsThinkingBlocksInHistory(t *testing.T) {
	req := mustClaudeRequest(t, `{"model":"claude-sonnet-4.5","max_tokens":10,"messages":[
		{"role":"user","content":"q1"},
		{"role":"assistant","content":[
			{"type":"thinking","thinking":"secret plan","signature":"abc"},
			{"type":"redacted_thinking","data":"opaque"},
			{"type":"text","text":"a1"}]},
		{"role":"user","content":"q2"}
	]}`)
	if msg := validateClaudeRequestShape(req); msg != "" {
		t.Fatalf("validate: %s", msg)
	}
	if err := prepareClaudeMedia(context.Background(), req); err != nil {
		t.Fatalf("prepare: %v", err)
	}
	raw, _ := json.Marshal(ClaudeToKiro(req, false))
	if strings.Contains(string(raw), "secret plan") || strings.Contains(string(raw), "opaque") || !strings.Contains(string(raw), "a1") {
		t.Fatalf("payload = %s", raw)
	}
}

func TestResponsesHandlerForwardsInputFile(t *testing.T) {
	var body []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(awsEventStreamFrame(t, "assistantResponseEvent", map[string]interface{}{"content": "ok"}))
		_, _ = w.Write(awsEventStreamFrame(t, "metadataEvent", map[string]interface{}{"stopReason": "end_turn"}))
	}))
	defer server.Close()
	h := setupIntegrityPathTest(t, server)

	rec := httptest.NewRecorder()
	h.handleOpenAIResponses(rec, httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{
		"model":"claude-sonnet-4.5",
		"input":[{"role":"user","content":[{"type":"input_text","text":"summarize"},
			{"type":"input_file","filename":"paper.pdf","file_data":"data:application/pdf;base64,`+testPDF+`"}]}]}`)))
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(string(body), `"documents":[{"name":"paper","format":"pdf"`) {
		t.Fatalf("upstream body has no document: %s", body)
	}
}

// A long conversation resends every earlier attachment; it must keep working
// once the total passes the per-request limit, keeping the newest documents.
func TestDocumentsAcrossTurnsKeepNewest(t *testing.T) {
	doc := `{"type":"document","title":"%s","source":{"type":"base64","media_type":"application/pdf","data":"` + testPDF + `"}}`
	var msgs []string
	for turn := 1; turn <= 4; turn++ {
		msgs = append(msgs,
			fmt.Sprintf(`{"role":"user","content":[`+doc+`,`+doc+`,{"type":"text","text":"turn %d"}]}`, fmt.Sprintf("t%da", turn), fmt.Sprintf("t%db", turn), turn),
			`{"role":"assistant","content":"ok"}`)
	}
	msgs = append(msgs, `{"role":"user","content":"summarize"}`)
	req := mustClaudeRequest(t, `{"model":"claude-sonnet-4.5","max_tokens":10,"messages":[`+strings.Join(msgs, ",")+`]}`)
	if err := prepareClaudeMedia(context.Background(), req); err != nil {
		t.Fatalf("8 documents over 4 turns must be accepted: %v", err)
	}
	payload := ClaudeToKiro(req, false)
	var total int
	var oldestNote bool
	for _, h := range payload.ConversationState.History {
		if m := h.UserInputMessage; m != nil {
			total += len(m.Documents)
			if strings.Contains(m.Content, "omitted from this request") {
				oldestNote = true
			}
			for _, d := range m.Documents {
				if strings.HasPrefix(d.Name, "t1") {
					t.Errorf("oldest document %q should have been dropped", d.Name)
				}
			}
		}
	}
	if total > maxDocumentsPerRequest || total == 0 || !oldestNote {
		t.Fatalf("documents kept = %d (max %d), note = %v", total, maxDocumentsPerRequest, oldestNote)
	}
}
