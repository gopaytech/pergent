package review

import (
	"context"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/gopaytech/pergent/internal/runner"
	"github.com/gopaytech/pergent/internal/skill"
)

// metered is a completed result that carries usage, one step and tool totals
// that depend on n, so a test can tell the runs apart.
func metered(output string, n int) runner.RunResult {
	res := completed(output)
	res.Usage = runner.Usage{Input: 100 * n, Output: 10 * n}
	res.Steps = []runner.StepUsage{{Attempt: 1, Usage: res.Usage, Tools: []runner.ToolCall{{Tool: "read", Bytes: n}}}}
	res.ToolTotals = runner.ToolTotals{Read: runner.ToolUse{Calls: 1, Bytes: n}}
	return res
}

func runLabels(runs []RunUsage) []string {
	var out []string
	for _, r := range runs {
		out = append(out, r.Label)
	}
	return out
}

func sumUsage(runs []RunUsage) runner.Usage {
	var u runner.Usage
	for _, r := range runs {
		u = u.Add(r.Usage)
	}
	return u
}

func TestRun_RecordsEachRun(t *testing.T) {
	f := &fake{}
	f.respond = func(n int, _ call) runner.RunResult { return metered(answer(t), n) }
	req := newRequest(t, f, twoBatchDiff())
	req.ContextLimit = twoBatchContextLimit

	res := Run(context.Background(), req)

	runs := res[0].Runs
	if got := runLabels(runs); !slices.Equal(got, []string{"1/2", "2/2"}) {
		t.Fatalf("labels = %q, want [1/2 2/2]", got)
	}
	for _, r := range runs {
		// The fake numbers its results by call, and the batches run in
		// parallel, so the run's own usage tells which call it was.
		n := r.Usage.Input / 100
		want := metered("", n)
		if r.Usage != want.Usage || !reflect.DeepEqual(r.Steps, want.Steps) || r.ToolTotals != want.ToolTotals {
			t.Errorf("run %s = %+v, want the fake's result %d", r.Label, r, n)
		}
	}
	if got := sumUsage(runs); got != res[0].Usage {
		t.Errorf("sum of run usage = %+v, want the skill's %+v", got, res[0].Usage)
	}
}

func TestRun_RecordsRepairRun(t *testing.T) {
	f := &fake{}
	f.respond = func(n int, _ call) runner.RunResult {
		if n == 1 {
			return metered("The code looks fine.", n)
		}
		return metered(answer(t), n)
	}
	res := Run(context.Background(), newRequest(t, f, patch("cmd/a.go", "x")))

	if got := runLabels(res[0].Runs); !slices.Equal(got, []string{"1/1", "1/1 repair"}) {
		t.Fatalf("labels = %q, want [1/1 1/1 repair]", got)
	}
	if got := sumUsage(res[0].Runs); got != res[0].Usage {
		t.Errorf("sum of run usage = %+v, want the skill's %+v", got, res[0].Usage)
	}
}

func TestRun_RunSizes(t *testing.T) {
	s := skill.Skill{Name: "s", Body: "Review it."}
	t.Run("plain", func(t *testing.T) {
		f := &fake{respond: func(int, call) runner.RunResult { return completed(answer(t)) }}
		res := Run(context.Background(), newRequest(t, f, patch("cmd/a.go", "x"), s))
		checkSizes(t, res[0].Runs, f.calls, len(s.Prompt()))
	})
	t.Run("hardened", func(t *testing.T) {
		f := &fake{respond: func(int, call) runner.RunResult { return completed(answer(t)) }}
		req := newRequest(t, f, patch("cmd/a.go", "x"), s)
		req.Sandbox = Sandbox{Hardened: true, Instructions: []string{"/i.md"}}
		res := Run(context.Background(), req)
		want := len(strings.TrimRight(s.Prompt(), "\n") + "\n\n" + untrustedSection + "\n")
		checkSizes(t, res[0].Runs, f.calls, want)
	})
}

func checkSizes(t *testing.T, runs []RunUsage, calls []call, promptBytes int) {
	t.Helper()
	if len(runs) != 1 || len(calls) != 1 {
		t.Fatalf("runs = %d, calls = %d, want 1 each", len(runs), len(calls))
	}
	r := runs[0]
	if r.PromptBytes != promptBytes {
		t.Errorf("PromptBytes = %d, want %d", r.PromptBytes, promptBytes)
	}
	var names []string
	for i, a := range r.Attachments {
		names = append(names, a.Name)
		if i < len(calls[0].contents) && a.Bytes != len(calls[0].contents[i]) {
			t.Errorf("attachment %s Bytes = %d, want %d", a.Name, a.Bytes, len(calls[0].contents[i]))
		}
	}
	if !slices.Equal(names, []string{"diff.patch", "changed-files.md"}) {
		t.Errorf("attachments = %q, want [diff.patch changed-files.md]", names)
	}
}

func TestRun_SameSkillNameKeepsItsOwnRuns(t *testing.T) {
	f := &fake{respond: func(int, call) runner.RunResult { return completed(answer(t)) }}
	req := newRequest(t, f, patch("cmd/a.go", "x"),
		skill.Skill{Name: "s", Body: "Review one."}, skill.Skill{Name: "s", Body: "Review two."})

	res := Run(context.Background(), req)

	if len(res) != 2 || len(res[0].Runs) != 1 || len(res[1].Runs) != 1 {
		t.Errorf("runs = %d and %d, want 1 each", len(res[0].Runs), len(res[1].Runs))
	}
}

func TestRun_NoRunsWhenNothingRan(t *testing.T) {
	f := &fake{respond: func(int, call) runner.RunResult { return completed(answer(t)) }}
	res := Run(context.Background(), newRequest(t, f, " \n"))
	if res[0].Runs != nil {
		t.Errorf("Runs = %+v, want nil", res[0].Runs)
	}
}

func TestImpact_RecordsRuns(t *testing.T) {
	f := impactFake(t, answer(t, blockingWithRealCaller()), nil)
	link := LinkedRepo{Name: "billing", Path: billingClone(t), CanBlock: true}

	rs := Run(context.Background(), impactRequest(t, f, patch("render/r.go", "x"), []LinkedRepo{link}))

	impact := resultFor(t, rs, ImpactSkill)
	if got := runLabels(impact.Runs); !slices.Equal(got, []string{"check", "verify"}) {
		t.Errorf("impact labels = %q, want [check verify]", got)
	}
	for _, r := range rs {
		if r.Skill == ImpactSkill {
			continue
		}
		for _, run := range r.Runs {
			if run.Label == "check" || run.Label == "verify" {
				t.Errorf("skill %s holds the impact run %q", r.Skill, run.Label)
			}
		}
	}
}

func TestRunOrder(t *testing.T) {
	got := []string{"verify", "10/12", "2/12", "2/12 repair", "cross", "references"}
	slices.SortFunc(got, runOrder)
	want := []string{"2/12", "2/12 repair", "10/12", "cross", "references", "verify"}
	if !slices.Equal(got, want) {
		t.Errorf("order = %q, want %q", got, want)
	}
}
