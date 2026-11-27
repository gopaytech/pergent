package scan

import (
	"bytes"
	"encoding/json"
	"regexp"
	"sort"
	"unicode/utf8"

	"github.com/gopaytech/pergent/internal/redact"
	"github.com/gopaytech/pergent/internal/result"
	"github.com/gopaytech/pergent/internal/runner"
	"github.com/gopaytech/pergent/internal/sarif"
)

// The size limits of the envelope. pergent.sh sets its body limit to MaxEnvelope.
const (
	MaxEnvelope  = 12 << 20
	MaxSARIF     = 8 << 20
	MaxResults   = 5000
	MaxState     = 512 << 10
	MaxStateFile = 512 << 10
)

// The fixed reason texts. Model text never goes into a reason, except the
// incomplete reason that passes incompleteReasonRe.
const (
	reasonCut        = "the SARIF was cut to fit the size limit"
	reasonNotMarked  = "the skill did not mark the run complete"
	reasonStartFmt   = "the scan could not start: "
	reasonModelFails = "the model run failed: "
	reasonDownload   = "the source could not be downloaded"
)

var incompleteReasonRe = regexp.MustCompile(`^[a-z0-9_]{1,64}$`)

// SkillRef names the skill that made the scan and its pinned commit.
type SkillRef struct {
	Name   string `json:"name"`
	Commit string `json:"commit"`
}

// ModelUsage is one model's share of the usage.
type ModelUsage struct {
	Provider string        `json:"provider"`
	Model    string        `json:"model"`
	Steps    int           `json:"steps"`
	Tokens   result.Tokens `json:"tokens"`
}

// Usage is what the scan used. Source is stats, stats_poll or stream.
type Usage struct {
	Source    string        `json:"source"`
	Sessions  int           `json:"sessions"`
	Subagents int           `json:"subagents"`
	Steps     int           `json:"steps"`
	Tokens    result.Tokens `json:"tokens"`
	Models    []ModelUsage  `json:"models"`
}

// Envelope is the body pergent POSTs to pergent.sh.
type Envelope struct {
	Kind     string            `json:"kind"`
	Status   string            `json:"status"`
	Reason   string            `json:"reason"`
	Commit   string            `json:"commit"`
	Skill    SkillRef          `json:"skill"`
	Usage    Usage             `json:"usage"`
	Coverage Coverage          `json:"coverage"`
	State    map[string]string `json:"state"`
	SARIF    sarif.Log         `json:"sarif"`
}

// Stop says why the run ended.
type Stop int

const (
	StopNone    Stop = iota // the run ended on its own
	StopBudget              // the token budget was reached
	StopSteps               // a step limit was reached
	StopTimeout             // the time limit was reached
	StopSignal              // pergent got a signal
	StopFailed              // opencode failed on its own
	StopSetup               // a failure before opencode ran
	StopSource              // the source could not be used
)

// EndInput is everything Build needs.
type EndInput struct {
	Commit     string
	Stop       Stop
	FailKind   runner.CauseKind
	SetupCause string
	Source     *SourceError
	Out        Converted
	State      map[string]string
	Usage      Usage
	Manifest   Manifest
}

// UsageFrom makes the usage of a scan. s is the final or last polled stats
// (nil when no read worked: then only the main session is known and the
// source is always "stream"). Each token count is at least the stream's.
func UsageFrom(s *runner.Stats, source string, stream runner.Usage) Usage {
	if s == nil {
		return Usage{Source: "stream", Tokens: tokensOf(stream), Models: []ModelUsage{}}
	}
	u := Usage{
		Source: source, Sessions: s.Sessions, Subagents: s.Subagents, Steps: s.Steps,
		Tokens: tokensOf(maxUsage(s.Tokens, stream)), Models: []ModelUsage{},
	}
	for _, m := range s.Models {
		u.Models = append(u.Models, ModelUsage{Provider: m.Provider, Model: m.Model, Steps: m.Steps, Tokens: tokensOf(m.Tokens)})
	}
	return u
}

func tokensOf(u runner.Usage) result.Tokens {
	return result.Tokens{Input: u.Input, Output: u.Output, Reasoning: u.Reasoning, CacheRead: u.CacheRead, CacheWrite: u.CacheWrite}
}

