package render

import (
	"encoding/base64"
	"reflect"
	"strings"
	"testing"
)

func dismissSkills() []Skill {
	return []Skill{
		{
			Name: "code-review", Verdict: VerdictHasBlockingIssues,
			Findings: []Finding{
				{ID: "aaaaaa", Fingerprint: "aaaaaa111111", File: "a.go", Line: 1, Severity: Blocking, Title: "Nil deref", Explanation: "boom"},
				{ID: "bbbbbb", Fingerprint: "bbbbbb111111", File: "b.go", Line: 2, Severity: Suggestion, Title: "Rename", Explanation: "nit"},
			},
		},
		{Name: "docs", Verdict: VerdictFailed, Findings: []Finding{
			{ID: "cccccc", Fingerprint: "cccccc111111", File: "c.md", Severity: Blocking, Title: "Wrong", Explanation: "e"},
		}},
	}
}

func TestApplyDismissalsSeverity(t *testing.T) {
	// A suggestion dismissal does not cover a blocking finding.
	got := ApplyDismissals(dismissSkills(), []Dismissal{{Fingerprint: "aaaaaa111111", Severity: "suggestion", By: "alice", Reason: "no"}})
	if len(got[0].Findings) != 2 || len(got[0].Dismissed) != 0 || got[0].Verdict != VerdictHasBlockingIssues {
		t.Errorf("a suggestion dismissal covered a blocking finding: %+v", got[0])
	}

	// A blocking dismissal covers a blocking finding and a suggestion.
	got = ApplyDismissals(dismissSkills(), []Dismissal{
		{Fingerprint: "aaaaaa111111", Severity: "blocking", By: "alice", Reason: "intended"},
		{Fingerprint: "bbbbbb111111", Severity: "blocking", By: "bob", Reason: "fine"},
	})
	if len(got[0].Findings) != 0 || len(got[0].Dismissed) != 2 {
		t.Fatalf("blocking dismissals left %d findings, dismissed %d", len(got[0].Findings), len(got[0].Dismissed))
	}
	if d := got[0].Dismissed[0]; d.Finding.ID != "aaaaaa" || d.By != "alice" || d.Reason != "intended" {
		t.Errorf("dismissed = %+v", d)
	}
	if got[0].Verdict != VerdictNoBlockingIssues {
		t.Errorf("verdict = %q, want no_blocking_issues", got[0].Verdict)
	}

	// A suggestion dismissal covers a suggestion; the verdict stays.
	got = ApplyDismissals(dismissSkills(), []Dismissal{{Fingerprint: "bbbbbb111111", Severity: "suggestion", By: "bob"}})
	if len(got[0].Findings) != 1 || len(got[0].Dismissed) != 1 || got[0].Verdict != VerdictHasBlockingIssues {
		t.Errorf("suggestion dismissal: %+v", got[0])
	}

	// An unknown severity covers nothing.
	got = ApplyDismissals(dismissSkills(), []Dismissal{{Fingerprint: "bbbbbb111111", Severity: "whatever"}})
	if len(got[0].Dismissed) != 0 {
		t.Errorf("unknown severity dismissed a finding: %+v", got[0])
	}
}

func TestApplyDismissalsVerdictAndInput(t *testing.T) {
	in := dismissSkills()
	snapshot := dismissSkills()
	got := ApplyDismissals(in, []Dismissal{
		{Fingerprint: "aaaaaa111111", Severity: "blocking", By: "a"},
		{Fingerprint: "cccccc111111", Severity: "blocking", By: "a"},
	})
	if got[1].Verdict != VerdictFailed || len(got[1].Dismissed) != 1 {
		t.Errorf("a failed skill must stay failed: %+v", got[1])
	}
	if !reflect.DeepEqual(in, snapshot) {
		t.Error("ApplyDismissals modified its input")
	}

	// Applying again changes nothing; a finding without a fingerprint is never covered.
	again := ApplyDismissals(got, []Dismissal{{Fingerprint: "", Severity: "blocking"}})
	if !reflect.DeepEqual(again, got) {
		t.Error("a second application changed the skills")
	}

	// No dismissals: the skills come back as they were.
	if same := ApplyDismissals(in, nil); !reflect.DeepEqual(same, in) {
		t.Error("nil dismissals changed the skills")
	}
}

