package proxy

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"kiro-go/config"
)

// disabledModelMessage returns a client-facing error when the resolved model
// was switched off in the dashboard, or "" when it may be used.
func disabledModelMessage(resolvedModel string) string {
	if !config.IsModelDisabled(resolvedModel) {
		return ""
	}
	return fmt.Sprintf("model %q is disabled on this gateway", resolvedModel)
}

// filterDisabledModels drops /v1/models entries whose upstream model is
// disabled (including thinking variants and aliases pointing at it).
func filterDisabledModels(models []map[string]interface{}, thinkingSuffix string, mappings []config.ModelMapping) []map[string]interface{} {
	disabled := config.GetDisabledModels()
	if len(disabled) == 0 {
		return models
	}
	off := map[string]bool{}
	for _, d := range disabled {
		off[strings.ToLower(d)] = true
	}
	aliasTarget := map[string]string{}
	for _, m := range mappings {
		aliasTarget[strings.ToLower(m.Source)] = strings.ToLower(m.Target)
	}
	suffix := strings.ToLower(thinkingSuffix)
	out := models[:0]
	for _, m := range models {
		id, _ := m["id"].(string)
		base := strings.ToLower(id)
		if suffix != "" && strings.HasSuffix(base, suffix) {
			base = strings.TrimSuffix(base, suffix)
		}
		if t, ok := aliasTarget[base]; ok {
			base = t
		}
		if off[base] {
			continue
		}
		out = append(out, m)
	}
	return out
}

// apiSetModelEnabled handles POST /admin/api/models/toggle {"id","enabled"}.
func (h *Handler) apiSetModelEnabled(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID      string `json:"id"`
		Enabled bool   `json:"enabled"`
	}
	if err := decodeJSONBody(r, &req); err != nil || strings.TrimSpace(req.ID) == "" {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "id is required"})
		return
	}
	if err := config.SetModelEnabled(req.ID, req.Enabled); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, map[string]interface{}{"success": true, "disabledModels": config.GetDisabledModels()})
}

func writeJSON(w http.ResponseWriter, v interface{}) {
	_ = json.NewEncoder(w).Encode(v)
}

func decodeJSONBody(r *http.Request, v interface{}) error {
	return json.NewDecoder(r.Body).Decode(v)
}
