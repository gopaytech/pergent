package finding

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestFingerprint_StableAndNormalized(t *testing.T) {
	a := Fingerprint("s", "a.go", "Nil  Deref ")
	b := Fingerprint("s", "a.go", "nil deref")
	if a != b {
		t.Errorf("Fingerprint not normalized: %q != %q", a, b)
	}
	if len(a) != 12 {
		t.Errorf("len(Fingerprint) = %d, want 12", len(a))
	}
	if c := Fingerprint("s", "b.go", "nil deref"); c == a {
		t.Errorf("Fingerprint for different file should differ, both %q", a)
	}
}

func TestDedup_KeepsFirst(t *testing.T) {
	in := []Finding{
		{File: "a.go", Line: 3, Title: "Nil  Deref", Explanation: "first"},
		{File: "b.go", Line: 1, Title: "other", Explanation: "second"},
		{File: "a.go", Line: 3, Title: "nil deref ", Explanation: "third"},
	}
	got := Dedup(in)
	if len(got) != 2 {
		t.Fatalf("len(Dedup) = %d, want 2", len(got))
	}
	if got[0].Explanation != "first" {
		t.Errorf("got[0].Explanation = %q, want %q", got[0].Explanation, "first")
	}
	if got[1].Explanation != "second" {
		t.Errorf("got[1].Explanation = %q, want %q", got[1].Explanation, "second")
	}
}

func TestDedup_BlockingBeatsSuggestion(t *testing.T) {
	in := []Finding{
		{File: "a.go", Line: 3, Severity: Suggestion, Title: "Nil Deref", Explanation: "suggestion"},
		{File: "b.go", Line: 1, Severity: Suggestion, Title: "other", Explanation: "second"},
		{File: "a.go", Line: 3, Severity: Blocking, Title: "nil deref", Explanation: "blocking"},
	}
	got := Dedup(in)
	if len(got) != 2 {
		t.Fatalf("len(Dedup) = %d, want 2", len(got))
	}
	if got[0].Severity != Blocking || got[0].Explanation != "blocking" {
		t.Errorf("got[0] = %+v, want the blocking finding in the first one's place", got[0])
	}
	if got[1].Explanation != "second" {
		t.Errorf("got[1].Explanation = %q, want %q", got[1].Explanation, "second")
	}
}

func TestDedup_BlockingStaysOverLaterSuggestion(t *testing.T) {
	in := []Finding{
		{File: "a.go", Line: 3, Severity: Blocking, Title: "t", Explanation: "blocking"},
		{File: "a.go", Line: 3, Severity: Suggestion, Title: "t", Explanation: "suggestion"},
		{File: "a.go", Line: 3, Severity: Blocking, Title: "t", Explanation: "later blocking"},
	}
	got := Dedup(in)
	if len(got) != 1 || got[0].Explanation != "blocking" {
		t.Errorf("Dedup = %+v, want only the first blocking finding", got)
	}
}

func TestFindingLinkedRoundTrip(t *testing.T) {
	in := Finding{
		File: "api/router.go", Line: 12, Severity: Blocking, Title: "Breaks a caller",
		Explanation: "The signature changed.",
		Linked:      &Caller{Repo: "billing", File: "svc/client.go", Line: 88},
	}
	data, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	if want := `"linked":{"repo":"billing","file":"svc/client.go","line":88}`; !strings.Contains(string(data), want) {
		t.Errorf("json = %s, want it to hold %s", data, want)
	}
	var out Finding
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	if out.Linked == nil || *out.Linked != *in.Linked {
		t.Errorf("Linked = %+v, want %+v", out.Linked, in.Linked)
	}
}

func TestFindingWithoutCallerHasNoLinkedKey(t *testing.T) {
	data, err := json.Marshal(Finding{File: "a.go", Severity: Suggestion, Title: "t", Explanation: "e"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "linked") {
		t.Errorf("json = %s, want no linked key", data)
	}
}
