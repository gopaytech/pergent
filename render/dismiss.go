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

// covering returns the first dismissal that covers f. A merged finding stands
// for a group, so a dismissal of any fingerprint in the group (f's own, or one
// in MultiReview.Also) covers it.
func covering(ds []Dismissal, f Finding) (Dismissal, bool) {
	for _, d := range ds {
		if covers(d, f.Fingerprint, f.Severity) {
			return d, true
		}
		if f.MultiReview == nil {
			continue
		}
		for _, fp := range f.MultiReview.Also {
			if covers(d, fp, f.Severity) {
				return d, true
			}
		}
	}
	return Dismissal{}, false
}

// Covers reports whether a dismissal in ds covers a finding with this
// fingerprint and severity, by the rule ApplyDismissals uses: the same
// fingerprint, and a severity no higher than the dismissal's. A finding
// without a fingerprint can't be dismissed.
func Covers(ds []Dismissal, fingerprint string, sev Severity) bool {
	for _, d := range ds {
		if covers(d, fingerprint, sev) {
			return true
		}
	}
	return false
}

// covers is the one rule of what a dismissal covers.
func covers(d Dismissal, fingerprint string, sev Severity) bool {
	return fingerprint != "" && d.Fingerprint == fingerprint && severityRank(sev) <= dismissalRank(d.Severity)
}

func hasBlocking(fs []Finding) bool {
	for _, f := range fs {
		if f.Severity != Suggestion {
			return true
		}
	}
	return false
}
