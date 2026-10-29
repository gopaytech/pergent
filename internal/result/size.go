package result

import (
	"encoding/json"
	"fmt"
)

// MaxSize is the largest encoded result: 10 MB.
const MaxSize = 10 * 1024 * 1024

const (
	checksOmittedNote    = "Checks omitted to fit the 10 MB result limit."
	summariesOmittedNote = "Summaries omitted to fit the 10 MB result limit."
)

// Encode redacts every string of the result and encodes it as JSON. When
// the JSON would exceed MaxSize it drops every skill's per-batch checks,
// then, if still too large, every skill's summaries, adding a note to each
// skill that lost something. It does not change r.
func Encode(r Result) ([]byte, error) {
	r = redactResult(r)
	b, err := json.Marshal(r)
	if err != nil {
		return nil, err
	}
	if len(b) <= MaxSize {
		return b, nil
	}

	r.Skills = append([]Skill(nil), r.Skills...)
	drop(&r, checksOmittedNote, func(s *Skill) bool {
		if len(s.Checks) == 0 {
			return false
		}
		s.Checks = nil
		return true
	})
	if b, err = json.Marshal(r); err != nil {
		return nil, err
	}
	if len(b) <= MaxSize {
		return b, nil
	}

	drop(&r, summariesOmittedNote, func(s *Skill) bool {
		if len(s.Summaries) == 0 {
			return false
		}
		s.Summaries = nil
		return true
	})
	if b, err = json.Marshal(r); err != nil {
		return nil, err
	}
	if len(b) > MaxSize {
		return nil, fmt.Errorf("result is %d bytes, over the %d byte limit even without checks and summaries", len(b), MaxSize)
	}
	return b, nil
}

// drop applies clear to every skill (already a copy owned by r) and adds
// note to those it changed.
func drop(r *Result, note string, clear func(*Skill) bool) {
	for i := range r.Skills {
		if clear(&r.Skills[i]) {
			r.Skills[i].Notes = append(append([]string(nil), r.Skills[i].Notes...), note)
		}
	}
}
