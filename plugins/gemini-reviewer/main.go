package main

import (
	"encoding/json"
	"errors"
	"fmt"

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
	defaultModel    = "gemini-pro"
	defaultEndpoint = "https://generativelanguage.googleapis.com/v1beta/models/"
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

	apiKey, ok := input.Vars["gemini_reviewer_api_key"]
	if !ok {
		pdk.SetError(errors.New("GEMINI_REVIEWER_API_KEY is not provided"))
		return 1
	}

	model, ok := input.Vars["gemini_reviewer_model"]
	if !ok {
		model = defaultModel
	}

	prompt, ok := input.Vars["gemini_reviewer_prompt"]
	if !ok {
		prompt = defaultPrompt
	}

	endpoint, ok := input.Vars["gemini_reviewer_endpoint"]
	if !ok {
		endpoint = defaultEndpoint
	}

	description := ""
	if input.Description != "" {
		description = fmt.Sprintf("Description: %s\n", input.Description)
	}

	mr := fmt.Sprintf("\nTitle: %s\nAuthor: %s\n", input.Title, input.Author)

	prompt = prompt + mr + description + "# Diff\n```" + string(input.Diffs) + "\n```\n"

	result, err := review(prompt, endpoint, apiKey, model)
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

type GeminiRequest struct {
	Contents []Content `json:"contents"`
}

type Content struct {
	Parts []Part `json:"parts"`
}

type Part struct {
	Text string `json:"text"`
}

type GeminiResponse struct {
	Candidates []Candidate `json:"candidates"`
}

type Candidate struct {
	Content Content `json:"content"`
}

func review(prompt, endpoint, apiKey, model string) (string, error) {
	url := fmt.Sprintf("%s%s:generateContent?key=%s", endpoint, model, apiKey)
	req := pdk.NewHTTPRequest(pdk.MethodPost, url)
	req.SetHeader("Content-Type", "application/json")

	geminiReq := GeminiRequest{
		Contents: []Content{
			{
				Parts: []Part{
					{
						Text: prompt,
					},
				},
			},
		},
	}

	b, err := json.Marshal(geminiReq)
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

	candidates := v.GetArray("candidates")
	if len(candidates) == 0 {
		return "", errors.New("no candidates in response")
	}

	parts := candidates[0].GetArray("content", "parts")
	if len(parts) == 0 {
		return "", errors.New("no parts in candidate")
	}

	text := parts[0].GetStringBytes("text")

	return string(text), nil
}

func main() {}
