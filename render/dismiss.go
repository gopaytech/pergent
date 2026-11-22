package render

// Dismissal sets aside the findings with one fingerprint, up to a severity.
type Dismissal struct {
	Fingerprint string
	Severity    string // "blocking" covers blocking findings and suggestions; "suggestion" only suggestions
	By          string
	Reason      string
}

// severityRank orders what a dismissal covers. Like the comment, it counts
// any severity but Suggestion as blocking.
func severityRank(s Severity) int {
	if s == Suggestion {
		return 1
	}
	return 2
}

// dismissalRank is the highest severityRank a dismissal covers. An unknown
// severity covers nothing.
func dismissalRank(s string) int {
	switch Severity(s) {
	case Suggestion:
		return 1
	case Blocking:
		return 2
	}
	return 0
}

// ApplyDismissals moves each finding a dismissal covers into Skill.Dismissed.
// A dismissal covers a finding with the same fingerprint whose severity is no
// higher than the dismissal's, so a suggestion dismissal leaves a blocking
// finding in place. When several dismissals cover a finding, the first one
// wins.
//
// It recomputes the verdict of each skill it changed: a has_blocking_issues
// skill left with no blocking finding becomes no_blocking_issues. A failed
// skill stays failed. skills is not modified.
func ApplyDismissals(skills []Skill, ds []Dismissal) []Skill {
	out := make([]Skill, len(skills))
	copy(out, skills)
	if len(ds) == 0 {
		return out
	}
	for i := range out {
		s := &out[i]
		kept := []Finding{}
		dismissed := append([]DismissedFinding(nil), s.Dismissed...)
		moved := false
		for _, f := range s.Findings {
			if d, ok := covering(ds, f); ok {
				dismissed = append(dismissed, DismissedFinding{Finding: f, By: d.By, Reason: d.Reason})
				moved = true
				continue
			}
			kept = append(kept, f)
		}
		if !moved {
			continue
		}
		s.Findings = kept
		s.Dismissed = dismissed
		if s.Verdict == VerdictHasBlockingIssues && !hasBlocking(kept) {
			s.Verdict = VerdictNoBlockingIssues
		}
	}
	return out
}

// covering returns the first dismissal that covers f. A finding without a
// fingerprint can't be dismissed.
func covering(ds []Dismissal, f Finding) (Dismissal, bool) {
	if f.Fingerprint == "" {
		return Dismissal{}, false
	}
	for _, d := range ds {
		if d.Fingerprint == f.Fingerprint && severityRank(f.Severity) <= dismissalRank(d.Severity) {
			return d, true
		}
	}
	return Dismissal{}, false
}

func hasBlocking(fs []Finding) bool {
	for _, f := range fs {
		if f.Severity != Suggestion {
			return true
		}
	}
	return false
}
