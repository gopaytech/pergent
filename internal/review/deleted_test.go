package review

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/gopaytech/pergent/internal/diff"
	"github.com/gopaytech/pergent/internal/finding"
	"github.com/gopaytech/pergent/internal/runner"
	"github.com/gopaytech/pergent/internal/skill"
)

// deletedContent returns the contents of the run's deleted-files.md, and
// whether the run had one.
func deletedContent(c call) (string, bool) {
	for i, p := range c.opts.Files {
		if filepath.Base(p) == "deleted-files.md" {
			return c.contents[i], true
		}
	}
	return "", false
}

func hasFile(c call, name string) bool {
	for _, p := range c.opts.Files {
		if filepath.Base(p) == name {
			return true
		}
	}
	return false
}

// deletedReq is a request with the check on, the default in production.
func deletedReq(t *testing.T, f *fake, diffText string, skills ...skill.Skill) Request {
	t.Helper()
	req := newRequest(t, f, diffText, skills...)
	req.CheckDeleted = true
	req.Budget = 5 * time.Minute
	return req
}

func answering(t *testing.T, fs ...finding.Finding) *fake {
	f := &fake{}
	f.respond = func(int, call) runner.RunResult { return completed(answer(t, fs...)) }
	return f
}

func TestCheckDeleted_BatchRunGetsTheList(t *testing.T) {
	f := answering(t)
	req := deletedReq(t, f, patch("a/x.go", "x")+deletedPatch("a/gone.go"))

	r := Run(context.Background(), req)[0]

	if len(f.calls) != 1 {
		t.Fatalf("runner called %d times, want 1", len(f.calls))
	}
	c := f.calls[0]
	got, ok := deletedContent(c)
	if !ok || got != "\"a/gone.go\"\n" {
		t.Errorf("deleted-files.md = %q (attached %v), want the quoted path", got, ok)
	}
	if want := singleBatchMessage + " " + deletedMessage; c.opts.Message != want {
		t.Errorf("Message = %q, want %q", c.opts.Message, want)
	}
	if !hasFile(c, "diff.patch") {
		t.Error("the batch run lost its diff")
	}
	// The deleted file stays skipped and is not reviewed.
	if !slices.Equal(r.Reviewed, []string{"a/x.go"}) {
		t.Errorf("Reviewed = %v, want only a/x.go", r.Reviewed)
	}
	if !slices.Contains(r.Skipped, diff.Skip{Path: "a/gone.go", Rule: "deleted"}) {
		t.Errorf("Skipped = %v, want a/gone.go skipped as deleted", r.Skipped)
	}
}

func TestCheckDeleted_OffChangesNothing(t *testing.T) {
	f := answering(t)
	req := deletedReq(t, f, patch("a/x.go", "x")+deletedPatch("a/gone.go"))
	req.CheckDeleted = false

	Run(context.Background(), req)

	if len(f.calls) != 1 {
		t.Fatalf("runner called %d times, want 1", len(f.calls))
	}
	c := f.calls[0]
	if _, ok := deletedContent(c); ok || len(c.opts.Files) != 2 {
		t.Errorf("Files = %v, want only diff.patch and changed-files.md", c.opts.Files)
	}
	if c.opts.Message != singleBatchMessage {
		t.Errorf("Message = %q, want %q", c.opts.Message, singleBatchMessage)
	}
}

func TestCheckDeleted_OffRunsNothingForADeleteOnlyChange(t *testing.T) {
	f := answering(t)
	req := deletedReq(t, f, deletedPatch("a/gone.go"))
	req.CheckDeleted = false

	r := Run(context.Background(), req)[0]

	if len(f.calls) != 0 || r.Verdict != NoBlockingIssues || !slices.Contains(r.Notes, noFilesNote) {
		t.Errorf("calls = %d, verdict = %q, notes = %q, want no run and no files in scope", len(f.calls), r.Verdict, r.Notes)
	}
}

func TestCheckDeleted_EveryBatchGetsTheList(t *testing.T) {
	f := answering(t)
	req := deletedReq(t, f, twoBatchDiff()+deletedPatch("c/gone.go"))
	req.ContextLimit = twoBatchContextLimit

	Run(context.Background(), req)

	if len(f.calls) != 2 {
		t.Fatalf("runner called %d times, want 2", len(f.calls))
	}
	for _, c := range f.calls {
		if got, _ := deletedContent(c); got != "\"c/gone.go\"\n" {
			t.Errorf("batch %q got deleted-files.md %q", c.opts.Message, got)
		}
	}
}

