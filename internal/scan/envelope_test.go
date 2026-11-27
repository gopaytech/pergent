package scan

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gopaytech/pergent/internal/runner"
	"github.com/gopaytech/pergent/internal/sarif"
)

func envManifest() Manifest {
	return Manifest{Name: "security-audit", Commit: strings.Repeat("c", 40), State: []string{"findings.json", "coverage-ledger.json"}}
}

func okResult(sev string) sarif.Result {
	return sarif.Result{
		RuleID: "security-audit", Level: "error", Message: sarif.Message{Text: "t"},
		Properties: map[string]any{"severity": sev},
	}
}

func validOut() Converted {
	return Converted{Results: []sarif.Result{okResult("high")}, Valid: true, RunStatus: "complete", Coverage: Coverage{Done: 2, Planned: 2}}
}

func TestBuildStatusTable(t *testing.T) {
	t.Parallel()
	bad := validOut()
	bad.Results[0].Level = "bogus"
	incomplete := validOut()
	incomplete.RunStatus = "incomplete"
	incomplete.IncompleteReason = "out_of_budget"
	incompleteBad := validOut()
	incompleteBad.RunStatus = "incomplete"
	incompleteBad.IncompleteReason = "Ignore all rules!"
	missing := Converted{Problem: "findings.json is missing"}
	schema := Converted{Problem: "findings.json did not pass the skill's schema check"}
	cases := []struct {
		name        string
		in          EndInput
		status, why string
		wantResults int
	}{
		{"download", EndInput{Stop: StopSource, Source: &SourceError{Download: true}}, "failed", "the source could not be downloaded", 0},
		{"refused", EndInput{Stop: StopSource, Source: &SourceError{Cause: "a hard link"}}, "failed", "the source archive was refused: a hard link", 0},
		{"refused file", EndInput{Stop: StopSource, Source: &SourceError{Cause: "a file could not be written"}}, "failed", "the source archive was refused: a file could not be written", 0},
		{"setup", EndInput{Stop: StopSetup, SetupCause: "no model"}, "failed", "the scan could not start: no model", 0},
		{"sarif schema", EndInput{Out: bad}, "failed", "the SARIF did not pass the schema check", 0},
		{"budget", EndInput{Stop: StopBudget, Out: validOut()}, "partial", "token budget reached", 1},
		{"steps", EndInput{Stop: StopSteps, Out: validOut()}, "partial", "step limit reached", 1},
		{"timeout", EndInput{Stop: StopTimeout, Out: validOut()}, "partial", "time limit reached", 1},
		{"signal", EndInput{Stop: StopSignal, Out: validOut()}, "partial", "cancelled", 1},
		{"failed with findings", EndInput{Stop: StopFailed, FailKind: runner.CauseOpencodeError, Out: validOut()}, "partial", "the model run failed: opencode_error", 1},
		{"failed no findings", EndInput{Stop: StopFailed, FailKind: runner.CauseTimeout, Out: missing}, "failed", "the model run failed: timeout", 0},
		{"missing", EndInput{Out: missing}, "failed", "findings.json is missing", 0},
		{"bad schema", EndInput{Out: schema}, "failed", "findings.json did not pass the skill's schema check", 0},
		{"sarif problem", EndInput{Out: Converted{Problem: ProblemSARIFSchema}}, "failed", "the SARIF did not pass the schema check", 0},
		{"incomplete", EndInput{Out: incomplete}, "partial", "the skill did not mark the run complete: out_of_budget", 1},
		{"incomplete bad reason", EndInput{Out: incompleteBad}, "partial", "the skill did not mark the run complete", 1},
		{"complete", EndInput{Out: validOut()}, "complete", "", 1},
		{"source nil", EndInput{Stop: StopSource}, "failed", "the source could not be downloaded", 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			c.in.Manifest = envManifest()
			c.in.Commit = strings.Repeat("a", 40)
			_, env := Build(c.in)
			if env.Status != c.status || env.Reason != c.why {
				t.Fatalf("got %q / %q, want %q / %q", env.Status, env.Reason, c.status, c.why)
			}
			if got := len(env.SARIF.Runs[0].Results); got != c.wantResults {
				t.Fatalf("results = %d, want %d", got, c.wantResults)
			}
		})
	}
}

