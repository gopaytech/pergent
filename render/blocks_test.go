package render

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// lay parses a layout, failing the test when it is not valid.
func lay(t *testing.T, entries ...string) []Block {
	t.Helper()
	blocks, err := ParseLayout(entries)
	if err != nil {
		t.Fatalf("ParseLayout(%v): %v", entries, err)
	}
	return blocks
}

// layoutSkill is one skill with a blocking finding, a suggestion, a dismissed
// finding, a summary and four checks: 3 of 4 passed, 1 n/a and 1 failed.
func layoutSkill(name string) Skill {
	return Skill{
		Name: name, Verdict: VerdictHasBlockingIssues,
		Findings: []Finding{
			{File: "a.go", Line: 3, Severity: Blocking, Title: "Nil map write", Explanation: "It panics."},
			{File: "b.go", Line: 9, Severity: Suggestion, Title: "Extract helper", Explanation: "Two copies."},
		},
		Dismissed: []DismissedFinding{{
			Finding: Finding{File: "b.go", Line: 1, Severity: Suggestion, Title: "Rename var"},
			By:      "sam", Reason: "on purpose",
		}},
		Reviewed: []string{"a.go", "b.go"},
		Batches:  1, BatchesFinished: 1,
		Summaries: []Summary{{Batch: 1, Text: "Checked the handler."}},
		Checks: []BatchChecks{{Batch: 1, Findings: 1, Checks: []Check{
			chk("Tests", "Unit tests ran", CheckPass),
			chk("Tests", "Lint ran", CheckPass),
			chk("Docs", "Docs updated", CheckNA),
			chk("Docs", "Changelog", CheckPass),
			chk("Safety", "No panics", CheckFail),
		}}},
	}
}

var layoutFiles = []string{"a.go", "b.go"}

func TestParseLayout(t *testing.T) {
	bad := []struct {
		name    string
		entries []string
		names   string // text the error must hold
	}{
		{"unknown block", []string{"findings:open", "colour:open"}, "colour:open"},
		{"unknown state", []string{"findings:shut"}, "findings:shut"},
		{"duplicate", []string{"findings:open", "files:open", "files:collapsed"}, "files:collapsed"},
		{"empty", nil, "empty"},
		{"no findings", []string{"summary:open", "checks:open"}, "findings"},
		{"no state", []string{"findings"}, `"findings"`},
	}
	for _, tt := range bad {
		t.Run(tt.name, func(t *testing.T) {
			blocks, err := ParseLayout(tt.entries)
			if err == nil {
				t.Fatalf("ParseLayout(%v) = %v, want an error", tt.entries, blocks)
			}
			if !strings.Contains(err.Error(), tt.names) {
				t.Errorf("error %q does not name %q", err, tt.names)
			}
		})
	}

	entries := []string{"checks:open", "findings:collapsed", "files:open", "summary:collapsed", "dismissed:collapsed"}
	blocks, err := ParseLayout(entries)
	if err != nil {
		t.Fatal(err)
	}
	for i, b := range blocks {
		if b.String() != entries[i] {
			t.Errorf("block %d is %q, want %q", i, b, entries[i])
		}
	}
	if len(blocks) != len(entries) {
		t.Errorf("got %d blocks, want %d", len(blocks), len(entries))
	}
}

func TestDefaultLayoutIsTheNilLayout(t *testing.T) {
	want := []string{"summary:open", "findings:collapsed", "dismissed:collapsed", "checks:open", "files:collapsed"}
	var got []string
	for _, b := range DefaultLayout() {
		got = append(got, b.String())
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("DefaultLayout() = %v, want %v", got, want)
	}

	inputs, err := filepath.Glob(filepath.Join("testdata", "golden", "*.json"))
	if err != nil || len(inputs) == 0 {
		t.Fatalf("no golden inputs: %v", err)
	}
	for _, path := range inputs {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var in struct {
			Skills       []Skill
			ChangedFiles []string
		}
		if err := json.Unmarshal(data, &in); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		for _, maxLen := range []int{0, 2000, 6000} {
			o := Options{MaxLen: maxLen, ChangedFiles: in.ChangedFiles}
			plain := Comment(in.Skills, o)
			o.Layout = DefaultLayout()
			if got := Comment(in.Skills, o); got != plain {
				t.Errorf("%s at %d: the default layout differs from the nil layout", filepath.Base(path), maxLen)
			}
		}
	}
}

