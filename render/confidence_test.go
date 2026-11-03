package render

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSkillScore(t *testing.T) {
	blocking := Finding{File: "a.go", Severity: Blocking, Title: "B"}
	sugg := Finding{File: "a.go", Severity: Suggestion, Title: "S"}
	for _, c := range []struct {
		name string
		s    Skill
		want Score
	}{
		{"model score kept", Skill{Verdict: VerdictNoBlockingIssues, Confidence: 5, Findings: []Finding{sugg}}, Score{5, ""}},
		{"not scored", Skill{Verdict: VerdictNoBlockingIssues}, Score{}},
		{"out of range", Skill{Verdict: VerdictNoBlockingIssues, Confidence: 9}, Score{}},
		{"failed", Skill{Verdict: VerdictFailed, Confidence: 5}, Score{}},
		{"empty verdict", Skill{Confidence: 5}, Score{}},
		{"blocking caps at 3", Skill{Verdict: VerdictHasBlockingIssues, Confidence: 5, Findings: []Finding{blocking}}, Score{3, "1 blocking finding"}},
		{"two blocking", Skill{Verdict: VerdictHasBlockingIssues, Confidence: 4, Findings: []Finding{blocking, blocking}}, Score{3, "2 blocking findings"}},
		{"blocking under cap", Skill{Verdict: VerdictHasBlockingIssues, Confidence: 2, Findings: []Finding{blocking}}, Score{2, ""}},
		{"skipped generated caps at 4", Skill{Verdict: VerdictNoBlockingIssues, Confidence: 5, Skipped: []Skipped{{"gen.go", "generated"}}}, Score{4, "1 skipped file not reviewed"}},
		{"out of scope doesn't cap", Skill{Verdict: VerdictNoBlockingIssues, Confidence: 5, Skipped: []Skipped{{"x.md", "out of scope"}, {"y.go", "excluded by skill"}, {"z.go", "no content changes"}}}, Score{5, ""}},
		{"blocking cap wins", Skill{Verdict: VerdictHasBlockingIssues, Confidence: 5, Findings: []Finding{blocking}, Skipped: []Skipped{{"go.sum", "lockfile"}}}, Score{3, "1 blocking finding"}},
	} {
		if got := SkillScore(c.s); got != c.want {
			t.Errorf("%s: got %+v, want %+v", c.name, got, c.want)
		}
	}
}

func TestSkillScore_DismissalRestoresModelScore(t *testing.T) {
	s := Skill{Name: "a", Verdict: VerdictHasBlockingIssues, Confidence: 5,
		Findings: []Finding{{Fingerprint: "fp1", File: "a.go", Severity: Blocking, Title: "B"}}}
	got := SkillScore(ApplyDismissals([]Skill{s}, []Dismissal{{Fingerprint: "fp1", Severity: "blocking", By: "x", Reason: "r"}})[0])
	if got != (Score{5, ""}) {
		t.Errorf("got %+v, want {5 }", got)
	}
}

func TestReviewScore(t *testing.T) {
	skills := []Skill{
		{Name: "a", Verdict: VerdictNoBlockingIssues, Confidence: 4},
		{Name: "b", Verdict: VerdictFailed, Confidence: 1},
		{Name: "c", Verdict: VerdictNoBlockingIssues, Confidence: 4},
	}
	if got, i := ReviewScore(skills); got != (Score{4, ""}) || i != 0 {
		t.Errorf("got %+v at %d, want {4 } at 0", got, i)
	}
	if _, i := ReviewScore([]Skill{{Verdict: VerdictFailed, Confidence: 3}}); i != -1 {
		t.Errorf("all failed: index %d, want -1", i)
	}
}

func TestComment_ConfidenceHeaderAndTable(t *testing.T) {
	skills := []Skill{
		{Name: "code-review", Verdict: VerdictHasBlockingIssues, Confidence: 4, ConfidenceReason: "One blocking issue: a nil map write.",
			Findings: []Finding{
				{File: "a.go", Line: 1, Severity: Blocking, Title: "B"},
				{File: "a.go", Line: 2, Severity: Suggestion, Title: "S1"},
				{File: "a.go", Line: 3, Severity: Suggestion, Title: "S2"},
			}},
		{Name: "security", Verdict: VerdictNoBlockingIssues, Confidence: 5, Findings: []Finding{}},
		{Name: "docs", Verdict: VerdictFailed, Confidence: 5, Findings: []Finding{}},
	}
	out := Comment(skills, Options{})
	want := Marker + "\n### ❌ 1 blocking issue · Confidence 3/5 (capped)\n\n" +
		"<sub>1 blocking · 2 suggestions · 1 skill failed</sub>\n\n" +
		"> `code-review`: One blocking issue: a nil map write. (capped: 1 blocking finding)\n\n" +
		"| Skill | Verdict | Confidence | Findings |\n|---|---|---|---|\n" +
		"| `code-review` | ❌ Has blocking issues | 3/5 (capped) | 1 blocking · 2 suggestions |\n" +
		"| `security` | ✅ No blocking issues | 5/5 | none |\n" +
		"| `docs` | ⚠️ Failed | not scored | none |\n\n"
	if !strings.HasPrefix(out, want) {
		t.Errorf("comment does not start with the headline, meta line, reason and table:\n%s", out)
	}
}

