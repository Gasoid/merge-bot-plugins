package main

import (
	"encoding/json"
	"testing"

	shared "github.com/gasoid/merge-bot-plugins/plugins/shared"
)

func TestTools(t *testing.T) {
	tools := tools()
	if len(tools) != 4 {
		t.Fatalf("expected 4 tools, got %d", len(tools))
	}

	names := map[string]bool{}
	for _, tool := range tools {
		names[tool.Name] = true
	}

	for _, name := range []string{"get_git_file", "search_code", "fetch_web_content", "get_ci_failed_jobs"} {
		if !names[name] {
			t.Errorf("expected tool '%s' not found", name)
		}
	}
}

func TestGetGitFileToolParams(t *testing.T) {
	tools := tools()
	var getGitFileTool ClaudeTool
	for _, tool := range tools {
		if tool.Name == "get_git_file" {
			getGitFileTool = tool
			break
		}
	}

	if getGitFileTool.InputSchema.Type != "object" {
		t.Fatalf("expected input_schema type 'object', got '%s'", getGitFileTool.InputSchema.Type)
	}

	if _, ok := getGitFileTool.InputSchema.Properties["file_path"]; !ok {
		t.Errorf("expected 'file_path' property in input_schema")
	}

	if _, ok := getGitFileTool.InputSchema.Properties["branch"]; !ok {
		t.Errorf("expected 'branch' property in input_schema")
	}

	if len(getGitFileTool.InputSchema.Required) != 1 || getGitFileTool.InputSchema.Required[0] != "file_path" {
		t.Errorf("expected required ['file_path'], got %v", getGitFileTool.InputSchema.Required)
	}
}

func TestClaudeRequestSerialization(t *testing.T) {
	req := ClaudeRequest{
		Model:     "claude-test",
		MaxTokens: 4096,
		Messages: []Message{
			{
				Role: "user",
				Content: []ContentBlock{
					{Type: "text", Text: "Please review this diff"},
				},
			},
			{
				Role: "assistant",
				Content: []ContentBlock{
					{Type: "tool_use", ID: "toolu_1", Name: "get_git_file", Input: map[string]interface{}{"file_path": "main.go", "branch": "main"}},
				},
			},
			{
				Role: "user",
				Content: []ContentBlock{
					{Type: "tool_result", ToolUseID: "toolu_1", Content: `{"content":"package main\n"}`},
				},
			},
		},
		Tools: tools(),
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("failed to marshal ClaudeRequest: %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("failed to unmarshal JSON: %v", err)
	}

	messages, ok := parsed["messages"].([]interface{})
	if !ok || len(messages) != 3 {
		t.Fatalf("expected 3 messages, got %v", messages)
	}

	toolsParsed, ok := parsed["tools"].([]interface{})
	if !ok || len(toolsParsed) != 4 {
		t.Fatalf("expected 4 tools, got %v", toolsParsed)
	}
}

func TestClaudeResponseParsing(t *testing.T) {
	textJSON := `{
		"id": "msg_1",
		"type": "message",
		"role": "assistant",
		"model": "claude-test",
		"content": [
			{"type": "text", "text": "LGTM! Looks good to merge."}
		],
		"stop_reason": "end_turn"
	}`

	var resp1 ClaudeResponse
	if err := json.Unmarshal([]byte(textJSON), &resp1); err != nil {
		t.Fatalf("failed to unmarshal text response: %v", err)
	}

	if len(resp1.Content) != 1 || resp1.Content[0].Text != "LGTM! Looks good to merge." {
		t.Errorf("unexpected parsed content: %+v", resp1)
	}

	toolJSON := `{
		"id": "msg_2",
		"type": "message",
		"role": "assistant",
		"model": "claude-test",
		"content": [
			{"type": "tool_use", "id": "toolu_1", "name": "get_git_file", "input": {"file_path": "pkg/handlers/provider.go", "branch": "main"}}
		],
		"stop_reason": "tool_use"
	}`

	var resp2 ClaudeResponse
	if err := json.Unmarshal([]byte(toolJSON), &resp2); err != nil {
		t.Fatalf("failed to unmarshal tool use response: %v", err)
	}

	if len(resp2.Content) != 1 {
		t.Fatalf("expected 1 content block, got %d", len(resp2.Content))
	}
	block := resp2.Content[0]
	if block.Type != "tool_use" || block.Name != "get_git_file" || block.ID != "toolu_1" {
		t.Errorf("unexpected tool_use block: %+v", block)
	}
	filePath := shared.ExtractStringArg(block.Input, "file_path", "filePath", "path")
	if filePath != "pkg/handlers/provider.go" {
		t.Errorf("expected path 'pkg/handlers/provider.go', got '%v'", block.Input)
	}
}
