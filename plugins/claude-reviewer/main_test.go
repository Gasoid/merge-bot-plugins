// Tests in this file are compile-checked only. Package main imports
// github.com/extism/go-pdk, whose host functions have no bodies outside wasm, so
// `go test` cannot build it for the host; and a wasip1/wasm test binary declares
// extism:host/env imports that a plain wasm runtime cannot resolve, so it fails
// to instantiate. CI runs `go test -c` here to keep them compiling.
//
// Assertions that need to actually execute belong in internal/wire, which
// imports only encoding/json and runs under
// `go test ./plugins/claude-reviewer/internal/...`.
package main

import (
	"testing"

	"github.com/gasoid/merge-bot-plugins/plugins/claude-reviewer/internal/wire"
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
	var getGitFileTool wire.Tool
	for _, tool := range tools() {
		if tool.Name == "get_git_file" {
			getGitFileTool = tool
			break
		}
	}

	if getGitFileTool.InputSchema.Type != "object" {
		t.Fatalf("expected input_schema type 'object', got '%s'", getGitFileTool.InputSchema.Type)
	}

	for _, prop := range []string{"file_path", "branch"} {
		if _, ok := getGitFileTool.InputSchema.Properties[prop]; !ok {
			t.Errorf("expected '%s' property in input_schema", prop)
		}
	}

	if len(getGitFileTool.InputSchema.Required) != 1 || getGitFileTool.InputSchema.Required[0] != "file_path" {
		t.Errorf("expected required ['file_path'], got %v", getGitFileTool.InputSchema.Required)
	}
}

// get_ci_failed_jobs takes no arguments. Anthropic needs an object schema with
// an empty properties map here; Gemini instead needs the parameters block
// omitted entirely, which is why the shared definition carries an empty map
// rather than a nil one.
func TestNoArgToolHasEmptyProperties(t *testing.T) {
	var tool wire.Tool
	for _, candidate := range tools() {
		if candidate.Name == "get_ci_failed_jobs" {
			tool = candidate
			break
		}
	}

	if tool.Name == "" {
		t.Fatal("get_ci_failed_jobs not found")
	}
	if tool.InputSchema.Properties == nil {
		t.Error("properties should be an empty map, not nil")
	}
	if len(tool.InputSchema.Properties) != 0 {
		t.Errorf("expected no properties, got %v", tool.InputSchema.Properties)
	}
}

func TestToolArgExtraction(t *testing.T) {
	block := wire.ContentBlock{
		Type:  "tool_use",
		ID:    "toolu_1",
		Name:  "get_git_file",
		Input: map[string]interface{}{"file_path": "pkg/handlers/provider.go"},
	}

	if got := shared.ExtractStringArg(block.Input, "file_path", "filePath", "path"); got != "pkg/handlers/provider.go" {
		t.Errorf("expected path 'pkg/handlers/provider.go', got '%s'", got)
	}
}
