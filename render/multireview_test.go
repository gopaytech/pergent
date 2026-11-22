package render_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gopaytech/pergent/render"
)

func multiReviewNames() *render.MultiReviewNames {
	return &render.MultiReviewNames{
		Reviewers: []string{"deepseek", "qwen", "kimi"},
		Judge:     "qwen",
		Skipped:   []render.SkippedReviewer{{Name: "glm", Reason: "out of tokens"}},
	}
}

func multiReviewSkill() []render.Skill {
	return []render.Skill{{
		Name: "code-review", Verdict: render.VerdictHasBlockingIssues, Confidence: 3,
		Reviewed:    []string{"a.go", "b.go", "c.go", "d.go", "e.go"},
		MultiReview: &render.SkillMultiReview{Reviewers: []int{1, 2, 3}},
		Findings: []render.Finding{
			{ID: "f1", Fingerprint: "fp1", File: "a.go", Line: 1, Severity: render.Blocking, Title: "Two found this",
				Explanation: "Seen twice.", MultiReview: &render.FindingMultiReview{FoundBy: []int{1, 2}, Also: []string{"fpx"}}},
			{ID: "f2", Fingerprint: "fp2", File: "b.go", Line: 2, Severity: render.Blocking, Title: "No answer",
				Explanation: "Not checked.", MultiReview: &render.FindingMultiReview{FoundBy: []int{3}, Unjudged: true}},
			{ID: "f3", Fingerprint: "fp3", File: "c.go", Line: 3, Severity: render.Suggestion, Title: "Demoted",
				Explanation: "Doubtful.", MultiReview: &render.FindingMultiReview{FoundBy: []int{1}, JudgeDisagreed: true, Reason: "@someone says <b>no</b> here"}},
			{ID: "f4", Fingerprint: "fp4", File: "d.go", Line: 4, Severity: render.Blocking, Title: "Mixed ratings",
				Explanation: "Unsure.", MultiReview: &render.FindingMultiReview{FoundBy: []int{2}, RatedSuggestionBy: []int{1, 3}}},
			{ID: "f5", Fingerprint: "fp5", File: "e.go", Line: 5, Severity: render.Blocking, Title: "All found this",
				Explanation: "Seen by all.", MultiReview: &render.FindingMultiReview{FoundBy: []int{1, 2, 3}}},
		},
	}}
}

func multiReviewOneSkill() []render.Skill {
	return []render.Skill{{
		Name: "code-review", Verdict: render.VerdictHasBlockingIssues, Confidence: 3,
		Reviewed:    []string{"a.go"},
		MultiReview: &render.SkillMultiReview{Reviewers: []int{2}, Failed: []int{1, 3}, Exact: true},
		Findings: []render.Finding{
			{ID: "f1", Fingerprint: "fp1", File: "a.go", Line: 1, Severity: render.Blocking, Title: "One reviewer found this",
				Explanation: "Seen once.", MultiReview: &render.FindingMultiReview{FoundBy: []int{2}}},
		},
	}}
}

// oneFinding is a skill with one blocking finding and the given multi review.
func oneFinding(name string, s *render.SkillMultiReview, f *render.FindingMultiReview) render.Skill {
	return render.Skill{
		Name: name, Verdict: render.VerdictHasBlockingIssues, Confidence: 3,
		Reviewed:    []string{"a.go"},
		MultiReview: s,
		Findings: []render.Finding{
			{ID: "f1", Fingerprint: "fp-" + name, File: "a.go", Line: 1, Severity: render.Blocking, Title: "A finding",
				Explanation: "Text.", MultiReview: f},
		},
	}
}

