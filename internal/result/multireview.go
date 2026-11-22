package result

import (
	"slices"

	"github.com/gopaytech/pergent/internal/review"
)

// RoleJudge is the Role of a provider error that came from the judge run of
// a multi review.
const RoleJudge = "judge"

// MultiReview is what a multi review says about its run: each reviewer, and
// the judge.
type MultiReview struct {
	Reviewers []MultiReviewer `json:"reviewers"`
	Judge     MultiJudge      `json:"judge"`
}

// MultiReviewer is one reviewer. Number starts at 1. Usable is false when
// the reviewer gave no usable review of any skill.
type MultiReviewer struct {
	Number int  `json:"number"`
	Usable bool `json:"usable"`
}

// MultiJudge names the judge's model. Judged is false when no judge run was
// allowed (for example with --no-judge); then Provider and Model are empty.
type MultiJudge struct {
	Provider string `json:"provider,omitempty"`
	Model    string `json:"model,omitempty"`
	Judged   bool   `json:"judged"`
}

// SkillMultiReview is how a multi review merged one skill. Reviewers are the
// usable ones, and Failed the others. Judge is the status of the judge run.
type SkillMultiReview struct {
	Reviewers  []int  `json:"reviewers"`
	Failed     []int  `json:"failed,omitempty"`
	Judge      string `json:"judge"`
	JudgeCause *Cause `json:"judge_cause,omitempty"`
	ExactMerge bool   `json:"exact_merge,omitempty"`
}

// FindingMultiReview is what the merge says about one finding: the reviewers
// that found it, the fingerprints of the same finding from other reviewers,
// and what the judge said.
type FindingMultiReview struct {
	FoundBy           []int    `json:"found_by"`
	Also              []string `json:"also,omitempty"`
	JudgeDisagreed    bool     `json:"judge_disagreed,omitempty"`
	Reason            string   `json:"reason,omitempty"`
	RatedSuggestionBy []int    `json:"rated_suggestion_by,omitempty"`
	Unjudged          bool     `json:"unjudged,omitempty"`
}

// BuildMultiReview is Build plus the multi review fields. The top
// provider_error is the most severe of Build's and the judge causes' (role
// "judge", with meta's provider and model). Build's wins a tie. It does not
// change m or skills.
func BuildMultiReview(meta Meta, m MultiReview, skills []review.SkillResult) Result {
	r := Build(meta, skills)
	mc := MultiReview{
		Reviewers: append([]MultiReviewer{}, m.Reviewers...),
		Judge:     m.Judge,
	}
	r.MultiReview = &mc
	for i, s := range skills {
		if s.MultiReview == nil || i >= len(r.Skills) {
			continue
		}
		buildSkillMultiReview(meta, &r.Skills[i], s)
		jc := r.Skills[i].MultiReview.JudgeCause
		if jc == nil || jc.ProviderError == nil {
			continue
		}
		pe := jc.ProviderError
		if r.ProviderError == nil || providerSeverity[pe.Class] > providerSeverity[r.ProviderError.Class] {
			c := *pe
			r.ProviderError = &c
		}
	}
	return r
}

func buildSkillMultiReview(meta Meta, out *Skill, s review.SkillResult) {
	mr := s.MultiReview
	sm := &SkillMultiReview{
		Reviewers:  clonedInts(mr.Reviewers),
		Failed:     slices.Clone(mr.Failed),
		Judge:      mr.Judge,
		ExactMerge: mr.Exact,
	}
	if len(sm.Failed) == 0 {
		sm.Failed = nil
	}
	if mr.Cause != nil {
		c := &Cause{Kind: string(mr.Cause.Kind), Message: plainText(mr.Cause.Message())}
		if pe := providerError(meta, mr.Cause); pe != nil {
			pe.Role = RoleJudge
			c.ProviderError = pe
		}
		sm.JudgeCause = c
	}
	out.MultiReview = sm
	for i, t := range mr.Tags {
		if i >= len(out.Findings) {
			break
		}
		out.Findings[i].MultiReview = &FindingMultiReview{
			FoundBy:           clonedInts(t.FoundBy),
			Also:              slices.Clone(t.Also),
			JudgeDisagreed:    t.JudgeDisagreed,
			Reason:            t.Reason,
			RatedSuggestionBy: slices.Clone(t.RatedSuggestionBy),
			Unjudged:          t.Unjudged,
		}
	}
}

// clonedInts is a copy of in that is never nil, so it encodes as [].
func clonedInts(in []int) []int {
	return append([]int{}, in...)
}