func TestCheckDeleted_CrossBatchAndVerifyGetTheListRepairDoesNot(t *testing.T) {
	blocking := finding.Finding{File: "docs/use.md", Line: 4, Severity: finding.Blocking, Title: "Still links gone.go", Explanation: "e"}
	f := &fake{}
	badFirst := true
	f.respond = func(_ int, c call) runner.RunResult {
		switch {
		case isVerifyCall(c):
			return completed(verifyAnswer(t, map[string]any{"id": "1", "real": true, "reason": "r"}))
		case isCrossBatchCall(c):
			return completed(answer(t, blocking))
		case strings.HasPrefix(c.opts.Message, "The attached review is not in the required output format"):
			return completed(answer(t))
		case badFirst && strings.Contains(c.opts.Message, "batch 1 of 3"):
			badFirst = false
			return completed("no json here")
		}
		return completed(answer(t))
	}
	diffText, _ := nBatchDiff(3)
	req := deletedReq(t, f, diffText+deletedPatch("c/gone.go"), skill.Skill{Name: "s", Body: "Review it.", CrossBatch: true})
	req.ContextLimit = twoBatchContextLimit

	r := Run(context.Background(), req)[0]

	var cross, verify, repair, batch int
	for _, c := range f.calls {
		got, has := deletedContent(c)
		switch {
		case isVerifyCall(c):
			verify++
			if !has || got != "\"c/gone.go\"\n" || !strings.Contains(c.opts.Message, verifyDeletedMessage) {
				t.Errorf("verification run: deleted-files.md %q (attached %v), message %q", got, has, c.opts.Message)
			}
		case isCrossBatchCall(c):
			cross++
			if !has || got != "\"c/gone.go\"\n" || !strings.HasSuffix(c.opts.Message, deletedMessage) {
				t.Errorf("cross-batch run: deleted-files.md %q (attached %v), message %q", got, has, c.opts.Message)
			}
		case strings.HasPrefix(c.opts.Message, "The attached review is not in the required output format"):
			repair++
			if has || strings.Contains(c.opts.Message, "deleted") {
				t.Errorf("repair run got the deleted list: files %v, message %q", c.opts.Files, c.opts.Message)
			}
		default:
			batch++
			if !has {
				t.Errorf("batch run %q has no deleted-files.md", c.opts.Message)
			}
		}
	}
	if batch != 3 || cross != 1 || verify != 1 || repair != 1 {
		t.Errorf("runs: batch %d cross %d verify %d repair %d, want 3, 1, 1, 1", batch, cross, verify, repair)
	}
	if r.Verdict != HasBlockingIssues {
		t.Errorf("Verdict = %q, want %q (a verified finding on a remaining file)", r.Verdict, HasBlockingIssues)
	}
}

