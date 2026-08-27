package shared

import (
	"encoding/json"
	"strings"
	"testing"
)

// The tool schema declares job_id as an integer, but the value reaches
// ExecuteTool through json.Unmarshal into a map[string]interface{} (see each
// plugin's tool-call handling), so in production it is always a float64 — or a
// string, which models emit regardless of the schema. int64 and json.Number
// cannot occur today and are accepted only as cheap tolerance.
func TestExtractJobIDArgAccepts(t *testing.T) {
	for name, args := range map[string]map[string]interface{}{
		"float64 (the real decoded shape)": {"job_id": float64(42)},
		"string":                           {"job_id": "42"},
		"string with surrounding space":    {"job_id": " 42 "},
		"int":                              {"job_id": 42},
		"int64":                            {"job_id": int64(42)},
		"json.Number":                      {"job_id": json.Number("42")},
		"jobId alias":                      {"jobId": float64(42)},
		"id alias":                         {"id": "42"},
		"first key wins":                   {"job_id": float64(42), "id": float64(99)},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := ExtractJobIDArg(args, "job_id", "jobId", "id")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != 42 {
				t.Errorf("expected 42, got %d", got)
			}
		})
	}
}

func TestExtractJobIDArgRejects(t *testing.T) {
	for name, tc := range map[string]struct {
		args    map[string]interface{}
		wantMsg string
	}{
		"missing key":     {map[string]interface{}{"other": float64(1)}, "is missing"},
		"nil args":        {nil, "is missing"},
		"explicit null":   {map[string]interface{}{"job_id": nil}, "is missing"},
		"unparseable":     {map[string]interface{}{"job_id": "abc"}, "must be an integer"},
		"empty string":    {map[string]interface{}{"job_id": ""}, "must be an integer"},
		"bool":            {map[string]interface{}{"job_id": true}, "must be an integer"},
		"fractional":      {map[string]interface{}{"job_id": float64(42.7)}, "must be a whole number"},
		"beyond float64":  {map[string]interface{}{"job_id": float64(1 << 54)}, "out of range"},
		"zero":            {map[string]interface{}{"job_id": float64(0)}, "must be positive"},
		"negative":        {map[string]interface{}{"job_id": float64(-1)}, "must be positive"},
		"negative string": {map[string]interface{}{"job_id": "-7"}, "must be positive"},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := ExtractJobIDArg(tc.args, "job_id", "jobId", "id")
			if err == nil {
				t.Fatalf("expected an error, got job ID %d", got)
			}
			if got != 0 {
				t.Errorf("expected 0 alongside the error, got %d", got)
			}
			// The message goes back to the model as the tool result, so it has
			// to say which of the three problems it hit.
			if !strings.Contains(err.Error(), tc.wantMsg) {
				t.Errorf("expected message containing %q, got %q", tc.wantMsg, err.Error())
			}
			if !strings.Contains(err.Error(), "job_id") {
				t.Errorf("message should name the canonical key, got %q", err.Error())
			}
		})
	}
}

// A malformed number must not be reported as "must be positive": that sends the
// model looking for a different ID instead of fixing the encoding.
func TestExtractJobIDArgReportsBadNumberDistinctly(t *testing.T) {
	_, err := ExtractJobIDArg(map[string]interface{}{"job_id": json.Number("12.5")}, "job_id")
	if err == nil {
		t.Fatal("expected an error for a non-integral json.Number")
	}
	if strings.Contains(err.Error(), "must be positive") {
		t.Errorf("a parse failure should not be reported as a sign problem: %q", err.Error())
	}
}

func buildPromptInput(ci *CIInfo) PluginInput {
	return PluginInput{
		Title:  "Fix the thing",
		Author: "someone",
		Branch: "feature",
		Diffs:  []byte("--- a\n+++ b\n"),
		CIInfo: ci,
	}
}

// The host omits ci_info entirely when the MR has no head pipeline, and the
// prompt must then not advertise a section the model could mine for job IDs.
func TestBuildPromptOmitsCISectionWhenAbsent(t *testing.T) {
	got, branch, err := BuildPrompt(buildPromptInput(nil), "PROMPT")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if branch != "feature" {
		t.Errorf("expected default branch 'feature', got %q", branch)
	}
	if strings.Contains(got, "# CI Pipeline Info") {
		t.Errorf("nil CIInfo should render no CI section:\n%s", got)
	}
}

func TestBuildPromptRendersFailedJobIDs(t *testing.T) {
	got, _, err := BuildPrompt(buildPromptInput(&CIInfo{
		PipelineStatus: "failed",
		FailedJobs: []JobRef{
			{Name: "unit", Stage: "test", ID: 4242, AllowFailure: false},
			{Name: "lint", Stage: "check", ID: 77, AllowFailure: true},
		},
	}), "PROMPT")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(got, "# CI Pipeline Info") {
		t.Fatalf("expected a CI section:\n%s", got)
	}
	if !strings.Contains(got, "Pipeline Status: failed") {
		t.Errorf("expected the pipeline status:\n%s", got)
	}
	// The tool description tells the model to copy IDs out of this list, so the
	// IDs have to actually be here and be the ones get_ci_job_log accepts.
	for _, want := range []string{"ID: 4242", "ID: 77", "unit", "lint"} {
		if !strings.Contains(got, want) {
			t.Errorf("expected %q in the CI section:\n%s", want, got)
		}
	}
}

