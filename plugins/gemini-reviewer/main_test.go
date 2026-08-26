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
		for _, decl := range tool.FunctionDeclarations {
			names[decl.Name] = true
		}
	}

	for _, name := range []string{"get_git_file", "search_code", "fetch_web_content", "get_ci_failed_jobs"} {
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
			got := shared.ExtractStringArg(tt.args, "file_path", "filePath", "path")
			if got != tt.expected {
				t.Errorf("ExtractStringArg() = %v, want %v", got, tt.expected)
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
			branch := shared.ExtractStringArg(tt.args, "branch", "ref")
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
	if !ok || len(toolsParsed) != 4 {
		t.Fatalf("expected 4 tools, got %v", toolsParsed)
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
	filePath := shared.ExtractStringArg(part.FunctionCall.Args, "file_path", "filePath", "path")
	if filePath != "pkg/handlers/provider.go" {
		t.Errorf("expected path 'pkg/handlers/provider.go', got '%v'", part.FunctionCall.Args)
	}
	branch := shared.ExtractStringArg(part.FunctionCall.Args, "branch", "ref")
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

func TestParseOutputWithThreads(t *testing.T) {
	result := `{
		"comment": "Looks good overall",
		"threads": [
			{
				"new_line": 12,
				"new_path": "app/file.py",
				"old_path": "app/file.py",
				"body": "fix this"
			},
			{
				"old_line": 30,
				"old_path": "app/file.py",
				"new_path": "app/file.py",
				"body": "consider removing"
			}
		]
	}`

	output := shared.ParseOutput(result)
	if output.Comment != "Looks good overall" {
		t.Errorf("expected comment 'Looks good overall', got '%s'", output.Comment)
	}
	if len(output.Threads) != 2 {
		t.Fatalf("expected 2 threads, got %d", len(output.Threads))
	}

	added := output.Threads[0]
	if added.NewLine != 12 || added.OldLine != 0 {
		t.Errorf("expected added thread new_line=12 old_line=0, got %+v", added)
	}

	removed := output.Threads[1]
	if removed.OldLine != 30 || removed.NewLine != 0 {
		t.Errorf("expected removed thread old_line=30 new_line=0, got %+v", removed)
	}
}

func TestParseOutputPlainText(t *testing.T) {
	output := shared.ParseOutput("This is a plain text review, not JSON.")
	if output.Comment != "This is a plain text review, not JSON." {
		t.Errorf("expected plain text comment, got '%s'", output.Comment)
	}
	if len(output.Threads) != 0 {
		t.Errorf("expected 0 threads, got %d", len(output.Threads))
	}
}

func TestParseOutputCodeFence(t *testing.T) {
	result := "```json\n{\"comment\": \"wrapped\"}\n```"
	output := shared.ParseOutput(result)
	if output.Comment != "wrapped" {
		t.Errorf("expected comment 'wrapped', got '%s'", output.Comment)
	}
}

func TestParseOutputBackticksInBody(t *testing.T) {
	result := "```json\n{\"comment\": \"use `code` fences\"}\n```"
	output := shared.ParseOutput(result)
	if output.Comment != "use `code` fences" {
		t.Errorf("expected comment 'use `code` fences', got '%s'", output.Comment)
	}
}

func TestParseOutputPlainBacktickFence(t *testing.T) {
	result := "```\n{\"comment\": \"no lang\"}\n```"
	output := shared.ParseOutput(result)
	if output.Comment != "no lang" {
		t.Errorf("expected comment 'no lang', got '%s'", output.Comment)
	}
}

func TestCountLines(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected int
	}{
		{"empty string", "", 0},
		{"single newline", "\n", 1},
		{"no trailing newline", "a\nb\nc", 3},
		{"trailing newline", "a\nb\nc\n", 3},
		{"multiple trailing newlines", "a\nb\n\n\n", 2},
		{"single line no newline", "a", 1},
		{"blank lines only", "\n\n", 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shared.CountLines(tt.input)
			if got != tt.expected {
				t.Errorf("CountLines(%q) = %d, want %d", tt.input, got, tt.expected)
			}
		})
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		maxBytes int
	}{
		{"no truncation", "short", 100},
		{"exact boundary", "12345", 5},
		{"ascii truncation", "123456789", 5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shared.Truncate(tt.input, tt.maxBytes)
			if tt.name == "no truncation" && got != tt.input {
				t.Errorf("expected no truncation, got %q", got)
			}
			if tt.name == "exact boundary" && got != tt.input {
				t.Errorf("expected no truncation at boundary, got %q", got)
			}
			if len(got) > tt.maxBytes && tt.name == "ascii truncation" {
				t.Errorf("expected result within %d bytes before marker, got %q", tt.maxBytes, got)
			}
		})
	}
}

func TestTruncateUTF8(t *testing.T) {
	input := "héllo wörld"
	got := shared.Truncate(input, 4)

	if len(got) > 0 {
		cut := got
		if len(got) >= len("\n... [truncated]") {
			cut = got[:len(got)-len("\n... [truncated]")]
		}
		for i := 0; i < len(cut); i++ {
			if cut[i]&0xC0 == 0x80 {
				t.Errorf("truncate split a UTF-8 continuation byte at position %d: %q", i, cut)
			}
		}
	}
}
