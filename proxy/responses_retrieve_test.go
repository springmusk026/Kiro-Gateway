package proxy

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestResponsesRetrieveDeleteCancel(t *testing.T) {
	mustInitConfig(t)
	h := &Handler{}
	stored := &ResponsesObject{
		ID:        "resp_retrieve_test",
		Object:    "response",
		CreatedAt: time.Now().Unix(),
		Status:    "completed",
		Model:     "claude-sonnet-4.5",
		Output: []ResponseOutputItem{{
			ID: "msg_1", Type: "message", Role: "assistant", Status: "completed",
			Content: []ResponseContentPart{{Type: "output_text", Text: "hello"}},
		}},
	}
	if err := saveResponse(stored); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/responses/resp_retrieve_test", nil))
	var got map[string]interface{}
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &got) != nil || got["id"] != "resp_retrieve_test" || got["truncation"] != "disabled" {
		t.Fatalf("get: %d %s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/responses/resp_retrieve_test/cancel", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("cancel: %d %s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/v1/responses/resp_retrieve_test", nil))
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &got) != nil || got["deleted"] != true {
		t.Fatalf("delete: %d %s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/responses/resp_retrieve_test", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("get after delete: %d %s", rec.Code, rec.Body.String())
	}
}
