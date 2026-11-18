package result

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/gopaytech/pergent/internal/review"
	"github.com/gopaytech/pergent/internal/runner"
)

func usageRun() review.RunUsage {
	return review.RunUsage{
		Label:       "1/1",
		PromptBytes: 100,
		Attachments: []review.Attachment{{Name: "diff.patch", Bytes: 40}},
		Usage:       runner.Usage{Input: 10, Output: 5, Reasoning: 2, CacheRead: 7, CacheWrite: 3},
		Steps: []runner.StepUsage{{
			Attempt: 1,
			Usage:   runner.Usage{Input: 10, Output: 5},
			Tools: []runner.ToolCall{
				{Tool: "read", Target: "pergent", Bytes: 12},
				{Tool: "grep", Target: "repository", Bytes: 3, Failed: true},
			},
		}},
		ToolTotals: runner.ToolTotals{
			Read: runner.ToolUse{Calls: 1, Bytes: 12},
			Grep: runner.ToolUse{Calls: 1, Bytes: 3},
		},
	}
}

func TestBuild_Runs(t *testing.T) {
	sr := review.SkillResult{Skill: "s", Verdict: review.NoBlockingIssues, Runs: []review.RunUsage{usageRun()}}
	b, err := Encode(Build(testMeta(), []review.SkillResult{sr}))
	if err != nil {
		t.Fatal(err)
	}
	got := decode(t, b)["skills"].([]any)[0].(map[string]any)["runs"].([]any)[0]
	want := map[string]any{
		"run":          "1/1",
		"prompt_bytes": 100.0,
		"attachments":  []any{map[string]any{"name": "diff.patch", "bytes": 40.0}},
		"tokens":       map[string]any{"input": 10.0, "output": 5.0, "reasoning": 2.0, "cache_read": 7.0, "cache_write": 3.0},
		"tool_totals": map[string]any{
			"read":  map[string]any{"calls": 1.0, "bytes": 12.0},
			"grep":  map[string]any{"calls": 1.0, "bytes": 3.0},
			"glob":  map[string]any{"calls": 0.0, "bytes": 0.0},
			"list":  map[string]any{"calls": 0.0, "bytes": 0.0},
			"other": map[string]any{"calls": 0.0, "bytes": 0.0},
		},
		"steps": []any{map[string]any{
			"attempt": 1.0,
			"tokens":  map[string]any{"input": 10.0, "output": 5.0, "reasoning": 0.0, "cache_read": 0.0, "cache_write": 0.0},
			"tools": []any{
				map[string]any{"tool": "read", "target": "pergent", "bytes": 12.0},
				map[string]any{"tool": "grep", "target": "repository", "bytes": 3.0, "failed": true},
			},
		}},
	}
	if !reflect.DeepEqual(got, want) {
		gb, _ := json.MarshalIndent(got, "", " ")
		wb, _ := json.MarshalIndent(want, "", " ")
		t.Errorf("runs[0] =\n%s\nwant\n%s", gb, wb)
	}
}

func TestBuild_NoRunsOmitsKey(t *testing.T) {
	sr := review.SkillResult{Skill: "s", Verdict: review.NoBlockingIssues}
	b, err := Encode(Build(testMeta(), []review.SkillResult{sr}))
	if err != nil {
		t.Fatal(err)
	}
	skill := decode(t, b)["skills"].([]any)[0].(map[string]any)
	if _, ok := skill["runs"]; ok {
		t.Errorf("runs key present: %v", skill["runs"])
	}
}

func TestBuild_RunsDoNotChangeTheRest(t *testing.T) {
	with := review.SkillResult{Skill: "s", Verdict: review.NoBlockingIssues, Runs: []review.RunUsage{usageRun()}}
	without := with
	without.Runs = nil
	bw, err := Encode(Build(testMeta(), []review.SkillResult{with}))
	if err != nil {
		t.Fatal(err)
	}
	bo, err := Encode(Build(testMeta(), []review.SkillResult{without}))
	if err != nil {
		t.Fatal(err)
	}
	mw, mo := decode(t, bw), decode(t, bo)
	for _, s := range mw["skills"].([]any) {
		delete(s.(map[string]any), "runs")
	}
	if !reflect.DeepEqual(mw, mo) {
		t.Errorf("runs changed other fields:\n%s\n%s", bw, bo)
	}
}

func TestEncodeUsage(t *testing.T) {
	t.Setenv("GITLAB_TOKEN", "glpat-secretvalue")
	a := review.SkillResult{
		Skill: "a", Usage: runner.Usage{Input: 4, Output: 2},
		Runs: []review.RunUsage{{Label: "1/1", Steps: []runner.StepUsage{{Attempt: 1}}}},
	}
	b := review.SkillResult{Skill: "b-glpat-secretvalue"}
	out, err := EncodeUsage([]review.SkillResult{a, b})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasSuffix(out, []byte("}\n")) {
		t.Errorf("no trailing newline: %q", out[len(out)-3:])
	}
	if bytes.Contains(out, []byte("glpat-secretvalue")) {
		t.Errorf("token leaked: %s", out)
	}
	var doc UsageDoc
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Skills) != 2 || doc.Skills[0].Name != "a" || !strings.HasPrefix(doc.Skills[1].Name, "b-") {
		t.Fatalf("skills = %+v", doc.Skills)
	}
	if doc.Skills[0].Tokens.Input != 4 || doc.Skills[0].Tokens.Output != 2 {
		t.Errorf("tokens = %+v", doc.Skills[0].Tokens)
	}
	raw := decode(t, out)["skills"].([]any)
	if r, ok := raw[1].(map[string]any)["runs"].([]any); !ok || len(r) != 0 {
		t.Errorf("skill with no runs: runs = %v, want []", raw[1].(map[string]any)["runs"])
	}
	run := raw[0].(map[string]any)["runs"].([]any)[0].(map[string]any)
	if att, ok := run["attachments"].([]any); !ok || len(att) != 0 {
		t.Errorf("attachments = %v, want []", run["attachments"])
	}
	step := run["steps"].([]any)[0].(map[string]any)
	if tools, ok := step["tools"].([]any); !ok || len(tools) != 0 {
		t.Errorf("tools = %v, want []", step["tools"])
	}
}