func maxUsage(a, b runner.Usage) runner.Usage {
	return runner.Usage{
		Input: max(a.Input, b.Input), Output: max(a.Output, b.Output), Reasoning: max(a.Reasoning, b.Reasoning),
		CacheRead: max(a.CacheRead, b.CacheRead), CacheWrite: max(a.CacheWrite, b.CacheWrite),
	}
}

// ReadState reads the manifest's state files from outDir. A file that is
// missing, not a regular file (a link is refused), over MaxStateFile or not
// valid UTF-8 is left out. A file that would take the total over MaxState is
// left out too, so the earlier files in manifest order win.
func ReadState(m Manifest, outDir string) map[string]string {
	out := map[string]string{}
	total := 0
	for _, name := range m.State {
		data, err := ReadOutFile(outDir, name, MaxStateFile)
		if err != nil || !utf8.Valid(data) || total+len(data) > MaxState {
			continue
		}
		total += len(data)
		out[name] = string(data)
	}
	return out
}

// Build decides the status and makes the envelope. It returns the JSON body
// and the envelope it came from.
func Build(in EndInput) ([]byte, Envelope) {
	m := in.Manifest
	driver := sarif.Driver{Name: m.Name, Version: m.Commit}
	env := Envelope{
		Kind:     "security",
		Commit:   in.Commit,
		Skill:    SkillRef{Name: m.Name, Commit: m.Commit},
		Usage:    in.Usage,
		Coverage: in.Out.Coverage,
		State:    map[string]string{},
	}
	if env.Usage.Models == nil {
		env.Usage.Models = []ModelUsage{}
	}
	for k, v := range in.State {
		env.State[redact.String(k)] = redact.String(v)
	}

	var reasons []string
	status := "complete"
	var results []sarif.Result
	var rules []sarif.Rule
	useResults := false

	switch in.Stop {
	case StopSource:
		status = "failed"
		if in.Source != nil {
			reasons = append(reasons, in.Source.Error())
		} else {
			reasons = append(reasons, reasonDownload)
		}
	case StopSetup:
		status = "failed"
		reasons = append(reasons, reasonStartFmt+in.SetupCause)
	case StopBudget, StopSteps, StopTimeout, StopSignal:
		status = "partial"
		reasons = append(reasons, stopText(in.Stop))
		useResults = in.Out.Valid
	case StopFailed:
		useResults = in.Out.Valid
		status = "failed"
		if in.Out.Valid {
			status = "partial"
		}
		reasons = append(reasons, reasonModelFails+string(in.FailKind))
	default:
		if !in.Out.Valid {
			status = "failed"
			reasons = append(reasons, in.Out.Problem)
			break
		}
		useResults = true
		if in.Out.RunStatus != "complete" {
			status = "partial"
			r := reasonNotMarked
			if incompleteReasonRe.MatchString(in.Out.IncompleteReason) {
				r += ": " + in.Out.IncompleteReason
			}
			reasons = append(reasons, r)
		}
	}

	if useResults {
		results = redactResults(in.Out.Results)
		rules = redactRules(in.Out.Rules)
		var cut bool
		results, cut = cutResults(results, driver, rules)
		if cut {
			status = "partial"
			reasons = append(reasons, reasonCut)
		}
	}
	driver.Rules = rules
	env.SARIF = sarif.Empty(driver)
	if results != nil {
		env.SARIF.Runs[0].Results = results
	}

	// The SARIF pergent built must pass the schema, or the scan failed.
	if data, err := marshal(env.SARIF); err != nil || len(sarif.Validate(data)) > 0 {
		status = "failed"
		reasons = []string{ProblemSARIFSchema}
		driver.Rules = nil
		env.SARIF = sarif.Empty(driver)
	}

	env.Status = status
	env.Reason = redact.String(join(reasons))
	if status == "complete" {
		env.Reason = ""
	}
	body, _ := marshal(env)
	if len(body) > MaxEnvelope {
		// Models are the only unbounded part left; state follows if needed.
		env.Usage.Models = []ModelUsage{}
		body, _ = marshal(env)
		if len(body) > MaxEnvelope {
			env.State = map[string]string{}
			body, _ = marshal(env)
		}
	}
	return body, env
}

