package render

import "testing"

func TestAssignIDs(t *testing.T) {
	got := AssignIDs([]string{"abcdef111111", "abcdef222222", "123456777777"})
	want := map[string]string{
		"abcdef111111": "abcdef11",
		"abcdef222222": "abcdef22",
		"123456777777": "123456",
	}
	for fp, id := range want {
		if got[fp] != id {
			t.Errorf("AssignIDs[%q] = %q, want %q", fp, got[fp], id)
		}
	}

	single := AssignIDs([]string{"abcdef111111"})
	if single["abcdef111111"] != "abcdef" {
		t.Errorf("single fingerprint ID = %q, want abcdef", single["abcdef111111"])
	}

	// The same fingerprint twice is not a collision.
	dup := AssignIDs([]string{"abcdef111111", "abcdef111111"})
	if dup["abcdef111111"] != "abcdef" {
		t.Errorf("repeated fingerprint ID = %q, want abcdef", dup["abcdef111111"])
	}
}

func TestAssignIDsCollision(t *testing.T) {
	// Two fingerprints sharing their first 6 characters both get 8.
	a, b := "abcdef111111", "abcdef222222"
	got := AssignIDs([]string{a, b, "zzzzzz000000"})
	if got[a] != "abcdef11" || got[b] != "abcdef22" {
		t.Errorf("IDs = %q, %q; want 8 characters each", got[a], got[b])
	}
	if got["zzzzzz000000"] != "zzzzzz" {
		t.Errorf("unrelated ID = %q, want 6 characters", got["zzzzzz000000"])
	}
}

func TestAssignIDs_WidensUntilUnique(t *testing.T) {
	// These share their first 10 characters.
	a, b := "abcdef1234aaaa", "abcdef1234bbbb"
	got := AssignIDs([]string{a, b, "zzzzzz000000"})
	if got[a] == got[b] {
		t.Fatalf("IDs not unique: %q", got[a])
	}
	if got[a] != "abcdef1234a" || got[b] != "abcdef1234b" {
		t.Errorf("IDs = %q, %q", got[a], got[b])
	}
	if got["zzzzzz000000"] != "zzzzzz" {
		t.Errorf("unrelated ID = %q", got["zzzzzz000000"])
	}
}

func TestAssignIDs_IdenticalShareID(t *testing.T) {
	got := AssignIDs([]string{"abcdef111111", "abcdef222222", "abcdef111111"})
	if got["abcdef111111"] != "abcdef11" || got["abcdef222222"] != "abcdef22" {
		t.Errorf("IDs = %v", got)
	}
}