func checkMultiReviewGolden(t *testing.T, name string, got string) {
	t.Helper()
	path := filepath.Join("testdata", "golden", name)
	if *updateGoldens {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if want := readFile(t, path); got != want {
		t.Errorf("comment differs from %s:\n got:\n%s\nwant:\n%s", path, got, want)
	}
}

// metaLine returns the <sub> line that holds the multi review part.
func metaLine(t *testing.T, c string) string {
	t.Helper()
	for _, l := range strings.Split(c, "\n") {
		if strings.HasPrefix(l, "<sub>") && strings.Contains(l, "Multi review:") {
			return l
		}
	}
	t.Fatalf("no meta line with the multi review part:\n%s", c)
	return ""
}

func TestCommentMultiReview(t *testing.T) {
	got := render.Comment(multiReviewSkill(), render.Options{MultiReview: multiReviewNames()})
	checkMultiReviewGolden(t, "multi-review-0.md", got)
}

func TestCommentMultiReviewOne(t *testing.T) {
	got := render.Comment(multiReviewOneSkill(), render.Options{MultiReview: multiReviewNames()})
	checkMultiReviewGolden(t, "multi-review-one-0.md", got)
	for _, want := range []string{
		"> Multi review: deepseek could not review this.\n",
		"> Multi review: kimi could not review this.\n",
		"> Multi review: the judge did not run. Every blocking finding stays blocking.\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "found by") {
		t.Errorf("found by with one reviewer:\n%s", got)
	}
}

func TestCommentMultiReviewNamesByNumber(t *testing.T) {
	s := oneFinding("code-review", &render.SkillMultiReview{Reviewers: []int{1, 3}, Failed: []int{2}},
		&render.FindingMultiReview{FoundBy: []int{1, 3}})
	got := render.Comment([]render.Skill{s}, render.Options{MultiReview: &render.MultiReviewNames{Reviewers: []string{"a", "b", "c"}, Judge: "j"}})
	if !strings.Contains(got, " · found by 2 of 2 reviewers (a, c)") {
		t.Errorf("row suffix missing:\n%s", got)
	}
	if !strings.Contains(got, "> Multi review: b could not review this.\n") {
		t.Errorf("failed note missing:\n%s", got)
	}
}

func TestCommentMultiReviewMissingName(t *testing.T) {
	s := oneFinding("code-review", &render.SkillMultiReview{Reviewers: []int{1, 2}},
		&render.FindingMultiReview{FoundBy: []int{2}})
	got := render.Comment([]render.Skill{s}, render.Options{MultiReview: &render.MultiReviewNames{Reviewers: []string{"a"}}})
	if !strings.Contains(got, " · found by reviewer 2</summary>") && !strings.Contains(got, " · found by reviewer 2 ·") {
		t.Errorf("no number for the missing name:\n%s", got)
	}
	// An empty name counts as missing too.
	got = render.Comment([]render.Skill{s}, render.Options{MultiReview: &render.MultiReviewNames{Reviewers: []string{"a", ""}}})
	if !strings.Contains(got, " · found by reviewer 2") {
		t.Errorf("no number for the empty name:\n%s", got)
	}
	if !strings.Contains(metaLine(t, got), "Multi review: a, reviewer 2") {
		t.Errorf("no number for the empty name in the meta line:\n%s", got)
	}
}

func TestCommentMultiReviewNoJudgeName(t *testing.T) {
	s := oneFinding("code-review", &render.SkillMultiReview{Reviewers: []int{1, 2}},
		&render.FindingMultiReview{FoundBy: []int{1, 2}})
	got := render.Comment([]render.Skill{s}, render.Options{MultiReview: &render.MultiReviewNames{Reviewers: []string{"a", "b"}}})
	line := metaLine(t, got)
	if !strings.Contains(line, "Multi review: a, b</sub>") || strings.Contains(line, "judge") {
		t.Errorf("meta part: %q", line)
	}
	got = render.Comment([]render.Skill{s}, render.Options{MultiReview: &render.MultiReviewNames{Reviewers: []string{"a", "b"}, Judge: "j"}})
	if !strings.Contains(metaLine(t, got), "Multi review: a, b · judge j</sub>") {
		t.Errorf("meta part with a judge: %q", metaLine(t, got))
	}
}

func TestCommentMultiReviewMetaBeforeMetaNote(t *testing.T) {
	s := oneFinding("code-review", &render.SkillMultiReview{Reviewers: []int{1, 2}},
		&render.FindingMultiReview{FoundBy: []int{1, 2}})
	got := render.Comment([]render.Skill{s, {Name: "other", Verdict: render.VerdictFailed}},
		render.Options{MetaNote: "the note", MultiReview: &render.MultiReviewNames{Reviewers: []string{"a", "b"}, Judge: "j"}})
	line := metaLine(t, got)
	if !strings.Contains(line, "1 skill failed · Multi review: a, b · judge j · the note</sub>") {
		t.Errorf("meta part order: %q", line)
	}
}

func TestCommentNoMultiReviewNamesWritesNothing(t *testing.T) {
	for _, skills := range [][]render.Skill{multiReviewSkill(), multiReviewOneSkill()} {
		got := render.Comment(skills, render.Options{})
		for _, bad := range []string{"Multi review", "found by", "judge", "rated it", "could not review"} {
			if strings.Contains(got, bad) {
				t.Errorf("%q without Options.MultiReview:\n%s", bad, got)
			}
		}
	}
}

func TestMultiReviewReasonIsInert(t *testing.T) {
	got := render.Comment(multiReviewSkill(), render.Options{MultiReview: multiReviewNames()})
	if strings.Contains(got, "@someone") || strings.Contains(got, "<b>no") {
		t.Errorf("reason is not inert:\n%s", got)
	}
	if !strings.Contains(got, "@\u200dsomeone") || !strings.Contains(got, "<\u200db>") {
		t.Errorf("no joiner after @ or <:\n%s", got)
	}

	s := multiReviewSkill()
	s[0].Findings[2].MultiReview.Reason = "first\nsecond " + strings.Repeat("x", 1000)
	got = render.Comment(s, render.Options{MultiReview: multiReviewNames()})
	var line string
	for _, l := range strings.Split(got, "\n") {
		if strings.HasPrefix(l, "_judge disagreed: ") {
			line = l
		}
	}
	reason := strings.TrimSuffix(strings.TrimPrefix(line, "_judge disagreed: "), "_")
	if line == "" || !strings.HasSuffix(line, "_") || !strings.HasPrefix(reason, "first second x") || len(reason) != 300 {
		t.Errorf("reason not one line of 300 bytes (%d): %q", len(reason), line)
	}
	if strings.Contains(got, "\nsecond") {
		t.Errorf("reason split across lines:\n%s", got)
	}
}

func TestMultiReviewNamesAreInert(t *testing.T) {
	s := oneFinding("code-review", &render.SkillMultiReview{Reviewers: []int{1, 2}, Failed: []int{3}},
		&render.FindingMultiReview{FoundBy: []int{1}, RatedSuggestionBy: []int{2}})
	got := render.Comment([]render.Skill{s}, render.Options{MultiReview: &render.MultiReviewNames{
		Reviewers: []string{"@one", "<b>two</b>", "@three\nx"},
		Judge:     "@judge",
		Skipped:   []render.SkippedReviewer{{Name: "@four", Reason: "@five <i>"}},
	}})
	for _, bad := range []string{"@one", "<b>two", "@three", "@judge", "@four", "@five", "<i>"} {
		if strings.Contains(got, bad) {
			t.Errorf("%q is not inert:\n%s", bad, got)
		}
	}
	if !strings.Contains(got, "> Multi review: @\u200dthree x could not review this.\n") {
		t.Errorf("name not on one line:\n%s", got)
	}
}

func TestMultiReviewLeadsWithNoReason(t *testing.T) {
	s := oneFinding("code-review", &render.SkillMultiReview{Reviewers: []int{1, 2}},
		&render.FindingMultiReview{FoundBy: []int{1}, JudgeDisagreed: true, Reason: "  "})
	got := render.Comment([]render.Skill{s}, render.Options{MultiReview: &render.MultiReviewNames{
		Reviewers: []string{"a", "b"},
		Skipped:   []render.SkippedReviewer{{Name: "c"}},
	}})
	if !strings.Contains(got, "\n_judge disagreed_\n") {
		t.Errorf("no plain disagreed lead:\n%s", got)
	}
	if !strings.Contains(got, "> Multi review: c was skipped.\n") {
		t.Errorf("no plain skipped note:\n%s", got)
	}
}

func TestMultiReviewNotesHavePrefixWithTwoSkills(t *testing.T) {
	skills := []render.Skill{
		oneFinding("one", &render.SkillMultiReview{Reviewers: []int{1, 2}, Failed: []int{3}},
			&render.FindingMultiReview{FoundBy: []int{1, 2}}),
		oneFinding("two", &render.SkillMultiReview{Reviewers: []int{1}, Exact: true},
			&render.FindingMultiReview{FoundBy: []int{1}}),
	}
	skills[0].Notes = []string{"A note of its own."}
	got := render.Comment(skills, render.Options{MultiReview: multiReviewNames()})
	skipped := "> Multi review: glm was skipped: out of tokens.\n"
	own := "> `one`: A note of its own.\n"
	failed := "> `one`: Multi review: kimi could not review this.\n"
	exact := "> `two`: Multi review: the judge did not run. Every blocking finding stays blocking.\n"
	for _, want := range []string{skipped, own, failed, exact} {
		if strings.Count(got, want) != 1 {
			t.Errorf("want %q once:\n%s", want, got)
		}
	}
	if !(strings.Index(got, skipped) < strings.Index(got, own) && strings.Index(got, own) < strings.Index(got, failed) &&
		strings.Index(got, failed) < strings.Index(got, exact)) {
		t.Errorf("notes out of order:\n%s", got)
	}
	if strings.Contains(got, "`one`: Multi review: glm") || strings.Contains(got, "`two`: Multi review: glm") {
		t.Errorf("skipped note has a skill prefix:\n%s", got)
	}
}

// summaryLines returns the <summary> lines of the comment.
func summaryLines(c string) []string {
	var out []string
	for _, l := range strings.Split(c, "\n") {
		if strings.Contains(l, "<summary>") {
			out = append(out, l)
		}
	}
	return out
}

// hostileNameComment is a comment whose reviewer 1 has the given name, with
// a finding that reviewer found alone and one it found with reviewer 2.
func hostileNameComment(name string) string {
	s := oneFinding("code-review", &render.SkillMultiReview{Reviewers: []int{1, 2}, Failed: []int{1}},
		&render.FindingMultiReview{FoundBy: []int{1}, RatedSuggestionBy: []int{1}})
	s.Findings = append(s.Findings, render.Finding{ID: "f2", Fingerprint: "fp2", File: "b.go", Line: 2,
		Severity: render.Blocking, Title: "B", MultiReview: &render.FindingMultiReview{FoundBy: []int{1, 2}}})
	return render.Comment([]render.Skill{s}, render.Options{MultiReview: &render.MultiReviewNames{
		Reviewers: []string{name, "b"}, Judge: name,
		Skipped: []render.SkippedReviewer{{Name: name, Reason: name}},
	}})
}

// A name with a code span is not Markdown in the <summary> line, which is
// HTML: it must be escaped there like the title.
func TestMultiReviewNameInSummaryIsEscaped(t *testing.T) {
	got := hostileNameComment("x `</summary></details><img src=a>` y")
	// A Markdown line keeps the name's code span, where a tag is code, not
	// HTML; the layout tags in it are still escaped, as in skillRef.
	for _, l := range strings.Split(got, "\n") {
		if strings.Contains(l, "<img") && (strings.Contains(l, "<summary>") ||
			strings.Count(l, "<img") != strings.Count(l, "`&lt;/summary>&lt;/details><img src=a>`")) {
			t.Errorf("live <img: %q", l)
		}
	}
	if n, m := strings.Count(got, "</summary>"), strings.Count(got, "<summary>"); n != m {
		t.Errorf("%d </summary> for %d <summary>:\n%s", n, m, got)
	}
	if strings.Count(got, "</details>") != strings.Count(got, "<details") {
		t.Errorf("a </details> with no <details:\n%s", got)
	}
	var found int
	for _, l := range summaryLines(got) {
		if !strings.Contains(l, "found by") {
			continue
		}
		found++
		if !strings.Contains(l, "x `&lt;/summary&gt;&lt;/details&gt;&lt;img src=a&gt;` y") {
			t.Errorf("name not escaped in the summary line: %q", l)
		}
	}
	if found != 2 {
		t.Errorf("want 2 found by rows, got %d:\n%s", found, got)
	}
}

func TestMultiReviewNameCommentIsInert(t *testing.T) {
	got := hostileNameComment("x `<!--` y")
	// Only the comment's own 2: the marker at the top and the findings block.
	if n := strings.Count(got, "<!--"); n != 2 {
		t.Errorf("%d <!-- in the comment, want 2:\n%s", n, got)
	}
	for _, l := range summaryLines(got) {
		if strings.Contains(l, "found by") && !strings.Contains(l, "x `&lt;!--` y") {
			t.Errorf("name not escaped in the summary line: %q", l)
		}
	}
}

func TestMultiReviewNameInSummaryBreaksMentions(t *testing.T) {
	got := hostileNameComment("@someone")
	if strings.Contains(got, "@someone") {
		t.Errorf("raw mention:\n%s", got)
	}
	for _, l := range summaryLines(got) {
		if strings.Contains(l, "found by") && !strings.Contains(l, "@\u200dsomeone") {
			t.Errorf("no joiner in the summary line: %q", l)
		}
	}
}

func TestMultiReviewNoReviewerNamesWritesNoMetaPart(t *testing.T) {
	s := oneFinding("code-review", &render.SkillMultiReview{Reviewers: []int{1, 2}},
		&render.FindingMultiReview{FoundBy: []int{1, 2}})
	got := render.Comment([]render.Skill{s}, render.Options{MultiReview: &render.MultiReviewNames{Judge: "j"}})
	if strings.Contains(got, "<sub>") && strings.Contains(got, "Multi review:") && strings.Contains(got, "judge j") {
		t.Errorf("meta part with no names:\n%s", got)
	}
}

func TestMultiReviewSkippedReasonEndsWithOneDot(t *testing.T) {
	s := oneFinding("code-review", &render.SkillMultiReview{Reviewers: []int{1}},
		&render.FindingMultiReview{FoundBy: []int{1}})
	got := render.Comment([]render.Skill{s}, render.Options{MultiReview: &render.MultiReviewNames{
		Reviewers: []string{"a"}, Skipped: []render.SkippedReviewer{{Name: "c", Reason: "out of tokens."}},
	}})
	if !strings.Contains(got, "> Multi review: c was skipped: out of tokens.\n") {
		t.Errorf("skipped note:\n%s", got)
	}
}
