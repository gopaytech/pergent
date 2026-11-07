package render_test

import (
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/gopaytech/pergent/render"
)

// shortFixture is one skill with a blocking finding, a suggestion, a dismissed
// finding, a failure, and a pass, an n/a and a failed check over two changed
// files.
func shortFixture() ([]render.Skill, []string) {
	files := []string{"a.go", "b.go"}
	skills := []render.Skill{{
		Name:    "code-review",
		Verdict: render.VerdictHasBlockingIssues,
		Findings: []render.Finding{
			{ID: "aaaaaa", Fingerprint: "aaaaaa111111", File: "a.go", Line: 3, Severity: render.Blocking, Title: "Nil map write", Explanation: "It panics."},
			{ID: "bbbbbb", Fingerprint: "bbbbbb222222", File: "b.go", Line: 9, Severity: render.Suggestion, Title: "Extract helper", Explanation: "Two copies."},
		},
		Dismissed: []render.DismissedFinding{{
			Finding: render.Finding{ID: "cccccc", Fingerprint: "cccccc333333", File: "b.go", Line: 1, Severity: render.Suggestion, Title: "Rename var"},
			By:      "sam", Reason: "on purpose",
		}},
		Failures: []render.Failure{{Files: []string{"b.go"}, Message: "Timed out waiting for the model."}},
		Reviewed: files,
		Batches:  2, BatchesFinished: 1,
		Summaries: []render.Summary{{Batch: 1, Text: "Checked."}},
		Checks: []render.BatchChecks{{Batch: 1, Findings: 1, Checks: []render.Check{
			{Section: "Tests", Name: "Unit tests ran", Status: render.CheckPass, Detail: "passed detail"},
			{Section: "Docs", Name: "Docs updated", Status: render.CheckNA, Detail: "na detail"},
			{Section: "Safety", Name: "No panics", Status: render.CheckFail, Detail: "failed detail"},
		}}},
	}}
	return skills, files
}

var (
	// passedAreasRe matches the areas of the fixture that hold no failed check.
	passedAreasRe = regexp.MustCompile(`#### (?:Tests|Docs)\n\n(?:\|.*\n)+\n`)
	filesBlockRe  = regexp.MustCompile(`(?s)<details><summary>Files \(.*?</details>\n\n`)
)

func TestShortLeavesOutFilesAndPassedChecks(t *testing.T) {
	skills, files := shortFixture()
	got := render.Comment(skills, render.Options{ChangedFiles: files, Short: true})
	for _, banned := range []string{"<summary>Files (", "#### Tests", "#### Docs", "Unit tests ran", "Docs updated", "passed detail", "na detail", "omitted"} {
		if strings.Contains(got, banned) {
			t.Errorf("Short comment holds %q:\n%s", banned, got)
		}
	}
	off := render.Comment(skills, render.Options{ChangedFiles: files})
	for _, want := range []string{"<summary>Files (", "#### Tests", "Unit tests ran", "#### Docs", "Docs updated"} {
		if !strings.Contains(off, want) {
			t.Fatalf("the fixture lacks %q with Short off:\n%s", want, off)
		}
	}
}

// Short is the off comment minus the Files list and the areas that hold only
// passed and n/a checks.
func TestShortRemovesNothingElse(t *testing.T) {
	skills, files := shortFixture()
	off := render.Comment(skills, render.Options{ChangedFiles: files})
	want := passedAreasRe.ReplaceAllString(off, "")
	want = filesBlockRe.ReplaceAllString(want, "")
	if want == off {
		t.Fatal("the cut removed nothing")
	}
	got := render.Comment(skills, render.Options{ChangedFiles: files, Short: true})
	if got != want {
		t.Errorf("Short differs from the off comment minus the two blocks:\n got %q\nwant %q", got, want)
	}
	if !strings.HasSuffix(got, render.FindingsBlock(skills)) {
		t.Error("the hidden findings block is missing")
	}
}

func TestShortKeepsTheMetaLine(t *testing.T) {
	skills, files := shortFixture()
	got := render.Comment(skills, render.Options{ChangedFiles: files, Short: true})
	for _, want := range []string{"2 files", "1 of 2 checks passed"} {
		if !strings.Contains(got, want) {
			t.Errorf("meta line lacks %q:\n%s", want, got)
		}
	}
}

func TestShortKeepsWhatMatters(t *testing.T) {
	skills, files := shortFixture()
	got := render.Comment(skills, render.Options{ChangedFiles: files, Short: true})
	for _, want := range []string{
		"#### Safety", "No panics", "failed detail", "- **Batch 1 of 2:** Checked.",
		"**Review failed:** Timed out waiting for the model.",
		"Nil map write", "Extract helper",
		"Dismissed (1)", "Rename var",
		"<!-- pergent:findings ",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("Short comment lacks %q:\n%s", want, got)
		}
	}
}