func TestLayoutOrder(t *testing.T) {
	o := Options{ChangedFiles: layoutFiles, Layout: lay(t, "checks:open", "findings:collapsed")}
	got := Comment([]Skill{layoutSkill("code-review")}, o)
	if !strings.HasPrefix(got, Marker+"\n### ❌ 1 blocking issue") {
		t.Errorf("the headline is not first:\n%s", got)
	}
	inOrder(t, got, "### ❌", "#### Safety", "#### Findings")
}

func TestFindingsOpen(t *testing.T) {
	skills := []Skill{layoutSkill("code-review")}
	open := Comment(skills, Options{Layout: lay(t, "findings:open")})
	for _, want := range []string{"<details open><summary>💡 <b>Extract helper</b>", "<details open><summary>❌ <b>Nil map write</b>"} {
		if !strings.Contains(open, want) {
			t.Errorf("findings:open lacks %q:\n%s", want, open)
		}
	}
	folded := Comment(skills, Options{Layout: lay(t, "findings:collapsed")})
	if !strings.Contains(folded, "<details><summary>💡 <b>Extract helper</b>") {
		t.Errorf("findings:collapsed opens the suggestion:\n%s", folded)
	}
	if !strings.Contains(folded, "<details open><summary>❌ <b>Nil map write</b>") {
		t.Errorf("findings:collapsed folds the blocking finding:\n%s", folded)
	}
}

// detailsOf returns the <details> block that starts with the given summary line.
func detailsOf(t *testing.T, s, summary string) string {
	t.Helper()
	i := strings.Index(s, "<details><summary>"+summary)
	if i < 0 {
		t.Fatalf("comment lacks a <details> labelled %q:\n%s", summary, s)
	}
	j := strings.Index(s[i:], "</details>")
	if j < 0 {
		t.Fatalf("the <details> labelled %q is not closed:\n%s", summary, s)
	}
	return s[i : i+j]
}

func TestSummaryCollapsed(t *testing.T) {
	folded := lay(t, "summary:collapsed", "findings:collapsed", "checks:open")

	t.Run("one summary", func(t *testing.T) {
		got := Comment([]Skill{layoutSkill("code-review")}, Options{Layout: folded})
		want := "<details><summary>Summary</summary>\n\n**Summary:** Checked the handler.\n\n</details>\n\n"
		if !strings.Contains(got, want) {
			t.Errorf("no folded summary %q:\n%s", want, got)
		}
		open := Comment([]Skill{layoutSkill("code-review")}, Options{Layout: lay(t, "summary:open", "findings:collapsed")})
		if strings.Contains(open, "<summary>Summary</summary>") || !strings.Contains(open, "**Summary:** Checked the handler.\n\n") {
			t.Errorf("summary:open is not plain text:\n%s", open)
		}
	})

	t.Run("batch list", func(t *testing.T) {
		s := layoutSkill("code-review")
		s.Batches, s.BatchesFinished = 3, 3
		s.Summaries = []Summary{{Batch: 1, Text: "First."}, {Batch: 3, Text: "Third."}}
		got := Comment([]Skill{s}, Options{Layout: folded})
		want := "<details><summary>Summary</summary>\n\n- **Batch 1 of 3:** First.\n- **Batch 3 of 3:** Third.\n\n</details>\n\n"
		if !strings.Contains(got, want) {
			t.Errorf("no folded batch list %q:\n%s", want, got)
		}
	})

	t.Run("skill sections", func(t *testing.T) {
		a, b := layoutSkill("alpha"), layoutSkill("beta")
		got := Comment([]Skill{a, b}, Options{Layout: folded})
		sections := regexp.MustCompile("(?s)#### `(alpha|beta)`\n\n<details><summary>Summary</summary>\n\n\\*\\*Summary:\\*\\* Checked the handler\\.\n\n</details>\n\n").FindAllString(got, -1)
		if len(sections) != 2 {
			t.Errorf("found %d folded summaries in the skill sections, want 2:\n%s", len(sections), got)
		}
	})
}

