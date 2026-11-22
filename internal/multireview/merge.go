// Package multireview merges the findings that 1 to MaxReviewers reviewer
// models reported for one skill of one change into one list. It groups the
// findings the judge says are the same issue, then decides each group's
// severity from the judge's answers. It runs no model and reads no file.
package multireview

import (
	"fmt"
	"slices"
	"strings"

	"github.com/gopaytech/pergent/internal/finding"
)

// MaxReviewers is the most reviewers one multi review takes.
const MaxReviewers = 5

// Item is one reviewer's finding with its ID for the judge: "<n>.<i>", where
// n is the reviewer number and i the finding's place in that reviewer's
// list, both from 1.
type Item struct {
	ID       string
	Reviewer int
	finding.Finding
}

// IDs numbers fs for reviewer: "<reviewer>.1", "<reviewer>.2", and so on.
func IDs(reviewer int, fs []finding.Finding) []Item {
	out := make([]Item, len(fs))
	for i, f := range fs {
		out[i] = Item{ID: fmt.Sprintf("%d.%d", reviewer, i+1), Reviewer: reviewer, Finding: f}
	}
	return out
}

// Judgement is what the judge answered about one finding. Real is nil for a
// suggestion, or when the answer had no usable value. SameAs are the IDs of
// other reviewers' findings on the same issue.
type Judgement struct {
	Real   *bool
	Reason string
	SameAs []string
}

// Answers are the judge's judgements, by finding ID. Nil means the judge gave
// no usable answer.
type Answers map[string]Judgement

// Tag is what the merge says about one finding of the merged list.
type Tag struct {
	// FoundBy are the reviewers with a finding in the group, ascending.
	FoundBy []int
	// Also are the fingerprints of the group's other findings, without the
	// finding's own: a dismissal of any of them dismisses the finding.
	Also []string
	// JudgeDisagreed is true when the judge's "not real" made the finding a
	// suggestion.
	JudgeDisagreed bool
	// Reason is the judge's reason, on one line.
	Reason string
	// RatedSuggestionBy are the reviewers that had only a suggestion in the
	// group while one reviewer said blocking, ascending.
	RatedSuggestionBy []int
	// Unjudged is true when the judge gave no usable answer about the
	// finding: it stays blocking.
	Unjudged bool
}

// Merged is one finding of the merged list: the text of the group's first
// blocking finding (or its first finding), with the group's severity.
type Merged struct {
	finding.Finding
	Tag
}

// Merge groups items by the judge's same_as links and decides each group.
// items must be in reviewer order, each reviewer's in its own order. A link
// is ignored when an ID is unknown, when both ends are one reviewer's, or
// when either end is in dismissed. Nil answers: Exact.
//
// Before the links, findings of different reviewers with the same
// finding.Key are one group (dismissed ones aside, as for links). So an
// exact match is always one group, and the judge cannot split it by
// leaving out same_as: a finding 2 reviewers marked blocking stays blocking.
func Merge(items []Item, answers Answers, dismissed map[string]bool) []Merged {
	if answers == nil {
		return Exact(items)
	}
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
	union := func(i, k int) {
		ri, rk := find(i), find(k)
		if rk < ri {
			ri, rk = rk, ri
		}
		parent[rk] = ri // the lowest index is the root: map order can't change the result
	}
	byKey := map[string][]int{}
	for i, it := range items {
		if !dismissed[it.ID] {
			byKey[finding.Key(it.Finding)] = append(byKey[finding.Key(it.Finding)], i)
		}
	}
	for _, same := range byKey {
		// Join each item to the first one of another reviewer: with 2+
		// reviewers in the list, that joins the whole list.
		for _, i := range same {
			for _, k := range same {
				if items[k].Reviewer != items[i].Reviewer {
					union(i, k)
					break
				}
			}
		}
	}
	for id, j := range answers {
		i, ok := index[id]
		if !ok || dismissed[id] {
			continue
		}
		for _, to := range j.SameAs {
			k, ok := index[to]
			if !ok || dismissed[to] || items[k].Reviewer == items[i].Reviewer {
				continue
			}
			union(i, k)
		}
	}
	return grouped(items, find, answers, true)
}

// Exact merges items by finding.Key, the exact match Dedup uses, with no
// judgement: every blocking finding stays blocking. The first item with a
// key is its group's root.
func Exact(items []Item) []Merged {
	first := map[string]int{}
	for i, it := range items {
		if _, ok := first[finding.Key(it.Finding)]; !ok {
			first[finding.Key(it.Finding)] = i
		}
	}
	return grouped(items, func(i int) int { return first[finding.Key(items[i].Finding)] }, nil, false)
}

// grouped collects each root's members in item order and decides each
// group, ordered by root index. The root is always the group's lowest index.
func grouped(items []Item, root func(int) int, answers Answers, judged bool) []Merged {
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
		out = append(out, decide(items, members[r], answers, judged))
	}
	return out
}

// decide picks a group's text and severity. A group with no blocking finding
// is a suggestion and is never judged. A group 2+ reviewers marked blocking
// stays blocking: the judge cannot demote it. A group one reviewer marked
// blocking is a suggestion only when the judge said "not real" about every
// blocking finding of that reviewer in the group; with no usable answer about
// any of them it stays blocking.
func decide(items []Item, group []int, answers Answers, judged bool) Merged {
	text := group[0]
	var found []int
	blockers := map[int]bool{}
	for _, i := range group {
		it := items[i]
		if !slices.Contains(found, it.Reviewer) {
			found = append(found, it.Reviewer)
		}
		if it.Severity == finding.Blocking {
			blockers[it.Reviewer] = true
			if items[text].Severity != finding.Blocking {
				text = i // the first blocking finding: the lowest reviewer number
			}
		}
	}
	slices.Sort(found)
	m := Merged{Finding: items[text].Finding, Tag: Tag{FoundBy: found}}
	for _, i := range group {
		if fp := items[i].Fingerprint; fp != "" && fp != m.Fingerprint && !slices.Contains(m.Also, fp) {
			m.Also = append(m.Also, fp)
		}
	}
	switch {
	case len(blockers) == 0:
		m.Severity = finding.Suggestion // never judged
		return m
	case len(blockers) >= 2:
		m.Severity = finding.Blocking // the judge cannot demote it
		return m
	}
	owner := items[text].Reviewer
	for _, n := range found {
		if n != owner {
			m.RatedSuggestionBy = append(m.RatedSuggestionBy, n)
		}
	}
	m.Severity = finding.Blocking
	if !judged {
		return m
	}
	confirmed, unanswered, reason := false, false, ""
	for _, i := range group {
		it := items[i]
		if it.Reviewer != owner || it.Severity != finding.Blocking {
			continue
		}
		j, ok := answers[it.ID]
		switch {
		case !ok || j.Real == nil:
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
		m.Unjudged = true
	default:
		m.Severity = finding.Suggestion
		m.JudgeDisagreed, m.Reason = true, reason
	}
	return m
}
