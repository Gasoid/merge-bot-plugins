//go:build wasip1

// This file holds everything in the package that touches github.com/extism/go-pdk,
// whose host functions have no bodies outside wasm. Keeping it behind the wasip1
// tag leaves the rest of the package buildable — and therefore testable — on the
// host, the same reasoning as the claude-reviewer/internal/wire package.
// Nothing in the untagged files may reference a symbol declared here.

package shared

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/extism/go-pdk"
)

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
	case "get_ci_job_log":
		resOffset = host_get_ci_job_log(mem.Offset())
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

func GetCIJobLog(jobID int64) (*CIJobLog, error) {
	var result GetCIJobLogResult
	err := CallHost("get_ci_job_log", map[string]int64{"job_id": jobID}, &result)
	if err != nil {
		return nil, err
	}
	if result.Error != "" {
		return nil, errors.New(result.Error)
	}
	return result.Job, nil
}

//go:wasmimport extism:host/user get_git_file
func host_get_git_file(argsPtr uint64) uint64

//go:wasmimport extism:host/user search_code
func host_search_code(argsPtr uint64) uint64

//go:wasmimport extism:host/user fetch_web_content
func host_fetch_web_content(argsPtr uint64) uint64

//go:wasmimport extism:host/user get_ci_job_log
func host_get_ci_job_log(argsPtr uint64) uint64

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

	case "get_ci_job_log":
		jobID, err := ExtractJobIDArg(args, "job_id", "jobId", "id")
		if err != nil {
			return map[string]interface{}{"error": err.Error()}
		}
		jobLog, err := GetCIJobLog(jobID)
		if err != nil {
			return map[string]interface{}{"error": fmt.Sprintf("failed to get CI job log: %s", err.Error())}
		}
		if jobLog == nil {
			return map[string]interface{}{"error": "job log is empty"}
		}
		jobLog.Log = Truncate(jobLog.Log, MaxToolResultBytes)
		return map[string]interface{}{"log": jobLog.Log, "job_id": jobLog.ID, "job_name": jobLog.Name, "stage": jobLog.Stage}

	default:
		return map[string]interface{}{"error": fmt.Sprintf("unknown tool: %s", name)}
	}
}