func TestChecksCollapsed(t *testing.T) {
	folded := lay(t, "summary:open", "findings:collapsed", "checks:collapsed")

	t.Run("one skill", func(t *testing.T) {
		got := Comment([]Skill{layoutSkill("code-review")}, Options{Layout: folded})
		label := "Checks: 3 of 4 passed, 1 n/a"
		inner := detailsOf(t, got, label+"</summary>")
		for _, want := range []string{"#### Tests", "Unit tests ran", "#### Docs", "Docs updated", "Changelog"} {
			if !strings.Contains(inner, want) {
				t.Errorf("the folded checks lack %q:\n%s", want, inner)
			}
		}
		if strings.Contains(inner, "No panics") || strings.Contains(inner, "Safety") {
			t.Errorf("a failed check is inside the fold:\n%s", inner)
		}
		inOrder(t, got, "#### Safety", "No panics", "<details><summary>"+label)
		if strings.Contains(got[:strings.Index(got, "<summary>"+label)], "Unit tests ran") {
			t.Errorf("a passed check is outside the fold:\n%s", got)
		}
	})

	t.Run("two skills", func(t *testing.T) {
		a := layoutSkill("alpha")
		b := layoutSkill("beta")
		b.Checks = []BatchChecks{{Batch: 1, Checks: []Check{chk("Tests", "Beta ran", CheckPass), chk("", "Beta loose", CheckNA)}}}
		got := Comment([]Skill{a, b}, Options{Layout: folded})
		inA := detailsOf(t, got, "Checks: 3 of 4 passed, 1 n/a</summary>")
		inB := detailsOf(t, got, "Checks: 1 of 1 passed, 1 n/a</summary>")
		if !strings.Contains(inA, "Unit tests ran") || strings.Contains(inA, "Beta ran") {
			t.Errorf("alpha's fold is wrong:\n%s", inA)
		}
		if !strings.Contains(inB, "Beta ran") || !strings.Contains(inB, "Beta loose") || strings.Contains(inB, "Unit tests ran") {
			t.Errorf("beta's fold is wrong:\n%s", inB)
		}
		// Each skill's summary stays outside its fold, under the skill's heading.
		if strings.Contains(inA, "**Summary:**") || strings.Contains(inB, "**Summary:**") {
			t.Errorf("a summary is inside a checks fold:\n%s\n%s", inA, inB)
		}
		inOrder(t, got, "#### `alpha`", "**Summary:** Checked the handler.", "No panics", "<summary>Checks: 3 of 4", "#### `beta`", "<summary>Checks: 1 of 1")
	})

	t.Run("nothing to fold", func(t *testing.T) {
		s := layoutSkill("code-review")
		s.Checks = []BatchChecks{{Batch: 1, Checks: []Check{chk("Safety", "No panics", CheckFail)}}}
		got := Comment([]Skill{s}, Options{Layout: folded})
		if strings.Contains(got, "<summary>Checks:") {
			t.Errorf("an empty fold was written:\n%s", got)
		}
	})
}

func TestDismissedAndFilesOpen(t *testing.T) {
	skills := []Skill{layoutSkill("code-review")}
	o := Options{ChangedFiles: layoutFiles, Layout: lay(t, "findings:collapsed", "dismissed:open", "files:open")}
	got := Comment(skills, o)
	for _, want := range []string{"<details open><summary>Dismissed (1)</summary>", "<details open><summary>Files (2)</summary>"} {
		if !strings.Contains(got, want) {
			t.Errorf("comment lacks %q:\n%s", want, got)
		}
	}
	o.Layout = lay(t, "findings:collapsed", "dismissed:collapsed", "files:collapsed")
	got = Comment(skills, o)
	for _, want := range []string{"<details><summary>Dismissed (1)</summary>", "<details><summary>Files (2)</summary>"} {
		if !strings.Contains(got, want) {
			t.Errorf("comment lacks %q:\n%s", want, got)
		}
	}
}

func TestLeftOutBlocks(t *testing.T) {
	skills := []Skill{layoutSkill("code-review")}
	o := Options{ChangedFiles: layoutFiles}
	cases := []struct {
		name   string
		layout []Block
		banned []string
	}{
		{"dismissed", lay(t, "summary:open", "findings:collapsed", "checks:open", "files:collapsed"), []string{"Dismissed (", "Rename var", "dismissed finding"}},
		{"files", lay(t, "summary:open", "findings:collapsed", "dismissed:collapsed", "checks:open"), []string{"Files (", "`a.go`"}},
		{"summary", lay(t, "findings:collapsed", "dismissed:collapsed", "checks:open", "files:collapsed"), []string{"Summary", "Checked the handler", "Summaries omitted"}},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			o := o
			o.Layout = tt.layout
			// Over every size step too: nothing was cut, so there is no note.
			outs := []string{Comment(skills, o)}
			for _, l := range shrinkSteps {
				outs = append(outs, render(skills, o, l))
			}
			for i, got := range outs {
				for _, banned := range tt.banned {
					if strings.Contains(got, banned) {
						t.Errorf("output %d holds %q:\n%s", i, banned, got)
					}
				}
				if strings.Contains(got, "omitted") && i == 0 {
					t.Errorf("an omitted note without a size limit:\n%s", got)
				}
			}
		})
	}

	t.Run("a clean headline still counts the dismissed", func(t *testing.T) {
		s := layoutSkill("code-review")
		s.Verdict, s.Findings = VerdictNoBlockingIssues, nil
		s.Dismissed = []DismissedFinding{{Finding: Finding{File: "a.go", Line: 3, Severity: Blocking, Title: "Nil map write"}, By: "sam", Reason: "ok"}}
		got := Comment([]Skill{s}, Options{Layout: lay(t, "findings:collapsed")})
		if !strings.Contains(got, "### ✅ No blocking issues (1 dismissed)") || strings.Contains(got, "Nil map write") {
			t.Errorf("headline wrong, or the dismissed list is there:\n%s", got)
		}
	})
}

