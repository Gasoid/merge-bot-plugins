package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/extism/go-pdk"
	"github.com/gasoid/merge-bot-plugins/plugins/claude-reviewer/internal/wire"
	shared "github.com/gasoid/merge-bot-plugins/plugins/shared"
)

const (
	defaultModel    = "claude-sonnet-5"
	defaultEndpoint = "https://api.anthropic.com/v1/messages"
	// max_tokens caps thinking plus response text, and current models think by
	// default, so this needs room for both. Kept under ~16k because the request
	// is not streamed.
	defaultMaxTokens        = 16000
	defaultAnthropicVersion = "2023-06-01"
	// A tool-capable turn plus a final answer turn is the shortest run that can
	// use the tools the prompt advertises.
	minMaxTurns = 2
	// Below this there is no room for thinking plus a structured review, so the
	// run would only ever stop on max_tokens. The floor sits above the 4096 this
	// plugin used to document as its default, so configs pinned to the old value
	// fall back to defaultMaxTokens instead of failing every review once the
	// model started thinking by default.
	minMaxTokens = 8192
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

	maxTokens := shared.ParseIntVar(input.Vars, "claude_reviewer_max_tokens", defaultMaxTokens, minMaxTokens)

	anthropicVersion, ok := input.Vars["claude_reviewer_anthropic_version"]
	if !ok {
		anthropicVersion = defaultAnthropicVersion
	}

	maxTurns := shared.ParseIntVarRange(input.Vars, "claude_reviewer_max_turns", shared.DefaultMaxTurns, minMaxTurns, shared.MaxAllowedTurns)
	maxRetries := shared.ParseIntVarRange(input.Vars, "claude_reviewer_max_retries", shared.DefaultMaxRetries, 0, shared.MaxAllowedRetries)

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

func tools() []wire.Tool {
	defs := shared.Tools()
	claudeTools := make([]wire.Tool, 0, len(defs))
	for _, d := range defs {
		props := make(map[string]wire.Property, len(d.Parameters.Properties))
		for k, p := range d.Parameters.Properties {
			props[k] = wire.Property{Type: p.Type, Description: p.Description}
		}
		claudeTools = append(claudeTools, wire.Tool{
			Name:        d.Name,
			Description: d.Description,
			InputSchema: wire.InputSchema{
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

	messages := []wire.Message{
		{
			Role: "user",
			Content: []wire.ContentBlock{
				{
					Type: "text",
					Text: initialPrompt,
				},
			},
		},
	}

	for turn := 0; turn < maxTurns; turn++ {
		req := wire.Request{
			Model:     model,
			MaxTokens: maxTokens,
			Messages:  messages,
			Tools:     toolDefs,
		}
		// tools must stay on every request: once the history holds tool_use and
		// tool_result blocks, the API rejects a request that omits it. Forbid
		// further tool calls on the final turn with tool_choice instead, so the
		// model has to produce its answer.
		if turn == maxTurns-1 {
			req.ToolChoice = &wire.ToolChoice{Type: "none"}
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

		var claudeResp wire.Response
		if err := json.Unmarshal(resp.Body(), &claudeResp); err != nil {
			return "", fmt.Errorf("failed to parse Claude response: %w", err)
		}

		if claudeResp.StopReason == "max_tokens" {
			return "", fmt.Errorf("response truncated: max_tokens (%d) reached, increase claude_reviewer_max_tokens", maxTokens)
		}

		var toolUseBlocks []wire.ContentBlock
		var textParts []string

		for _, block := range claudeResp.Content {
			// Every tool_use block needs a matching tool_result in the next turn,
			// including a malformed one: the whole response is echoed back, and
			// the API rejects a tool_use with no result. shared.ExecuteTool
			// answers an unknown name with an error result.
			if block.Type == "tool_use" {
				toolUseBlocks = append(toolUseBlocks, block)
			}
			if block.Type == "text" && block.Text != "" {
				textParts = append(textParts, block.Text)
			}
		}

		// Only text from a turn that made no tool calls is an answer. Text
		// alongside a tool_use is a preamble ("I'll fetch that file first"), and
		// returning it would have the bot post a preamble as its review.
		if len(toolUseBlocks) == 0 {
			if len(textParts) > 0 {
				return strings.Join(textParts, "\n"), nil
			}
			return "", errors.New("model returned neither text nor tool use")
		}

		messages = append(messages, wire.Message{
			Role:    "assistant",
			Content: claudeResp.Content,
		})

		var toolResultBlocks []wire.ContentBlock
		for _, block := range toolUseBlocks {
			result := shared.ExecuteTool(block.Name, block.Input, defaultBranch)
			resultJSON, err := json.Marshal(result)
			if err != nil {
				return "", err
			}
			toolResultBlocks = append(toolResultBlocks, wire.ContentBlock{
				Type:      "tool_result",
				ToolUseID: block.ID,
				Content:   string(resultJSON),
			})
		}

		messages = append(messages, wire.Message{
			Role:    "user",
			Content: toolResultBlocks,
		})
	}

	return "", fmt.Errorf("agent reached max turns (%d) without completing review", maxTurns)
}

func main() {}
