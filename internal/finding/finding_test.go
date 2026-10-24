package finding

import "testing"

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
