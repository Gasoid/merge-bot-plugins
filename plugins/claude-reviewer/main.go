package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/extism/go-pdk"
	shared "github.com/gasoid/merge-bot-plugins/plugins/shared"
)

const (
	defaultModel            = "claude-3-5-sonnet-20240620"
	defaultEndpoint         = "https://api.anthropic.com/v1/messages"
	defaultMaxTokens        = 4096
	defaultAnthropicVersion = "2023-06-01"
)

//go:wasmexport review
func Review() int32 {
	input := shared.PluginInput{}
	if err := pdk.InputJSON(&input); err != nil {
		pdk.SetError(err)
		return 1
	}

	apiKey, ok := input.Vars["claude_reviewer_api_key"]
	if !ok {
		pdk.SetError(errors.New("CLAUDE_REVIEWER_API_KEY is not provided"))
		return 1
	}

	model, ok := input.Vars["claude_reviewer_model"]
	if !ok {
		model = defaultModel
	}

	prompt, ok := input.Vars["claude_reviewer_prompt"]
	if !ok {
		prompt = shared.DefaultPrompt
	}

	endpoint, ok := input.Vars["claude_reviewer_endpoint"]
	if !ok {
		endpoint = defaultEndpoint
	}

	maxTokens := shared.ParseIntVar(input.Vars, "claude_reviewer_max_tokens", defaultMaxTokens, 1)

	anthropicVersion, ok := input.Vars["claude_reviewer_anthropic_version"]
	if !ok {
		anthropicVersion = defaultAnthropicVersion
	}

	maxTurns := shared.ParseIntVar(input.Vars, "claude_reviewer_max_turns", shared.DefaultMaxTurns, 1)
	maxRetries := shared.ParseIntVar(input.Vars, "claude_reviewer_max_retries", shared.DefaultMaxRetries, 0)

	fullPrompt, defaultBranch, err := shared.BuildPrompt(input, prompt)
	if err != nil {
		pdk.SetError(err)
		return 1
	}

	result, err := review(fullPrompt, endpoint, apiKey, model, anthropicVersion, defaultBranch, maxTokens, maxTurns, maxRetries)
	if err != nil {
		pdk.SetError(err)
		return 1
	}

	output := shared.ParseOutput(result)
	output.Threads = shared.ValidateThreads(output.Threads, defaultBranch, input.TargetBranch)
	pdk.OutputJSON(output)

	return 0
}

type ClaudeRequest struct {
	Model     string       `json:"model"`
	MaxTokens int          `json:"max_tokens"`
	Messages  []Message    `json:"messages"`
	System    string       `json:"system,omitempty"`
	Tools     []ClaudeTool `json:"tools,omitempty"`
}

type Message struct {
	Role    string         `json:"role"`
	Content []ContentBlock `json:"content"`
}

type ContentBlock struct {
	Type      string                 `json:"type"`
	Text      string                 `json:"text,omitempty"`
	ID        string                 `json:"id,omitempty"`
	Name      string                 `json:"name,omitempty"`
	Input     map[string]interface{} `json:"input,omitempty"`
	ToolUseID string                 `json:"tool_use_id,omitempty"`
	Content   string                 `json:"content,omitempty"`
}

type ClaudeTool struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	InputSchema InputSchema `json:"input_schema"`
}

type InputSchema struct {
	Type       string              `json:"type"`
	Properties map[string]Property `json:"properties"`
	Required   []string            `json:"required,omitempty"`
}

type Property struct {
	Type        string `json:"type"`
	Description string `json:"description,omitempty"`
}

type ClaudeResponse struct {
	ID         string         `json:"id"`
	Type       string         `json:"type"`
	Role       string         `json:"role"`
	Model      string         `json:"model"`
	Content    []ContentBlock `json:"content"`
	StopReason string         `json:"stop_reason"`
}

func tools() []ClaudeTool {
	defs := shared.Tools()
	claudeTools := make([]ClaudeTool, 0, len(defs))
	for _, d := range defs {
		props := make(map[string]Property, len(d.Parameters.Properties))
		for k, p := range d.Parameters.Properties {
			props[k] = Property{Type: p.Type, Description: p.Description}
		}
		claudeTools = append(claudeTools, ClaudeTool{
			Name:        d.Name,
			Description: d.Description,
			InputSchema: InputSchema{
				Type:       d.Parameters.Type,
				Properties: props,
				Required:   d.Parameters.Required,
			},
		})
	}
	return claudeTools
}

func review(initialPrompt, endpoint, apiKey, model, anthropicVersion, defaultBranch string, maxTokens, maxTurns, maxRetries int) (string, error) {
	toolDefs := tools()

	messages := []Message{
		{
			Role: "user",
			Content: []ContentBlock{
				{
					Type: "text",
					Text: initialPrompt,
				},
			},
		},
	}

	var latestText string

	for turn := 0; turn < maxTurns; turn++ {
		req := ClaudeRequest{
			Model:     model,
			MaxTokens: maxTokens,
			Messages:  messages,
		}
		if turn < maxTurns-1 {
			req.Tools = toolDefs
		}

		b, err := json.Marshal(req)
		if err != nil {
			return "", err
		}

		headers := map[string]string{
			"x-api-key":         apiKey,
			"anthropic-version": anthropicVersion,
		}
		resp, err := shared.SendHTTPRequestWithRetry(endpoint, headers, b, maxRetries)
		if err != nil {
			return "", err
		}

		var claudeResp ClaudeResponse
		if err := json.Unmarshal(resp.Body(), &claudeResp); err != nil {
			return "", fmt.Errorf("failed to parse Claude response: %w", err)
		}

		if claudeResp.StopReason == "max_tokens" {
			return "", fmt.Errorf("response truncated: max_tokens (%d) reached, increase claude_reviewer_max_tokens", maxTokens)
		}

		var toolUseBlocks []ContentBlock
		var textParts []string

		for _, block := range claudeResp.Content {
			if block.Type == "tool_use" && block.Name != "" {
				toolUseBlocks = append(toolUseBlocks, block)
			}
			if block.Type == "text" && block.Text != "" {
				textParts = append(textParts, block.Text)
			}
		}

		if len(textParts) > 0 {
			latestText = strings.Join(textParts, "\n")
		}

		if len(toolUseBlocks) == 0 {
			if latestText != "" {
				return latestText, nil
			}
			return "", errors.New("model returned neither text nor tool use")
		}

		messages = append(messages, Message{
			Role:    "assistant",
			Content: claudeResp.Content,
		})

		var toolResultBlocks []ContentBlock
		for _, block := range toolUseBlocks {
			result := shared.ExecuteTool(block.Name, block.Input, defaultBranch)
			resultJSON, err := json.Marshal(result)
			if err != nil {
				return "", err
			}
			toolResultBlocks = append(toolResultBlocks, ContentBlock{
				Type:      "tool_result",
				ToolUseID: block.ID,
				Content:   string(resultJSON),
			})
		}

		messages = append(messages, Message{
			Role:    "user",
			Content: toolResultBlocks,
		})
	}

	if latestText != "" {
		return latestText, nil
	}

	return "", fmt.Errorf("agent reached max turns (%d) without completing review", maxTurns)
}

func main() {}
