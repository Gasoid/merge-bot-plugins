package main

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/extism/go-pdk"
	"github.com/valyala/fastjson"
)

const (
	statusOk      = 200
	defaultPrompt = `
You are Reviewer of Merge Request (Gitlab). Analyze changes in code.

CRITICAL: This is an automated review. You suggest what to fix/make better and user will fix issues in code.

`
	defaultModel    = "gpt-5.1-codex-mini"
	defaultEndpoint = "https://api.openai.com/v1/responses"
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
		prompt = defaultPrompt
	}

	endpoint, ok := input.Vars["reviewer_endpoint"]
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

type Body struct {
	Model string `json:"model"`
	Input string `json:"input"`
}

func review(prompt, endpoint, apiKey, model string) (string, error) {
	req := pdk.NewHTTPRequest(pdk.MethodPost, endpoint)
	req.SetHeader("Authorization", "Bearer "+apiKey)
	req.SetHeader("Content-Type", "application/json")

	body := Body{
		Model: model,
		Input: prompt,
	}

	b, err := json.Marshal(body)
	if err != nil {
		return "", err
	}

	req.SetBody(b)
	resp := req.Send()
	if resp.Status() != statusOk {
		return "", fmt.Errorf("status is not ok: %d, %s", resp.Status(), resp.Body())
	}

	var p fastjson.Parser
	v, err := p.ParseBytes(resp.Body())
	if err != nil {
		return "", err
	}

	for _, output := range v.GetArray("output") {
		keyType := output.GetStringBytes("type")
		if string(keyType) != "message" {
			continue
		}

		text := output.GetStringBytes("content", "0", "text")

		return string(text), nil
	}

	return "", errors.New("no results")
}

func main() {}
