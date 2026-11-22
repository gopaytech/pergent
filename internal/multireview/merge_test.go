package multireview

import (
	"fmt"
	"reflect"
	"slices"
	"testing"

	"github.com/gopaytech/pergent/internal/finding"
)

func blocking(fp, file, title string) finding.Finding {
	return finding.Finding{File: file, Line: 1, Severity: finding.Blocking, Title: title, Explanation: "x", Fingerprint: fp}
}
func suggestion(fp, file, title string) finding.Finding {
	f := blocking(fp, file, title)
	f.Severity = finding.Suggestion
	return f
}
func isReal(v bool) *bool { return &v }
func items(lists ...[]finding.Finding) []Item {
	var out []Item
	for i, l := range lists {
		out = append(out, IDs(i+1, l)...)
	}
	return out
}

// fps lists the fingerprints of got, in order.
func fps(got []Merged) []string {
	var out []string
	for _, m := range got {
		out = append(out, m.Fingerprint)
	}
	return out
}

func TestMergeTwoBlockingReviewersNotDemoted(t *testing.T) {
	in := items(
		[]finding.Finding{blocking("f1", "x.go", "Nil check")},
		[]finding.Finding{blocking("f2", "x.go", "Missing nil check")},
		[]finding.Finding{},
	)
	got := Merge(in, Answers{
		"1.1": {Real: isReal(false), Reason: "no", SameAs: []string{"2.1"}},
		"2.1": {Real: isReal(false), Reason: "no"},
	}, nil)
	if len(got) != 1 || got[0].Severity != finding.Blocking || got[0].JudgeDisagreed {
		t.Fatalf("got %+v", got)
	}
	if !slices.Equal(got[0].FoundBy, []int{1, 2}) || got[0].Fingerprint != "f1" || !slices.Equal(got[0].Also, []string{"f2"}) {
		t.Fatalf("tag %+v", got[0].Tag)
	}
}

func TestIDs(t *testing.T) {
	got := IDs(3, []finding.Finding{blocking("a", "a.go", "A"), suggestion("b", "b.go", "B")})
	if len(got) != 2 {
		t.Fatalf("got %+v", got)
	}
	for i, want := range []string{"3.1", "3.2"} {
		if got[i].ID != want || got[i].Reviewer != 3 {
			t.Fatalf("item %d: %+v", i, got[i])
		}
	}
	if got[0].Fingerprint != "a" || got[1].Fingerprint != "b" {
		t.Fatalf("findings out of order: %+v", got)
	}
}

func TestMergeOneBlockingJudgedRealStays(t *testing.T) {
	got := Merge(items([]finding.Finding{blocking("f1", "x.go", "A")}), Answers{
		"1.1": {Real: isReal(true), Reason: "yes"},
	}, nil)
	if len(got) != 1 || got[0].Severity != finding.Blocking || !slices.Equal(got[0].FoundBy, []int{1}) {
		t.Fatalf("got %+v", got)
	}
	if g := got[0].Tag; g.JudgeDisagreed || g.Unjudged || g.Reason != "" || g.RatedSuggestionBy != nil || g.Also != nil {
		t.Fatalf("flags %+v", g)
	}
}

func TestMergeOneBlockingNotRealIsDemoted(t *testing.T) {
	got := Merge(items([]finding.Finding{blocking("f1", "x.go", "A")}), Answers{
		"1.1": {Real: isReal(false), Reason: "  The  guard is above. "},
	}, nil)
	if len(got) != 1 || got[0].Severity != finding.Suggestion || !got[0].JudgeDisagreed || got[0].Unjudged {
		t.Fatalf("got %+v", got)
	}
	if got[0].Reason != "The guard is above." {
		t.Fatalf("reason %q", got[0].Reason)
	}
}

func TestMergeNoAnswerKeepsBlocking(t *testing.T) {
	got := Merge(items([]finding.Finding{blocking("f1", "x.go", "A")}), Answers{}, nil)
	if len(got) != 1 || got[0].Severity != finding.Blocking || !got[0].Unjudged || got[0].JudgeDisagreed {
		t.Fatalf("got %+v", got)
	}
}

