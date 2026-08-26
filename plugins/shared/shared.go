package shared

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
	MaxToolResultBytes    = 64 * 1024
	DefaultInitialBackoff = 2 * time.Second
	DefaultMaxTurns       = 20
	DefaultMaxRetries     = 5
)

const DefaultPrompt = `
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

type HostResult struct {
	Error string `json:"error"`
}

type GetGitFileResult struct {
	HostResult
	Data []byte `json:"data"`
}

type SearchCodeResult struct {
	HostResult
	Results []SearchResult `json:"results"`
}

type SearchResult struct {
	Path string `json:"path"`
	Line int64  `json:"line"`
}

type FetchWebContentResult struct {
	HostResult
	Content []byte `json:"content"`
}

type CIFailedJobsResult struct {
	HostResult
	Jobs []CIFailedJob `json:"jobs"`
}

type CIFailedJob struct {
	Log   string `json:"log"`
	ID    int64  `json:"job_id"`
	Name  string `json:"job_name"`
	Stage string `json:"stage"`
}

func (j *CIFailedJob) UnmarshalJSON(data []byte) error {
	type Alias CIFailedJob
	aux := &struct {
		Log []byte `json:"log"`
		*Alias
	}{
		Alias: (*Alias)(j),
	}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	j.Log = string(aux.Log)
	return nil
}

func CallHost(name string, params interface{}, result interface{}) error {
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
	case "get_ci_failed_jobs":
		resOffset = host_get_ci_failed_jobs(mem.Offset())
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

func GetGitFile(branch, filePath string) ([]byte, error) {
	var result GetGitFileResult
	err := CallHost("get_git_file", map[string]string{
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

func SearchCode(branch, query string) ([]SearchResult, error) {
	var result SearchCodeResult
	err := CallHost("search_code", map[string]string{
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

func FetchWebContent(url string) (string, error) {
	var result FetchWebContentResult
	err := CallHost("fetch_web_content", map[string]string{
		"url": url,
	}, &result)
	if err != nil {
		return "", err
	}
	if result.Error != "" {
		return "", errors.New(result.Error)
	}
	return string(result.Content), nil
}

func GetCIFailedJobs() ([]CIFailedJob, error) {
	var result CIFailedJobsResult
	err := CallHost("get_ci_failed_jobs", map[string]string{}, &result)
	if err != nil {
		return nil, err
	}
	if result.Error != "" {
		return nil, errors.New(result.Error)
	}
	return result.Jobs, nil
}

//go:wasmimport extism:host/user get_git_file
func host_get_git_file(argsPtr uint64) uint64

//go:wasmimport extism:host/user search_code
func host_search_code(argsPtr uint64) uint64

//go:wasmimport extism:host/user fetch_web_content
func host_fetch_web_content(argsPtr uint64) uint64

//go:wasmimport extism:host/user get_ci_failed_jobs
func host_get_ci_failed_jobs(argsPtr uint64) uint64

func ExtractStringArg(args map[string]interface{}, keys ...string) string {
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

func Truncate(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	for maxBytes > 0 && maxBytes < len(s) && s[maxBytes]&0xC0 == 0x80 {
		maxBytes--
	}
	return s[:maxBytes] + "\n... [truncated]"
}

func ParseOutput(result string) PluginOutput {
	trimmed := strings.TrimSpace(result)
	if strings.HasPrefix(trimmed, "```") {
		trimmed = strings.TrimPrefix(trimmed, "```json")
		trimmed = strings.TrimPrefix(trimmed, "```")
		if idx := strings.LastIndex(trimmed, "```"); idx != -1 {
			trimmed = trimmed[:idx]
		}
		trimmed = strings.TrimSpace(trimmed)
	}

	output := PluginOutput{}
	if err := json.Unmarshal([]byte(trimmed), &output); err != nil {
		return PluginOutput{Comment: result}
	}

	return output
}

func CountLines(data string) int {
	if len(data) == 0 {
		return 0
	}
	return strings.Count(strings.TrimRight(data, "\n"), "\n") + 1
}

func ValidateThreads(threads []Thread, sourceBranch, targetBranch string) []Thread {
	type fileKey struct {
		branch, path string
	}
	fileLines := map[fileKey]int{}
	valid := make([]Thread, 0, len(threads))
	for _, t := range threads {
		if t.NewLine > 0 && t.NewPath == "" {
			continue
		}
		if t.OldLine > 0 && t.OldPath == "" {
			continue
		}
		if t.NewLine > 0 && t.NewPath != "" {
			key := fileKey{sourceBranch, t.NewPath}
			lineCount, ok := fileLines[key]
			if !ok {
				data, err := GetGitFile(sourceBranch, t.NewPath)
				if err != nil {
					fileLines[key] = -1
				} else {
					fileLines[key] = CountLines(string(data))
					lineCount = fileLines[key]
				}
			}
			if lineCount < 0 || int(t.NewLine) > lineCount {
				continue
			}
		}
		if t.OldLine > 0 && t.OldPath != "" {
			branch := targetBranch
			if branch == "" {
				branch = sourceBranch
			}
			key := fileKey{branch, t.OldPath}
			lineCount, ok := fileLines[key]
			if !ok {
				data, err := GetGitFile(branch, t.OldPath)
				if err != nil {
					fileLines[key] = -1
				} else {
					fileLines[key] = CountLines(string(data))
					lineCount = fileLines[key]
				}
			}
			if lineCount < 0 || int(t.OldLine) > lineCount {
				continue
			}
		}
		valid = append(valid, t)
	}
	return valid
}

