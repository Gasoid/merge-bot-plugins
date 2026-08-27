package wire

import (
	"encoding/json"
	"testing"
)

// decodeBlock marshals a block and decodes it into a generic map so tests can
// assert on field presence, which is what the API actually validates.
func decodeBlock(t *testing.T, b ContentBlock) map[string]interface{} {
	t.Helper()
	data, err := json.Marshal(b)
	if err != nil {
		t.Fatalf("marshal %s block: %v", b.Type, err)
	}
	var out map[string]interface{}
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("unmarshal %s block: %v", b.Type, err)
	}
	return out
}

// A tool_use block for a tool that takes no arguments still has to carry
// "input". Claude returns "input": {} for such calls, and the block is echoed
// back as an assistant message on the next turn; if omitempty drops the empty
// map the API rejects the follow-up request.
func TestToolUseBlockKeepsEmptyInput(t *testing.T) {
	for name, input := range map[string]map[string]interface{}{
		"with args": {"job_id": float64(42)},
		"nil map":   nil,
	} {
		t.Run(name, func(t *testing.T) {
			got := decodeBlock(t, ContentBlock{
				Type:  "tool_use",
				ID:    "toolu_1",
				Name:  "get_ci_job_log",
				Input: input,
			})

			raw, ok := got["input"]
			if !ok {
				t.Fatalf("tool_use block dropped required 'input' field: %v", got)
			}
			// A JSON null decodes into interface{} as nil and would satisfy the
			// presence check above, so the type assertion is what actually
			// pins down "input": {} rather than "input": null.
			args, ok := raw.(map[string]interface{})
			if !ok {
				t.Fatalf("'input' should encode as an object, got %T (%v)", raw, raw)
			}
			switch name {
			case "with args":
				if args["job_id"] != float64(42) {
					t.Errorf("expected job_id 42, got %v", args["job_id"])
				}
			case "nil map":
				if len(args) != 0 {
					t.Errorf("nil input should encode as an empty object, got %v", args)
				}
			}
			for _, field := range []string{"id", "name"} {
				if _, ok := got[field]; !ok {
					t.Errorf("tool_use block missing required '%s'", field)
				}
			}
		})
	}
}

func TestToolUseBlockKeepsArguments(t *testing.T) {
	got := decodeBlock(t, ContentBlock{
		Type:  "tool_use",
		ID:    "toolu_2",
		Name:  "get_git_file",
		Input: map[string]interface{}{"file_path": "main.go", "branch": "main"},
	})

	args, ok := got["input"].(map[string]interface{})
	if !ok {
		t.Fatalf("'input' should encode as an object, got %T", got["input"])
	}
	if args["file_path"] != "main.go" || args["branch"] != "main" {
		t.Errorf("arguments not preserved: %v", args)
	}
}

// "text" is required on a text block, so an empty string must still serialize.
func TestTextBlockKeepsEmptyText(t *testing.T) {
	got := decodeBlock(t, ContentBlock{Type: "text"})

	if _, ok := got["text"]; !ok {
		t.Errorf("text block dropped required 'text' field: %v", got)
	}
}

// A text block carries no tool fields, and a tool_result carries no text
// fields; leaking either would be rejected as an unexpected key.
func TestBlocksOmitFieldsFromOtherTypes(t *testing.T) {
	text := decodeBlock(t, ContentBlock{Type: "text", Text: "LGTM"})
	for _, field := range []string{"id", "name", "input", "tool_use_id", "content"} {
		if _, ok := text[field]; ok {
			t.Errorf("text block should not contain '%s'", field)
		}
	}

	result := decodeBlock(t, ContentBlock{
		Type:      "tool_result",
		ToolUseID: "toolu_1",
		Content:   `{"content":"package main\n"}`,
	})
	if result["tool_use_id"] != "toolu_1" {
		t.Errorf("tool_result lost tool_use_id: %v", result)
	}
	if _, ok := result["content"]; !ok {
		t.Errorf("tool_result missing required 'content': %v", result)
	}
	for _, field := range []string{"text", "id", "name", "input"} {
		if _, ok := result[field]; ok {
			t.Errorf("tool_result block should not contain '%s'", field)
		}
	}
}

// A thinking block must be replayed unchanged, so both thinking and signature
// have to survive the round trip. Current models think by default, and with the
// default display of "omitted" the thinking text is empty while the signature is
// not — so an omitempty on thinking would ship a block the API rejects.
func TestThinkingBlockKeepsSignature(t *testing.T) {
	got := decodeBlock(t, ContentBlock{
		Type:      "thinking",
		Thinking:  "",
		Signature: "ErUBCkYIBxgCKkBt3f8",
	})

	if _, ok := got["thinking"]; !ok {
		t.Errorf("thinking block dropped required 'thinking' field: %v", got)
	}
	if got["signature"] != "ErUBCkYIBxgCKkBt3f8" {
		t.Errorf("thinking block lost 'signature': %v", got)
	}
	for _, field := range []string{"text", "id", "name", "input", "tool_use_id", "content"} {
		if _, ok := got[field]; ok {
			t.Errorf("thinking block should not contain '%s'", field)
		}
	}
}

