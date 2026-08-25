package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/extism/go-pdk"
)

const (
	defaultPrompt = `
You are a reviewer of a Merge Request for GitLab. Analyze the provided code changes (diff) and offer specific suggestions for improvement.
Focus on identifying potential bugs, security vulnerabilities, and areas where the code deviates from best practices.
Your feedback should be clear, concise, and directly related to the code in the diff.
This is an automated review. You suggest what to fix/make better and user will fix issues in code.

You have access to tools to gather additional context for a thorough review:
- "get_git_file" — fetch the full content of any file from the repository.
- "search_code" — search for code patterns across the repository, results are limited to 100.
- "fetch_web_content" — fetch documentation or web resources (limited to approved domains like pkg.go.dev, docs.python.org, developer.mozilla.org, golang.org).
Use these tools only when necessary, and provide your complete review as soon as you have gathered sufficient information.

## Output format

Return ONLY a valid JSON object (no markdown fences, no extra text) in the following shape:

{
  "comment": "Brief summary of the merge request",
  "threads": [
    {
      "new_line": 123,
      "old_line": 123,
      "new_path": "app/file.py",
      "old_path": "app/file.py",
      "body": "problem description and suggestion to fix"
    }
  ]
}

Rules for threads (inline comments per line):
- old_path is the file path before the change; omit it if it does not exist or is /dev/null.
- new_path is the file path after the change; omit it if it does not exist or is /dev/null.
- old_line is the line number before the change (optional); omit it if the line did not exist.
- new_line is the line number after the change (optional); omit it if the line is deleted.
- To comment on an added line, use new_line and omit old_line.
- To comment on a removed line, use old_line and omit new_line.
- To comment on an unchanged line, include both new_line and old_line; they may differ if earlier changes shifted line numbers.

LINE NUMBER ACCURACY IS CRITICAL:
1. Find the hunk header: @@ -old_start,old_count +new_start,new_count @@ (e.g. @@ -10,5 +12,6 @@ means old starts at 10, new starts at 12).
2. Count from the start: lines starting with '-' exist only in the OLD version (use old_line); lines starting with '+' exist only in the NEW version (use new_line); lines starting with ' ' exist in both (use both).
3. NEVER guess or calculate line numbers. Use only what you can count directly from the diff. If you cannot determine a line number with 100% certainty, OMIT that field.
4. Invalid line numbers cause the review to fail. Double-check every number.

If you have no inline (thread) comments, return {"comment": "..."} with an empty or omitted threads array.
`
	defaultModel          = "gemini-2.5-flash-lite"
	defaultEndpoint       = "https://generativelanguage.googleapis.com/v1beta/models/"
	defaultMaxTurns       = 20
	defaultMaxRetries     = 5
	defaultInitialBackoff = 2 * time.Second
	maxToolResultBytes    = 64 * 1024
)

type PluginInput struct {
	Title        string            `json:"title"`
	Description  string            `json:"description"`
	Author       string            `json:"author"`
	Branch       string            `json:"branch"`
	TargetBranch string            `json:"target_branch"`
	Diffs        []byte            `json:"diffs"`
	Vars         map[string]string `json:"vars"`
}

type PluginOutput struct {
	Comment string   `json:"comment"`
	Threads []Thread `json:"threads,omitempty"`
}

type Thread struct {
	NewLine int64  `json:"new_line,omitempty"`
	OldLine int64  `json:"old_line,omitempty"`
	Body    string `json:"body,omitempty"`
	NewPath string `json:"new_path,omitempty"`
	OldPath string `json:"old_path,omitempty"`
}

type hostResult struct {
	Error string `json:"error"`
}

type getGitFileResult struct {
	hostResult
	Data []byte `json:"data"`
}

type searchCodeResult struct {
	hostResult
	Results []searchResult `json:"results"`
}

type searchResult struct {
	Path string `json:"path"`
	Line int64  `json:"line"`
}

type fetchWebContentResult struct {
	hostResult
	Content string `json:"content"`
}

func callHost(name string, params interface{}, result interface{}) error {
	paramsBytes, err := json.Marshal(params)
	if err != nil {
		return fmt.Errorf("failed to marshal params for %s: %w", name, err)
	}

	mem := pdk.AllocateBytes(paramsBytes)
	defer mem.Free()

	var resOffset uint64
	switch name {
	case "get_git_file":
		resOffset = host_get_git_file(mem.Offset())
	case "search_code":
		resOffset = host_search_code(mem.Offset())
	case "fetch_web_content":
		resOffset = host_fetch_web_content(mem.Offset())
	default:
		return fmt.Errorf("unknown host function: %s", name)
	}

	if resOffset == 0 {
		return errors.New("host function returned null")
	}

	resMem := pdk.FindMemory(resOffset)
	if resMem.Length() == 0 {
		return fmt.Errorf("host function %s returned empty result", name)
	}

	if err := json.Unmarshal(resMem.ReadBytes(), result); err != nil {
		return fmt.Errorf("failed to unmarshal result from %s: %w", name, err)
	}

	return nil
}