// GitLab JUnit reports frequently leave "file" empty, and classname is then the
// only locator the model has.
func TestBuildPromptRendersTestLocator(t *testing.T) {
	for name, tc := range map[string]struct {
		test    TestRef
		want    string
		notWant string
	}{
		"file preferred when set": {
			test: TestRef{Suite: "pkg", Name: "TestA", File: "pkg/a_test.go", ClassName: "pkg.ClassA"},
			want: "(pkg/a_test.go)",
		},
		"classname fallback when file empty": {
			test: TestRef{Suite: "pkg", Name: "TestB", ClassName: "pkg.ClassB"},
			want: "(pkg.ClassB)",
		},
		"no locator when both empty": {
			test:    TestRef{Suite: "pkg", Name: "TestC"},
			want:    "pkg::TestC",
			notWant: "(",
		},
	} {
		t.Run(name, func(t *testing.T) {
			got, _, err := BuildPrompt(buildPromptInput(&CIInfo{
				PipelineStatus: "failed",
				FailedTests:    []TestRef{tc.test},
			}), "PROMPT")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			line := ""
			for _, l := range strings.Split(got, "\n") {
				if strings.Contains(l, tc.test.Name) {
					line = l
					break
				}
			}
			if line == "" {
				t.Fatalf("no line rendered for %s:\n%s", tc.test.Name, got)
			}
			if !strings.Contains(line, tc.want) {
				t.Errorf("expected %q in %q", tc.want, line)
			}
			if tc.notWant != "" && strings.Contains(line, tc.notWant) {
				t.Errorf("did not expect %q in %q", tc.notWant, line)
			}
		})
	}
}

func TestBuildPromptRequiresABranch(t *testing.T) {
	if _, _, err := BuildPrompt(PluginInput{Title: "t"}, "PROMPT"); err == nil {
		t.Error("expected an error when neither branch is set")
	}
}

// The prompt and the tool description both point the model at a section title
// that BuildPrompt has to actually emit; naming a field the prompt never
// contains costs a turn on a hallucinated ID.
func TestCIJobLogGuidanceMatchesRenderedPrompt(t *testing.T) {
	rendered, _, err := BuildPrompt(buildPromptInput(&CIInfo{
		PipelineStatus: "failed",
		FailedJobs:     []JobRef{{Name: "unit", Stage: "test", ID: 1}},
	}), DefaultPrompt)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var tool ToolDef
	for _, candidate := range Tools() {
		if candidate.Name == "get_ci_job_log" {
			tool = candidate
			break
		}
	}
	if tool.Name == "" {
		t.Fatal("get_ci_job_log not found in Tools()")
	}

	for _, section := range []string{"# CI Pipeline Info", "Failed Jobs"} {
		if !strings.Contains(rendered, section) {
			t.Errorf("BuildPrompt does not render %q", section)
		}
		if !strings.Contains(tool.Description, section) {
			t.Errorf("tool description does not reference %q", section)
		}
	}
	if strings.Contains(tool.Description, "ci_info.failed_jobs") ||
		strings.Contains(DefaultPrompt, "ci_info.failed_jobs") {
		t.Error("guidance still names ci_info.failed_jobs, which the prompt never renders")
	}
}

func TestCIJobLogToolSchema(t *testing.T) {
	var tool ToolDef
	for _, candidate := range Tools() {
		if candidate.Name == "get_ci_job_log" {
			tool = candidate
			break
		}
	}
	if tool.Name == "" {
		t.Fatal("get_ci_job_log not found in Tools()")
	}

	if tool.Parameters.Type != "object" {
		t.Errorf("expected parameters type 'object', got %q", tool.Parameters.Type)
	}
	prop, ok := tool.Parameters.Properties["job_id"]
	if !ok {
		t.Fatalf("expected a job_id property, got %v", tool.Parameters.Properties)
	}
	if prop.Type != "integer" {
		t.Errorf("expected job_id type 'integer', got %q", prop.Type)
	}
	if len(tool.Parameters.Required) != 1 || tool.Parameters.Required[0] != "job_id" {
		t.Errorf("expected required ['job_id'], got %v", tool.Parameters.Required)
	}
}

// The host sends the log as a []byte, which encoding/json renders as base64.
func TestCIJobLogDecodesBase64Log(t *testing.T) {
	var log CIJobLog
	if err := json.Unmarshal([]byte(`{"log":"aGVsbG8=","job_id":42,"job_name":"unit","stage":"test"}`), &log); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if log.Log != "hello" {
		t.Errorf("expected decoded log 'hello', got %q", log.Log)
	}
	if log.ID != 42 || log.Name != "unit" || log.Stage != "test" {
		t.Errorf("lost a field: %+v", log)
	}
}