func TestChecksLeftOutKeepsFailed(t *testing.T) {
	a, b := layoutSkill("alpha"), layoutSkill("beta")
	b.Checks = []BatchChecks{{Batch: 1, Checks: []Check{chk("Tests", "Beta ran", CheckPass)}}}
	got := Comment([]Skill{a, b}, Options{Layout: lay(t, "findings:collapsed")})
	inOrder(t, got, "#### Findings", "#### `alpha`", "##### Safety", "No panics")
	for _, banned := range []string{"Unit tests ran", "Docs updated", "Beta ran", "#### `beta`", "Checked the handler", "omitted"} {
		if strings.Contains(got, banned) {
			t.Errorf("comment holds %q:\n%s", banned, got)
		}
	}
}

func TestDetailsFallBack(t *testing.T) {
	t.Run("after a single summary", func(t *testing.T) {
		got := Comment([]Skill{layoutSkill("code-review")}, Options{Layout: lay(t, "summary:open", "findings:collapsed")})
		inOrder(t, got, "**Summary:** Checked the handler.", "#### Safety", "No panics", "#### Findings")
		if strings.Contains(got, "Unit tests ran") {
			t.Errorf("a passed check is shown:\n%s", got)
		}
	})
	t.Run("a batch list at summary", func(t *testing.T) {
		s := layoutSkill("code-review")
		s.Batches, s.BatchesFinished = 3, 3
		s.Summaries = []Summary{{Batch: 1, Text: "First."}, {Batch: 2, Text: "Second."}}
		got := Comment([]Skill{s}, Options{Layout: lay(t, "summary:open", "findings:collapsed")})
		inOrder(t, got, "- **Batch 1 of 3:** First.", "- **Batch 2 of 3:** Second.", "#### Safety", "#### Findings")
	})
	t.Run("after findings", func(t *testing.T) {
		got := Comment([]Skill{layoutSkill("code-review")}, Options{Layout: lay(t, "findings:collapsed", "dismissed:collapsed")})
		inOrder(t, got, "#### Findings", "#### Safety", "No panics", "Dismissed (1)")
		if strings.Contains(got, "Checked the handler") {
			t.Errorf("a summary is shown with summary left out:\n%s", got)
		}
	})
	t.Run("checks at its own place", func(t *testing.T) {
		got := Comment([]Skill{layoutSkill("code-review")}, Options{Layout: lay(t, "findings:collapsed", "checks:open", "summary:open")})
		inOrder(t, got, "#### Findings", "#### Safety", "**Summary:** Checked the handler.")
	})
}

func TestQuietWinsOverLayout(t *testing.T) {
	skills := []Skill{layoutSkill("code-review")}
	o := Options{ChangedFiles: layoutFiles, Short: true, Layout: lay(t, "findings:collapsed", "files:open", "checks:open")}
	for _, l := range append([]layout{{listLimit: -1}}, shrinkSteps...) {
		got := render(skills, o, l)
		for _, banned := range []string{"Files (", "Unit tests ran", "Docs updated", "#### Tests", "#### Docs"} {
			if strings.Contains(got, banned) {
				t.Errorf("quiet comment holds %q:\n%s", banned, got)
			}
		}
		if strings.Contains(got, "Passed and n/a") {
			t.Errorf("quiet comment holds a passed-checks note:\n%s", got)
		}
	}
	// Folded checks: only failed rows exist, so there is no fold.
	o.Layout = lay(t, "findings:collapsed", "checks:collapsed")
	if got := Comment(skills, o); strings.Contains(got, "<summary>Checks:") || !strings.Contains(got, "No panics") {
		t.Errorf("quiet with folded checks:\n%s", got)
	}
}

