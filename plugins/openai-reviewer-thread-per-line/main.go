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

Output format is json, markdown syntax is allowed only in comment and body fields.

## JSON template:
<code>
{
  "comment": "Brief summary of MR",
  "threads": [
     {
      "new_line": 123,
	  "old_line": 123,
	  "new_path": "app/file.py",
	  "old_path": "app/file.py",
	  "body": "problem description and suggestion to fix",
     },
  ]
}
</code>

## Rules:
- old_path file path before change, omit it if old_path doesn't exist or it is /dev/null
- new_path file path after change, omit it if new_path doesn't exist or it is /dev/null
- old_line the line number before change (optional), don't include it if line didn't exist.
- new_line the line number after change (optional), don't include it if line is deleted.
- Both old_path and new_path are required and must refer to the file path before and after the change.
- To create a thread on an added line, use new_line and don't include old_line.
- To create a thread on a removed line, use old_line and don't include new_line.
- To create a thread on an unchanged line, include both new_line and old_line for the line. These positions might not be the same if earlier changes in the file changed the line number. 

LINE NUMBER ACCURACY IS CRITICAL:

**How to determine line numbers from diff:**

1. Find the hunk header: @@ -old_start,old_count +new_start,new_count @@
   Example: @@ -10,5 +12,6 @@ means old starts at line 10, new starts at line 12

2. Count from the start:
   - Lines starting with -: exist in OLD version only → use old_line
   - Lines starting with +: exist in NEW version only → use new_line
   - Lines starting with space: exist in BOTH → use both old_line and new_line

3. **NEVER guess or calculate line numbers**
   - Use ONLY what you can directly count from the diff
   - If you cannot determine a line number with 100% certainty, OMIT that field
   - It's better to have no line number than a wrong one

4. **When to omit fields:**
   - Omit old_line if the line is ADDED (starts with +)
   - Omit new_line if the line is DELETED (starts with -)
   - If you're unsure about any line number, create a general file comment without line numbers

**Invalid line numbers will cause the review to fail. Double-check every number.**

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

type Thread struct {
	NewLine int    `json:"new_line"`
	OldLine int    `json:"old_line"`
	Body    string `json:"body"`
	NewPath string `json:"new_path"`
	OldPath string `json:"old_path"`
}

type PluginOutput struct {
	Comment string   `json:"comment"`
	Threads []Thread `json:"threads"`
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

	prompt = prompt + mr + description + "# Diff\n ```" + string(input.Diffs) + "\n```\n"

	result, err := review(prompt, endpoint, apiKey, model)
	if err != nil {
		pdk.SetError(fmt.Errorf("review func failed: %w", err))
		return 1
	}

	output := PluginOutput{}

	if err := json.Unmarshal(result, &output); err != nil {
		pdk.SetError(fmt.Errorf("unmarshal func failed: %w, output: %+v, result: %s", err, output, result))
		return 1
	}

	for i := range output.Threads {
		if output.Threads[i].NewLine > 0 {
			output.Threads[i].NewLine++
		}

		if output.Threads[i].OldLine > 0 {
			output.Threads[i].OldLine++
		}
	}

	pdk.OutputJSON(output)

	return 0
}

type Body struct {
	Model string `json:"model"`
	Input string `json:"input"`
}

func review(prompt, endpoint, apiKey, model string) ([]byte, error) {
	req := pdk.NewHTTPRequest(pdk.MethodPost, endpoint)
	req.SetHeader("Authorization", "Bearer "+apiKey)
	req.SetHeader("Content-Type", "application/json")

	body := Body{
		Model: model,
		Input: prompt,
	}

	b, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	req.SetBody(b)
	resp := req.Send()
	if resp.Status() != statusOk {
		return nil, fmt.Errorf("status is not ok: %d, %s", resp.Status(), resp.Body())
	}

	var p fastjson.Parser
	v, err := p.ParseBytes(resp.Body())
	if err != nil {
		return nil, err
	}

	for _, output := range v.GetArray("output") {
		keyType := output.GetStringBytes("type")
		if string(keyType) != "message" {
			continue
		}

		text := output.GetStringBytes("content", "0", "text")

		return text, nil
	}

	return nil, errors.New("no results")
}

func main() {}