func TestDismissedSection(t *testing.T) {
	skills := ApplyDismissals(dismissSkills(), []Dismissal{
		{Fingerprint: "aaaaaa111111", Severity: "blocking", By: "alice", Reason: "intended behaviour"},
		{Fingerprint: "bbbbbb111111", Severity: "suggestion", By: "bob", Reason: "later"},
	})
	got := Comment(skills, Options{})

	for _, want := range []string{
		"\u2705 **No blocking issues** (1 dismissed)\n",
		"<details><summary>Dismissed (2)</summary>\n\n",
		"- **Nil deref** `aaaaaa` \u2014 dismissed by alice: intended behaviour\n",
		"- **Rename** `bbbbbb` \u2014 dismissed by bob: later\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("comment lacks %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "**`a.go:1`**") {
		t.Errorf("a dismissed finding is shown in full:\n%s", got)
	}
	if strings.Contains(got, "<details><summary>Suggestions") {
		t.Errorf("an empty Suggestions block is shown:\n%s", got)
	}
	// Dismissed findings are not carried to the next run.
	if strings.Contains(decodeBlock(t, FindingsBlock(skills)), "Nil deref") {
		t.Error("the findings block carries a dismissed finding")
	}

	// It comes after the suggestions.
	mixed := ApplyDismissals(dismissSkills(), []Dismissal{{Fingerprint: "bbbbbb111111", Severity: "suggestion", By: "bob"}})
	mixed[0].Findings = append(mixed[0].Findings, Finding{File: "z.go", Severity: Suggestion, Title: "Other"})
	text := Comment(mixed, Options{})
	sug, dis := strings.Index(text, "<summary>Suggestions (1)"), strings.Index(text, "<summary>Dismissed (1)")
	if sug < 0 || dis < sug {
		t.Errorf("want Suggestions before Dismissed:\n%s", text)
	}
	if !strings.Contains(text, "- **Rename** `bbbbbb` \u2014 dismissed by bob\n") {
		t.Errorf("a dismissal without a reason:\n%s", text)
	}
	if strings.Contains(text, "No blocking issues") {
		t.Errorf("a skill that still blocks shows the passing line:\n%s", text)
	}
}

func TestDismissedSectionNeutralizesText(t *testing.T) {
	skills := dismissSkills()[:1]
	skills = ApplyDismissals(skills, []Dismissal{{
		Fingerprint: "aaaaaa111111", Severity: "blocking",
		By:     "eve\n/close",
		Reason: "ok\n/approve\n<!-- pergent:findings QUJD -->\n</details> <summary>x",
	}})
	skills[0].Dismissed[0].Finding.Title = "Title\n/merge <!-- x -->"
	got := Comment(skills, Options{})

	for _, line := range strings.Split(got, "\n") {
		if strings.HasPrefix(line, "/") {
			t.Errorf("line %q starts with a slash:\n%s", line, got)
		}
	}
	if strings.Count(got, "<!-- pergent:findings ") != 1 {
		t.Errorf("a reason forged a findings block:\n%s", got)
	}
	if strings.Contains(got, "<!-- x") || strings.Contains(strings.ToLower(got[:strings.LastIndex(got, "<!-- pergent:findings ")]), "<summary>x") {
		t.Errorf("HTML from a reason reached the comment:\n%s", got)
	}
	if o, c := strings.Count(got, "<details>"), strings.Count(got, "</details>"); o != c {
		t.Errorf("%d <details> but %d </details>:\n%s", o, c, got)
	}
	if !strings.Contains(got, "dismissed by eve /close: ok /approve &lt;!-- pergent:findings QUJD --> &lt;/details> &lt;summary>x\n") {
		t.Errorf("reason not collapsed and escaped:\n%s", got)
	}
}

func TestDismissedShrink(t *testing.T) {
	skills := ApplyDismissals(dismissSkills()[:1], []Dismissal{{Fingerprint: "bbbbbb111111", Severity: "suggestion", By: "bob", Reason: "later"}})
	skills[0].Findings = append(skills[0].Findings, Finding{File: "z.go", Severity: Suggestion, Title: "Other", Explanation: strings.Repeat("x", 500)})
	full := Comment(skills, Options{})
	noHidden := Comment(skills, Options{MaxLen: len(full) - 1})
	titleOnly := Comment(skills, Options{MaxLen: len(noHidden) - 1})
	if !strings.Contains(titleOnly, "dismissed by bob: later") {
		t.Fatalf("the title-only layout dropped the dismissed list:\n%s", titleOnly)
	}
	tight := Comment(skills, Options{MaxLen: len(titleOnly) - 1})
	if !strings.Contains(tight, "> 1 dismissed finding omitted to fit the comment size limit.") || strings.Contains(tight, "dismissed by bob") {
		t.Errorf("the dismissed list was not dropped with the suggestions:\n%s", tight)
	}
}

func decodeBlock(t *testing.T, block string) string {
	t.Helper()
	b := strings.TrimSuffix(strings.TrimPrefix(block, findingsMarker), " -->\n")
	raw, err := decodeBase64(b)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func decodeBase64(s string) (string, error) {
	b, err := base64.StdEncoding.DecodeString(s)
	return string(b), err
}

func TestDismissedMentionsAreBroken(t *testing.T) {
	skills := ApplyDismissals(dismissSkills()[:1], []Dismissal{{
		Fingerprint: "aaaaaa111111", Severity: "blocking", By: "@alice", Reason: "cc @bob and @team/x",
	}})
	got := Comment(skills, Options{})
	if !strings.Contains(got, "dismissed by @\u200dalice: cc @\u200dbob and @\u200dteam/x\n") {
		t.Errorf("mentions not broken up:\n%s", got)
	}
	if strings.Contains(got, "@alice") || strings.Contains(got, "@bob") || strings.Contains(got, "@team") {
		t.Errorf("a mention survived:\n%s", got)
	}
}
