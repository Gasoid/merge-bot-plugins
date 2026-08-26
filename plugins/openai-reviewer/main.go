package main

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/extism/go-pdk"
	shared "github.com/gasoid/merge-bot-plugins/plugins/shared"
)

const (
	defaultModel    = "gpt-5.1-codex-mini"
	defaultEndpoint = "https://api.openai.com/v1/chat/completions"
)

//go:wasmexport review
func Review() int32 {
	input := shared.PluginInput{}
	if err := pdk.InputJSON(&input); err != nil {
		pdk.SetError(err)
		return 1
	}

	apiKey, ok := input.Vars["reviewer_api_key"]
	if !ok {
		pdk.SetError(errors.New("REVIEWER_API_KEY is not provided"))
		return 1
	}

	model, ok := input.Vars["reviewer_model"]
	if !ok {
		model = defaultModel
	}

	prompt, ok := input.Vars["reviewer_prompt"]
	if !ok {
		prompt = shared.DefaultPrompt
	}

	endpoint, ok := input.Vars["reviewer_endpoint"]
	if !ok {
		endpoint = defaultEndpoint
	}

	maxTurns := shared.ParseIntVar(input.Vars, "reviewer_max_turns", shared.DefaultMaxTurns, 1)
	maxRetries := shared.ParseIntVar(input.Vars, "reviewer_max_retries", shared.DefaultMaxRetries, 0)

	fullPrompt, defaultBranch, err := shared.BuildPrompt(input, prompt)
	if err != nil {
		pdk.SetError(err)
		return 1
	}

	result, err := review(fullPrompt, endpoint, apiKey, model, defaultBranch, maxTurns, maxRetries)
	if err != nil {
		pdk.SetError(err)
		return 1
	}

	output := shared.ParseOutput(result)
	output.Threads = shared.ValidateThreads(output.Threads, defaultBranch, input.TargetBranch)
	pdk.OutputJSON(output)

	return 0
}

type OpenAITool struct {
	Type     string             `json:"type"`
	Function FunctionDefinition `json:"function"`
}

type FunctionDefinition struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Parameters  *Parameters `json:"parameters,omitempty"`
}

type Parameters struct {
	Type       string              `json:"type"`
	Properties map[string]Property `json:"properties"`
	Required   []string            `json:"required,omitempty"`
}

type Property struct {
	Type        string `json:"type"`
	Description string `json:"description,omitempty"`
}

type OpenAIMessage struct {
	Role       string     `json:"role"`
	Content    string     `json:"content,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	Name       string     `json:"name,omitempty"`
}

type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function FunctionCall `json:"function"`
}

type FunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type OpenAIRequest struct {
	Model    string          `json:"model"`
	Messages []OpenAIMessage `json:"messages"`
	Tools    []OpenAITool    `json:"tools,omitempty"`
}

type OpenAIResponse struct {
	Choices []Choice `json:"choices"`
}

type Choice struct {
	Message      OpenAIMessage `json:"message"`
	FinishReason string        `json:"finish_reason"`
}

func tools() []OpenAITool {
	defs := shared.Tools()
	tools := make([]OpenAITool, 0, len(defs))
	for _, d := range defs {
		props := make(map[string]Property, len(d.Parameters.Properties))
		for k, p := range d.Parameters.Properties {
			props[k] = Property{Type: p.Type, Description: p.Description}
		}
		tools = append(tools, OpenAITool{
			Type: "function",
			Function: FunctionDefinition{
				Name:        d.Name,
				Description: d.Description,
				Parameters: &Parameters{
					Type:       d.Parameters.Type,
					Properties: props,
					Required:   d.Parameters.Required,
				},
			},
		})
	}
	return tools
}

func handleFunctionCall(fnCall *FunctionCall, defaultBranch string) string {
	var argsMap map[string]interface{}
	if fnCall.Arguments != "" {
		if err := json.Unmarshal([]byte(fnCall.Arguments), &argsMap); err != nil {
			return fmt.Sprintf(`{"error": "failed to parse arguments: %s"}`, err.Error())
		}
	}

	result := shared.ExecuteTool(fnCall.Name, argsMap, defaultBranch)

	b, err := json.Marshal(result)
	if err != nil {
		return fmt.Sprintf(`{"error": "failed to marshal result: %s"}`, err.Error())
	}
	return string(b)
}

func review(initialPrompt, endpoint, apiKey, model, defaultBranch string, maxTurns, maxRetries int) (string, error) {
	toolDefs := tools()

	messages := []OpenAIMessage{
		{
			Role:    "user",
			Content: initialPrompt,
		},
	}

	var latestContent string

	for turn := 0; turn < maxTurns; turn++ {
		req := OpenAIRequest{
			Model:    model,
			Messages: messages,
		}
		if turn < maxTurns-1 {
			req.Tools = toolDefs
		}

		b, err := json.Marshal(req)
		if err != nil {
			return "", err
		}

		headers := map[string]string{"Authorization": "Bearer " + apiKey}
		resp, err := shared.SendHTTPRequestWithRetry(endpoint, headers, b, maxRetries)
		if err != nil {
			return "", err
		}

		var openaiResp OpenAIResponse
		if err := json.Unmarshal(resp.Body(), &openaiResp); err != nil {
			return "", fmt.Errorf("failed to parse OpenAI response: %w", err)
		}

		if len(openaiResp.Choices) == 0 {
			return "", errors.New("no choices in response")
		}

		choice := openaiResp.Choices[0]
		msg := choice.Message

		if msg.Content != "" {
			latestContent = msg.Content
		}

		if len(msg.ToolCalls) == 0 {
			if latestContent != "" {
				return latestContent, nil
			}
			return "", errors.New("model returned neither content nor tool calls")
		}

		messages = append(messages, msg)

		for _, tc := range msg.ToolCalls {
			result := handleFunctionCall(&tc.Function, defaultBranch)
			messages = append(messages, OpenAIMessage{
				Role:       "tool",
				ToolCallID: tc.ID,
				Name:       tc.Function.Name,
				Content:    result,
			})
		}
	}

	if latestContent != "" {
		return latestContent, nil
	}

	return "", fmt.Errorf("agent reached max turns (%d) without completing review", maxTurns)
}

func main() {}
