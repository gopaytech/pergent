package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/gopaytech/pergent/internal/result"
	"github.com/gopaytech/pergent/internal/runner"
	"github.com/gopaytech/pergent/rule"
)

const twoRules = `[{"source":"repo","path":"*.go","rule":"GO RULE"},{"source":"dashboard:base","path":"**/*.md","rule":"MD RULE"}]`

// rulesRun answers every run with no findings and records the file-rules.md
// each run got ("" for none).
type rulesRun struct {
	mu    sync.Mutex
	rules []string
}

func (r *rulesRun) run(t *testing.T) func(context.Context, runner.Options) runner.RunResult {
	inner := answerRun(t)
	return func(ctx context.Context, opts runner.Options) runner.RunResult {
		got := ""
		for _, p := range opts.Files {
			if filepath.Base(p) == "file-rules.md" {
				data, err := os.ReadFile(p)
				if err != nil {
					t.Error(err)
				}
				got = string(data)
			}
		}
		r.mu.Lock()
		r.rules = append(r.rules, got)
		r.mu.Unlock()
		return inner(ctx, opts)
	}
}

func rulesFileOutside(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "rules.json")
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func agentRulesResult(t *testing.T, args []string, env func(string) string) (result.Result, *rulesRun) {
	t.Helper()
	sandboxEnv(t)
	var stdout, stderr bytes.Buffer
	rr := &rulesRun{}
	if code := runAgent(context.Background(), args, env, &stdout, &stderr, rr.run(t)); code != 0 {
		t.Fatalf("exit code = %d; stderr:\n%s", code, stderr.String())
	}
	return decodeOnly(t, stdout.Bytes()), rr
}

func TestAgentRulesPassed(t *testing.T) {
	dir, base, head := agentRepo(t, nil)
	res, rr := agentRulesResult(t, agentArgs(dir, base, head, oneSkill(t), "--rules", rulesFileOutside(t, twoRules)), agentEnv(nil))
	if res.Rules == nil || !res.Rules.BuiltIn || res.Rules.Entries != 2+len(builtinNames()) {
		t.Errorf("rules = %+v, want built-ins on and 2 team entries", res.Rules)
	}
	if len(rr.rules) != 1 || !strings.Contains(rr.rules[0], "GO RULE") || !strings.Contains(rr.rules[0], `Files: "a.go"`) {
		t.Errorf("file-rules.md = %q, want the Go rule for a.go", rr.rules)
	}
	if fr := res.Skills[0].Files.Rules; len(fr) != 1 || fr[0] != (result.FileRule{Path: "a.go", Rule: "repo:*.go"}) {
		t.Errorf("files.rules = %+v", fr)
	}
}

func TestAgentRulesFromEnv(t *testing.T) {
	sha := strings.Repeat("a", 40)
	args := []string{"--repo-path", "/r", "--base", sha, "--head", sha, "--skills-dir", "/s", "--result", "-"}
	env := agentEnv(map[string]string{"PERGENT_RULES": "/env/rules.json"})
	in, e := parseAgent(args, env)
	if e != nil || in.rulesPath != "/env/rules.json" {
		t.Fatalf("rulesPath = %q, %+v; want the PERGENT_RULES path", in.rulesPath, e)
	}
	in, e = parseAgent(append(args, "--rules", "/flag/rules.json"), env)
	if e != nil || in.rulesPath != "/flag/rules.json" {
		t.Errorf("rulesPath = %q, %+v; want the flag to win", in.rulesPath, e)
	}
}

func TestAgentRulesInsideRepoRefused(t *testing.T) {
	repo, base, head := agentRepo(t, func(dir string) { writeFile(t, dir, "rules.json", twoRules) })
	in := mustParse(t, agentArgs(repo, base, head, oneSkill(t), "--rules", filepath.Join(repo, "rules.json")))
	e := prepareAgent(&in, quietLog)
	if e == nil || e.Code != result.CodeUsage || !strings.Contains(e.Message, "--rules") {
		t.Errorf("prepareAgent() = %+v, want a usage error naming --rules", e)
	}
}

func TestAgentRulesMalformedIsUsageError(t *testing.T) {
	repo, base, head := agentRepo(t, nil)
	p := rulesFileOutside(t, `[{"source":"repo","path":"api/","rule":"x"}]`)
	in := mustParse(t, agentArgs(repo, base, head, oneSkill(t), "--rules", p))
	e := prepareAgent(&in, quietLog)
	want := "--rules " + p + ": rules list: entry 1: path: "
	if e == nil || e.Code != result.CodeUsage || !strings.HasPrefix(e.Message, want) {
		t.Errorf("prepareAgent() = %+v, want a usage error starting %q", e, want)
	}
}

