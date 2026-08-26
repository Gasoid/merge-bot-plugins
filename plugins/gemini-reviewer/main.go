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
	defaultModel    = "gemini-2.5-flash-lite"
	defaultEndpoint = "https://generativelanguage.googleapis.com/v1beta/models/"
)

//go:wasmexport review
func Review() int32 {
	input := shared.PluginInput{}
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
		prompt = shared.DefaultPrompt
	}

	endpoint, ok := input.Vars["gemini_reviewer_endpoint"]
	if !ok {
		endpoint = defaultEndpoint
	}

	maxTurns := shared.ParseIntVar(input.Vars, "gemini_reviewer_max_turns", shared.DefaultMaxTurns, 1)
	maxRetries := shared.ParseIntVar(input.Vars, "gemini_reviewer_max_retries", shared.DefaultMaxRetries, 0)

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

type GeminiRequest struct {
	Contents []Content `json:"contents"`
	Tools    []Tool    `json:"tools,omitempty"`
}

type Tool struct {
	FunctionDeclarations []FunctionDeclaration `json:"functionDeclarations,omitempty"`
}

type FunctionDeclaration struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Parameters  *Parameters `json:"parameters,omitempty"`
}

type Parameters struct {
	Type       string              `json:"type"`
	Properties map[string]Property `json:"properties,omitempty"`
	Required   []string            `json:"required,omitempty"`
}

type Property struct {
	Type        string `json:"type"`
	Description string `json:"description,omitempty"`
}

type Content struct {
	Role  string `json:"role"`
	Parts []Part `json:"parts"`
}

type Part struct {
	Text             string            `json:"text,omitempty"`
	Thought          bool              `json:"thought,omitempty"`
	ThoughtSignature string            `json:"thoughtSignature,omitempty"`
	FunctionCall     *FunctionCall     `json:"functionCall,omitempty"`
	FunctionResponse *FunctionResponse `json:"functionResponse,omitempty"`
}

func (p *Part) UnmarshalJSON(data []byte) error {
	type Alias Part
	aux := struct {
		*Alias
		ThoughtSignatureSnake string `json:"thought_signature,omitempty"`
	}{
		Alias: (*Alias)(p),
	}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	if p.ThoughtSignature == "" && aux.ThoughtSignatureSnake != "" {
		p.ThoughtSignature = aux.ThoughtSignatureSnake
	}
	return nil
}

type FunctionCall struct {
	Name string                 `json:"name"`
	Args map[string]interface{} `json:"args,omitempty"`
}

type FunctionResponse struct {
	Name     string                 `json:"name"`
	Response map[string]interface{} `json:"response"`
}

type GeminiResponse struct {
	Candidates []Candidate `json:"candidates"`
}

type Candidate struct {
	Content Content `json:"content"`
}

func tools() []Tool {
	defs := shared.Tools()
	tools := make([]Tool, 0, len(defs))
	for _, d := range defs {
		props := make(map[string]Property, len(d.Parameters.Properties))
		for k, p := range d.Parameters.Properties {
			props[k] = Property{Type: strings.ToUpper(p.Type), Description: p.Description}
		}
		tools = append(tools, Tool{
			FunctionDeclarations: []FunctionDeclaration{
				{
					Name:        d.Name,
					Description: d.Description,
					Parameters: &Parameters{
						Type:       strings.ToUpper(d.Parameters.Type),
						Properties: props,
						Required:   d.Parameters.Required,
					},
				},
			},
		})
	}
	return tools
}

func handleFunctionCall(fnCall *FunctionCall, defaultBranch string) *FunctionResponse {
	return &FunctionResponse{
		Name:     fnCall.Name,
		Response: shared.ExecuteTool(fnCall.Name, fnCall.Args, defaultBranch),
	}
}

func review(initialPrompt, endpoint, apiKey, model, defaultBranch string, maxTurns, maxRetries int) (string, error) {
	url := fmt.Sprintf("%s%s:generateContent?key=%s", endpoint, model, apiKey)
	tools := tools()

	contents := []Content{
		{
			Role: "user",
			Parts: []Part{
				{
					Text: initialPrompt,
				},
			},
		},
	}

	var latestText string

	for turn := 0; turn < maxTurns; turn++ {
		geminiReq := GeminiRequest{
			Contents: contents,
		}
		if turn < maxTurns-1 {
			geminiReq.Tools = tools
		}

		b, err := json.Marshal(geminiReq)
		if err != nil {
			return "", err
		}

		resp, err := shared.SendHTTPRequestWithRetry(url, nil, b, maxRetries)
		if err != nil {
			return "", err
		}

		var geminiResp GeminiResponse
		if err := json.Unmarshal(resp.Body(), &geminiResp); err != nil {
			return "", fmt.Errorf("failed to parse Gemini response: %w", err)
		}

		if len(geminiResp.Candidates) == 0 {
			return "", errors.New("no candidates in response")
		}

		candidate := geminiResp.Candidates[0]
		modelParts := candidate.Content.Parts
		if len(modelParts) == 0 {
			return "", errors.New("no parts in candidate content")
		}

		var functionCalls []*FunctionCall
		var textParts []string

		for _, p := range modelParts {
			if p.FunctionCall != nil {
				functionCalls = append(functionCalls, p.FunctionCall)
			}
			if p.Text != "" && !p.Thought {
				textParts = append(textParts, p.Text)
			}
		}

		if len(textParts) > 0 {
			latestText = strings.Join(textParts, "\n")
		}

		if len(functionCalls) == 0 {
			if latestText != "" {
				return latestText, nil
			}
			return "", errors.New("model returned neither text nor function call")
		}

		contents = append(contents, Content{
			Role:  "model",
			Parts: modelParts,
		})

		var functionResponseParts []Part
		for _, fnCall := range functionCalls {
			fnResp := handleFunctionCall(fnCall, defaultBranch)
			functionResponseParts = append(functionResponseParts, Part{
				FunctionResponse: fnResp,
			})
		}

		contents = append(contents, Content{
			Role:  "user",
			Parts: functionResponseParts,
		})
	}

	if latestText != "" {
		return latestText, nil
	}

	return "", fmt.Errorf("agent reached max turns (%d) without completing review", maxTurns)
}

func main() {}