func TestComment_ConfidenceNotScoredWhenAllFailed(t *testing.T) {
	out := Comment([]Skill{{Name: "docs", Verdict: VerdictFailed, Confidence: 5}}, Options{})
	want := Marker + "\n### ⚠️ 1 skill failed · Confidence not scored\n\n"
	if !strings.HasPrefix(out, want) {
		t.Errorf("comment lacks the not-scored line:\n%s", out)
	}
}

func TestComment_ConfidenceReasonIsMentionSafe(t *testing.T) {
	out := Comment([]Skill{{Name: "a", Verdict: VerdictNoBlockingIssues, Confidence: 5, ConfidenceReason: "ask @bob <b>\nnow"}}, Options{})
	if !strings.Contains(out, "> ask @‍bob &lt;b> now\n\n") {
		t.Errorf("reason was not made safe:\n%s", out)
	}
}

func TestComment_ConfidenceKeptAtSmallestLimit(t *testing.T) {
	skills := []Skill{{Name: "s", Verdict: VerdictHasBlockingIssues, Confidence: 4,
		Findings: []Finding{{File: "a.go", Line: 1, Severity: Blocking, Title: "B", Explanation: strings.Repeat("long explanation ", 100)}}}}
	out := Comment(skills, Options{MaxLen: 400})
	if !strings.Contains(out, "### ❌ 1 blocking issue · Confidence 3/5 (capped)\n") {
		t.Errorf("headline lost when shrinking:\n%s", out)
	}
	if !strings.Contains(out, "> Capped: 1 blocking finding\n") {
		t.Errorf("reason lost when shrinking:\n%s", out)
	}
}

func TestComment_NoConfidenceUnchanged(t *testing.T) {
	skills := []Skill{{Name: "s", Verdict: VerdictNoBlockingIssues, Findings: []Finding{}}}
	out := Comment(skills, Options{})
	if strings.Contains(out, "Confidence") || strings.Contains(out, "| Skill |") {
		t.Errorf("a skill without a score added confidence output:\n%s", out)
	}
	if !strings.HasPrefix(out, Marker+"\n### ✅ No blocking issues\n\n") {
		t.Errorf("comment layout changed:\n%s", out)
	}
}

func TestFindingsBlock_CarriesModelScoreAndSuggestion(t *testing.T) {
	skills := []Skill{
		{Name: "s", Verdict: VerdictHasBlockingIssues, Confidence: 5, ConfidenceReason: "sure",
			Findings: []Finding{{File: "a.go", Line: 42, EndLine: 43, Severity: Blocking, Title: "B", Suggestion: "x"}}},
		{Name: "none", Verdict: VerdictNoBlockingIssues},
	}
	var got struct {
		Skills map[string][]struct {
			EndLine    int    `json:"end_line"`
			Suggestion string `json:"suggestion"`
		} `json:"skills"`
		Confidence map[string]struct {
			Score  int    `json:"score"`
			Reason string `json:"reason"`
		} `json:"confidence"`
	}
	if err := json.Unmarshal([]byte(decodeBlock(t, FindingsBlock(skills))), &got); err != nil {
		t.Fatal(err)
	}
	if c := got.Confidence["s"]; c.Score != 5 || c.Reason != "sure" {
		t.Errorf("confidence[s] = %+v, want the model's score 5 (not the capped 3)", c)
	}
	if _, ok := got.Confidence["none"]; ok {
		t.Errorf("a skill without a score has an entry: %+v", got.Confidence)
	}
	if f := got.Skills["s"][0]; f.EndLine != 43 || f.Suggestion != "x" {
		t.Errorf("finding = %+v, want end_line 43 and suggestion x", f)
	}
}

func TestFindingsBlock_NoScoreOmitsConfidence(t *testing.T) {
	raw := decodeBlock(t, FindingsBlock([]Skill{{Name: "s", Findings: []Finding{{File: "a.go", Severity: Blocking, Title: "B"}}}}))
	if strings.Contains(raw, "confidence") || strings.Contains(raw, "end_line") || strings.Contains(raw, "suggestion\"") {
		t.Errorf("block carries new fields for old input: %s", raw)
	}
}
