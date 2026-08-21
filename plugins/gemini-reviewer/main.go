package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/extism/go-pdk"
)

const (
	defaultPrompt = `
You are a reviewer of a Merge Request for GitLab. Analyze the provided code changes (diff) and offer specific suggestions for improvement.
Focus on identifying potential bugs, security vulnerabilities, and areas where the code deviates from best practices.
Your feedback should be clear, concise, and directly related to the code in the diff.
This is an automated review. You suggest what to fix/make better and user will fix issues in code.

You have access to a tool "get_git_file" to fetch the full content of any file from the repository if you need more context (e.g. surrounding code, type definitions, function implementations, or imported modules) to perform a thorough and accurate review.
`
	defaultModel    = "gemini-2.5-flash-lite"
	defaultEndpoint = "https://generativelanguage.googleapis.com/v1beta/models/"
	defaultMaxTurns = 5
)

type PluginInput struct {
	Title       string            `json:"title"`
	Description string            `json:"description"`
	Author      string            `json:"author"`
	ProjectID   int64             `json:"project_id"`
	Branch      string            `json:"branch"`
	ID          int64             `json:"mr_id"`
	Provider    string            `json:"provider"`
	Diffs       []byte            `json:"diffs"`
	Vars        map[string]string `json:"vars"`
}

type PluginOutput struct {
	Comment string `json:"comment"`
}

//go:wasmimport extism:host/user get_git_file
func host_get_git_file(providerPtr uint64, projectID int64, mrID int64, filePathPtr uint64) uint64

func getGitFile(provider string, projectID int64, mrID int64, filePath string) ([]byte, error) {
	memProvider := pdk.AllocateString(provider)
	defer memProvider.Free()
	memFilePath := pdk.AllocateString(filePath)
	defer memFilePath.Free()

	resOffset := host_get_git_file(memProvider.Offset(), projectID, mrID, memFilePath.Offset())
	if resOffset == 0 {
		return nil, errors.New("file not found or host error")
	}

	resMem := pdk.FindMemory(resOffset)
	if resMem.Length() == 0 {
		return []byte{}, nil
	}

	return resMem.ReadBytes(), nil
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

	provider := input.Provider
	if provider == "" {
		if v, ok := input.Vars["provider"]; ok && v != "" {
			provider = v
		} else {
			provider = "gitlab"
		}
	}

	maxTurns := defaultMaxTurns
	if v, ok := input.Vars["gemini_reviewer_max_turns"]; ok {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			maxTurns = n
		}
	}

	description := ""
	if input.Description != "" {
		description = fmt.Sprintf("Description: %s\n", input.Description)
	}

	branch := ""
	if input.Branch != "" {
		branch = fmt.Sprintf("Branch: %s\n", input.Branch)
	}

	mr := fmt.Sprintf("\nTitle: %s\nAuthor: %s\n%s", input.Title, input.Author, branch)

	fullPrompt := prompt + mr + description + "# Diff\n```\n" + string(input.Diffs) + "\n```\n"

	result, err := review(fullPrompt, endpoint, apiKey, model, provider, input.ProjectID, input.ID, maxTurns)
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
	Type       string               `json:"type"`
	Properties map[string]Property  `json:"properties,omitempty"`
	Required   []string             `json:"required,omitempty"`
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
	FunctionCall     *FunctionCall     `json:"functionCall,omitempty"`
	FunctionResponse *FunctionResponse `json:"functionResponse,omitempty"`
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

func getGitFileTool() Tool {
	return Tool{
		FunctionDeclarations: []FunctionDeclaration{
			{
				Name:        "get_git_file",
				Description: "Fetch the content of a file from the repository at the current merge request branch context. Use this whenever you need to see the full content of a file modified in the diff, imports, or referenced code for more context.",
				Parameters: &Parameters{
					Type: "OBJECT",
					Properties: map[string]Property{
						"file_path": {
							Type:        "STRING",
							Description: "The relative path to the file in the repository (e.g. 'pkg/server/server.go').",
						},
					},
					Required: []string{"file_path"},
				},
			},
		},
	}
}

func extractFilePath(args map[string]interface{}) string {
	if args == nil {
		return ""
	}
	if v, ok := args["file_path"].(string); ok && v != "" {
		return v
	}
	if v, ok := args["filePath"].(string); ok && v != "" {
		return v
	}
	if v, ok := args["path"].(string); ok && v != "" {
		return v
	}
	return ""
}

func handleFunctionCall(fnCall *FunctionCall, provider string, projectID, mrID int64) *FunctionResponse {
	if fnCall.Name == "get_git_file" {
		filePath := extractFilePath(fnCall.Args)
		if filePath == "" {
			return &FunctionResponse{
				Name: fnCall.Name,
				Response: map[string]interface{}{
					"error": "file_path argument is missing",
				},
			}
		}

		fileData, err := getGitFile(provider, projectID, mrID, filePath)
		if err != nil {
			return &FunctionResponse{
				Name: fnCall.Name,
				Response: map[string]interface{}{
					"error": fmt.Sprintf("failed to get file %s: %s", filePath, err.Error()),
				},
			}
		}

		return &FunctionResponse{
			Name: fnCall.Name,
			Response: map[string]interface{}{
				"content": string(fileData),
			},
		}
	}

	return &FunctionResponse{
		Name: fnCall.Name,
		Response: map[string]interface{}{
			"error": fmt.Sprintf("unknown tool: %s", fnCall.Name),
		},
	}
}

func review(initialPrompt, endpoint, apiKey, model, provider string, projectID, mrID int64, maxTurns int) (string, error) {
	url := fmt.Sprintf("%s%s:generateContent?key=%s", endpoint, model, apiKey)
	tools := []Tool{getGitFileTool()}

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
			Tools:    tools,
		}

		b, err := json.Marshal(geminiReq)
		if err != nil {
			return "", err
		}

		req := pdk.NewHTTPRequest(pdk.MethodPost, url)
		req.SetHeader("Content-Type", "application/json")
		req.SetBody(b)

		resp := req.Send()
		if resp.Status() < 200 || resp.Status() >= 300 {
			return "", fmt.Errorf("request failed with status %d: %s", resp.Status(), string(resp.Body()))
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
			if p.Text != "" {
				textParts = append(textParts, p.Text)
			}
		}

		if len(textParts) > 0 {
			latestText = strings.Join(textParts, "\n")
		}

		// If no function calls, the model has finished its review
		if len(functionCalls) == 0 {
			if latestText != "" {
				return latestText, nil
			}
			return "", errors.New("model returned neither text nor function call")
		}

		// Append the model's turn to conversation history
		contents = append(contents, Content{
			Role:  "model",
			Parts: modelParts,
		})

		// Execute function calls and collect function responses
		var functionResponseParts []Part
		for _, fnCall := range functionCalls {
			fnResp := handleFunctionCall(fnCall, provider, projectID, mrID)
			functionResponseParts = append(functionResponseParts, Part{
				FunctionResponse: fnResp,
			})
		}

		// Append the function responses turn to conversation history
		contents = append(contents, Content{
			Role:  "function",
			Parts: functionResponseParts,
		})
	}

	if latestText != "" {
		return latestText, nil
	}

	return "", fmt.Errorf("agent reached max turns (%d) without completing review", maxTurns)
}

func main() {}