func stopText(s Stop) string {
	switch s {
	case StopBudget:
		return "token budget reached"
	case StopSteps:
		return "step limit reached"
	case StopTimeout:
		return "time limit reached"
	}
	return "cancelled"
}

func join(rs []string) string {
	var b bytes.Buffer
	for i, r := range rs {
		if i > 0 {
			b.WriteString("; ")
		}
		b.WriteString(r)
	}
	return b.String()
}

// marshal writes JSON without HTML escaping, so the size is the real one.
func marshal(v any) ([]byte, error) {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(b.Bytes(), "\n"), nil
}

// severityRank is higher for a worse result. A result with no severity
// (needs validation) is the lowest.
func severityRank(r sarif.Result) int {
	if s, ok := r.Properties["severity"].(string); ok {
		switch s {
		case "critical":
			return 4
		case "high":
			return 3
		case "medium":
			return 2
		case "low":
			return 1
		}
	}
	if r.Level == "error" {
		return 3
	}
	return 0
}

// cutResults keeps the results that fit MaxResults and MaxSARIF. It drops the
// lowest severity first and, inside one severity, the last result first. The
// kept results stay in their first order.
func cutResults(rs []sarif.Result, d sarif.Driver, rules []sarif.Rule) ([]sarif.Result, bool) {
	d.Rules = rules
	base, _ := marshal(sarif.Empty(d))
	sizes := make([]int, len(rs))
	total := len(base)
	for i, r := range rs {
		b, _ := marshal(r)
		sizes[i] = len(b) + 1 // the comma
		total += sizes[i]
	}
	if len(rs) <= MaxResults && total <= MaxSARIF {
		return rs, false
	}
	order := make([]int, len(rs))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool { return severityRank(rs[order[a]]) > severityRank(rs[order[b]]) })
	keep := make([]bool, len(rs))
	used, n := len(base), 0
	for _, i := range order {
		if n >= MaxResults || used+sizes[i] > MaxSARIF {
			break
		}
		keep[i] = true
		used += sizes[i]
		n++
	}
	out := make([]sarif.Result, 0, n)
	for i, r := range rs {
		if keep[i] {
			out = append(out, r)
		}
	}
	return out, true
}

func redactResults(in []sarif.Result) []sarif.Result {
	out := make([]sarif.Result, len(in))
	for i, r := range in {
		r.RuleID = redact.String(r.RuleID)
		r.Level = redact.String(r.Level)
		r.Message.Text = redact.String(r.Message.Text)
		if r.Locations != nil {
			locs := make([]sarif.Location, len(r.Locations))
			for j, l := range r.Locations {
				l.PhysicalLocation.ArtifactLocation.URI = redact.String(l.PhysicalLocation.ArtifactLocation.URI)
				locs[j] = l
			}
			r.Locations = locs
		}
		if r.Fingerprints != nil {
			fp := make(map[string]string, len(r.Fingerprints))
			for k, v := range r.Fingerprints {
				fp[redact.String(k)] = redact.String(v)
			}
			r.Fingerprints = fp
		}
		if r.Properties != nil {
			p := make(map[string]any, len(r.Properties))
			for k, v := range r.Properties {
				p[redact.String(k)] = redactAny(v)
			}
			r.Properties = p
		}
		out[i] = r
	}
	return out
}

func redactAny(v any) any {
	switch t := v.(type) {
	case string:
		return redact.String(t)
	case []any:
		o := make([]any, len(t))
		for i, e := range t {
			o[i] = redactAny(e)
		}
		return o
	case []string:
		o := make([]string, len(t))
		for i, e := range t {
			o[i] = redact.String(e)
		}
		return o
	case map[string]any:
		o := make(map[string]any, len(t))
		for k, e := range t {
			o[redact.String(k)] = redactAny(e)
		}
		return o
	}
	return v
}

func redactRules(in []sarif.Rule) []sarif.Rule {
	if in == nil {
		return nil
	}
	out := make([]sarif.Rule, len(in))
	for i, r := range in {
		r.ID = redact.String(r.ID)
		if r.Properties != nil {
			p := *r.Properties
			p.Tags = make([]string, len(r.Properties.Tags))
			for j, t := range r.Properties.Tags {
				p.Tags[j] = redact.String(t)
			}
			r.Properties = &p
		}
		out[i] = r
	}
	return out
}