func TestCheckDeleted_ListFollowsTheSkillsScope(t *testing.T) {
	diffText := patch("a/x.go", "x") +
		deletedPatch("a/gone.go") + deletedPatch("a/skip/gone.go") + deletedPatch("b/gone.go") +
		deletedPatch("a/vendor/lib/gone.go") + deletedPatch("a/package-lock.json") + deletedPatch("a/gen/gone.go")
	classifier := diff.NewClassifierFrom([]byte("a/gen/** linguist-generated\n"), nil)
	tests := []struct {
		name  string
		skill skill.Skill
		want  string
	}{
		{"paths and exclude decide", skill.Skill{Name: "s", Body: "b", Paths: []string{"a/gone.go", "a/skip/gone.go"}, Exclude: []string{"a/skip/**"}},
			"\"a/gone.go\"\n"},
		{"no paths: every other rule still applies", skill.Skill{Name: "s", Body: "b"},
			"\"a/gone.go\"\n\"a/skip/gone.go\"\n\"b/gone.go\"\n"},
		{"paths opt in to a vendored, lockfile or generated file", skill.Skill{Name: "s", Body: "b", Paths: []string{"a/vendor/**", "a/package-lock.json", "a/gen/**", "a/x.go"}},
			"\"a/vendor/lib/gone.go\"\n\"a/package-lock.json\"\n\"a/gen/gone.go\"\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := answering(t)
			req := deletedReq(t, f, diffText, tt.skill)
			req.Classifier = &classifier

			Run(context.Background(), req)

			if len(f.calls) != 1 {
				t.Fatalf("runner called %d times, want 1", len(f.calls))
			}
			if got, _ := deletedContent(f.calls[0]); got != tt.want {
				t.Errorf("deleted-files.md = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCheckDeleted_ListIsCapped(t *testing.T) {
	var b strings.Builder
	b.WriteString(patch("a/x.go", "x"))
	for i := range 250 {
		b.WriteString(deletedPatch(fmt.Sprintf("old/f%03d.go", i)))
	}
	f := answering(t)

	Run(context.Background(), deletedReq(t, f, b.String()))

	got, _ := deletedContent(f.calls[0])
	lines := strings.Split(strings.TrimSuffix(got, "\n"), "\n")
	if len(lines) != 201 || lines[0] != `"old/f000.go"` || lines[199] != `"old/f199.go"` || lines[200] != "... and 50 more" {
		t.Errorf("list has %d lines, first %q, 200th %q, last %q; want 200 paths then \"... and 50 more\"", len(lines), lines[0], lines[199], lines[len(lines)-1])
	}
}

func TestCheckDeleted_ExactlyTheCapHasNoMoreLine(t *testing.T) {
	var paths []string
	for i := range 200 {
		paths = append(paths, fmt.Sprintf("f%d", i))
	}
	if got := deletedList(paths); strings.Contains(got, "more") || strings.Count(got, "\n") != 200 {
		t.Errorf("deletedList(200 paths) = %d lines, want 200 and no \"more\" line", strings.Count(got, "\n"))
	}
}

func TestCheckDeleted_PathsAreQuoted(t *testing.T) {
	f := answering(t)
	// A path with a space and a quote cannot start a new line or sentence.
	req := deletedReq(t, f, patch("a/x.go", "x")+deletedPatch("old/a b.go"))

	Run(context.Background(), req)

	if got, _ := deletedContent(f.calls[0]); got != "\"old/a b.go\"\n" {
		t.Errorf("deleted-files.md = %q", got)
	}
}

func TestCheckDeleted_DeleteOnlyChangeGetsOneRunWithNoDiff(t *testing.T) {
	finding1 := finding.Finding{File: "cmd/main.go", Line: 7, Severity: finding.Blocking, Title: "Imports the deleted package", Explanation: "e"}
	f := &fake{}
	f.respond = func(_ int, c call) runner.RunResult {
		if isVerifyCall(c) {
			return completed(verifyAnswer(t, map[string]any{"id": "1", "real": true, "reason": "r"}))
		}
		return completed(answer(t, finding1))
	}
	req := deletedReq(t, f, deletedPatch("pkg/gone.go"))

	r := Run(context.Background(), req)[0]

	if len(f.calls) != 2 {
		t.Fatalf("runner called %d times, want the one run and its verification", len(f.calls))
	}
	c := f.calls[0]
	if hasFile(c, "diff.patch") {
		t.Errorf("the run has a diff: %v", c.opts.Files)
	}
	if got, ok := deletedContent(c); !ok || got != "\"pkg/gone.go\"\n" {
		t.Errorf("deleted-files.md = %q (attached %v)", got, ok)
	}
	if want := noDiffMessage + " " + deletedMessage; c.opts.Message != want {
		t.Errorf("Message = %q, want %q", c.opts.Message, want)
	}
	if !isVerifyCall(f.calls[1]) {
		t.Errorf("second run = %q, want the verification", f.calls[1].opts.Message)
	}
	if r.Verdict != HasBlockingIssues || len(r.Findings) != 1 {
		t.Errorf("Verdict = %q, findings = %v, want the verified blocking finding", r.Verdict, r.Findings)
	}
	if len(r.Reviewed) != 0 || r.Batches != 0 || r.BatchesFinished != 0 {
		t.Errorf("Reviewed = %v, Batches = %d/%d, want none: nothing was in the diff", r.Reviewed, r.BatchesFinished, r.Batches)
	}
	if !slices.Contains(r.Skipped, diff.Skip{Path: "pkg/gone.go", Rule: "deleted"}) {
		t.Errorf("Skipped = %v, want pkg/gone.go skipped as deleted", r.Skipped)
	}
	if r.Confidence != 0 {
		t.Errorf("Confidence = %d, want none: the run scores no reviewed file", r.Confidence)
	}
}

func TestCheckDeleted_DeleteOnlyChangeWithNothingLeftIsClean(t *testing.T) {
	f := answering(t)
	r := Run(context.Background(), deletedReq(t, f, deletedPatch("pkg/gone.go")))[0]

	if len(f.calls) != 1 {
		t.Fatalf("runner called %d times, want 1", len(f.calls))
	}
	if r.Verdict != NoBlockingIssues || len(r.Findings) != 0 || len(r.Failures) != 0 {
		t.Errorf("Verdict = %q, findings = %v, failures = %v, want a clean skill", r.Verdict, r.Findings, r.Failures)
	}
}

func TestCheckDeleted_NoRunWhenNothingDeletedIsInScope(t *testing.T) {
	f := answering(t)
	req := deletedReq(t, f, deletedPatch("pkg/gone.go")+deletedPatch("vendor/x/y.go")+patch("docs/a.md", "x"),
		skill.Skill{Name: "s", Body: "b", Paths: []string{"docs/**"}})
	req.Concurrency = 1

	Run(context.Background(), req)

	// docs/a.md is one batch; the out-of-scope deletions are not listed.
	if len(f.calls) != 1 {
		t.Fatalf("runner called %d times, want 1", len(f.calls))
	}
	if _, ok := deletedContent(f.calls[0]); ok {
		t.Error("a deleted file outside the skill's paths was listed")
	}

	f = answering(t)
	r := Run(context.Background(), deletedReq(t, f, deletedPatch("pkg/gone.go")+deletedPatch("vendor/x/y.go"),
		skill.Skill{Name: "s", Body: "b", Paths: []string{"docs/**"}}))[0]
	if len(f.calls) != 0 || !slices.Contains(r.Notes, noFilesNote) {
		t.Errorf("calls = %d, notes = %q, want no run and no files in scope", len(f.calls), r.Notes)
	}
	f = answering(t)
	Run(context.Background(), deletedReq(t, f, deletedPatch("vendor/x/y.go")+deletedPatch("go.sum")))
	if len(f.calls) != 0 {
		t.Errorf("runner called %d times for vendored and lockfile deletions, want 0", len(f.calls))
	}
}

func TestCheckDeleted_FailedDeleteOnlyRunIsANoteNotAFailure(t *testing.T) {
	f := &fake{}
	f.respond = func(int, call) runner.RunResult {
		return runner.RunResult{Status: runner.StatusFailed, Cause: &runner.Cause{Kind: runner.CauseNoAnswer}}
	}
	r := Run(context.Background(), deletedReq(t, f, deletedPatch("pkg/gone.go")))[0]

	if r.Verdict != NoBlockingIssues || len(r.Failures) != 0 {
		t.Errorf("Verdict = %q, failures = %v, want what a delete-only change had before", r.Verdict, r.Failures)
	}
	if !strings.Contains(strings.Join(r.Notes, "\n"), "Deleted-files check failed") {
		t.Errorf("Notes = %q, want a note that the check failed", r.Notes)
	}
}

func TestCheckDeleted_EachSkillGetsItsOwnList(t *testing.T) {
	f := answering(t)
	req := deletedReq(t, f, patch("a/x.go", "x")+patch("b/y.go", "y")+deletedPatch("a/gone.go")+deletedPatch("b/gone.go"),
		skill.Skill{Name: "sa", Body: "b", Paths: []string{"a/**"}},
		skill.Skill{Name: "sb", Body: "b", Paths: []string{"b/**"}})

	Run(context.Background(), req)

	for _, c := range f.calls {
		got, _ := deletedContent(c)
		want := map[string]string{"sa": "\"a/gone.go\"\n", "sb": "\"b/gone.go\"\n"}[c.opts.SkillName]
		if got != want {
			t.Errorf("skill %s got %q, want %q", c.opts.SkillName, got, want)
		}
	}
	if len(f.calls) != 2 {
		t.Errorf("runner called %d times, want 2", len(f.calls))
	}
}