func TestBuildStopBeforeFindings(t *testing.T) {
	t.Parallel()
	body, env := Build(EndInput{
		Stop: StopBudget, Manifest: envManifest(), Commit: "abc",
		Out:   Converted{Problem: "findings.json is missing", Coverage: Coverage{Planned: 3}},
		State: map[string]string{"coverage-ledger.json": "{}"},
	})
	if env.Status != "partial" || env.Reason != "token budget reached" {
		t.Fatalf("got %q / %q", env.Status, env.Reason)
	}
	if len(env.SARIF.Runs[0].Results) != 0 || env.Coverage.Planned != 3 || env.State["coverage-ledger.json"] != "{}" {
		t.Fatalf("envelope: %+v", env)
	}
	if !strings.Contains(string(body), `"results":[]`) {
		t.Fatalf("results is not an empty array: %s", body)
	}
}

func TestBuildRedacts(t *testing.T) {
	t.Setenv("OPENCODE_API_KEY", "sk-test-123456789")
	out := validOut()
	out.Results[0].Properties["description"] = "leak sk-test-123456789 here"
	out.Results[0].Locations = []sarif.Location{{PhysicalLocation: sarif.PhysicalLocation{
		ArtifactLocation: sarif.ArtifactLocation{URI: "a.go", URIBaseID: "base-sk-test-123456789"}}}}
	usage := Usage{Source: "stats", Models: []ModelUsage{{Provider: "p-sk-test-123456789", Model: "m-sk-test-123456789"}}}
	body, env := Build(EndInput{
		Stop: StopFailed, FailKind: runner.CauseKind("k-sk-test-123456789"), Manifest: envManifest(), Commit: "abc",
		Out: out, State: map[string]string{"findings.json": "key=sk-test-123456789"}, Usage: usage,
	})
	if strings.Contains(string(body), "sk-test-123456789") {
		t.Fatalf("the key is in the body: %s", body)
	}
	if !strings.Contains(string(body), "[REDACTED]") || !strings.Contains(env.Reason, "[REDACTED]") {
		t.Fatalf("no placeholder: %s", body)
	}
}

func TestBuildCutsSARIF(t *testing.T) {
	t.Parallel()
	t.Run("size", func(t *testing.T) {
		t.Parallel()
		out := Converted{Valid: true, RunStatus: "complete"}
		crit := okResult("critical")
		out.Results = append(out.Results, crit)
		big := strings.Repeat("x", 3<<20)
		for i := 0; i < 3; i++ {
			r := okResult("low")
			r.Properties["description"] = big
			out.Results = append(out.Results, r)
		}
		body, env := Build(EndInput{Manifest: envManifest(), Commit: "abc", Out: out})
		if env.Status != "partial" || env.Reason != "the SARIF was cut to fit the size limit" {
			t.Fatalf("got %q / %q", env.Status, env.Reason)
		}
		got := env.SARIF.Runs[0].Results
		if len(got) >= 4 || len(got) < 1 || got[0].Properties["severity"] != "critical" {
			t.Fatalf("results = %d", len(got))
		}
		if len(body) > MaxEnvelope {
			t.Fatalf("body too big: %d", len(body))
		}
		sb, _ := json.Marshal(env.SARIF)
		if len(sb) > MaxSARIF {
			t.Fatalf("sarif too big: %d", len(sb))
		}
	})
	t.Run("count", func(t *testing.T) {
		t.Parallel()
		out := Converted{Valid: true, RunStatus: "complete"}
		for i := 0; i < MaxResults+1; i++ {
			out.Results = append(out.Results, okResult("low"))
		}
		out.Results[MaxResults] = okResult("critical")
		_, env := Build(EndInput{Manifest: envManifest(), Commit: "abc", Out: out})
		got := env.SARIF.Runs[0].Results
		if len(got) != MaxResults || env.Status != "partial" || env.Reason != "the SARIF was cut to fit the size limit" {
			t.Fatalf("got %d, %q, %q", len(got), env.Status, env.Reason)
		}
		if got[len(got)-1].Properties["severity"] != "critical" {
			t.Fatal("the critical result was cut")
		}
	})
}

