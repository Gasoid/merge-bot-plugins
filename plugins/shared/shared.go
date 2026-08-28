package shared

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

const (
	MaxToolResultBytes    = 64 * 1024
	DefaultInitialBackoff = 2 * time.Second
	DefaultMaxTurns       = 20
	DefaultMaxRetries     = 5
	// Ceilings for the loop-driving vars. Anything higher is a
	// misconfiguration rather than an intent, and each turn is a paid API call.
	MaxAllowedTurns   = 50
	MaxAllowedRetries = 10
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
- "get_ci_job_log" — fetch the log of a specific CI job by its ID. Take the ID from the "Failed Jobs" list in the "# CI Pipeline Info" section below; if that section is absent or lists no jobs, no job IDs exist and you must not call this tool.
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
	CIInfo       *CIInfo           `json:"ci_info,omitempty"`
}

type CIInfo struct {
	PipelineStatus string    `json:"pipeline_status"`
	FailedJobs     []JobRef  `json:"failed_jobs,omitempty"`
	FailedTests    []TestRef `json:"failed_tests,omitempty"`
}

type JobRef struct {
	Name         string `json:"name"`
	Stage        string `json:"stage"`
	ID           int64  `json:"id"`
	AllowFailure bool   `json:"allow_failure"`
}

type TestRef struct {
	Name      string `json:"name"`
	Suite     string `json:"test_suite"`
	Output    string `json:"output,omitempty"`
	File      string `json:"file,omitempty"`
	ClassName string `json:"classname,omitempty"`
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

type GetCIJobLogResult struct {
	HostResult
	Job *CIJobLog `json:"job"`
}

type CIJobLog struct {
	Log   string `json:"log"`
	ID    int64  `json:"job_id"`
	Name  string `json:"job_name"`
	Stage string `json:"stage"`
}

func (j *CIJobLog) UnmarshalJSON(data []byte) error {
	type Alias CIJobLog
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

// maxSafeJSONInt is 2^53-1, the largest integer that survives a float64 round
// trip. json.Unmarshal into an interface{} decodes every number as a float64,
// so a value above this is already imprecise by the time it arrives and is
// rejected rather than silently returned as the wrong ID. The bound excludes
// 2^53 itself: that value is representable, but so is nothing between it and
// 2^53+2, so 9007199254740993 decodes to 9007199254740992 and accepting it
// would fetch a job the model never asked for.
const maxSafeJSONInt = 1<<53 - 1

// ExtractJobIDArg reads a job ID out of decoded tool arguments. Despite the
// "integer" declared in the tool schema, models routinely send the value as a
// JSON string and vary the key casing, and each mismatch costs an agent turn
// that ends with a review that never saw the log it asked for. Keys are tried
// in order, matching ExtractStringArg.
func ExtractJobIDArg(args map[string]interface{}, keys ...string) (int64, error) {
	var raw interface{}
	found := false
	for _, key := range keys {
		if v, ok := args[key]; ok && v != nil {
			raw, found = v, true
			break
		}
	}
	if !found {
		return 0, fmt.Errorf("%s argument is missing", keys[0])
	}

	var jobID int64
	switch v := raw.(type) {
	case float64:
		// Two distinct problems, and the model can only act on the right one:
		// a fractional value is a formatting mistake worth resending, an
		// out-of-range one means the ID itself is wrong.
		if v != math.Trunc(v) {
			return 0, fmt.Errorf("%s must be a whole number, got %v", keys[0], v)
		}
		if v < -maxSafeJSONInt || v > maxSafeJSONInt {
			return 0, fmt.Errorf("%s is out of range for a job ID, got %v", keys[0], v)
		}
		jobID = int64(v)
	case int:
		jobID = int64(v)
	case int64:
		jobID = v
	case json.Number:
		parsed, err := v.Int64()
		if err != nil {
			return 0, fmt.Errorf("%s must be an integer, got %q", keys[0], v.String())
		}
		jobID = parsed
	case string:
		parsed, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
		if err != nil {
			return 0, fmt.Errorf("%s must be an integer, got %q", keys[0], v)
		}
		jobID = parsed
	default:
		return 0, fmt.Errorf("%s must be an integer, got %T", keys[0], raw)
	}

	if jobID <= 0 {
		return 0, fmt.Errorf("%s must be positive, got %d", keys[0], jobID)
	}
	return jobID, nil
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
	if err := json.Unmarshal([]byte(trimmed), &output); err == nil {
		return output
	}

	// Models often wrap the object in a sentence or two ("Here is my review:
	// {...}"). Retry on the outermost braces before giving up, otherwise the
	// inline thread comments are dropped and the raw text is posted instead.
	if start, end := strings.Index(trimmed, "{"), strings.LastIndex(trimmed, "}"); start != -1 && end > start {
		candidate := PluginOutput{}
		if err := json.Unmarshal([]byte(trimmed[start:end+1]), &candidate); err == nil {
			return candidate
		}
	}

	return PluginOutput{Comment: result}
}

func CountLines(data string) int {
	if len(data) == 0 {
		return 0
	}
	return strings.Count(strings.TrimRight(data, "\n"), "\n") + 1
}

// ParseIntVarRange is ParseIntVar with an upper bound, for values that drive a
// loop. Without a ceiling a mistyped max_turns spends real money on API calls
// before anything notices. Out-of-range values fall back to def, matching
// ParseIntVar.
func ParseIntVarRange(vars map[string]string, key string, def, min, max int) int {
	n := ParseIntVar(vars, key, def, min)
	if n > max {
		return def
	}
	return n
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

	ciInfo := ""
	if input.CIInfo != nil {
		ciInfo = fmt.Sprintf("\n# CI Pipeline Info\nPipeline Status: %s\n", input.CIInfo.PipelineStatus)
		if len(input.CIInfo.FailedJobs) > 0 {
			ciInfo += "Failed Jobs:\n"
			for _, j := range input.CIInfo.FailedJobs {
				ciInfo += fmt.Sprintf("  - %s (ID: %d, Stage: %s, AllowFailure: %v)\n", j.Name, j.ID, j.Stage, j.AllowFailure)
			}
		}
		if len(input.CIInfo.FailedTests) > 0 {
			ciInfo += "Failed Tests:\n"
			for _, t := range input.CIInfo.FailedTests {
				ciInfo += fmt.Sprintf("  - %s::%s", t.Suite, t.Name)
				// GitLab JUnit reports often leave "file" empty, and then
				// classname is the only thing that locates the test.
				if loc := t.File; loc != "" {
					ciInfo += fmt.Sprintf(" (%s)", loc)
				} else if t.ClassName != "" {
					ciInfo += fmt.Sprintf(" (%s)", t.ClassName)
				}
				ciInfo += "\n"
				if t.Output != "" {
					ciInfo += fmt.Sprintf("    Output: %s\n", t.Output)
				}
			}
		}
	}

	fullPrompt = prompt + mr + description + ciInfo + "# Diff\n```\n" + string(input.Diffs) + "\n```\n"
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
			Name:        "get_ci_job_log",
			Description: "Fetch the log of a specific CI/CD job by its ID. Returns the job's name, stage, and recent log output (up to 200 lines). Use this to understand why a specific CI job failed. Valid job IDs are the ones listed under 'Failed Jobs' in the '# CI Pipeline Info' section of the prompt; do not call this tool if the prompt has no such section, and never guess an ID.",
			Parameters: ToolParameters{
				Type: "object",
				Properties: map[string]ToolProperty{
					"job_id": {
						Type:        "integer",
						Description: "The ID of the CI job to fetch the log for, copied from the 'Failed Jobs' list in the '# CI Pipeline Info' section of the prompt.",
					},
				},
				Required: []string{"job_id"},
			},
		},
	}
}