func TestMergeRealNullKeepsBlocking(t *testing.T) {
	got := Merge(items([]finding.Finding{blocking("f1", "x.go", "A")}), Answers{
		"1.1": {Real: nil, Reason: "not sure"},
	}, nil)
	if len(got) != 1 || got[0].Severity != finding.Blocking || !got[0].Unjudged || got[0].JudgeDisagreed {
		t.Fatalf("got %+v", got)
	}
}

func mixed(real bool) []Merged {
	return Merge(items(
		[]finding.Finding{blocking("f1", "x.go", "Blocking text")},
		[]finding.Finding{suggestion("f2", "x.go", "Two")},
		[]finding.Finding{suggestion("f3", "x.go", "Three")},
	), Answers{
		"1.1": {Real: isReal(real), Reason: "the reason", SameAs: []string{"2.1", "3.1"}},
	}, nil)
}

func TestMergeMixedSeverity(t *testing.T) {
	got := mixed(true)
	if len(got) != 1 || got[0].Severity != finding.Blocking || got[0].JudgeDisagreed || got[0].Unjudged {
		t.Fatalf("got %+v", got)
	}
	if !slices.Equal(got[0].FoundBy, []int{1, 2, 3}) || !slices.Equal(got[0].RatedSuggestionBy, []int{2, 3}) {
		t.Fatalf("tag %+v", got[0].Tag)
	}
	if got[0].Title != "Blocking text" || got[0].Fingerprint != "f1" || !slices.Equal(got[0].Also, []string{"f2", "f3"}) {
		t.Fatalf("text %+v", got[0])
	}
}

func TestMergeMixedSeverityDemoted(t *testing.T) {
	got := mixed(false)
	if len(got) != 1 || got[0].Severity != finding.Suggestion || !got[0].JudgeDisagreed || got[0].Reason != "the reason" {
		t.Fatalf("got %+v", got)
	}
	if !slices.Equal(got[0].RatedSuggestionBy, []int{2, 3}) || got[0].Fingerprint != "f1" {
		t.Fatalf("tag %+v", got[0].Tag)
	}
}

func TestMergeSuggestionsOnly(t *testing.T) {
	got := Merge(items(
		[]finding.Finding{suggestion("f1", "x.go", "One")},
		[]finding.Finding{suggestion("f2", "x.go", "Two")},
	), Answers{
		"1.1": {Real: isReal(false), Reason: "no", SameAs: []string{"2.1"}},
		"2.1": {Real: isReal(false), Reason: "no"},
	}, nil)
	if len(got) != 1 || got[0].Severity != finding.Suggestion || !slices.Equal(got[0].FoundBy, []int{1, 2}) {
		t.Fatalf("got %+v", got)
	}
	if g := got[0].Tag; g.JudgeDisagreed || g.Unjudged || g.Reason != "" || g.RatedSuggestionBy != nil {
		t.Fatalf("judged a suggestion: %+v", g)
	}
}

func TestMergeTextFromLowestBlocking(t *testing.T) {
	got := Merge(items(
		[]finding.Finding{suggestion("f1", "x.go", "One")},
		[]finding.Finding{blocking("f2", "x.go", "Two")},
		[]finding.Finding{blocking("f3", "x.go", "Three")},
	), Answers{
		"1.1": {SameAs: []string{"3.1"}},
		"3.1": {Real: isReal(true), SameAs: []string{"2.1"}},
	}, nil)
	if len(got) != 1 || got[0].Severity != finding.Blocking || !slices.Equal(got[0].FoundBy, []int{1, 2, 3}) {
		t.Fatalf("got %+v", got)
	}
	if got[0].Title != "Two" || got[0].Fingerprint != "f2" || !slices.Equal(got[0].Also, []string{"f1", "f3"}) {
		t.Fatalf("text %+v", got[0])
	}
}

func severalOfOne(answers Answers) []Merged {
	return Merge(items(
		[]finding.Finding{blocking("f1", "x.go", "One"), blocking("f12", "x.go", "One again")},
		[]finding.Finding{suggestion("f2", "x.go", "Two")},
	), answers, nil)
}

