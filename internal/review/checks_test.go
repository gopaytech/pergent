package review

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/gopaytech/pergent/internal/finding"
	"github.com/gopaytech/pergent/internal/runner"
)

func checksAnswer(checks string, findings string) string {
	return "```json\n{\"checks\":[" + checks + "],\"findings\":[" + findings + "]}\n```\n"
}

const (
	passCheck = `{"section":"S","name":"one","status":"pass","detail":"d"}`
	failCheck = `{"section":"S","name":"two","status":"fail"}`
	aFinding  = `{"file":"cmd/a.go","line":1,"severity":"suggestion","title":"t","explanation":"e"}`
)

func TestRun_ChecksKept(t *testing.T) {
	f := &fake{}
	f.respond = func(int, call) runner.RunResult { return completed(checksAnswer(passCheck+","+failCheck, aFinding)) }
	r := Run(context.Background(), newRequest(t, f, patch("cmd/a.go", "x")))[0]

	if len(r.Checks) != 1 {
		t.Fatalf("Checks = %+v, want one batch entry", r.Checks)
	}
	bc := r.Checks[0]
	want := []finding.Check{
		{Section: "S", Name: "one", Status: finding.CheckPass, Detail: "d"},
		{Section: "S", Name: "two", Status: finding.CheckFail},
	}
	if bc.Batch != 1 || bc.Dropped != 0 || bc.Findings != 1 || len(bc.Checks) != 2 || bc.Checks[0] != want[0] || bc.Checks[1] != want[1] {
		t.Errorf("Checks[0] = %+v, want batch 1 with %+v and 1 finding", bc, want)
	}
}

func TestRun_ChecksOnlyFinishedBatches(t *testing.T) {
	f := &fake{}
	f.respond = func(_ int, c call) runner.RunResult {
		if strings.Contains(c.contents[0], "a/x.go") {
			return runner.RunResult{Status: runner.StatusFailed, Cause: &runner.Cause{Kind: runner.CauseTimeout, Timeout: time.Minute}}
		}
		return completed(checksAnswer(passCheck, ""))
	}
	req := newRequest(t, f, twoBatchDiff())
	req.ContextLimit = twoBatchContextLimit

	r := Run(context.Background(), req)[0]
	if len(r.Checks) != 1 || r.Checks[0].Batch != 2 || len(r.Checks[0].Checks) != 1 {
		t.Errorf("Checks = %+v, want only batch 2", r.Checks)
	}
}

func TestRun_ChecksFromRepair(t *testing.T) {
	f := &fake{}
	f.respond = func(n int, _ call) runner.RunResult {
		if n == 1 {
			return completed("Looks fine.")
		}
		return completed(checksAnswer(passCheck, ""))
	}
	r := Run(context.Background(), newRequest(t, f, patch("cmd/a.go", "x")))[0]
	if len(r.Checks) != 1 || r.Checks[0].Batch != 1 || len(r.Checks[0].Checks) != 1 {
		t.Errorf("Checks = %+v, want the repaired answer's check", r.Checks)
	}
}

func TestRun_NoChecksNoEntry(t *testing.T) {
	f := &fake{}
	f.respond = func(int, call) runner.RunResult { return completed(answer(t)) }
	r := Run(context.Background(), newRequest(t, f, patch("cmd/a.go", "x")))[0]
	if len(r.Checks) != 0 {
		t.Errorf("Checks = %+v, want none", r.Checks)
	}
}