func TestShortAllChecksPassed(t *testing.T) {
	skills, files := shortFixture()
	skills[0].Checks = []render.BatchChecks{{Batch: 1, Checks: []render.Check{
		{Name: "One", Status: render.CheckPass},
		{Name: "Two", Status: render.CheckPass},
		{Name: "Three", Status: render.CheckNA},
	}}}
	got := render.Comment(skills, render.Options{ChangedFiles: files, Short: true})
	for _, banned := range []string{"| Check |", "omitted", "One", "Three"} {
		if strings.Contains(got, banned) {
			t.Errorf("comment holds %q:\n%s", banned, got)
		}
	}
	if !strings.Contains(got, "2 of 2 checks passed") {
		t.Errorf("meta line lacks its count:\n%s", got)
	}
}

// shortInputs are the inputs the size-limit sweeps run over.
func shortInputs(t *testing.T) map[string]goldenInput {
	t.Helper()
	ins := map[string]goldenInput{}
	for _, name := range []string{"checks-big", "checks-multi", "big", "failures"} {
		var in goldenInput
		readJSON(t, filepath.Join("testdata", "golden", name+".json"), &in)
		ins[name] = in
	}
	skills, files := shortFixture()
	ins["fixture"] = goldenInput{Skills: skills, ChangedFiles: files}
	return ins
}

func TestShortNotesAtEverySizeLimit(t *testing.T) {
	for name, in := range shortInputs(t) {
		t.Run(name, func(t *testing.T) {
			full := len(render.Comment(in.Skills, render.Options{ChangedFiles: in.ChangedFiles}))
			for maxLen := 200; maxLen <= full; maxLen += 97 {
				got := render.Comment(in.Skills, render.Options{MaxLen: maxLen, ChangedFiles: in.ChangedFiles, Short: true})
				for _, banned := range []string{"Passed and n/a checks omitted", "not listed to fit", "<summary>Files (", "| ✅ |", "| ➖ |"} {
					if strings.Contains(got, banned) {
						t.Fatalf("maxLen %d: comment holds %q:\n%s", maxLen, banned, got)
					}
				}
				if len(got) > maxLen {
					t.Fatalf("maxLen %d: comment is %d bytes", maxLen, len(got))
				}
			}
		})
	}
}

func TestShortNoTablesNoteWithoutFailedChecks(t *testing.T) {
	skills, files := shortFixture()
	skills[0].Checks = []render.BatchChecks{{Batch: 1, Checks: []render.Check{
		{Name: "One", Status: render.CheckPass, Detail: strings.Repeat("p", 300)},
		{Name: "Two", Status: render.CheckNA, Detail: strings.Repeat("n", 300)},
	}}}
	opts := render.Options{ChangedFiles: files, Short: true}
	// fit is the smallest limit the comment fits without being cut. Below it
	// every step has run, including the ones that touch the checks.
	fit := len(render.Comment(skills, opts))
	for ; ; fit-- {
		opts.MaxLen = fit - 1
		if strings.Contains(render.Comment(skills, opts), "Comment truncated") {
			break
		}
	}
	for maxLen := fit; maxLen > fit-300; maxLen-- {
		opts.MaxLen = maxLen
		got := render.Comment(skills, opts)
		if strings.Contains(got, "Check tables omitted") {
			t.Fatalf("maxLen %d: a note for tables that were never there:\n%s", maxLen, got)
		}
		if len(got) > maxLen {
			t.Fatalf("maxLen %d: comment is %d bytes", maxLen, len(got))
		}
	}
}

func TestShortTablesNoteWhenAFailedTableIsCut(t *testing.T) {
	skills, files := shortFixture()
	skills[0].Checks[0].Checks[2].Detail = strings.Repeat("f", 2000)
	opts := render.Options{ChangedFiles: files, Short: true}
	opts.MaxLen = len(render.Comment(skills, opts)) - 1500
	got := render.Comment(skills, opts)
	if len(got) > opts.MaxLen {
		t.Fatalf("comment is %d bytes over the limit %d", len(got), opts.MaxLen)
	}
	if !strings.Contains(got, "Check tables omitted") {
		t.Errorf("no note for the cut failed table:\n%s", got)
	}
	if !strings.Contains(got, "1 of 2 checks passed") {
		t.Errorf("meta line lost its count:\n%s", got)
	}
}

func TestShortKeepsBlockingAndFailuresAtEveryStep(t *testing.T) {
	skills, files := shortFixture()
	full := len(render.Comment(skills, render.Options{ChangedFiles: files}))
	for maxLen := 200; maxLen <= full; maxLen += 97 {
		got := render.Comment(skills, render.Options{MaxLen: maxLen, ChangedFiles: files, Short: true})
		if strings.Contains(got, "Comment truncated") {
			continue
		}
		for _, want := range []string{"Nil map write", "**Review failed:** Timed out waiting for the model."} {
			if !strings.Contains(got, want) {
				t.Fatalf("maxLen %d: comment lacks %q:\n%s", maxLen, want, got)
			}
		}
	}
}