func TestMergeSeveralBlockingOfOneReviewer(t *testing.T) {
	// 1.1 links to 2.1, and 2.1 links to 1.2, which has no answer of its own.
	got := severalOfOne(Answers{
		"1.1": {Real: isReal(false), Reason: "first", SameAs: []string{"2.1"}},
		"2.1": {SameAs: []string{"1.2"}},
	})
	if len(got) != 1 || got[0].Severity != finding.Blocking || !got[0].Unjudged || got[0].JudgeDisagreed {
		t.Fatalf("got %+v", got)
	}
	if !slices.Equal(got[0].FoundBy, []int{1, 2}) || !slices.Equal(got[0].Also, []string{"f12", "f2"}) {
		t.Fatalf("tag %+v", got[0].Tag)
	}
}

func TestMergeAllNotRealDemoted(t *testing.T) {
	got := severalOfOne(Answers{
		"1.1": {Real: isReal(false), Reason: "first", SameAs: []string{"2.1"}},
		"1.2": {Real: isReal(false), Reason: "second", SameAs: []string{"2.1"}},
	})
	if len(got) != 1 || got[0].Severity != finding.Suggestion || !got[0].JudgeDisagreed || got[0].Unjudged {
		t.Fatalf("got %+v", got)
	}
	if got[0].Reason != "first" {
		t.Fatalf("reason %q, want 1.1's", got[0].Reason)
	}
}

func TestMergeUnionAcrossFive(t *testing.T) {
	in := items(
		[]finding.Finding{blocking("f1", "x.go", "One")},
		[]finding.Finding{blocking("f2", "x.go", "Two")},
		[]finding.Finding{blocking("f3", "x.go", "Three")},
		[]finding.Finding{blocking("f4", "x.go", "Four")},
		[]finding.Finding{blocking("f5", "y.go", "Five")},
	)
	answers := Answers{
		"1.1": {Real: isReal(true), SameAs: []string{"2.1"}},
		"2.1": {Real: isReal(true), SameAs: []string{"3.1"}},
		"4.1": {Real: isReal(true), SameAs: []string{"3.1"}},
		"5.1": {Real: isReal(true)},
	}
	for range 50 {
		got := Merge(in, answers, nil)
		if len(got) != 2 {
			t.Fatalf("got %d groups: %+v", len(got), got)
		}
		if !slices.Equal(got[0].FoundBy, []int{1, 2, 3, 4}) || got[0].Fingerprint != "f1" || !slices.Equal(got[0].Also, []string{"f2", "f3", "f4"}) {
			t.Fatalf("first group %+v", got[0])
		}
		if !slices.Equal(got[1].FoundBy, []int{5}) || got[1].Fingerprint != "f5" || got[1].Also != nil {
			t.Fatalf("second group %+v", got[1])
		}
	}
}

func TestMergeIgnoresBadLinks(t *testing.T) {
	in := items(
		[]finding.Finding{blocking("f1", "x.go", "One"), blocking("f12", "x.go", "One again")},
		[]finding.Finding{blocking("f2", "x.go", "Two")},
	)
	answers := Answers{
		"1.1": {Real: isReal(true), SameAs: []string{"1.2", "1.1", "9.9"}},
		"1.2": {Real: isReal(true)},
		"2.1": {Real: isReal(true), SameAs: []string{"9.9"}},
		"x":   {Real: isReal(true), SameAs: []string{"2.1"}},
	}
	for range 50 {
		got := Merge(in, answers, nil)
		if !slices.Equal(fps(got), []string{"f1", "f12", "f2"}) {
			t.Fatalf("got %v, want 3 groups", fps(got))
		}
		for _, m := range got {
			if m.Also != nil || len(m.FoundBy) != 1 || m.Severity != finding.Blocking {
				t.Fatalf("joined: %+v", m)
			}
		}
	}
}

func TestMergeIgnoresDismissedLinks(t *testing.T) {
	in := items(
		[]finding.Finding{blocking("f1", "x.go", "One")},
		[]finding.Finding{suggestion("f2", "x.go", "Two")},
		[]finding.Finding{blocking("f3", "x.go", "Three")},
	)
	got := Merge(in, Answers{
		"1.1": {Real: isReal(true), SameAs: []string{"2.1"}},
		"3.1": {Real: isReal(true), SameAs: []string{"2.1"}},
	}, map[string]bool{"2.1": true})
	if !slices.Equal(fps(got), []string{"f1", "f2", "f3"}) {
		t.Fatalf("got %v, want 3 groups", fps(got))
	}
	want := []finding.Severity{finding.Blocking, finding.Suggestion, finding.Blocking}
	for i, m := range got {
		if m.Severity != want[i] || m.Also != nil {
			t.Fatalf("group %d: %+v", i, m)
		}
	}
}