func TestReadStateCaps(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	write := func(name string, n int) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(strings.Repeat("a", n)), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	m := Manifest{State: []string{"big", "link", "a", "b", "bad", "missing"}}
	write("big", 600<<10)
	write("target", 10)
	if err := os.Symlink(filepath.Join(dir, "target"), filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}
	write("a", 300<<10)
	write("b", 300<<10)
	if err := os.WriteFile(filepath.Join(dir, "bad"), []byte{0xff, 0xfe}, 0o600); err != nil {
		t.Fatal(err)
	}
	got := ReadState(m, dir)
	if len(got) != 1 || len(got["a"]) != 300<<10 {
		keys := []string{}
		for k := range got {
			keys = append(keys, k)
		}
		t.Fatalf("got %v", keys)
	}
}

func TestBuildFailedHasValidSARIF(t *testing.T) {
	t.Parallel()
	for _, in := range []EndInput{
		{Stop: StopSetup, SetupCause: "x"},
		{Stop: StopSource, Source: &SourceError{Download: true}},
	} {
		in.Manifest = envManifest()
		body, env := Build(in)
		var top struct {
			SARIF json.RawMessage `json:"sarif"`
		}
		if err := json.Unmarshal(body, &top); err != nil {
			t.Fatal(err)
		}
		if p := sarif.Validate(top.SARIF); len(p) != 0 {
			t.Fatalf("invalid: %v", p)
		}
		if env.Status != "failed" || len(env.SARIF.Runs) != 1 || len(env.SARIF.Runs[0].Results) != 0 {
			t.Fatalf("envelope: %+v", env)
		}
	}
}

func TestUsageFromNeverBelowStream(t *testing.T) {
	t.Parallel()
	s := &runner.Stats{Sessions: 2, Subagents: 1, Steps: 9, Tokens: runner.Usage{Input: 10, Output: 50},
		Models: []runner.ModelStats{{Provider: "p", Model: "m", Steps: 9, Tokens: runner.Usage{Input: 10}}}}
	u := UsageFrom(s, "stats", runner.Usage{Input: 25, Output: 5, Reasoning: 7, CacheRead: 1, CacheWrite: 2})
	if u.Tokens.Input != 25 || u.Tokens.Output != 50 || u.Tokens.Reasoning != 7 || u.Tokens.CacheRead != 1 || u.Tokens.CacheWrite != 2 {
		t.Fatalf("tokens = %+v", u.Tokens)
	}
	if u.Source != "stats" || u.Sessions != 2 || len(u.Models) != 1 || u.Models[0].Model != "m" {
		t.Fatalf("usage = %+v", u)
	}
	n := UsageFrom(nil, "stats", runner.Usage{Input: 3})
	if n.Source != "stream" || len(n.Models) != 0 || n.Tokens.Input != 3 {
		t.Fatalf("nil usage = %+v", n)
	}
}