func getGitFile(branch, filePath string) ([]byte, error) {
	var result getGitFileResult
	err := callHost("get_git_file", map[string]string{
		"branch":    branch,
		"file_path": filePath,
	}, &result)
	if err != nil {
		return nil, err
	}
	if result.Error != "" {
		return nil, errors.New(result.Error)
	}
	return result.Data, nil
}

func searchCode(branch, query string) ([]searchResult, error) {
	var result searchCodeResult
	err := callHost("search_code", map[string]string{
		"branch": branch,
		"query":  query,
	}, &result)
	if err != nil {
		return nil, err
	}
	if result.Error != "" {
		return nil, errors.New(result.Error)
	}
	return result.Results, nil
}

func fetchWebContent(url string) (string, error) {
	var result fetchWebContentResult
	err := callHost("fetch_web_content", map[string]string{
		"url": url,
	}, &result)
	if err != nil {
		return "", err
	}
	if result.Error != "" {
		return "", errors.New(result.Error)
	}
	return result.Content, nil
}

//go:wasmimport extism:host/user get_git_file
func host_get_git_file(argsPtr uint64) uint64

//go:wasmimport extism:host/user search_code
func host_search_code(argsPtr uint64) uint64

//go:wasmimport extism:host/user fetch_web_content
func host_fetch_web_content(argsPtr uint64) uint64

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

	maxTurns := defaultMaxTurns
	if v, ok := input.Vars["gemini_reviewer_max_turns"]; ok {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			maxTurns = n
		}
	}

	maxRetries := defaultMaxRetries
	if v, ok := input.Vars["gemini_reviewer_max_retries"]; ok {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			maxRetries = n
		}
	}

	description := ""
	if input.Description != "" {
		description = fmt.Sprintf("Description: %s\n", input.Description)
	}

	branchInfo := ""
	if input.Branch != "" {
		branchInfo += fmt.Sprintf("Source Branch: %s\n", input.Branch)
	}
	if input.TargetBranch != "" {
		branchInfo += fmt.Sprintf("Target Branch: %s\n", input.TargetBranch)
	}

	defaultBranch := input.Branch
	if defaultBranch == "" {
		defaultBranch = input.TargetBranch
	}
	if defaultBranch == "" {
		pdk.SetError(errors.New("branch is not provided"))
		return 1
	}

	mr := fmt.Sprintf("\nTitle: %s\nAuthor: %s\n%s", input.Title, input.Author, branchInfo)

	fullPrompt := prompt + mr + description + "# Diff\n```\n" + string(input.Diffs) + "\n```\n"

	result, err := review(fullPrompt, endpoint, apiKey, model, defaultBranch, maxTurns, maxRetries)
	if err != nil {
		pdk.SetError(err)
		return 1
	}

	pdk.OutputJSON(parseOutput(result))

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
	return []Tool{
		{
			FunctionDeclarations: []FunctionDeclaration{
				{
					Name:        "get_git_file",
					Description: "Fetch the content of a file from the repository. By default, it fetches from the merge request source branch, but you can also specify the target branch to inspect the base version.",
					Parameters: &Parameters{
						Type: "OBJECT",
						Properties: map[string]Property{
							"file_path": {
								Type:        "STRING",
								Description: "The relative path to the file in the repository (e.g. 'pkg/server/server.go').",
							},
							"branch": {
								Type:        "STRING",
								Description: "Optional branch name to fetch the file from (e.g. source branch or target branch). If not specified, defaults to the merge request source branch.",
							},
						},
						Required: []string{"file_path"},
					},
				},
			},
		},
		{
			FunctionDeclarations: []FunctionDeclaration{
				{
					Name:        "search_code",
					Description: "Search for code patterns in the repository. Searches all files in the specified branch and returns matching file paths with line numbers (up to 100 results). The query supports filters: prefix a term with 'filename:', 'path:', or 'extension:' to narrow results (e.g. 'a query filename:some_name*'), and wildcards ('*') for glob matching. See https://docs.gitlab.com/api/search/#scope-blobs-2",
					Parameters: &Parameters{
						Type: "OBJECT",
						Properties: map[string]Property{
							"query": {
								Type:        "STRING",
								Description: "The search query to find code in the repository. Can be a function name, type name, variable name, or a code snippet. Supports filters like 'filename:', 'path:', 'extension:' and wildcards ('*'), e.g. 'handleRequest filename:*.go'.",
							},
							"branch": {
								Type:        "STRING",
								Description: "Optional branch name to search in (e.g. source branch or target branch). If not specified, defaults to the merge request source branch.",
							},
						},
						Required: []string{"query"},
					},
				},
			},
		},
		{
			FunctionDeclarations: []FunctionDeclaration{
				{
					Name:        "fetch_web_content",
					Description: "Fetch content from a web URL and convert it to Markdown. Only approved domains are allowed: pkg.go.dev, docs.python.org, developer.mozilla.org, golang.org. Use this to look up library documentation or API references.",
					Parameters: &Parameters{
						Type: "OBJECT",
						Properties: map[string]Property{
							"url": {
								Type:        "STRING",
								Description: "The full URL to fetch content from (e.g. 'https://pkg.go.dev/net/http'). Must be from an approved domain.",
							},
						},
						Required: []string{"url"},
					},
				},
			},
		},
	}
}

