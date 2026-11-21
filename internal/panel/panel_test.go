package panel

import (
	"reflect"
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

func TestMergeBothBlockingIsNotJudged(t *testing.T) {
	a := IDs(A, []finding.Finding{blocking("fa", "x.go", "Nil check")})
	b := IDs(B, []finding.Finding{blocking("fb", "x.go", "Missing nil check")})
	// B says A's finding is not real, but both found it blocking: not judged.
	got := Merge(a, b, Answers{"a1": {Real: isReal(false), Reason: "no", SameAs: "b1"}}, Answers{})
	if len(got) != 1 || got[0].Severity != finding.Blocking || got[0].FoundBy != Both || got[0].DisagreedBy != "" {
		t.Fatalf("got %+v", got)
	}
	if got[0].Fingerprint != "fa" || len(got[0].Also) != 1 || got[0].Also[0] != "fb" {
		t.Fatalf("fingerprint %q also %v", got[0].Fingerprint, got[0].Also)
	}
}

func TestMergeOtherSaysRealStaysBlocking(t *testing.T) {
	a := IDs(A, []finding.Finding{blocking("fa", "x.go", "Nil check")})
	// B confirms A's lone blocking finding.
	got := Merge(a, nil, Answers{"a1": {Real: isReal(true), Reason: "yes"}}, Answers{})
	if len(got) != 1 || got[0].Severity != finding.Blocking || got[0].FoundBy != A {
		t.Fatalf("got %+v", got)
	}
	if got[0].DisagreedBy != "" || got[0].UnjudgedBy != "" || got[0].RatedSuggestionBy != "" || len(got[0].Also) != 0 {
		t.Fatalf("tag %+v", got[0].Tag)
	}
}

func TestMergeOtherSaysNotRealIsDemoted(t *testing.T) {
	a := IDs(A, []finding.Finding{blocking("fa", "x.go", "Nil check")})
	// B says A's lone blocking finding is not real: a suggestion, with B's reason on one line.
	got := Merge(a, nil, Answers{"a1": {Real: isReal(false), Reason: "  The  guard is above. "}}, Answers{})
	if len(got) != 1 || got[0].Severity != finding.Suggestion || got[0].FoundBy != A || got[0].DisagreedBy != B {
		t.Fatalf("got %+v", got)
	}
	if got[0].Reason != "The guard is above." || got[0].UnjudgedBy != "" {
		t.Fatalf("reason %q unjudged %q", got[0].Reason, got[0].UnjudgedBy)
	}
}

func TestMergeNoAnswerKeepsBlocking(t *testing.T) {
	a := IDs(A, []finding.Finding{blocking("fa", "x.go", "Nil check")})
	// B's run worked but said nothing about a1: it stays blocking.
	got := Merge(a, nil, Answers{}, Answers{})
	if len(got) != 1 || got[0].Severity != finding.Blocking || got[0].FoundBy != A || got[0].UnjudgedBy != B {
		t.Fatalf("got %+v", got)
	}
	if got[0].DisagreedBy != "" || got[0].Reason != "" {
		t.Fatalf("disagreed %q reason %q", got[0].DisagreedBy, got[0].Reason)
	}
}

func TestMergeBOwnerDemoted(t *testing.T) {
	b := IDs(B, []finding.Finding{blocking("fb", "x.go", "Nil check")})
	// A says B's lone blocking finding is not real: a suggestion, with A's reason.
	got := Merge(nil, b, Answers{}, Answers{"b1": {Real: isReal(false), Reason: "r"}})
	if len(got) != 1 || got[0].Severity != finding.Suggestion || got[0].FoundBy != B || got[0].DisagreedBy != A {
		t.Fatalf("got %+v", got)
	}
	if got[0].Reason != "r" || got[0].UnjudgedBy != "" || got[0].RatedSuggestionBy != "" {
		t.Fatalf("tag %+v", got[0].Tag)
	}
}

func TestMergeJudgeFailedKeepsBlocking(t *testing.T) {
	a := IDs(A, []finding.Finding{blocking("fa", "x.go", "Nil check")})
	b := IDs(B, []finding.Finding{suggestion("fb", "y.go", "Rename")})
	// B's judge run failed (ofA nil), A's worked: A's lone blocking finding stays blocking.
	got := Merge(a, b, nil, Answers{})
	if len(got) != 2 || got[0].Severity != finding.Blocking || got[0].FoundBy != A || got[0].UnjudgedBy != B {
		t.Fatalf("got %+v", got)
	}
	if got[0].DisagreedBy != "" || got[1].Severity != finding.Suggestion || got[1].FoundBy != B {
		t.Fatalf("got %+v", got)
	}
}

func TestMergeMixedSeverity(t *testing.T) {
	a := IDs(A, []finding.Finding{blocking("fa", "x.go", "Nil check")})
	b := IDs(B, []finding.Finding{suggestion("fb", "x.go", "Maybe a nil check")})
	// B rated it a suggestion but says A's blocking finding is real: blocking.
	got := Merge(a, b, Answers{"a1": {Real: isReal(true), Reason: "yes", SameAs: "b1"}}, Answers{})
	if len(got) != 1 || got[0].Severity != finding.Blocking || got[0].FoundBy != Both || got[0].RatedSuggestionBy != B {
		t.Fatalf("got %+v", got)
	}
	if got[0].Title != "Nil check" || got[0].Fingerprint != "fa" || len(got[0].Also) != 1 || got[0].Also[0] != "fb" {
		t.Fatalf("title %q fingerprint %q also %v", got[0].Title, got[0].Fingerprint, got[0].Also)
	}
	if got[0].DisagreedBy != "" || got[0].UnjudgedBy != "" {
		t.Fatalf("tag %+v", got[0].Tag)
	}
}

func TestMergeMixedSeverityDemoted(t *testing.T) {
	a := IDs(A, []finding.Finding{blocking("fa", "x.go", "Nil check")})
	b := IDs(B, []finding.Finding{suggestion("fb", "x.go", "Maybe a nil check")})
	// B rated it a suggestion and says A's blocking finding is not real: a suggestion.
	got := Merge(a, b, Answers{"a1": {Real: isReal(false), Reason: "guarded", SameAs: "b1"}}, Answers{})
	if len(got) != 1 || got[0].Severity != finding.Suggestion || got[0].FoundBy != Both || got[0].RatedSuggestionBy != B {
		t.Fatalf("got %+v", got)
	}
	if got[0].DisagreedBy != B || got[0].Reason != "guarded" || got[0].Fingerprint != "fa" {
		t.Fatalf("tag %+v fingerprint %q", got[0].Tag, got[0].Fingerprint)
	}
}

func TestMergeSuggestionsOnlyMerged(t *testing.T) {
	a := IDs(A, []finding.Finding{suggestion("fa", "x.go", "Rename var")})
	b := IDs(B, []finding.Finding{suggestion("fb", "x.go", "Better name")})
	// A links B's suggestion to its own; real is ignored for suggestions.
	got := Merge(a, b, Answers{}, Answers{"b1": {Real: isReal(false), Reason: "no", SameAs: "a1"}})
	if len(got) != 1 || got[0].Severity != finding.Suggestion || got[0].FoundBy != Both {
		t.Fatalf("got %+v", got)
	}
	if got[0].DisagreedBy != "" || got[0].Reason != "" || got[0].Fingerprint != "fa" || len(got[0].Also) != 1 || got[0].Also[0] != "fb" {
		t.Fatalf("tag %+v fingerprint %q", got[0].Tag, got[0].Fingerprint)
	}
}

func TestMergeTextFromMostSevere(t *testing.T) {
	a := IDs(A, []finding.Finding{suggestion("fa", "x.go", "Maybe a nil check")})
	b := IDs(B, []finding.Finding{blocking("fb", "x.go", "Nil deref")})
	// B's blocking b1 is linked to A's suggestion a1, and A says b1 is real.
	got := Merge(a, b, Answers{}, Answers{"b1": {Real: isReal(true), Reason: "yes", SameAs: "a1"}})
	if len(got) != 1 || got[0].Severity != finding.Blocking || got[0].FoundBy != Both || got[0].RatedSuggestionBy != A {
		t.Fatalf("got %+v", got)
	}
	if got[0].Title != "Nil deref" || got[0].Fingerprint != "fb" || len(got[0].Also) != 1 || got[0].Also[0] != "fa" {
		t.Fatalf("title %q fingerprint %q also %v", got[0].Title, got[0].Fingerprint, got[0].Also)
	}
}

func TestMergeUnionOfLinks(t *testing.T) {
	a := IDs(A, []finding.Finding{
		blocking("fa1", "x.go", "Leak"),
		blocking("fa2", "y.go", "Other"),
		blocking("fa3", "x.go", "Leak again"),
	})
	b := IDs(B, []finding.Finding{
		suggestion("fb1", "z.go", "Style"),
		suggestion("fb2", "x.go", "Possible leak"),
	})
	// a1 -> b2 and b2 -> a3 join a1, b2 and a3 in one group.
	ofA := Answers{"a1": {Real: isReal(true), SameAs: "b2"}, "a2": {Real: isReal(true)}}
	ofB := Answers{"b2": {SameAs: "a3"}}
	got := Merge(a, b, ofA, ofB)
	if len(got) != 3 || got[0].Severity != finding.Blocking || got[0].FoundBy != Both || got[0].Fingerprint != "fa1" {
		t.Fatalf("got %+v", got)
	}
	if !reflect.DeepEqual(got[0].Also, []string{"fa3", "fb2"}) {
		t.Fatalf("also %v", got[0].Also)
	}
	if got[0].UnjudgedBy != "" || got[0].DisagreedBy != "" || got[0].RatedSuggestionBy != B {
		t.Fatalf("tag %+v", got[0].Tag)
	}
	if got[1].Fingerprint != "fa2" || got[1].FoundBy != A || got[2].Fingerprint != "fb1" || got[2].FoundBy != B {
		t.Fatalf("got %+v", got)
	}
}

func TestMergeChainedLinks(t *testing.T) {
	a := IDs(A, []finding.Finding{blocking("fa1", "x.go", "Race"), blocking("fa2", "x.go", "Data race")})
	b := IDs(B, []finding.Finding{blocking("fb1", "x.go", "Race on map"), blocking("fb2", "x.go", "Map race")})
	// a1 -> b1, b2 -> a1, a2 -> b2: one group of 4; its text is its first member, a1.
	ofA := Answers{"a1": {SameAs: "b1"}, "a2": {SameAs: "b2"}}
	ofB := Answers{"b2": {SameAs: "a1"}}
	got := Merge(a, b, ofA, ofB)
	if len(got) != 1 || got[0].Severity != finding.Blocking || got[0].FoundBy != Both || got[0].Title != "Race" {
		t.Fatalf("got %+v", got)
	}
	if got[0].Fingerprint != "fa1" || !reflect.DeepEqual(got[0].Also, []string{"fa2", "fb1", "fb2"}) {
		t.Fatalf("fingerprint %q also %v", got[0].Fingerprint, got[0].Also)
	}
}

func TestMergeIgnoresBadLinks(t *testing.T) {
	// a1 has index 0, the index a lookup of a missing ID returns, so each
	// bad link starts or ends at a later finding: without its guard it would
	// join two findings.
	for name, c := range map[string]struct{ ofA, ofB Answers }{
		"missing ID":     {ofA: Answers{"a2": {Real: isReal(true), SameAs: "b9"}}, ofB: Answers{"b2": {Real: isReal(true), SameAs: "a9"}}},
		"own side":       {ofA: Answers{"a1": {Real: isReal(true), SameAs: "a2"}}, ofB: Answers{"b1": {Real: isReal(true), SameAs: "b2"}}},
		"key wrong side": {ofA: Answers{"b1": {SameAs: "b2"}}, ofB: Answers{"a1": {SameAs: "a2"}}},
		"unknown key":    {ofA: Answers{}, ofB: Answers{"c1": {SameAs: "a2"}}},
	} {
		t.Run(name, func(t *testing.T) {
			a := IDs(A, []finding.Finding{blocking("fa1", "x.go", "One"), blocking("fa2", "x.go", "Two")})
			b := IDs(B, []finding.Finding{blocking("fb1", "x.go", "Three"), blocking("fb2", "x.go", "Four")})
			got := Merge(a, b, c.ofA, c.ofB)
			if len(got) != 4 {
				t.Fatalf("got %d groups, want 4: %+v", len(got), got)
			}
			for i, want := range []struct{ fp, by string }{{"fa1", A}, {"fa2", A}, {"fb1", B}, {"fb2", B}} {
				if got[i].Fingerprint != want.fp || got[i].FoundBy != want.by || len(got[i].Also) != 0 || got[i].Severity != finding.Blocking {
					t.Fatalf("group %d: got %+v", i, got[i])
				}
			}
		})
	}
}

func TestMergeSeveralBlockingOfOneSide(t *testing.T) {
	a := IDs(A, []finding.Finding{blocking("fa1", "x.go", "Leak"), blocking("fa2", "x.go", "Leak too")})
	b := IDs(B, []finding.Finding{suggestion("fb1", "x.go", "Maybe a leak")})
	// Group {a1, a2, b1}: a1 not real, a2 no answer: not every member is judged not real.
	ofA := Answers{"a1": {Real: isReal(false), Reason: "closed", SameAs: "b1"}}
	ofB := Answers{"b1": {SameAs: "a2"}}
	got := Merge(a, b, ofA, ofB)
	if len(got) != 1 || got[0].Severity != finding.Blocking || got[0].FoundBy != Both || got[0].UnjudgedBy != B {
		t.Fatalf("got %+v", got)
	}
	if got[0].DisagreedBy != "" || got[0].Reason != "" || got[0].RatedSuggestionBy != B {
		t.Fatalf("tag %+v", got[0].Tag)
	}
}

func TestMergeAllNotRealDemoted(t *testing.T) {
	a := IDs(A, []finding.Finding{blocking("fa1", "x.go", "Leak"), blocking("fa2", "x.go", "Leak too")})
	// b1 is the suggestion that joins A's two blocking findings in one group.
	b := IDs(B, []finding.Finding{suggestion("fb1", "x.go", "Maybe a leak")})
	ofA := Answers{
		"a1": {Real: isReal(false), Reason: "closed in defer", SameAs: "b1"},
		"a2": {Real: isReal(false), Reason: "closed later", SameAs: "b1"},
	}
	got := Merge(a, b, ofA, Answers{})
	if len(got) != 1 || got[0].Severity != finding.Suggestion || got[0].FoundBy != Both || got[0].DisagreedBy != B {
		t.Fatalf("got %+v", got)
	}
	if got[0].Reason != "closed in defer" || got[0].UnjudgedBy != "" {
		t.Fatalf("reason %q unjudged %q", got[0].Reason, got[0].UnjudgedBy)
	}
}

func TestMergeBothFailedIsExact(t *testing.T) {
	a := IDs(A, []finding.Finding{blocking("fa1", "x.go", "Nil check"), suggestion("fa2", "y.go", "Rename")})
	b := IDs(B, []finding.Finding{blocking("fb1", "x.go", "nil  CHECK"), blocking("fb2", "z.go", "Leak")})
	got := Merge(a, b, nil, nil)
	if want := Exact(a, b); len(got) != 3 || !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v\nwant %+v", got, want)
	}
}

