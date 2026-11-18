package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gopaytech/pergent/internal/result"
	"github.com/gopaytech/pergent/internal/runner"
)

// These tests run `pergent agent` against the real opencode binary and a
// fake model server. They are opt-in: set PERGENT_TEST_OPENCODE to the
// binary's path, or they skip.

// requireOpencode skips unless PERGENT_TEST_OPENCODE names an executable. It
// returns the path of a private copy (so pgrep -f finds only this test's
// processes), puts its directory first on PATH and gives the test its own
// HOME, XDG_CONFIG_HOME and TMPDIR. The provider variables are set by
// runAgentE2E's caller through useFakeModel, once the fake model's URL is
// known.
func requireOpencode(t *testing.T) string {
	t.Helper()
	// The e2e tests run the real check against the real binary.
	old := checkOpencodeVersion
	checkOpencodeVersion = runner.CheckOpencodeVersion
	t.Cleanup(func() { checkOpencodeVersion = old })
	src := os.Getenv("PERGENT_TEST_OPENCODE")
	if src == "" {
		t.Skip("PERGENT_TEST_OPENCODE is not set")
	}
	if fi, err := os.Stat(src); err != nil || fi.IsDir() || fi.Mode()&0o111 == 0 {
		t.Skipf("PERGENT_TEST_OPENCODE %q is not an executable", src)
	}
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(t.TempDir(), "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(bin, "opencode")
	if err := os.WriteFile(dst, data, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("TMPDIR", t.TempDir())
	return dst
}

// useFakeModel points the runner's environment at the fake model.
func useFakeModel(t *testing.T, m *fakeModel) {
	t.Helper()
	t.Setenv("OPENCODE_PROVIDER", "fake")
	t.Setenv("OPENCODE_MODEL", "fake-model")
	t.Setenv("OPENCODE_NPM", "@ai-sdk/openai-compatible")
	t.Setenv("OPENCODE_BASE_URL", m.URL+"/v1")
	t.Setenv("OPENCODE_API_KEY", "e2e-dummy-key")
}

// runAgentE2E runs pergent agent with the real review and returns the
// delivered result and stderr. extraArgs come after the defaults, so a later
// flag overrides one.
func runAgentE2E(t *testing.T, repo, base, head string, extraArgs ...string) (result.Result, string) {
	t.Helper()
	out := filepath.Join(t.TempDir(), "result.json")
	args := []string{
		"--repo-path", repo, "--base", base, "--head", head,
		"--skills-dir", oneSkill(t),
		"--context-limit", "100000", "--concurrency", "1", "--timeout", "3m",
		"--result", out,
	}
	args = append(args, extraArgs...)
	var stdout, stderr bytes.Buffer
	code := runAgent(context.Background(), args, os.Getenv, &stdout, &stderr, nil)
	if code != 0 {
		t.Fatalf("runAgent exit code %d\nstderr:\n%s", code, stderr.String())
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	var res result.Result
	if err := json.Unmarshal(data, &res); err != nil {
		t.Fatalf("result is not JSON: %v\n%s", err, data)
	}
	return res, stderr.String()
}

// e2eRepo builds a repo whose base commit holds a.go and whose head commit
// adds files. HOME-relative paths are refused, as pergent agent refuses them.
func e2eRepo(t *testing.T, files map[string]string) (repo, base, head string) {
	t.Helper()
	home := os.Getenv("HOME")
	for p := range files {
		if filepath.IsAbs(p) || strings.HasPrefix(p, "~") || (home != "" && strings.HasPrefix(p, home)) {
			t.Fatalf("e2eRepo path %q must be relative to the repo", p)
		}
	}
	return agentRepo(t, func(dir string) {
		for p, c := range files {
			writeFile(t, dir, p, c)
		}
	})
}

func TestOpencodeE2E_OffersOnlyReadTools(t *testing.T) {
	requireOpencode(t)
	m := newFakeModel(t, nil, "")
	useFakeModel(t, m)
	repo, base, head := e2eRepo(t, map[string]string{"b.go": "package a\n"})
	_, _ = runAgentE2E(t, repo, base, head)

	allowed := map[string]bool{"read": true, "glob": true, "grep": true, "skill": true}
	var tools []string
	for _, r := range m.Requests() {
		if len(r.Tools) > 0 {
			tools = r.Tools
			break
		}
	}
	if len(tools) == 0 {
		t.Fatal("no request carried tools")
	}
	have := map[string]bool{}
	for _, n := range tools {
		have[n] = true
		if !allowed[n] {
			t.Errorf("opencode offered the model %q, which a read-only review must not have", n)
		}
	}
	for _, n := range []string{"read", "glob", "grep"} {
		if !have[n] {
			t.Errorf("opencode did not offer %q (offered %v)", n, tools)
		}
	}
}

// A user's global opencode config is loaded before pergent's, and opencode 2
// appends the agent's rules last. The global file here allows everything at
// top level and on the pergent agent itself; only the agent-level copy of
// pergent's allow-list can beat the latter. The CANARY instruction proves the
// global file is loaded at all. The test fails if a denied tool is offered or
// runs, or if the .env read is not a denial.
func TestOpencodeE2E_GlobalConfigCantLoosenTheSandbox(t *testing.T) {
	requireOpencode(t)
	cfgDir := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "opencode")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	canaryFile := filepath.Join(t.TempDir(), "canary.md")
	if err := os.WriteFile(canaryFile, []byte("CANARY-GLOBAL-CONFIG"), 0o644); err != nil {
		t.Fatal(err)
	}
	allowAll := map[string]any{"*": "allow", "read": "allow", "edit": "allow", "bash": "allow", "external_directory": "allow"}
	globalCfg, _ := json.Marshal(map[string]any{
		"instructions": []string{canaryFile},
		"permission":   allowAll,
		"agent":        map[string]any{"pergent": map[string]any{"permission": allowAll}},
	})
	if err := os.WriteFile(filepath.Join(cfgDir, "opencode.json"), globalCfg, 0o644); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(t.TempDir(), "pwned")
	const appGo = "package src\n"
	m := newFakeModel(t, [][]fakeCall{{
		{Name: "read", Args: map[string]any{"path": "src/.env"}},
		{Name: "execute", Args: map[string]any{"code": "return 1"}},
		{Name: "shell", Args: map[string]any{"command": "echo PWNED > " + marker}},
		{Name: "bash", Args: map[string]any{"command": "echo PWNED > " + marker}},
		{Name: "edit", Args: map[string]any{"filePath": "src/app.go", "oldString": "package src", "newString": "package pwned"}},
	}}, "")
	useFakeModel(t, m)
	repo, base, head := e2eRepo(t, map[string]string{"src/.env": "DB_PASSWORD=GLOBALSECRET\n", "src/app.go": appGo})
	_, _ = runAgentE2E(t, repo, base, head)

	allowed := map[string]bool{"read": true, "glob": true, "grep": true, "skill": true}
	canary, results := false, map[string]string{}
	for _, r := range m.Requests() {
		if strings.Contains(r.System, "CANARY-GLOBAL-CONFIG") {
			canary = true
		}
		if strings.Contains(r.System, "GLOBALSECRET") {
			t.Error("a request carried the secret")
		}
		for _, n := range r.Tools {
			if !allowed[n] {
				t.Errorf("the global config got the model a tool: %q", n)
			}
		}
		for id, text := range r.ToolResults {
			results[id] = text
			if strings.Contains(text, "GLOBALSECRET") {
				t.Errorf("a tool result carried the secret: %q", text)
			}
		}
	}
	if !canary {
		t.Error("the global config's instruction never reached the model, so the test proves nothing")
	}
	if got := strings.ToLower(results["call_0_0"]); !strings.Contains(got, "denied") {
		t.Errorf("the .env read was not a denial: %q", results["call_0_0"])
	}
	if len(results) < 5 {
		t.Errorf("the model saw %d tool results, want 5: %v", len(results), results)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Error("a shell command ran")
	}
	if got, err := os.ReadFile(filepath.Join(repo, "src/app.go")); err != nil || string(got) != appGo {
		t.Errorf("src/app.go = %q, %v, want it unchanged", got, err)
	}
}

// writeGlobalOpencodeConfig writes $XDG_CONFIG_HOME/opencode/opencode.json
// pointing at the fake model, with extra top-level keys merged in.
func writeGlobalOpencodeConfig(t *testing.T, m *fakeModel, extra map[string]any) {
	t.Helper()
	dir := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "opencode")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := map[string]any{
		"model": "fake/fake-model",
		"provider": map[string]any{"fake": map[string]any{
			"npm":     "@ai-sdk/openai-compatible",
			"options": map[string]any{"baseURL": m.URL + "/v1", "apiKey": "x"},
			"models":  map[string]any{"fake-model": map[string]any{"limit": map[string]any{"context": 100000, "output": 4000}}},
		}},
	}
	for k, v := range extra {
		cfg[k] = v
	}
	data, _ := json.Marshal(cfg)
	if err := os.WriteFile(filepath.Join(dir, "opencode.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

// A plain `opencode run` attaches to a shared server registered under its
// state directory, and that server ignores OPENCODE_CONFIG (the run then fails
// with: Agent not found "pergent"). Every pergent run is standalone and has a
// state directory of its own, so the service here must be invisible to it.
func TestOpencodeE2E_IgnoresASharedService(t *testing.T) {
	bin := requireOpencode(t)
	m := newFakeModel(t, [][]fakeCall{{{Name: "read", Args: map[string]any{"path": "src/.env"}}}}, "")
	useFakeModel(t, m)
	allow := map[string]any{"*": "allow", "read": "allow", "edit": "allow", "bash": "allow", "external_directory": "allow"}
	writeGlobalOpencodeConfig(t, m, map[string]any{"permission": allow})

	ocCmd := func(args ...string) *exec.Cmd {
		c := exec.Command(bin, args...)
		c.Env = os.Environ()
		return c
	}
	if out, err := ocCmd("service", "start").CombinedOutput(); err != nil {
		t.Fatalf("opencode service start: %v\n%s", err, out)
	}
	t.Cleanup(func() {
		_, _ = ocCmd("service", "stop").CombinedOutput()
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			if out, _ := exec.Command("pgrep", "-f", bin).Output(); len(bytes.TrimSpace(out)) == 0 {
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
		t.Errorf("the shared service is still running after service stop")
	})

	repo, base, head := e2eRepo(t, map[string]string{"src/.env": "DB_PASSWORD=SVCSECRET\n"})
	res, stderr := runAgentE2E(t, repo, base, head)

	for _, s := range res.Skills {
		if s.Verdict == "failed" {
			t.Errorf("skill %s failed: %+v\nstderr:\n%s", s.Name, s.Failures, stderr)
		}
	}
	var got string
	for _, r := range m.Requests() {
		for id, text := range r.ToolResults {
			if id == "call_0_0" {
				got = text
			}
			if strings.Contains(text, "SVCSECRET") {
				t.Errorf("a tool result carried the secret: %q", text)
			}
		}
	}
	if !strings.Contains(strings.ToLower(got), "denied") {
		t.Errorf("the .env read was not a permission refusal: %q\nstderr:\n%s", got, stderr)
	}
}

// A timed-out run must not leave opencode or anything it started behind.
func TestOpencodeE2E_NoProcessOutlivesATimedOutRun(t *testing.T) {
	bin := requireOpencode(t)
	m := newFakeModel(t, nil, "hang")
	useFakeModel(t, m)
	repo, base, head := e2eRepo(t, map[string]string{"b.go": "package a\n"})
	_, _ = runAgentE2E(t, repo, base, head, "--timeout", "20s")

	var out []byte
	deadline := time.Now().Add(5 * time.Second)
	for {
		out, _ = exec.Command("pgrep", "-f", bin).Output()
		if len(bytes.TrimSpace(out)) == 0 || time.Now().After(deadline) {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if pids := strings.TrimSpace(string(out)); pids != "" {
		t.Errorf("processes of %s still running 5s after the run ended: %s", bin, pids)
	}
}

// opencode 2 reads CLAUDE.md files (the checkout's, the user's ~/.claude)
// unless told not to; agent mode never loads the checkout's own files, so a
// run must not either. opencode's own global AGENTS.md stays, as the spec accepts.
func TestOpencodeE2E_LoadsNoClaudeMD(t *testing.T) {
	requireOpencode(t)
	cfgDir := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "opencode")
	claudeDir := filepath.Join(os.Getenv("HOME"), ".claude")
	for _, d := range []string{cfgDir, claudeDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(claudeDir, "CLAUDE.md"), []byte("CANARY-HOME-CLAUDE\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfgDir, "AGENTS.md"), []byte("CANARY-GLOBAL\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := newFakeModel(t, nil, "")
	useFakeModel(t, m)
	repo, base, head := e2eRepo(t, map[string]string{"CLAUDE.md": "CANARY-ROOT-CLAUDE\n", "b.go": "package a\n"})
	_, _ = runAgentE2E(t, repo, base, head)

	var global bool
	for _, r := range m.Requests() {
		for _, c := range []string{"CANARY-ROOT-CLAUDE", "CANARY-HOME-CLAUDE"} {
			if strings.Contains(r.System, c) {
				t.Errorf("a request's system prompt carried %s", c)
			}
		}
		global = global || strings.Contains(r.System, "CANARY-GLOBAL")
	}
	if len(m.Requests()) == 0 {
		t.Fatal("the model got no request")
	}
	if !global {
		t.Error("opencode's global AGENTS.md was not loaded, so the test proves nothing")
	}
}

// With OPENCODE_PROVIDER unset pergent writes no config and passes no --agent:
// the run uses the user's own opencode setup. Agent mode requires a provider,
// so this drives CI mode with --local in a subprocess.
func TestOpencodeE2E_OwnConfigMode(t *testing.T) {
	if repo := os.Getenv("PERGENT_TEST_OWNCONFIG_REPO"); repo != "" {
		os.Args = []string{"pergent", "--local", "--skill", os.Getenv("PERGENT_TEST_OWNCONFIG_SKILL"),
			"--repo-path", repo, "--concurrency", "1", "--timeout", "3m"}
		main()
		return
	}
	requireOpencode(t)
	m := newFakeModel(t, nil, "")
	writeGlobalOpencodeConfig(t, m, nil)
	repo, base, _ := e2eRepo(t, map[string]string{"b.go": "package a\n"})
	gitIn(t, repo, "update-ref", "refs/remotes/origin/main", base)
	skillPath := filepath.Join(oneSkill(t), "review.md")

	cmd := exec.Command(os.Args[0], "-test.run=^TestOpencodeE2E_OwnConfigMode$")
	cmd.Env = append(os.Environ(), "PERGENT_TEST_OWNCONFIG_REPO="+repo, "PERGENT_TEST_OWNCONFIG_SKILL="+skillPath,
		"OPENCODE_PROVIDER=", "OPENCODE_MODEL=", "OPENCODE_BASE_URL=", "OPENCODE_NPM=", "OPENCODE_API_KEY=")
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("pergent --local failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "No blocking issues") || strings.Contains(stdout.String(), "failed") {
		t.Errorf("the review did not complete:\n%s\nstderr:\n%s", stdout.String(), stderr.String())
	}
	if len(m.Requests()) == 0 {
		t.Error("the fake model got no request")
	}
}

// opencode 2 reports a provider failure as {type, message, status, response}.
// A 500 is retried by opencode itself (with backoff, so it takes a while) and
// then once by pergent; a context overflow is a provider.invalid-request that
// only its text marks as one.
func TestOpencodeE2E_ProviderErrors(t *testing.T) {
	requireOpencode(t)
	tests := []struct {
		mode     string
		kind     string
		contains string
	}{
		{"error500", "opencode_error", "Gave up after 2 attempts"},
		{"overflow", "context_overflow", "context window overflowed"},
	}
	for _, tt := range tests {
		t.Run(tt.mode, func(t *testing.T) {
			start := time.Now()
			m := newFakeModel(t, nil, tt.mode)
			useFakeModel(t, m)
			repo, base, head := e2eRepo(t, map[string]string{"b.go": "package a\n"})
			// opencode retries a 500 for about 85s per start and pergent starts it
			// twice (2m57s measured), so the default 3m budget would be a coin flip.
			res, stderr := runAgentE2E(t, repo, base, head, "--timeout", "5m")
			t.Logf("took %s", time.Since(start).Round(time.Second))

			if len(res.Skills) != 1 || res.Skills[0].Verdict != "failed" || len(res.Skills[0].Failures) == 0 {
				t.Fatalf("result = %+v, want one failed skill\nstderr:\n%s", res.Skills, stderr)
			}
			c := res.Skills[0].Failures[0].Cause
			if c == nil || c.Kind != tt.kind || !strings.Contains(c.Message, tt.contains) {
				t.Errorf("cause = %+v, want kind %s and %q in the message\nstderr:\n%s", c, tt.kind, tt.contains, stderr)
			}
		})
	}
}

// The impact run proves it looked inside a clone by the paths of its completed
// read, grep and glob calls (Transcript.Opened). The skill run of the same
// review gets no tool calls: only the run whose prompt is the impact prompt
// reads the clone.
func TestOpencodeE2E_ImpactRunRecordsOpenedClone(t *testing.T) {
	requireOpencode(t)
	clone := t.TempDir()
	const apiSrc = "package billing\n\nfunc CallerOfA() { A() }\n"
	writeFile(t, clone, "api.go", apiSrc)
	links := filepath.Join(t.TempDir(), "linked.json")
	data, _ := json.Marshal([]map[string]any{{"name": "billing", "path": clone, "watched": []string{}, "can_block": true}})
	if err := os.WriteFile(links, data, 0o644); err != nil {
		t.Fatal(err)
	}

	m := newFakeModelFor(t, "breaks code in other repositories that use it", [][]fakeCall{
		{{Name: "read", Args: map[string]any{"path": filepath.Join(clone, "api.go")}}},
	})
	useFakeModel(t, m)
	repo, base, head := e2eRepo(t, map[string]string{"b.go": "package a\n"})
	res, stderr := runAgentE2E(t, repo, base, head, "--linked-repos", links)

	var impact *result.Skill
	for i := range res.Skills {
		if res.Skills[i].Name == "impact" {
			impact = &res.Skills[i]
		}
	}
	if impact == nil {
		t.Fatalf("no impact skill in the result: %+v\nstderr:\n%s", res.Skills, stderr)
	}
	if impact.Verdict == "failed" {
		t.Fatalf("impact failed: %+v\nstderr:\n%s", impact.Failures, stderr)
	}
	found := false
	for _, r := range m.Requests() {
		for _, out := range r.ToolResults {
			if strings.Contains(out, "func CallerOfA") {
				found = true
			}
		}
	}
	if !found {
		t.Errorf("no tool result held the clone file's content\nstderr:\n%s", stderr)
	}
}
