// Package panel merges the findings two models reported for one skill of one
// change into one list. It groups the findings either model says are the
// same issue, then decides each group's severity from what the other model
// answered about it. It runs no model and reads no file.
package panel

import (
	"slices"
	"strconv"
	"strings"

	"github.com/gopaytech/pergent/internal/finding"
)

// The members of a panel, and a group both found.
const (
	A    = "a"
	B    = "b"
	Both = "both"
)

// Item is a finding with its ID in the judge runs: "a1", "a2"... for A's.
type Item struct {
	ID string
	finding.Finding
}

// IDs numbers fs for role.
func IDs(role string, fs []finding.Finding) []Item {
	out := make([]Item, len(fs))
	for i, f := range fs {
		out[i] = Item{ID: role + strconv.Itoa(i+1), Finding: f}
	}
	return out
}

// Judgement is what one model answered about one finding of the other model.
// Real is nil for a suggestion, or when the answer had no usable value.
// SameAs is the ID of the judge's own finding on the same issue, "" for none.
type Judgement struct {
	Real   *bool
	Reason string
	SameAs string
}

// Answers are one judge run's judgements, by the other model's finding ID.
// Nil means the run gave no usable answer.
type Answers map[string]Judgement

// Tag is what the merge says about one finding of the merged list.
type Tag struct {
	// FoundBy is A, B or Both.
	FoundBy string
	// Also are the fingerprints of the group's other members, without the
	// finding's own: a dismissal of any of them dismisses the finding.
	Also []string
	// DisagreedBy is the model whose judgement demoted the finding to a
	// suggestion, and Reason its reason on one line.
	DisagreedBy, Reason string
	// RatedSuggestionBy is the model that reported the group's issue only as
	// a suggestion while the other reported it as blocking.
	RatedSuggestionBy string
	// UnjudgedBy is the model that gave no usable answer about the finding:
	// it stays blocking.
	UnjudgedBy string
}

// Merged is one finding of the merged list: the text of the group's most
// severe member, with the group's severity.
type Merged struct {
	finding.Finding
	Tag
}

func roleOf(id string) string {
	if strings.HasPrefix(id, A) {
		return A
	}
	if strings.HasPrefix(id, B) {
		return B
	}
	return ""
}

// Merge groups a and b by the same_as links of both judge runs (ofA: B's
// answers about A's findings; ofB: A's answers about B's) and decides each
// group. A link to an ID that is not on the other side is ignored. With
// neither run usable it is Exact.
func Merge(a, b []Item, ofA, ofB Answers) []Merged {
	if ofA == nil && ofB == nil {
		return Exact(a, b)
	}
	items := append(append([]Item(nil), a...), b...)
	index := make(map[string]int, len(items))
	for i, it := range items {
		index[it.ID] = i
	}
	parent := make([]int, len(items))
	for i := range parent {
		parent[i] = i
	}
	find := func(i int) int {
		for parent[i] != i {
			parent[i] = parent[parent[i]]
			i = parent[i]
		}
		return i
	}
	link := func(answers Answers, from, to string) {
		for id, j := range answers {
			k, ok := index[j.SameAs]
			if _, known := index[id]; !known || !ok || roleOf(id) != from || roleOf(j.SameAs) != to {
				continue
			}
			ri, rk := find(index[id]), find(k)
			if rk < ri {
				ri, rk = rk, ri
			}
			if ri != rk {
				parent[rk] = ri
			}
		}
	}
	link(ofA, A, B)
	link(ofB, B, A)
	return grouped(items, func(i int) int { return find(i) }, ofA, ofB, true)
}

// Exact merges a and b by finding.Key, the exact match Dedup uses, with no
// judgement: every blocking finding stays blocking.
func Exact(a, b []Item) []Merged {
	items := append(append([]Item(nil), a...), b...)
	first := map[string]int{}
	for i, it := range items {
		if _, ok := first[finding.Key(it.Finding)]; !ok {
			first[finding.Key(it.Finding)] = i
		}
	}
	return grouped(items, func(i int) int { return first[finding.Key(items[i].Finding)] }, nil, nil, false)
}

// grouped decides each group, in the order of the groups' first members.
func grouped(items []Item, root func(int) int, ofA, ofB Answers, judged bool) []Merged {
	members := map[int][]int{}
	var order []int
	for i := range items {
		r := root(i)
		if _, ok := members[r]; !ok {
			order = append(order, r)
		}
		members[r] = append(members[r], i)
	}
	slices.Sort(order)
	out := make([]Merged, 0, len(order))
	for _, r := range order {
		out = append(out, decide(items, members[r], ofA, ofB, judged))
	}
	return out
}

func decide(items []Item, group []int, ofA, ofB Answers, judged bool) Merged {
	var anyA, anyB, blockA, blockB bool
	text := group[0]
	for _, i := range group {
		it := items[i]
		isA := roleOf(it.ID) == A
		anyA, anyB = anyA || isA, anyB || !isA
		if it.Severity == finding.Blocking {
			blockA, blockB = blockA || isA, blockB || !isA
			if items[text].Severity != finding.Blocking {
				text = i
			}
		}
	}
	m := Merged{Finding: items[text].Finding}
	switch {
	case anyA && anyB:
		m.FoundBy = Both
	case anyA:
		m.FoundBy = A
	default:
		m.FoundBy = B
	}
	for _, i := range group {
		if fp := items[i].Fingerprint; fp != "" && fp != m.Fingerprint && !slices.Contains(m.Also, fp) {
			m.Also = append(m.Also, fp)
		}
	}
	switch {
	case !blockA && !blockB:
		m.Severity = finding.Suggestion
		return m
	case blockA && blockB:
		m.Severity = finding.Blocking
		return m
	}
	owner, judge, answers := A, B, ofA
	if blockB {
		owner, judge, answers = B, A, ofB
	}
	if (owner == A && anyB) || (owner == B && anyA) {
		m.RatedSuggestionBy = judge
	}
	m.Severity = finding.Blocking
	if !judged {
		return m
	}
	confirmed, unanswered, reason := false, false, ""
	for _, i := range group {
		it := items[i]
		if roleOf(it.ID) != owner || it.Severity != finding.Blocking {
			continue
		}
		j, ok := answers[it.ID]
		switch {
		case answers == nil || !ok || j.Real == nil:
			unanswered = true
		case *j.Real:
			confirmed = true
		case reason == "":
			reason = strings.Join(strings.Fields(j.Reason), " ")
		}
	}
	switch {
	case confirmed:
	case unanswered:
		m.UnjudgedBy = judge
	default:
		m.Severity = finding.Suggestion
		m.DisagreedBy, m.Reason = judge, reason
	}
	return m
}