func TestExactMergesByKey(t *testing.T) {
	a := IDs(A, []finding.Finding{blocking("fa1", "x.go", "Nil check"), blocking("fa2", "y.go", "Leak")})
	b := IDs(B, []finding.Finding{blocking("fb1", "x.go", "nil  CHECK")})
	got := Exact(a, b)
	if len(got) != 2 || got[0].Severity != finding.Blocking || got[0].FoundBy != Both || got[0].Fingerprint != "fa1" {
		t.Fatalf("got %+v", got)
	}
	if len(got[0].Also) != 1 || got[0].Also[0] != "fb1" {
		t.Fatalf("also %v", got[0].Also)
	}
	// Every blocking finding stays blocking, with no judgement tag.
	if got[1].Severity != finding.Blocking || got[1].FoundBy != A || got[1].UnjudgedBy != "" || got[1].DisagreedBy != "" {
		t.Fatalf("got %+v", got[1])
	}
}

func TestExactKeepsBlockingOverSuggestion(t *testing.T) {
	a := IDs(A, []finding.Finding{suggestion("fa", "x.go", "nil check")})
	b := IDs(B, []finding.Finding{blocking("fb", "x.go", "Nil Check")})
	got := Exact(a, b)
	if len(got) != 1 || got[0].Severity != finding.Blocking || got[0].FoundBy != Both || got[0].RatedSuggestionBy != A {
		t.Fatalf("got %+v", got)
	}
	if got[0].Title != "Nil Check" || got[0].Fingerprint != "fb" || len(got[0].Also) != 1 || got[0].Also[0] != "fa" {
		t.Fatalf("title %q fingerprint %q also %v", got[0].Title, got[0].Fingerprint, got[0].Also)
	}
}

func TestMergeKeepsOrder(t *testing.T) {
	a := IDs(A, []finding.Finding{suggestion("fa1", "x.go", "First"), suggestion("fa2", "y.go", "Second")})
	b := IDs(B, []finding.Finding{suggestion("fb1", "z.go", "Third"), suggestion("fb2", "y.go", "Second too")})
	// Groups by their first member, A's list before B's: {a1}, {a2, b2}, {b1}.
	got := Merge(a, b, Answers{}, Answers{"b2": {SameAs: "a2"}})
	if len(got) != 3 || got[0].Fingerprint != "fa1" || got[1].Fingerprint != "fa2" || got[2].Fingerprint != "fb1" {
		t.Fatalf("got %+v", got)
	}
	if got[1].FoundBy != Both || len(got[1].Also) != 1 || got[1].Also[0] != "fb2" {
		t.Fatalf("got %+v", got[1])
	}
}