func TestAgentRulesUnreadableIsUsageError(t *testing.T) {
	repo, base, head := agentRepo(t, nil)
	p := filepath.Join(t.TempDir(), "missing.json")
	in := mustParse(t, agentArgs(repo, base, head, oneSkill(t), "--rules", p))
	e := prepareAgent(&in, quietLog)
	if e == nil || e.Code != result.CodeUsage || !strings.HasPrefix(e.Message, "--rules "+p+": ") {
		t.Errorf("prepareAgent() = %+v, want a usage error", e)
	}
	// Through runAgent the review is a failed result, never one without rules.
	sandboxEnv(t)
	var stdout, stderr bytes.Buffer
	code := runAgent(context.Background(), agentArgs(repo, base, head, oneSkill(t), "--rules", p), agentEnv(nil), &stdout, &stderr, answerRun(t))
	res := decodeOnly(t, stdout.Bytes())
	if code != 2 || res.Status != result.StatusError || res.Error == nil || res.Error.Code != result.CodeUsage {
		t.Errorf("code %d, result %+v; want exit 2 and a usage error result", code, res)
	}
}

func TestAgentNoRulesFlag(t *testing.T) {
	dir, base, head := agentRepo(t, nil)
	res, rr := agentRulesResult(t, agentArgs(dir, base, head, oneSkill(t)), agentEnv(nil))
	if res.Rules == nil || !res.Rules.BuiltIn || res.Rules.Entries != len(builtinNames()) {
		t.Errorf("rules = %+v, want built-ins on and no team entries", res.Rules)
	}
	// With no --rules, a run's file-rules.md holds built-in rules only.
	for _, got := range rr.rules {
		for _, line := range strings.Split(got, "\n") {
			if strings.HasPrefix(line, "## Rule: ") && !strings.HasPrefix(line, "## Rule: built-in:") {
				t.Errorf("a run got a team rule with no --rules: %q", line)
			}
		}
	}
}

func TestAgentNoBuiltInRules(t *testing.T) {
	dir, base, head := agentRepo(t, nil)
	res, _ := agentRulesResult(t, agentArgs(dir, base, head, oneSkill(t), "--no-built-in-rules"), agentEnv(nil))
	if res.Rules == nil || res.Rules.BuiltIn || res.Rules.Entries != 0 {
		t.Errorf("rules = %+v, want built-ins off", res.Rules)
	}
	t.Setenv("PERGENT_NO_BUILT_IN_RULES", "true")
	res, _ = agentRulesResult(t, agentArgs(dir, base, head, oneSkill(t)), agentEnv(nil))
	if res.Rules == nil || res.Rules.BuiltIn {
		t.Errorf("env: rules = %+v, want built-ins off", res.Rules)
	}
	t.Setenv("PERGENT_NO_BUILT_IN_RULES", "maybe")
	if _, e := parseAgent(agentArgs(dir, base, head, oneSkill(t)), agentEnv(nil)); e == nil || e.Code != result.CodeUsage {
		t.Errorf("parseAgent() with a bad value = %+v, want a usage error", e)
	}
}

// The change under review cannot add rules: agent mode never reads the
// checkout's .pergent/rules.yaml.
func TestAgentIgnoresCheckoutRulesFile(t *testing.T) {
	dir, base, head := agentRepo(t, func(dir string) {
		writeFile(t, dir, ".pergent/rules.yaml", "rules:\n  - path: \"*.go\"\n    rule: FROM THE CHANGE\n")
	})
	res, rr := agentRulesResult(t, agentArgs(dir, base, head, oneSkill(t)), agentEnv(nil))
	if res.Rules == nil || res.Rules.Entries != len(builtinNames()) {
		t.Errorf("rules = %+v, want no team entries", res.Rules)
	}
	for _, got := range rr.rules {
		if strings.Contains(got, "FROM THE CHANGE") {
			t.Error("a run got a rule from the change's own .pergent/rules.yaml")
		}
	}
}

// builtinNames lists the built-in rules this pergent ships.
func builtinNames() []string {
	var out []string
	for _, e := range rule.Builtin() {
		out = append(out, e.Name)
	}
	return out
}

// agentRulesLog is agentRulesResult that also returns pergent's log.
func agentRulesLog(t *testing.T, args []string) (result.Result, *rulesRun, string) {
	t.Helper()
	sandboxEnv(t)
	var stdout, stderr bytes.Buffer
	rr := &rulesRun{}
	if code := runAgent(context.Background(), args, agentEnv(nil), &stdout, &stderr, rr.run(t)); code != 0 {
		t.Fatalf("exit code = %d; stderr:\n%s", code, stderr.String())
	}
	return decodeOnly(t, stdout.Bytes()), rr, stderr.String()
}

