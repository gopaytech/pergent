package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
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
	for _, got := range rr.rules {
		if got != "" {
			t.Errorf("a run got file-rules.md with no rules: %q", got)
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
