package main

import (
	"encoding/json"
	"testing"
)

func TestTools(t *testing.T) {
	tools := tools()
	if len(tools) != 3 {
		t.Fatalf("expected 3 tools, got %d", len(tools))
	}

	names := map[string]bool{}
	for _, tool := range tools {
		for _, decl := range tool.FunctionDeclarations {
			names[decl.Name] = true
		}
	}

	for _, name := range []string{"get_git_file", "search_code", "fetch_web_content"} {
		if !names[name] {
			t.Errorf("expected tool '%s' not found", name)
		}
	}
}

func TestGetGitFileToolParams(t *testing.T) {
	tools := tools()
	var getGitFileDecl FunctionDeclaration
	for _, tool := range tools {
		for _, decl := range tool.FunctionDeclarations {
			if decl.Name == "get_git_file" {
				getGitFileDecl = decl
				break
			}
		}
	}

	if getGitFileDecl.Parameters == nil || getGitFileDecl.Parameters.Type != "OBJECT" {
		t.Fatalf("expected parameter type 'OBJECT', got '%v'", getGitFileDecl.Parameters)
	}

	if _, ok := getGitFileDecl.Parameters.Properties["file_path"]; !ok {
		t.Errorf("expected 'file_path' property in declaration parameters")
	}

	if _, ok := getGitFileDecl.Parameters.Properties["branch"]; !ok {
		t.Errorf("expected 'branch' property in declaration parameters")
	}
}

