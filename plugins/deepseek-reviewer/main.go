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
	defaultModel     = "deepseek-chat"
	defaultEndpoint  = "https://api.deepseek.com/chat/completions"
	defaultMaxTokens = 1024
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

	apiKey, ok := input.Vars["deepseek_reviewer_api_key"]
	if !ok {
		pdk.SetError(errors.New("DEEPSEEK_REVIEWER_API_KEY is not provided"))
		return 1
	}

	model, ok := input.Vars["deepseek_reviewer_model"]
	if !ok {
		model = defaultModel
	}

	prompt, ok := input.Vars["deepseek_reviewer_prompt"]
	if !ok {
		prompt = defaultPrompt
	}

	endpoint, ok := input.Vars["deepseek_reviewer_endpoint"]
	if !ok {
		endpoint = defaultEndpoint
	}

	maxTokensStr, ok := input.Vars["deepseek_reviewer_max_tokens"]
	maxTokens := defaultMaxTokens
	if ok {
		if i, err := strconv.Atoi(maxTokensStr); err == nil {
			maxTokens = i
		}
	}

	description := ""
	if input.Description != "" {
		description = fmt.Sprintf("Description: %s\n", input.Description)
	}

	mr := fmt.Sprintf("\nTitle: %s\nAuthor: %s\n", input.Title, input.Author)

	fullPrompt := prompt + mr + description + "# Diff\n```" + string(input.Diffs) + "\n```\n"

	result, err := review(fullPrompt, endpoint, apiKey, model, maxTokens)
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

type DeepseekRequest struct {
	Model     string    `json:"model"`
	MaxTokens int       `json:"max_tokens,omitempty"`
	Messages  []Message `json:"messages"`
}

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

func review(prompt, endpoint, apiKey, model string, maxTokens int) (string, error) {
	req := pdk.NewHTTPRequest(pdk.MethodPost, endpoint)
	req.SetHeader("Authorization", "Bearer "+apiKey)
	req.SetHeader("Content-Type", "application/json")

	deepseekReq := DeepseekRequest{
		Model:     model,
		MaxTokens: maxTokens,
		Messages: []Message{
			{
				Role:    "user",
				Content: prompt,
			},
		},
	}

	b, err := json.Marshal(deepseekReq)
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

	choices := v.GetArray("choices")
	if len(choices) == 0 {
		return "", errors.New("no choices in response")
	}

	content := choices[0].GetStringBytes("message", "content")
	return string(content), nil
}

func main() {}