func TestInvalidLayoutFallsBack(t *testing.T) {
	skills := []Skill{layoutSkill("code-review")}
	base := Options{ChangedFiles: layoutFiles}
	want := Comment(skills, base)
	for name, bad := range map[string][]Block{
		"unknown block": {{"colour", StateOpen}, {BlockFindings, StateOpen}},
		"unknown state": {{BlockFindings, "shut"}},
		"duplicate":     {{BlockFindings, StateOpen}, {BlockFiles, StateOpen}, {BlockFiles, StateCollapsed}},
		"no findings":   {{BlockChecks, StateOpen}},
		"empty":         {},
	} {
		t.Run(name, func(t *testing.T) {
			o := base
			o.Layout = bad
			if got := Comment(skills, o); got != want {
				t.Errorf("an invalid layout changed the comment:\n%s", got)
			}
		})
	}
}

// bulkSkills is a review that is long at every part: blocking findings, many
// suggestions, many checks and many files.
func bulkSkills() ([]Skill, []string) {
	s := layoutSkill("code-review")
	var cs []Check
	var files []string
	for i := 0; i < 25; i++ {
		cs = append(cs, chk(fmt.Sprintf("Area %d", i%4), fmt.Sprintf("Check number %d", i), []string{CheckPass, CheckNA, CheckFail}[i%3]))
		s.Findings = append(s.Findings, Finding{File: fmt.Sprintf("f%d.go", i), Line: i + 1, Severity: Suggestion, Title: fmt.Sprintf("Suggestion %d", i), Explanation: strings.Repeat("Explained. ", 10)})
		files = append(files, fmt.Sprintf("dir/file%d.go", i))
	}
	s.Checks = []BatchChecks{{Batch: 1, Findings: 1, Checks: cs}}
	s.Reviewed = files
	return []Skill{s}, files
}

func TestCustomOrderAtEverySize(t *testing.T) {
	skills, files := bulkSkills()
	custom := Options{ChangedFiles: files, Layout: lay(t, "checks:open", "findings:collapsed")}
	def := Options{ChangedFiles: files}
	full := len(Comment(skills, custom))
	if n := len(Comment(skills, def)); n > full {
		full = n
	}
	for limit := 100; limit <= full+50; limit += 37 {
		o := custom
		o.MaxLen = limit
		got := Comment(skills, o)
		if len(got) > limit {
			t.Errorf("limit %d: the comment is %d bytes", limit, len(got))
		}
		d := def
		d.MaxLen = limit
		if strings.Contains(Comment(skills, d), "Nil map write") && !strings.Contains(got, "Nil map write") {
			t.Errorf("limit %d: the default layout keeps the blocking title and the custom one loses it:\n%s", limit, got)
		}
	}
}

func TestFailedCutLeavesItsNote(t *testing.T) {
	// A failed check with a long detail, so cutting its table saves bytes.
	long := Check{Section: "Safety", Name: "No panics", Status: CheckFail, Detail: strings.Repeat("evidence ", 60)}
	s := layoutSkill("code-review")
	s.Checks = []BatchChecks{{Batch: 1, Findings: 1, Checks: []Check{chk("Tests", "Unit tests ran", CheckPass), long}}}
	skills := []Skill{s}
	o := Options{Layout: lay(t, "findings:collapsed")}

	// One byte under what step 4 needs: step 5 is the first that fits.
	o.MaxLen = len(render(skills, o, shrinkSteps[3])) - 1
	got := Comment(skills, o)
	if len(got) > o.MaxLen || !strings.Contains(got, "Check tables omitted") || strings.Contains(got, "No panics") {
		t.Errorf("a failed table cut at step 5 left no note, or was kept (%d of %d bytes):\n%s", len(got), o.MaxLen, got)
	}

	// Nothing failed and nothing was cut on purpose: no note at any size.
	s.Checks = []BatchChecks{{Batch: 1, Checks: []Check{chk("Tests", "Unit tests ran", CheckPass)}}}
	skills = []Skill{s}
	for _, o := range []Options{{Short: true, Layout: lay(t, "findings:collapsed", "checks:open")}, {Layout: lay(t, "findings:collapsed")}} {
		for i, l := range shrinkSteps {
			if got := render(skills, o, l); strings.Contains(got, "Check tables omitted") || strings.Contains(got, "Passed and n/a") {
				t.Errorf("step %d: a note about checks nobody wrote:\n%s", i+1, got)
			}
		}
	}
}
