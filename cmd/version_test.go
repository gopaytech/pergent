package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gopaytech/pergent/internal/result"
	"github.com/gopaytech/pergent/internal/runner"
)

// TestMain keeps the developer's own opencode out of every test that does not
// test the version check. A subprocess that runs main() sets
// PERGENT_TEST_REAL_VERSION_CHECK to get the real one, against its stub.
func TestMain(m *testing.M) {
	if os.Getenv("PERGENT_TEST_REAL_VERSION_CHECK") == "" {
		checkOpencodeVersion = func(context.Context) (string, error) { return "", nil }
	}
	os.Exit(m.Run())
}

func TestRunAgent_RefusesOldOpencode(t *testing.T) {
	sandboxEnv(t)
	repo, base, head := agentRepo(t, nil)
	old := checkOpencodeVersion
	checkOpencodeVersion = func(context.Context) (string, error) {
		return "", errors.New("opencode 1.19.4 is too old: pergent needs opencode 2.3.2 or later")
	}
	t.Cleanup(func() { checkOpencodeVersion = old })
	out := filepath.Join(t.TempDir(), "out.json")
	args := []string{"--repo-path", repo, "--base", base, "--head", head, "--skills-dir", oneSkill(t), "--result", out}
	called := false
	run := func(ctx context.Context, o runner.Options) runner.RunResult {
		called = true
		return answerRun(t)(ctx, o)
	}
	var stdout, stderr bytes.Buffer

	if code := runAgent(context.Background(), args, agentEnv(nil), &stdout, &stderr, run); code != 3 {
		t.Fatalf("exit code = %d, want 3; stderr:\n%s", code, stderr.String())
	}
	if called {
		t.Error("opencode ran after the version was refused")
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	r := decodeOnly(t, data)
	if r.Error == nil || r.Error.Code != result.CodeInternal || !strings.Contains(r.Error.Message, "2.3.2 or later") {
		t.Errorf("error = %+v, want %s naming 2.3.2", r.Error, result.CodeInternal)
	}
}

func TestRunAgent_LogsNewerOpencodeWarning(t *testing.T) {
	sandboxEnv(t)
	repo, base, head := agentRepo(t, nil)
	old := checkOpencodeVersion
	checkOpencodeVersion = func(context.Context) (string, error) {
		return "tested with opencode 2.3.2, found 2.5.0", nil
	}
	t.Cleanup(func() { checkOpencodeVersion = old })
	args := []string{"--repo-path", repo, "--base", base, "--head", head, "--skills-dir", oneSkill(t), "--result", filepath.Join(t.TempDir(), "out.json")}
	called := false
	run := func(ctx context.Context, o runner.Options) runner.RunResult {
		called = true
		return answerRun(t)(ctx, o)
	}
	var stdout, stderr bytes.Buffer

	if code := runAgent(context.Background(), args, agentEnv(nil), &stdout, &stderr, run); code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr:\n%s", code, stderr.String())
	}
	if !called {
		t.Error("the review did not run")
	}
	if !strings.Contains(stderr.String(), "warning: tested with opencode 2.3.2, found 2.5.0") {
		t.Errorf("stderr lacks the warning:\n%s", stderr.String())
	}
}

// main runs in a process of its own, against a stub opencode that is too old.
func TestMain_RefusesOldOpencode(t *testing.T) {
	if root := os.Getenv("PERGENT_TEST_OLD_REPO"); root != "" {
		os.Args = []string{"pergent", "--platform", "gitlab", "--skill", "code-review", "--repo-path", root}
		main()
		return
	}
	dir := t.TempDir()
	if err := os.WriteFile(dir+"/opencode", []byte("#!/bin/sh\necho 1.19.4\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestMain_RefusesOldOpencode$")
	cmd.Env = append(os.Environ(),
		"PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"PERGENT_TEST_OLD_REPO="+t.TempDir(),
		"PERGENT_TEST_REAL_VERSION_CHECK=1",
		"GITLAB_TOKEN=test-token",
		"CI_SERVER_URL=http://127.0.0.1:1",
		"CI_PROJECT_ID=1",
		"CI_MERGE_REQUEST_IID=1",
		"OPENCODE_PROVIDER=anthropic",
		"OPENCODE_MODEL=claude-sonnet-4",
	)
	out, err := cmd.CombinedOutput()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() == 0 {
		t.Fatalf("main() err = %v, want a non-zero exit\n%s", err, out)
	}
	for _, want := range []string{"config error:", "2.3.2 or later", "1.19.4"} {
		if !strings.Contains(string(out), want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
}