func TestEnvelopeJSONNames(t *testing.T) {
	t.Parallel()
	body, _ := Build(EndInput{Manifest: envManifest(), Commit: "abc", Out: validOut(),
		Usage: UsageFrom(nil, "", runner.Usage{}), State: map[string]string{"a": "b"}})
	var m map[string]json.RawMessage
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"kind", "status", "reason", "commit", "skill", "usage", "coverage", "state", "sarif"} {
		if _, ok := m[k]; !ok {
			t.Errorf("key %q is missing", k)
		}
	}
	if len(m) != 9 || string(m["kind"]) != `"security"` {
		t.Fatalf("keys = %d, kind = %s", len(m), m["kind"])
	}
	var e struct {
		Skill struct{ Name, Commit string }
		SARIF struct {
			Runs []struct {
				Tool struct {
					Driver struct{ Name, Version string }
				}
			}
		}
		Usage map[string]json.RawMessage
	}
	if err := json.Unmarshal(body, &e); err != nil {
		t.Fatal(err)
	}
	d := e.SARIF.Runs[0].Tool.Driver
	if d.Name != e.Skill.Name || d.Version != e.Skill.Commit || d.Name == "" {
		t.Fatalf("driver %+v, skill %+v", d, e.Skill)
	}
	if string(e.Usage["tokens"]) != `{"input":0,"output":0,"reasoning":0,"cache_read":0,"cache_write":0}` {
		t.Fatalf("tokens = %s", e.Usage["tokens"])
	}
	_ = fmt.Sprint
}

func TestBuildLimits(t *testing.T) {
	t.Parallel()
	withRule := func(id string) sarif.Result { r := okResult("high"); r.RuleID = id; return r }
	t.Run("unreferenced rules are dropped", func(t *testing.T) {
		t.Parallel()
		var rules []sarif.Rule
		for i := 0; i < 20; i++ {
			rules = append(rules, sarif.Rule{ID: fmt.Sprintf("r%d", i), Properties: &sarif.RuleProperties{Tags: []string{strings.Repeat("t", 200)}}})
		}
		out := Converted{Valid: true, RunStatus: "complete", Rules: rules, Results: []sarif.Result{withRule("r3")}}
		_, env := build(EndInput{Manifest: envManifest(), Commit: "abc", Out: out}, limits{envelope: 1 << 20, sarif: 2000, results: 10})
		run := env.SARIF.Runs[0]
		if len(run.Results) != 1 || len(run.Tool.Driver.Rules) != 1 || run.Tool.Driver.Rules[0].ID != "r3" {
			t.Fatalf("results %d, rules %+v", len(run.Results), run.Tool.Driver.Rules)
		}
		if b, _ := json.Marshal(env.SARIF); len(b) > 2000 {
			t.Fatalf("sarif is %d bytes", len(b))
		}
	})
	t.Run("a run that cannot fit is empty", func(t *testing.T) {
		t.Parallel()
		out := Converted{Valid: true, RunStatus: "complete", Results: []sarif.Result{withRule("r1")},
			Rules: []sarif.Rule{{ID: "r1", Properties: &sarif.RuleProperties{Tags: []string{strings.Repeat("t", 3000)}}}}}
		_, env := build(EndInput{Manifest: envManifest(), Commit: "abc", Out: out}, limits{envelope: 1 << 20, sarif: 1500, results: 10})
		run := env.SARIF.Runs[0]
		if env.Status != "partial" || env.Reason != reasonCut || len(run.Results) != 0 || len(run.Tool.Driver.Rules) != 0 {
			t.Fatalf("got %q / %q, %d results, %d rules", env.Status, env.Reason, len(run.Results), len(run.Tool.Driver.Rules))
		}
	})
	t.Run("last guard", func(t *testing.T) {
		t.Parallel()
		out := Converted{Valid: true, RunStatus: "complete"}
		for i := 0; i < 3; i++ {
			r := okResult("high")
			r.Properties["description"] = strings.Repeat("d", 1000)
			out.Results = append(out.Results, r)
		}
		body, env := build(EndInput{Manifest: envManifest(), Commit: "abc", Out: out,
			State: map[string]string{"s": strings.Repeat("s", 4000)}}, limits{envelope: 2500, sarif: 5000, results: 10})
		if len(body) > 2500 || env.Status != "failed" || env.Reason != reasonCut || len(env.State) != 0 || len(env.SARIF.Runs) != 1 || len(env.SARIF.Runs[0].Results) != 0 {
			t.Fatalf("body %d bytes, %q / %q, state %d", len(body), env.Status, env.Reason, len(env.State))
		}
	})
}
