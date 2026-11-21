package result

import (
	"github.com/gopaytech/pergent/internal/panel"
	"github.com/gopaytech/pergent/internal/review"
)

// Panel is what a review panel's merge says about its members: each one's
// provider and model, Mode "both", "a_only" or "b_only", and whether any
// judge run was allowed (false with --panel-no-judge).
type Panel struct {
	A      PanelMember `json:"a"`
	B      PanelMember `json:"b"`
	Mode   string      `json:"mode"`
	Judged bool        `json:"judged"`
}

// PanelMember names a member's model.
type PanelMember struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
}

// SkillPanel is how the skill was merged.
type SkillPanel struct {
	Mode       string       `json:"mode"`
	Judges     []PanelJudge `json:"judges,omitempty"`
	ExactMerge bool         `json:"exact_merge,omitempty"`
}

// PanelJudge is one judge run: Role is the model that judged.
type PanelJudge struct {
	Role   string `json:"role"`
	Status string `json:"status"`
	Cause  *Cause `json:"cause,omitempty"`
}

// FindingPanel is what the merge says about a finding.
type FindingPanel struct {
	FoundBy           string   `json:"found_by"`
	Also              []string `json:"also,omitempty"`
	DisagreedBy       string   `json:"disagreed_by,omitempty"`
	Reason            string   `json:"reason,omitempty"`
	RatedSuggestionBy string   `json:"rated_suggestion_by,omitempty"`
	UnjudgedBy        string   `json:"unjudged_by,omitempty"`
}

// ModelTokens is the tokens of a skill's runs on one model.
type ModelTokens struct {
	Role     string `json:"role"`
	Provider string `json:"provider"`
	Model    string `json:"model"`
	Tokens   Tokens `json:"tokens"`
}

// BuildPanel is Build for a review panel's merge: the skills carry their
// panel fields, tokens per model, and the top provider_error is the most
// severe of the judge runs' (with its role and that model's names).
func BuildPanel(meta Meta, p Panel, skills []review.SkillResult) Result {
	r := Build(meta, skills)
	pc := p
	r.Panel = &pc
	for i, s := range skills {
		if s.Panel == nil || i >= len(r.Skills) {
			continue
		}
		buildSkillPanel(&r.Skills[i], s, p)
	}
	// A judge run's failure is not a batch failure; the most severe error
	// of all of them is the top one. A batch error wins a tie.
	for _, sk := range r.Skills {
		if sk.Panel == nil {
			continue
		}
		for _, j := range sk.Panel.Judges {
			if j.Cause == nil || j.Cause.ProviderError == nil {
				continue
			}
			pe := j.Cause.ProviderError
			if r.ProviderError == nil || providerSeverity[pe.Class] > providerSeverity[r.ProviderError.Class] {
				c := *pe
				c.Role = j.Role
				r.ProviderError = &c
			}
		}
	}
	return r
}

// member is the names of the model a role stands for.
func (p Panel) member(role string) PanelMember {
	if role == panel.B {
		return p.B
	}
	return p.A
}

func buildSkillPanel(out *Skill, s review.SkillResult, p Panel) {
	sp := &SkillPanel{Mode: s.Panel.Mode, ExactMerge: s.Panel.Exact}
	for _, j := range s.Panel.Judges {
		pj := PanelJudge{Role: j.Role, Status: j.Status}
		if j.Cause != nil {
			m := p.member(j.Role)
			c := &Cause{Kind: string(j.Cause.Kind), Message: plainText(j.Cause.Message())}
			c.ProviderError = providerError(Meta{Provider: m.Provider, Model: m.Model}, j.Cause)
			pj.Cause = c
		}
		sp.Judges = append(sp.Judges, pj)
	}
	out.Panel = sp
	for i, t := range s.Panel.Tags {
		if i >= len(out.Findings) {
			break
		}
		out.Findings[i].Panel = &FindingPanel{
			FoundBy: t.FoundBy, Also: t.Also, DisagreedBy: t.DisagreedBy, Reason: t.Reason,
			RatedSuggestionBy: t.RatedSuggestionBy, UnjudgedBy: t.UnjudgedBy,
		}
	}
	for _, role := range []string{panel.A, panel.B} {
		u, ok := s.Panel.UsageByRole[role]
		if !ok {
			continue
		}
		m := p.member(role)
		out.TokensByModel = append(out.TokensByModel, ModelTokens{Role: role, Provider: m.Provider, Model: m.Model, Tokens: tokensOf(u)})
	}
}
