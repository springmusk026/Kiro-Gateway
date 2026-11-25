package proxy

import (
	"regexp"

	"github.com/springmusk026/Kiro-Gateway/config"
	"strconv"
	"strings"
	"sync"
)

// Model family upgrade.
//
// Clients ask for the Claude models they know: Claude Code's defaults are
// dated names such as claude-haiku-4-5-20251001 and claude-sonnet-4-5-20250929.
// Kiro offers its own generation (for example claude-haiku-5.5), and an
// account may not list the older ones at all, which used to fail every such
// request. When no account offers the requested Claude model, the newest
// offered model of the same family (haiku, sonnet or opus) is used instead.
// Operator model mappings are applied before this and are never changed.

var offeredModels sync.Map // lowercased model id -> struct{}

func recordOfferedModels(models []ModelInfo) {
	for _, m := range models {
		if id := strings.ToLower(strings.TrimSpace(m.ModelId)); id != "" {
			offeredModels.Store(id, struct{}{})
		}
	}
}

var (
	claudeDateSuffix  = regexp.MustCompile(`-\d{8}$`)
	claudeFamilyModel = regexp.MustCompile(`^claude-(opus|sonnet|haiku)-(\d+)(?:\.(\d+))?$`)
)

// stripClaudeDate removes a snapshot date from a Claude model id
// (claude-haiku-4.5-20251001 -> claude-haiku-4.5).
func stripClaudeDate(model string) string {
	if strings.HasPrefix(strings.ToLower(model), "claude-") {
		return claudeDateSuffix.ReplaceAllString(model, "")
	}
	return model
}

// upgradeClaudeFamily returns model unchanged when it is offered, or when no
// model list is known yet; otherwise the newest offered model of its family.
func upgradeClaudeFamily(model string) string {
	lower := strings.ToLower(strings.TrimSpace(model))
	m := claudeFamilyModel.FindStringSubmatch(lower)
	if m == nil {
		return model
	}
	if _, ok := offeredModels.Load(lower); ok {
		return model
	}
	if config.IsModelDisabled(lower) {
		return model // the operator turned it off: reject it, don't swap it
	}
	family := m[1]
	best, bestMajor, bestMinor := "", -1, -1
	offeredModels.Range(func(k, _ interface{}) bool {
		id := k.(string)
		c := claudeFamilyModel.FindStringSubmatch(id)
		if c == nil || c[1] != family || config.IsModelDisabled(id) {
			return true
		}
		major, _ := strconv.Atoi(c[2])
		minor := 0
		if c[3] != "" {
			minor, _ = strconv.Atoi(c[3])
		}
		if major > bestMajor || (major == bestMajor && minor > bestMinor) {
			best, bestMajor, bestMinor = id, major, minor
		}
		return true
	})
	if best == "" {
		return model
	}
	return best
}