func SendHTTPRequestWithRetry(url string, headers map[string]string, body []byte, maxRetries int) (pdk.HTTPResponse, error) {
	var resp pdk.HTTPResponse
	backoff := DefaultInitialBackoff

	for attempt := 0; attempt <= maxRetries; attempt++ {
		req := pdk.NewHTTPRequest(pdk.MethodPost, url)
		req.SetHeader("Content-Type", "application/json")
		for k, v := range headers {
			req.SetHeader(k, v)
		}
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

	return resp, errors.New("request failed: max retries exceeded")
}

func ParseIntVar(vars map[string]string, key string, def, min int) int {
	v, ok := vars[key]
	if !ok {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < min {
		return def
	}
	return n
}

func BuildPrompt(input PluginInput, prompt string) (fullPrompt, defaultBranch string, err error) {
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

	defaultBranch = input.Branch
	if defaultBranch == "" {
		defaultBranch = input.TargetBranch
	}
	if defaultBranch == "" {
		return "", "", errors.New("branch is not provided")
	}

	mr := fmt.Sprintf("\nTitle: %s\nAuthor: %s\n%s", input.Title, input.Author, branchInfo)

	fullPrompt = prompt + mr + description + "# Diff\n```\n" + string(input.Diffs) + "\n```\n"
	return fullPrompt, defaultBranch, nil
}

type ToolDef struct {
	Name        string
	Description string
	Parameters  ToolParameters
}

type ToolParameters struct {
	Type       string
	Properties map[string]ToolProperty
	Required   []string
}

type ToolProperty struct {
	Type        string
	Description string
}

func Tools() []ToolDef {
	return []ToolDef{
		{
			Name:        "get_git_file",
			Description: "Fetch the content of a file from the repository. By default, it fetches from the merge request source branch, but you can also specify the target branch to inspect the base version.",
			Parameters: ToolParameters{
				Type: "object",
				Properties: map[string]ToolProperty{
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
		{
			Name:        "search_code",
			Description: "Search for code patterns in the repository. Searches all files in the specified branch and returns matching file paths with line numbers (up to 100 results). The query supports filters: prefix a term with 'filename:', 'path:', or 'extension:' to narrow results (e.g. 'a query filename:some_name*'), and wildcards ('*') for glob matching. See https://docs.gitlab.com/api/search/#scope-blobs-2",
			Parameters: ToolParameters{
				Type: "object",
				Properties: map[string]ToolProperty{
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
		{
			Name:        "fetch_web_content",
			Description: "Fetch content from a web URL and convert it to Markdown. Only approved domains and their subdomains are allowed (e.g. pkg.go.dev, docs.python.org, developer.mozilla.org, golang.org). Use this to look up library documentation or API references.",
			Parameters: ToolParameters{
				Type: "object",
				Properties: map[string]ToolProperty{
					"url": {
						Type:        "string",
						Description: "The full URL to fetch content from (e.g. 'https://pkg.go.dev/net/http'). Must be from an approved domain or its subdomain.",
					},
				},
				Required: []string{"url"},
			},
		},
		{
			Name:        "get_ci_failed_jobs",
			Description: "Fetch logs of failed CI/CD jobs for the current merge request. Returns each failed job's name, stage, ID, and recent log output (up to 200 lines). Use this to understand why CI pipelines are failing.",
			Parameters: ToolParameters{
				Type:       "object",
				Properties: map[string]ToolProperty{},
			},
		},
	}
}

func ExecuteTool(name string, args map[string]interface{}, defaultBranch string) map[string]interface{} {
	switch name {
	case "get_git_file":
		filePath := ExtractStringArg(args, "file_path", "filePath", "path")
		if filePath == "" {
			return map[string]interface{}{"error": "file_path argument is missing"}
		}
		branch := ExtractStringArg(args, "branch", "ref")
		if branch == "" {
			branch = defaultBranch
		}
		fileData, err := GetGitFile(branch, filePath)
		if err != nil {
			return map[string]interface{}{"error": fmt.Sprintf("failed to get file %s on branch %s: %s", filePath, branch, err.Error())}
		}
		content := Truncate(string(fileData), MaxToolResultBytes)
		return map[string]interface{}{"content": content}

	case "search_code":
		query := ExtractStringArg(args, "query", "q")
		if query == "" {
			return map[string]interface{}{"error": "query argument is missing"}
		}
		branch := ExtractStringArg(args, "branch", "ref")
		if branch == "" {
			branch = defaultBranch
		}
		results, err := SearchCode(branch, query)
		if err != nil {
			return map[string]interface{}{"error": fmt.Sprintf("failed to search code: %s", err.Error())}
		}
		return map[string]interface{}{"results": results}

	case "fetch_web_content":
		url := ExtractStringArg(args, "url", "link")
		if url == "" {
			return map[string]interface{}{"error": "url argument is missing"}
		}
		content, err := FetchWebContent(url)
		if err != nil {
			return map[string]interface{}{"error": fmt.Sprintf("failed to fetch web content: %s", err.Error())}
		}
		return map[string]interface{}{"content": content}

	case "get_ci_failed_jobs":
		jobs, err := GetCIFailedJobs()
		if err != nil {
			return map[string]interface{}{"error": fmt.Sprintf("failed to get CI failed jobs: %s", err.Error())}
		}
		return map[string]interface{}{"jobs": jobs}

	default:
		return map[string]interface{}{"error": fmt.Sprintf("unknown tool: %s", name)}
	}
}