func extractStringArg(args map[string]interface{}, keys ...string) string {
	if args == nil {
		return ""
	}
	for _, key := range keys {
		if v, ok := args[key].(string); ok && v != "" {
			return v
		}
	}
	return ""
}

func truncate(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	return s[:maxBytes] + "\n... [truncated]"
}

func parseOutput(result string) PluginOutput {
	trimmed := strings.TrimSpace(result)
	trimmed = strings.TrimPrefix(trimmed, "```json")
	trimmed = strings.TrimPrefix(trimmed, "```")
	trimmed = strings.TrimSuffix(trimmed, "```")
	trimmed = strings.TrimSpace(trimmed)

	output := PluginOutput{}
	if err := json.Unmarshal([]byte(trimmed), &output); err != nil {
		return PluginOutput{Comment: result}
	}

	return output
}

func handleFunctionCall(fnCall *FunctionCall, defaultBranch string) *FunctionResponse {
	switch fnCall.Name {
	case "get_git_file":
		filePath := extractStringArg(fnCall.Args, "file_path", "filePath", "path")
		if filePath == "" {
			return &FunctionResponse{
				Name:     fnCall.Name,
				Response: map[string]interface{}{"error": "file_path argument is missing"},
			}
		}
		branch := extractStringArg(fnCall.Args, "branch", "ref")
		if branch == "" {
			branch = defaultBranch
		}
		fileData, err := getGitFile(branch, filePath)
		if err != nil {
			return &FunctionResponse{
				Name:     fnCall.Name,
				Response: map[string]interface{}{"error": fmt.Sprintf("failed to get file %s on branch %s: %s", filePath, branch, err.Error())},
			}
		}
		content := truncate(string(fileData), maxToolResultBytes)
		return &FunctionResponse{
			Name:     fnCall.Name,
			Response: map[string]interface{}{"content": content},
		}

	case "search_code":
		query := extractStringArg(fnCall.Args, "query", "q")
		if query == "" {
			return &FunctionResponse{
				Name:     fnCall.Name,
				Response: map[string]interface{}{"error": "query argument is missing"},
			}
		}
		branch := extractStringArg(fnCall.Args, "branch", "ref")
		if branch == "" {
			branch = defaultBranch
		}
		results, err := searchCode(branch, query)
		if err != nil {
			return &FunctionResponse{
				Name:     fnCall.Name,
				Response: map[string]interface{}{"error": fmt.Sprintf("failed to search code: %s", err.Error())},
			}
		}
		return &FunctionResponse{
			Name:     fnCall.Name,
			Response: map[string]interface{}{"results": results},
		}

	case "fetch_web_content":
		url := extractStringArg(fnCall.Args, "url", "link")
		if url == "" {
			return &FunctionResponse{
				Name:     fnCall.Name,
				Response: map[string]interface{}{"error": "url argument is missing"},
			}
		}
		content, err := fetchWebContent(url)
		if err != nil {
			return &FunctionResponse{
				Name:     fnCall.Name,
				Response: map[string]interface{}{"error": fmt.Sprintf("failed to fetch web content: %s", err.Error())},
			}
		}
		return &FunctionResponse{
			Name:     fnCall.Name,
			Response: map[string]interface{}{"content": content},
		}

	default:
		return &FunctionResponse{
			Name:     fnCall.Name,
			Response: map[string]interface{}{"error": fmt.Sprintf("unknown tool: %s", fnCall.Name)},
		}
	}
}

func sendHTTPRequestWithRetry(url string, body []byte, maxRetries int) (pdk.HTTPResponse, error) {
	var resp pdk.HTTPResponse
	backoff := defaultInitialBackoff

	for attempt := 0; attempt <= maxRetries; attempt++ {
		req := pdk.NewHTTPRequest(pdk.MethodPost, url)
		req.SetHeader("Content-Type", "application/json")
		req.SetBody(body)

		resp = req.Send()
		status := resp.Status()

		if status >= 200 && status < 300 {
			return resp, nil
		}

		isRetriable := status == 429 || status == 500 || status == 502 || status == 503 || status == 504
		if isRetriable && attempt < maxRetries {
			time.Sleep(backoff)
			backoff *= 2
			continue
		}

		return resp, fmt.Errorf("request failed with status %d: %s", status, string(resp.Body()))
	}

	return resp, fmt.Errorf("request failed after %d retries: status %d", maxRetries, resp.Status())
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

		resp, err := sendHTTPRequestWithRetry(url, b, maxRetries)
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