func TestExtractFilePath(t *testing.T) {
	tests := []struct {
		name     string
		args     map[string]interface{}
		expected string
	}{
		{
			name:     "nil args",
			args:     nil,
			expected: "",
		},
		{
			name:     "file_path key",
			args:     map[string]interface{}{"file_path": "cmd/main.go"},
			expected: "cmd/main.go",
		},
		{
			name:     "filePath camelCase key",
			args:     map[string]interface{}{"filePath": "pkg/handlers.go"},
			expected: "pkg/handlers.go",
		},
		{
			name:     "path key",
			args:     map[string]interface{}{"path": "README.md"},
			expected: "README.md",
		},
		{
			name:     "empty map",
			args:     map[string]interface{}{},
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractStringArg(tt.args, "file_path", "filePath", "path")
			if got != tt.expected {
				t.Errorf("extractStringArg() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestExtractBranch(t *testing.T) {
	tests := []struct {
		name          string
		args          map[string]interface{}
		defaultBranch string
		expected      string
	}{
		{
			name:          "nil args with default",
			args:          nil,
			defaultBranch: "feature-branch",
			expected:      "feature-branch",
		},
		{
			name:          "explicit branch key",
			args:          map[string]interface{}{"branch": "main"},
			defaultBranch: "feature-branch",
			expected:      "main",
		},
		{
			name:          "ref key fallback",
			args:          map[string]interface{}{"ref": "master"},
			defaultBranch: "feature-branch",
			expected:      "master",
		},
		{
			name:          "empty branch key uses default",
			args:          map[string]interface{}{"branch": ""},
			defaultBranch: "develop",
			expected:      "develop",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			branch := extractStringArg(tt.args, "branch", "ref")
			if branch == "" {
				branch = tt.defaultBranch
			}
			if branch != tt.expected {
				t.Errorf("extractBranch() = %v, want %v", branch, tt.expected)
			}
		})
	}
}

func TestGeminiRequestSerialization(t *testing.T) {
	req := GeminiRequest{
		Contents: []Content{
			{
				Role: "user",
				Parts: []Part{
					{Text: "Please review this diff"},
				},
			},
			{
				Role: "model",
				Parts: []Part{
					{
						FunctionCall: &FunctionCall{
							Name: "get_git_file",
							Args: map[string]interface{}{
								"file_path": "main.go",
								"branch":    "main",
							},
						},
					},
				},
			},
			{
				Role: "user",
				Parts: []Part{
					{
						FunctionResponse: &FunctionResponse{
							Name: "get_git_file",
							Response: map[string]interface{}{
								"content": "package main\n",
							},
						},
					},
				},
			},
		},
		Tools: tools(),
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("failed to marshal GeminiRequest: %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("failed to unmarshal JSON: %v", err)
	}

	contents, ok := parsed["contents"].([]interface{})
	if !ok || len(contents) != 3 {
		t.Fatalf("expected 3 content turns, got %v", contents)
	}

	toolsParsed, ok := parsed["tools"].([]interface{})
	if !ok || len(toolsParsed) != 3 {
		t.Fatalf("expected 3 tools, got %v", toolsParsed)
	}
}

func TestGeminiResponseParsing(t *testing.T) {
	textJSON := `{
		"candidates": [
			{
				"content": {
					"role": "model",
					"parts": [
						{
							"text": "LGTM! Looks good to merge."
						}
					]
				}
			}
		]
	}`

	var resp1 GeminiResponse
	if err := json.Unmarshal([]byte(textJSON), &resp1); err != nil {
		t.Fatalf("failed to unmarshal text response: %v", err)
	}

	if len(resp1.Candidates) != 1 || resp1.Candidates[0].Content.Parts[0].Text != "LGTM! Looks good to merge." {
		t.Errorf("unexpected parsed candidate text: %+v", resp1)
	}

	fnJSON := `{
		"candidates": [
			{
				"content": {
					"role": "model",
					"parts": [
						{
							"functionCall": {
								"name": "get_git_file",
								"args": {
									"file_path": "pkg/handlers/provider.go",
									"branch": "main"
								}
							}
						}
					]
				}
			}
		]
	}`

	var resp2 GeminiResponse
	if err := json.Unmarshal([]byte(fnJSON), &resp2); err != nil {
		t.Fatalf("failed to unmarshal function call response: %v", err)
	}

	if len(resp2.Candidates) != 1 {
		t.Fatalf("expected 1 candidate, got %d", len(resp2.Candidates))
	}
	part := resp2.Candidates[0].Content.Parts[0]
	if part.FunctionCall == nil {
		t.Fatalf("expected functionCall part, got nil")
	}
	if part.FunctionCall.Name != "get_git_file" {
		t.Errorf("expected name 'get_git_file', got '%s'", part.FunctionCall.Name)
	}
	filePath := extractStringArg(part.FunctionCall.Args, "file_path", "filePath", "path")
	if filePath != "pkg/handlers/provider.go" {
		t.Errorf("expected path 'pkg/handlers/provider.go', got '%v'", part.FunctionCall.Args)
	}
	branch := extractStringArg(part.FunctionCall.Args, "branch", "ref")
	if branch == "" {
		branch = "feature"
	}
	if branch != "main" {
		t.Errorf("expected branch 'main', got '%v'", part.FunctionCall.Args)
	}
}

func TestHandleUnknownFunctionCall(t *testing.T) {
	fnCall := &FunctionCall{
		Name: "unknown_func",
		Args: map[string]interface{}{},
	}

	resp := handleFunctionCall(fnCall, "main")
	if resp == nil {
		t.Fatalf("expected response, got nil")
	}
	if resp.Name != "unknown_func" {
		t.Errorf("expected name 'unknown_func', got '%s'", resp.Name)
	}
	if errStr, ok := resp.Response["error"].(string); !ok || errStr == "" {
		t.Errorf("expected error in response, got %v", resp.Response)
	}
}

func TestHandleSearchCodeMissingQuery(t *testing.T) {
	fnCall := &FunctionCall{
		Name: "search_code",
		Args: map[string]interface{}{"branch": "main"},
	}

	resp := handleFunctionCall(fnCall, "main")
	if resp == nil {
		t.Fatalf("expected response, got nil")
	}
	if errStr, ok := resp.Response["error"].(string); !ok || errStr == "" {
		t.Errorf("expected error in response, got %v", resp.Response)
	}
}

func TestHandleFetchWebContentMissingUrl(t *testing.T) {
	fnCall := &FunctionCall{
		Name: "fetch_web_content",
		Args: map[string]interface{}{},
	}

	resp := handleFunctionCall(fnCall, "main")
	if resp == nil {
		t.Fatalf("expected response, got nil")
	}
	if errStr, ok := resp.Response["error"].(string); !ok || errStr == "" {
		t.Errorf("expected error in response, got %v", resp.Response)
	}
}

func TestThoughtSignatureParsing(t *testing.T) {
	camelJSON := `{
		"functionCall": {
			"name": "get_git_file",
			"args": {"file_path": "main.go"}
		},
		"thoughtSignature": "test_sig_camel_123"
	}`
	var p1 Part
	if err := json.Unmarshal([]byte(camelJSON), &p1); err != nil {
		t.Fatalf("failed to unmarshal camelCase thoughtSignature: %v", err)
	}
	if p1.ThoughtSignature != "test_sig_camel_123" {
		t.Errorf("expected 'test_sig_camel_123', got '%s'", p1.ThoughtSignature)
	}

	snakeJSON := `{
		"functionCall": {
			"name": "get_git_file",
			"args": {"file_path": "main.go"}
		},
		"thought_signature": "test_sig_snake_456"
	}`
	var p2 Part
	if err := json.Unmarshal([]byte(snakeJSON), &p2); err != nil {
		t.Fatalf("failed to unmarshal snake_case thought_signature: %v", err)
	}
	if p2.ThoughtSignature != "test_sig_snake_456" {
		t.Errorf("expected 'test_sig_snake_456', got '%s'", p2.ThoughtSignature)
	}

	thoughtJSON := `{
		"thought": true,
		"text": "Analyzing the diff...",
		"thoughtSignature": "thought_sig_789"
	}`
	var p3 Part
	if err := json.Unmarshal([]byte(thoughtJSON), &p3); err != nil {
		t.Fatalf("failed to unmarshal thought part: %v", err)
	}
	if !p3.Thought {
		t.Errorf("expected Thought=true, got false")
	}
	if p3.Text != "Analyzing the diff..." {
		t.Errorf("expected text 'Analyzing the diff...', got '%s'", p3.Text)
	}
	if p3.ThoughtSignature != "thought_sig_789" {
		t.Errorf("expected 'thought_sig_789', got '%s'", p3.ThoughtSignature)
	}

	out, err := json.Marshal(p1)
	if err != nil {
		t.Fatalf("failed to marshal part: %v", err)
	}
	var roundtrip map[string]interface{}
	if err := json.Unmarshal(out, &roundtrip); err != nil {
		t.Fatalf("failed to unmarshal roundtrip JSON: %v", err)
	}
	if roundtrip["thoughtSignature"] != "test_sig_camel_123" {
		t.Errorf("expected 'test_sig_camel_123' in serialized JSON, got %v", roundtrip["thoughtSignature"])
	}
}