package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	"github.com/extism/go-pdk"
	"github.com/valyala/fastjson"
)

const (
	defaultPrompt = `
You are a reviewer of a Merge Request for GitLab. Analyze the provided code changes (diff) and offer specific suggestions for improvement.
Focus on identifying potential bugs, security vulnerabilities, and areas where the code deviates from best practices.
Your feedback should be clear, concise, and directly related to the code in the diff.
This is an automated review. You suggest what to fix/make better and user will fix issues in code.
`
	defaultModel            = "claude-3-5-sonnet-20240620"
	defaultEndpoint         = "https://api.anthropic.com/v1/messages"
	defaultMaxTokens        = 1024
	defaultAnthropicVersion = "2023-06-01"
)

type PluginInput struct {
	Title       string            `json:"title"`
	Description string            `json:"description"`
	Author      string            `json:"author"`
	Diffs       []byte            `json:"diffs"`
	Vars        map[string]string `json:"vars"`
}

type PluginOutput struct {
	Comment string `json:"comment"`
}

//go:wasmexport review
func Review() int32 {
	input := PluginInput{}
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
		prompt = defaultPrompt
	}

	endpoint, ok := input.Vars["claude_reviewer_endpoint"]
	if !ok {
		endpoint = defaultEndpoint
	}

	maxTokensStr, ok := input.Vars["claude_reviewer_max_tokens"]
	maxTokens := defaultMaxTokens
	if ok {
		if i, err := strconv.Atoi(maxTokensStr); err == nil {
			maxTokens = i
		}
	}

	anthropicVersion, ok := input.Vars["claude_reviewer_anthropic_version"]
	if !ok {
		anthropicVersion = defaultAnthropicVersion
	}

	description := ""
	if input.Description != "" {
		description = fmt.Sprintf("Description: %s\n", input.Description)
	}

	mr := fmt.Sprintf("\nTitle: %s\nAuthor: %s\n", input.Title, input.Author)

	fullPrompt := prompt + mr + description + "# Diff\n```" + string(input.Diffs) + "\n```\n"

	result, err := review(fullPrompt, endpoint, apiKey, model, maxTokens, anthropicVersion)
	if err != nil {
		pdk.SetError(err)
		return 1
	}

	output := PluginOutput{
		Comment: result,
	}

	pdk.OutputJSON(output)

	return 0
}

type ClaudeRequest struct {
	Model         string    `json:"model"`
	MaxTokens     int       `json:"max_tokens"`
	Messages      []Message `json:"messages"`
	System        string    `json:"system,omitempty"` // System prompt can be added here
	StopSequences []string  `json:"stop_sequences,omitempty"`
}

type Message struct {
	Role    string         `json:"role"`
	Content []ContentBlock `json:"content"`
}

type ContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type ClaudeResponse struct {
	ID           string         `json:"id"`
	Type         string         `json:"type"`
	Role         string         `json:"role"`
	Model        string         `json:"model"`
	Content      []ContentBlock `json:"content"`
	StopReason   string         `json:"stop_reason"`
	StopSequence string         `json:"stop_sequence"`
	Usage        struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

func review(prompt, endpoint, apiKey, model string, maxTokens int, anthropicVersion string) (string, error) {
	req := pdk.NewHTTPRequest(pdk.MethodPost, endpoint)
	req.SetHeader("x-api-key", apiKey)
	req.SetHeader("anthropic-version", anthropicVersion)
	req.SetHeader("Content-Type", "application/json")

	claudeReq := ClaudeRequest{
		Model:     model,
		MaxTokens: maxTokens,
		Messages: []Message{
			{
				Role: "user",
				Content: []ContentBlock{
					{
						Type: "text",
						Text: prompt,
					},
				},
			},
		},
	}

	b, err := json.Marshal(claudeReq)
	if err != nil {
		return "", err
	}

	req.SetBody(b)
	resp := req.Send()
	if resp.Status() < 200 || resp.Status() >= 300 {
		return "", fmt.Errorf("request failed with status %d: %s", resp.Status(), string(resp.Body()))
	}

	var p fastjson.Parser
	v, err := p.ParseBytes(resp.Body())
	if err != nil {
		return "", err
	}

	content := v.GetArray("content")
	if len(content) == 0 {
		return "", errors.New("no content in response")
	}

	text := content[0].GetStringBytes("text")

	return string(text), nil
}

func main() {}
