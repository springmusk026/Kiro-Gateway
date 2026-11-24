package proxy

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func modelsTestHandler(t *testing.T) *Handler {
	t.Helper()
	mustInitConfig(t)
	h := &Handler{}
	h.cachedModels = []ModelInfo{{ModelId: "claude-opus-5", ModelName: "Claude Opus 5"}}
	h.modelsCacheTime = time.Now().Unix()
	return h
}

func TestModelsListHasOfficialFields(t *testing.T) {
	h := modelsTestHandler(t)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/models", nil))
	var body struct {
		Object  string                   `json:"object"`
		Data    []map[string]interface{} `json:"data"`
		HasMore *bool                    `json:"has_more"`
		FirstID string                   `json:"first_id"`
		LastID  string                   `json:"last_id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Object != "list" || body.HasMore == nil || *body.HasMore || len(body.Data) == 0 {
		t.Fatalf("unexpected list: %s", rec.Body.String())
	}
	if body.FirstID != body.Data[0]["id"] || body.LastID != body.Data[len(body.Data)-1]["id"] {
		t.Fatalf("first/last id mismatch: %s", rec.Body.String())
	}
	for _, m := range body.Data {
		if m["created"] != float64(modelsListEpoch) || m["type"] != "model" || m["created_at"] == nil || m["object"] != "model" {
			t.Fatalf("entry missing fields: %v", m)
		}
	}
}

func TestRetrieveModel(t *testing.T) {
	h := modelsTestHandler(t)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/models/claude-opus-5", nil))
	var m map[string]interface{}
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &m) != nil || m["id"] != "claude-opus-5" {
		t.Fatalf("retrieve: %d %s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/models/nope", nil))
	var openai struct {
		Error map[string]interface{} `json:"error"`
	}
	if rec.Code != 404 || json.Unmarshal(rec.Body.Bytes(), &openai) != nil || openai.Error["code"] != "model_not_found" {
		t.Fatalf("openai 404: %d %s", rec.Code, rec.Body.String())
	}

	req := httptest.NewRequest(http.MethodGet, "/v1/models/nope", nil)
	req.Header.Set("anthropic-version", "2023-06-01")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var anthropic struct {
		Type  string            `json:"type"`
		Error map[string]string `json:"error"`
	}
	if rec.Code != 404 || json.Unmarshal(rec.Body.Bytes(), &anthropic) != nil || anthropic.Error["type"] != "not_found_error" {
		t.Fatalf("anthropic 404: %d %s", rec.Code, rec.Body.String())
	}
}