// A link from a dismissed finding is ignored too, not only a link to one.
func TestMergeIgnoresLinksFromDismissed(t *testing.T) {
	in := items(
		[]finding.Finding{blocking("f1", "x.go", "One")},
		[]finding.Finding{suggestion("f2", "x.go", "Two")},
	)
	got := Merge(in, Answers{
		"1.1": {Real: isReal(true)},
		"2.1": {SameAs: []string{"1.1"}},
	}, map[string]bool{"2.1": true})
	if !slices.Equal(fps(got), []string{"f1", "f2"}) || got[0].Also != nil || got[0].Severity != finding.Blocking {
		t.Fatalf("got %+v", got)
	}
}

func TestMergeNilAnswersIsExact(t *testing.T) {
	in := items(
		[]finding.Finding{blocking("f1", "x.go", "Same"), suggestion("f12", "y.go", "Other")},
		[]finding.Finding{suggestion("f2", "x.go", "same"), blocking("f22", "z.go", "Third")},
	)
	got, want := Merge(in, nil, nil), Exact(in)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Merge(nil) = %+v\nExact = %+v", got, want)
	}
	if len(got) != 3 {
		t.Fatalf("got %d groups, want 3", len(got))
	}
}

func TestExactMergesByKey(t *testing.T) {
	got := Exact(items(
		[]finding.Finding{blocking("f1", "x.go", "Nil  Check")},
		[]finding.Finding{blocking("f2", "x.go", "nil check")},
	))
	if len(got) != 1 || got[0].Severity != finding.Blocking || !slices.Equal(got[0].FoundBy, []int{1, 2}) {
		t.Fatalf("got %+v", got)
	}
	if got[0].Fingerprint != "f1" || !slices.Equal(got[0].Also, []string{"f2"}) || got[0].Unjudged {
		t.Fatalf("tag %+v", got[0])
	}
}

func TestExactKeepsBlockingOverSuggestion(t *testing.T) {
	got := Exact(items(
		[]finding.Finding{suggestion("f1", "x.go", "Nil check")},
		[]finding.Finding{blocking("f2", "x.go", "NIL CHECK")},
	))
	if len(got) != 1 || got[0].Severity != finding.Blocking || got[0].Fingerprint != "f2" {
		t.Fatalf("got %+v", got)
	}
	if !slices.Equal(got[0].RatedSuggestionBy, []int{1}) || got[0].Unjudged || got[0].JudgeDisagreed {
		t.Fatalf("tag %+v", got[0].Tag)
	}
}

func TestMergeKeepsOrder(t *testing.T) {
	in := items(
		[]finding.Finding{blocking("a", "a.go", "A"), blocking("b", "b.go", "B")},
		[]finding.Finding{blocking("c", "b.go", "C"), blocking("d", "a.go", "D"), blocking("e", "e.go", "E")},
	)
	answers := Answers{
		"2.2": {Real: isReal(true), SameAs: []string{"1.1"}},
		"2.1": {Real: isReal(true), SameAs: []string{"1.2"}},
		"2.3": {Real: isReal(true)},
	}
	for range 50 {
		got := Merge(in, answers, nil)
		if !slices.Equal(fps(got), []string{"a", "b", "e"}) {
			t.Fatalf("order %v, want [a b e]", fps(got))
		}
		if !slices.Equal(got[0].Also, []string{"d"}) || !slices.Equal(got[1].Also, []string{"c"}) {
			t.Fatalf("groups %+v", got)
		}
	}
}

func TestMergeLongSameAsList(t *testing.T) {
	in := items(
		[]finding.Finding{blocking("f1", "x.go", "One")},
		[]finding.Finding{blocking("f2", "x.go", "Two")},
	)
	var same []string
	for i := range 1000 {
		same = append(same, fmt.Sprintf("9.%d", i+1))
	}
	same = append(same, "2.1")
	got := Merge(in, Answers{"1.1": {Real: isReal(true), SameAs: same}}, nil)
	if len(got) != 1 || !slices.Equal(got[0].FoundBy, []int{1, 2}) || !slices.Equal(got[0].Also, []string{"f2"}) {
		t.Fatalf("got %+v", got)
	}
}