func TestRedactedThinkingBlockKeepsData(t *testing.T) {
	got := decodeBlock(t, ContentBlock{Type: "redacted_thinking", Data: "EvwBCkYIA"})

	if got["data"] != "EvwBCkYIA" {
		t.Errorf("redacted_thinking block lost 'data': %v", got)
	}
	if _, ok := got["thinking"]; ok {
		t.Errorf("redacted_thinking block should not contain 'thinking': %v", got)
	}
}

// Response blocks are decoded and then re-encoded verbatim as the assistant
// turn, so a decode/encode round trip has to preserve every required field. The
// content here is the shape a thinking model returns on a tool-calling turn.
func TestResponseBlocksSurviveRoundTrip(t *testing.T) {
	const body = `{
		"id": "msg_1",
		"type": "message",
		"role": "assistant",
		"model": "claude-test",
		"content": [
			{"type": "thinking", "thinking": "", "signature": "ErUBCkYIBxgCKkBt3f8"},
			{"type": "text", "text": "Checking CI first."},
			{"type": "tool_use", "id": "toolu_1", "name": "get_ci_job_log", "input": {"job_id": 42}}
		],
		"stop_reason": "tool_use"
	}`

	var resp Response
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if len(resp.Content) != 3 {
		t.Fatalf("expected 3 content blocks, got %d", len(resp.Content))
	}

	echoed, err := json.Marshal(Message{Role: "assistant", Content: resp.Content})
	if err != nil {
		t.Fatalf("marshal assistant echo: %v", err)
	}

	var parsed struct {
		Content []map[string]interface{} `json:"content"`
	}
	if err := json.Unmarshal(echoed, &parsed); err != nil {
		t.Fatalf("unmarshal assistant echo: %v", err)
	}
	if _, ok := parsed.Content[0]["thinking"]; !ok {
		t.Errorf("echoed thinking block lost 'thinking': %v", parsed.Content[0])
	}
	if parsed.Content[0]["signature"] != "ErUBCkYIBxgCKkBt3f8" {
		t.Errorf("echoed thinking block lost 'signature': %v", parsed.Content[0])
	}
	if _, ok := parsed.Content[1]["text"]; !ok {
		t.Errorf("echoed text block lost 'text': %v", parsed.Content[1])
	}
	if _, ok := parsed.Content[2]["input"]; !ok {
		t.Errorf("echoed tool_use block lost 'input': %v", parsed.Content[2])
	}
}

// A tool with arguments must serialize properties and required correctly.
func TestInputSchemaKeepsProperties(t *testing.T) {
	data, err := json.Marshal(Tool{
		Name:        "get_ci_job_log",
		Description: "Fetch CI job log.",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"job_id": {Type: "integer", Description: "CI job ID"},
			},
			Required: []string{"job_id"},
		},
	})
	if err != nil {
		t.Fatalf("marshal tool: %v", err)
	}

	var parsed struct {
		InputSchema map[string]interface{} `json:"input_schema"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("unmarshal tool: %v", err)
	}
	if _, ok := parsed.InputSchema["properties"]; !ok {
		t.Errorf("input_schema dropped 'properties': %v", parsed.InputSchema)
	}
	if _, ok := parsed.InputSchema["required"]; !ok {
		t.Errorf("input_schema dropped 'required': %v", parsed.InputSchema)
	}
}

// tool_choice is absent on tool-calling turns and "none" on the final turn.
func TestRequestToolChoice(t *testing.T) {
	base := Request{
		Model:     "claude-test",
		MaxTokens: 16000,
		Messages:  []Message{{Role: "user", Content: []ContentBlock{{Type: "text", Text: "review"}}}},
		Tools:     []Tool{{Name: "get_git_file", InputSchema: InputSchema{Type: "object", Properties: map[string]Property{}}}},
	}

	var withoutChoice map[string]interface{}
	data, err := json.Marshal(base)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	if err := json.Unmarshal(data, &withoutChoice); err != nil {
		t.Fatalf("unmarshal request: %v", err)
	}
	if _, ok := withoutChoice["tool_choice"]; ok {
		t.Errorf("tool_choice should be omitted when unset: %v", withoutChoice)
	}
	if _, ok := withoutChoice["tools"]; !ok {
		t.Errorf("tools must always be sent: %v", withoutChoice)
	}

	base.ToolChoice = &ToolChoice{Type: "none"}
	var withChoice struct {
		Tools      []interface{}     `json:"tools"`
		ToolChoice map[string]string `json:"tool_choice"`
	}
	data, err = json.Marshal(base)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	if err := json.Unmarshal(data, &withChoice); err != nil {
		t.Fatalf("unmarshal request: %v", err)
	}
	if withChoice.ToolChoice["type"] != "none" {
		t.Errorf("expected tool_choice type 'none', got %v", withChoice.ToolChoice)
	}
	// tools must stay defined alongside tool_choice "none": the API rejects a
	// request whose history holds tool_use blocks without a tools definition.
	if len(withChoice.Tools) != 1 {
		t.Errorf("tools must remain defined on the final turn, got %v", withChoice.Tools)
	}
}