// --built-in-rules loads only the named built-ins, in builtin.yaml order,
// and the result names them. PERGENT_BUILT_IN_RULES is its default, and the
// flag wins, even when empty.
func TestAgentBuiltInRulesChosen(t *testing.T) {
	t.Setenv("PERGENT_BUILT_IN_RULES", "")
	dir, base, head := agentRepo(t, nil)
	chosen, _ := rule.SelectBuiltin([]string{"go", "yaml", "github_workflows"})
	res, rr, _ := agentRulesLog(t, agentArgs(dir, base, head, oneSkill(t), "--built-in-rules", " yaml, go,,github_workflows"))
	if res.Rules == nil || !res.Rules.BuiltIn || res.Rules.Entries != len(chosen) ||
		!slices.Equal(res.Rules.BuiltInNames, []string{"github_workflows", "yaml", "go"}) {
		t.Errorf("rules = %+v, want the 3 chosen built-ins (%d entries) in builtin.yaml order", res.Rules, len(chosen))
	}
	if len(rr.rules) != 1 || !strings.Contains(rr.rules[0], "## Rule: built-in:go") {
		t.Errorf("file-rules.md = %q, want built-in:go for a.go", rr.rules)
	}

	t.Setenv("PERGENT_BUILT_IN_RULES", "yaml")
	res, rr, _ = agentRulesLog(t, agentArgs(dir, base, head, oneSkill(t)))
	if res.Rules == nil || !slices.Equal(res.Rules.BuiltInNames, []string{"yaml"}) || res.Rules.Entries != 2 {
		t.Errorf("env: rules = %+v, want yaml only (2 globs)", res.Rules)
	}
	if len(rr.rules) != 1 || rr.rules[0] != "" {
		t.Errorf("env: file-rules.md = %q, want none for a.go", rr.rules)
	}

	res, _, _ = agentRulesLog(t, agentArgs(dir, base, head, oneSkill(t), "--built-in-rules", "go"))
	if res.Rules == nil || !slices.Equal(res.Rules.BuiltInNames, []string{"go"}) {
		t.Errorf("flag over env: rules = %+v, want go only", res.Rules)
	}
	res, _, _ = agentRulesLog(t, agentArgs(dir, base, head, oneSkill(t), "--built-in-rules="))
	if res.Rules == nil || !res.Rules.BuiltIn || res.Rules.Entries != len(builtinNames()) || res.Rules.BuiltInNames != nil {
		t.Errorf("empty flag: rules = %+v, want all built-ins and no names", res.Rules)
	}
}

// An unknown name is skipped with one warning line, never an error: the
// caller may know a newer pergent's built-ins.
func TestAgentBuiltInRulesUnknown(t *testing.T) {
	t.Setenv("PERGENT_BUILT_IN_RULES", "")
	dir, base, head := agentRepo(t, nil)
	res, rr, logs := agentRulesLog(t, agentArgs(dir, base, head, oneSkill(t), "--built-in-rules", "nope,go,later"))
	if res.Rules == nil || !slices.Equal(res.Rules.BuiltInNames, []string{"go"}) {
		t.Errorf("rules = %+v, want go only", res.Rules)
	}
	if len(rr.rules) != 1 || !strings.Contains(rr.rules[0], "## Rule: built-in:go") {
		t.Errorf("file-rules.md = %q, want built-in:go", rr.rules)
	}
	want := `warning: --built-in-rules: unknown built-in rules skipped: "nope", "later"` + "\n"
	if strings.Count(logs, "unknown built-in") != 1 || !strings.Contains(logs, want) {
		t.Errorf("log = %q, want one line %q", logs, want)
	}

	res, rr, logs = agentRulesLog(t, agentArgs(dir, base, head, oneSkill(t), "--built-in-rules", "nope"))
	if res.Rules == nil || res.Rules.BuiltIn || res.Rules.Entries != 0 || res.Rules.BuiltInNames != nil {
		t.Errorf("no name known: rules = %+v, want no built-ins", res.Rules)
	}
	if len(rr.rules) != 1 || rr.rules[0] != "" {
		t.Errorf("no name known: file-rules.md = %q, want none", rr.rules)
	}
	if !strings.Contains(logs, `unknown built-in rules skipped: "nope"; no built-in rule is used`) {
		t.Errorf("no name known: log = %q", logs)
	}
}

// Choosing built-ins and turning them off is a usage error, exit 2.
func TestAgentBuiltInRulesConflict(t *testing.T) {
	dir, base, head := agentRepo(t, nil)
	sandboxEnv(t)
	for name, tt := range map[string]struct {
		env, noEnv string
		extra      []string
	}{
		"flags":      {extra: []string{"--built-in-rules", "go", "--no-built-in-rules"}},
		"env":        {env: "go", noEnv: "true"},
		"flag + env": {noEnv: "true", extra: []string{"--built-in-rules", "go"}},
	} {
		t.Setenv("PERGENT_BUILT_IN_RULES", tt.env)
		t.Setenv("PERGENT_NO_BUILT_IN_RULES", tt.noEnv)
		var stdout, stderr bytes.Buffer
		code := runAgent(context.Background(), agentArgs(dir, base, head, oneSkill(t), tt.extra...), agentEnv(nil), &stdout, &stderr, answerRun(t))
		res := decodeOnly(t, stdout.Bytes())
		if code != 2 || res.Error == nil || res.Error.Code != result.CodeUsage || !strings.Contains(res.Error.Message, "--built-in-rules") {
			t.Errorf("%s: code %d, result %+v; want exit 2 and a usage error", name, code, res)
		}
	}
}
