package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gopaytech/pergent/internal/result"
)

// These tests run `pergent agent` against the real opencode binary and a
// fake model server. They are opt-in: set PERGENT_TEST_OPENCODE to the
// binary's path, or they skip.

// requireOpencode skips unless PERGENT_TEST_OPENCODE names an executable. It
// returns the path of a private copy (so pgrep -f finds only this test's
// processes), puts its directory first on PATH and gives the test its own
// HOME, XDG_CONFIG_HOME and TMPDIR. The provider variables are set by
// runAgentE2E's caller through setFakeProvider, once the fake model's URL is
// known.
func requireOpencode(t *testing.T) string {
	t.Helper()
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
