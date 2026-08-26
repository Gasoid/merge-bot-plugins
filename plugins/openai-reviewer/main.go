package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	"github.com/extism/go-pdk"
	shared "github.com/gasoid/merge-bot-plugins/plugins/shared"
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
- "fetch_web_content" — fetch documentation or web resources (limited to approved domains and their subdomains: pkg.go.dev, docs.python.org, developer.mozilla.org, golang.org).
- "get_ci_failed_jobs" — fetch logs of failed CI jobs for this merge request.
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
		prompt = defaultPrompt
	}

	endpoint, ok := input.Vars["reviewer_endpoint"]
	if !ok {
		endpoint = defaultEndpoint
	}

	maxTurns := shared.DefaultMaxTurns
	if v, ok := input.Vars["reviewer_max_turns"]; ok {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			maxTurns = n
		}
	}

	maxRetries := shared.DefaultMaxRetries
	if v, ok := input.Vars["reviewer_max_retries"]; ok {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			maxRetries = n
		}
	}

	reasoningEffort, ok := input.Vars["reviewer_reasoning_effort"]
	if !ok {
		reasoningEffort = ""
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

	result, err := review(fullPrompt, endpoint, apiKey, model, defaultBranch, maxTurns, maxRetries, reasoningEffort)
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
	Model           string          `json:"model"`
	Messages        []OpenAIMessage `json:"messages"`
	Tools           []OpenAITool    `json:"tools,omitempty"`
	ReasoningEffort string          `json:"reasoning_effort,omitempty"`
}

type OpenAIResponse struct {
	Choices []Choice `json:"choices"`
}

type Choice struct {
	Message      OpenAIMessage `json:"message"`
	FinishReason string        `json:"finish_reason"`
}

func tools() []OpenAITool {
	return []OpenAITool{
		{
			Type: "function",
			Function: FunctionDefinition{
				Name:        "get_git_file",
				Description: "Fetch the content of a file from the repository. By default, it fetches from the merge request source branch, but you can also specify the target branch to inspect the base version.",
				Parameters: &Parameters{
					Type: "object",
					Properties: map[string]Property{
						"file_path": {
							Type:        "string",
							Description: "The relative path to the file in the repository (e.g. 'pkg/server/server.go').",
						},
						"branch": {
							Type:        "string",
							Description: "Optional branch name to fetch the file from (e.g. source branch or target branch). If not specified, defaults to the merge request source branch.",
						},
					},
					Required: []string{"file_path"},
				},
			},
		},
		{
			Type: "function",
			Function: FunctionDefinition{
				Name:        "search_code",
				Description: "Search for code patterns in the repository. Searches all files in the specified branch and returns matching file paths with line numbers (up to 100 results). The query supports filters: prefix a term with 'filename:', 'path:', or 'extension:' to narrow results (e.g. 'a query filename:some_name*'), and wildcards ('*') for glob matching. See https://docs.gitlab.com/api/search/#scope-blobs-2",
				Parameters: &Parameters{
					Type: "object",
					Properties: map[string]Property{
						"query": {
							Type:        "string",
							Description: "The search query to find code in the repository. Can be a function name, type name, variable name, or a code snippet. Supports filters like 'filename:', 'path:', 'extension:' and wildcards ('*'), e.g. 'handleRequest filename:*.go'.",
						},
						"branch": {
							Type:        "string",
							Description: "Optional branch name to search in (e.g. source branch or target branch). If not specified, defaults to the merge request source branch.",
						},
					},
					Required: []string{"query"},
				},
			},
		},
		{
			Type: "function",
			Function: FunctionDefinition{
				Name:        "fetch_web_content",
				Description: "Fetch content from a web URL and convert it to Markdown. Only approved domains and their subdomains are allowed (e.g. pkg.go.dev, docs.python.org, developer.mozilla.org, golang.org). Use this to look up library documentation or API references.",
				Parameters: &Parameters{
					Type: "object",
					Properties: map[string]Property{
						"url": {
							Type:        "string",
							Description: "The full URL to fetch content from (e.g. 'https://pkg.go.dev/net/http'). Must be from an approved domain or its subdomain.",
						},
					},
					Required: []string{"url"},
				},
			},
		},
		{
			Type: "function",
			Function: FunctionDefinition{
				Name:        "get_ci_failed_jobs",
				Description: "Fetch logs of failed CI/CD jobs for the current merge request. Returns each failed job's name, stage, ID, and recent log output (up to 200 lines). Use this to understand why CI pipelines are failing.",
				Parameters: &Parameters{
					Type:       "object",
					Properties: map[string]Property{},
				},
			},
		},
	}
}

func handleFunctionCall(fnCall *FunctionCall, defaultBranch string) string {
	var argsMap map[string]interface{}
	if fnCall.Arguments != "" {
		if err := json.Unmarshal([]byte(fnCall.Arguments), &argsMap); err != nil {
			return fmt.Sprintf(`{"error": "failed to parse arguments: %s"}`, err.Error())
		}
	}

	var result interface{}

	switch fnCall.Name {
	case "get_git_file":
		filePath := shared.ExtractStringArg(argsMap, "file_path", "filePath", "path")
		if filePath == "" {
			result = map[string]interface{}{"error": "file_path argument is missing"}
		} else {
			branch := shared.ExtractStringArg(argsMap, "branch", "ref")
			if branch == "" {
				branch = defaultBranch
			}
			fileData, err := shared.GetGitFile(branch, filePath)
			if err != nil {
				result = map[string]interface{}{"error": fmt.Sprintf("failed to get file %s on branch %s: %s", filePath, branch, err.Error())}
			} else {
				content := shared.Truncate(string(fileData), shared.MaxToolResultBytes)
				result = map[string]interface{}{"content": content}
			}
		}

	case "search_code":
		query := shared.ExtractStringArg(argsMap, "query", "q")
		if query == "" {
			result = map[string]interface{}{"error": "query argument is missing"}
		} else {
			branch := shared.ExtractStringArg(argsMap, "branch", "ref")
			if branch == "" {
				branch = defaultBranch
			}
			results, err := shared.SearchCode(branch, query)
			if err != nil {
				result = map[string]interface{}{"error": fmt.Sprintf("failed to search code: %s", err.Error())}
			} else {
				result = map[string]interface{}{"results": results}
			}
		}

	case "fetch_web_content":
		url := shared.ExtractStringArg(argsMap, "url", "link")
		if url == "" {
			result = map[string]interface{}{"error": "url argument is missing"}
		} else {
			content, err := shared.FetchWebContent(url)
			if err != nil {
				result = map[string]interface{}{"error": fmt.Sprintf("failed to fetch web content: %s", err.Error())}
			} else {
				result = map[string]interface{}{"content": content}
			}
		}

	case "get_ci_failed_jobs":
		jobs, err := shared.GetCIFailedJobs()
		if err != nil {
			result = map[string]interface{}{"error": fmt.Sprintf("failed to get CI failed jobs: %s", err.Error())}
		} else {
			result = map[string]interface{}{"jobs": jobs}
		}

	default:
		result = map[string]interface{}{"error": fmt.Sprintf("unknown tool: %s", fnCall.Name)}
	}

	b, err := json.Marshal(result)
	if err != nil {
		return fmt.Sprintf(`{"error": "failed to marshal result: %s"}`, err.Error())
	}
	return string(b)
}

func review(initialPrompt, endpoint, apiKey, model, defaultBranch string, maxTurns, maxRetries int, reasoningEffort string) (string, error) {
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
			if reasoningEffort != "" {
				req.ReasoningEffort = reasoningEffort
			} else {
				req.ReasoningEffort = "none"
			}
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
