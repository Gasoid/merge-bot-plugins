package shared

import (
	"encoding/json"
	"errors"
	"fmt"
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
