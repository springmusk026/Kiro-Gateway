package proxy

import (
	"encoding/json"
	"testing"
)

func TestFlattenOpenAIToolsRecordsNamespace(t *testing.T) {
	var tools []OpenAITool
	raw := `[{"type":"namespace","name":"collaboration","tools":[
		{"type":"function","name":"spawn_agent","parameters":{"type":"object"}},
		{"type":"function","name":"wait_agent","parameters":{"type":"object"}}]},
		{"type":"function","name":"shell","parameters":{"type":"object"}}]`
	if err := json.Unmarshal([]byte(raw), &tools); err != nil {
		t.Fatal(err)
	}
	ns := collectToolNamespaces(tools)
	if ns["spawn_agent"] != "collaboration" || ns["wait_agent"] != "collaboration" {
		t.Fatalf("namespaces not recorded: %#v", ns)
	}
	if _, ok := ns["shell"]; ok {
		t.Fatalf("top-level tool must not get a namespace: %#v", ns)
	}

	shape := shapeResponsesToolCall(KiroToolUse{ToolUseID: "c1", Name: "spawn_agent", Input: map[string]interface{}{"message": "hi"}}, nil, ns)
	item := shape.outputItem("fc_1", KiroToolUse{ToolUseID: "c1", Name: "spawn_agent"}, "completed", true)
	if item["namespace"] != "collaboration" {
		t.Fatalf("stream item missing namespace: %#v", item)
	}
	plain := shapeResponsesToolCall(KiroToolUse{ToolUseID: "c2", Name: "shell"}, nil, ns)
	if _, ok := plain.outputItem("fc_2", KiroToolUse{ToolUseID: "c2", Name: "shell"}, "completed", true)["namespace"]; ok {
		t.Fatal("non-namespaced tool must not carry a namespace field")
	}
}

func TestConvertResponsesInputParsesAgentMessage(t *testing.T) {
	items := []json.RawMessage{
		json.RawMessage(`{"type":"agent_message","author":"parent","recipient":"child","content":[{"type":"encrypted_content","encrypted_content":"reply with exactly: PONG"}]}`),
	}
	msgs, err := convertResponsesInputItems(items)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 || msgs[0].Role != "user" || extractOpenAIMessageText(msgs[0].Content) != "reply with exactly: PONG" {
		t.Fatalf("agent_message not converted: %#v", msgs)
	}
}

func TestCleanSchemaDropsEncryptedKeyword(t *testing.T) {
	m := map[string]interface{}{"type": "string", "encrypted": true}
	cleanSchema(m)
	if _, ok := m["encrypted"]; ok {
		t.Fatal("encrypted keyword should be stripped")
	}
}
