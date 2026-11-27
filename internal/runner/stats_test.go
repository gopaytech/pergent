package runner

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

func TestParseStats(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile(filepath.Join("testdata", "stats.json"))
	if err != nil {
		t.Fatal(err)
	}
	got, err := ParseStats(data)
	if err != nil {
		t.Fatalf("ParseStats() error: %v", err)
	}
	want := Stats{
		Sessions: 1, Subagents: 2, Steps: 7,
		Tokens: Usage{Input: 1200, Output: 340, Reasoning: 50, CacheRead: 9000, CacheWrite: 800},
		Models: []ModelStats{
			{Provider: "acme", Model: "big-model", Steps: 4, Tokens: Usage{Input: 1000, Output: 300, Reasoning: 50, CacheRead: 8000, CacheWrite: 700}},
			{Provider: "acme", Model: "small-model", Steps: 3, Tokens: Usage{Input: 200, Output: 40, CacheRead: 1000, CacheWrite: 100}},
		},
	}
	if got.Sessions != want.Sessions || got.Subagents != want.Subagents || got.Steps != want.Steps || got.Tokens != want.Tokens ||
		!slices.Equal(got.Models, want.Models) {
		t.Errorf("ParseStats() = %+v, want %+v", got, want)
	}
}

func TestParseStatsRefusesBad(t *testing.T) {
	t.Parallel()
	for name, in := range map[string]string{
		"not json":       "opencode: no database",
		"negative":       `{"sessions":1,"tokens":{"input":-1,"output":0,"reasoning":0,"cache":{"read":0,"write":0}}}`,
		"negative model": `{"models":[{"model":{"id":"m","providerID":"p"},"steps":1,"tokens":{"input":0,"output":0,"reasoning":0,"cache":{"read":0,"write":-5}}}]}`,
		"negative steps": `{"steps":-1}`,
	} {
		if _, err := ParseStats([]byte(in)); err == nil {
			t.Errorf("%s: ParseStats() error = nil, want an error", name)
		}
	}
}

func TestReadStatsCommand(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake opencode is a shell script")
	}
	dir := t.TempDir()
	seen := filepath.Join(dir, "seen")
	fixture, _ := filepath.Abs(filepath.Join("testdata", "stats.json"))
	script := "#!/bin/sh\n{ echo \"ARGS $*\"; echo \"PWD $(pwd) $PWD\"; env; } > " + seen + "\ncat " + fixture + "\n"
	bin := filepath.Join(dir, "bin")
	os.Mkdir(bin, 0o755)
	if err := os.WriteFile(filepath.Join(bin, "opencode"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("OPENCODE_API_KEY", "secret-key")
	t.Setenv("GITLAB_TOKEN", "secret-token")
	data := filepath.Join(dir, "data")

	got, err := ReadStats(context.Background(), data)
	if err != nil {
		t.Fatalf("ReadStats() error: %v", err)
	}
	if got.Subagents != 2 || len(got.Models) != 2 {
		t.Errorf("ReadStats() = %+v, want the fixture", got)
	}
	out, err := os.ReadFile(seen)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(out), "\n")
	if lines[0] != "ARGS stats --standalone --all --models --json" {
		t.Errorf("args line = %q", lines[0])
	}
	if !slices.Contains(lines, "XDG_DATA_HOME="+data) {
		t.Errorf("env has no XDG_DATA_HOME=%s:\n%s", data, out)
	}
	for _, l := range lines {
		if strings.HasPrefix(l, "OPENCODE_API_KEY") || strings.HasPrefix(l, "GITLAB_TOKEN") {
			t.Errorf("env holds %q, want no key or token", strings.SplitN(l, "=", 2)[0])
		}
	}
	for _, want := range []string{"OPENCODE_DISABLE_AUTOUPDATE=true", "OPENCODE_DISABLE_PROJECT_CONFIG=true", "OPENCODE_CONFIG_PROJECT_DISABLE=true", "OPENCODE_DISABLE_CLAUDE_CODE=true"} {
		if !slices.Contains(lines, want) {
			t.Errorf("env has no %s", want)
		}
	}
	// cwd and PWD are one empty directory of its own, not the caller's.
	cwd, _ := os.Getwd()
	f := strings.Fields(lines[1])
	if len(f) != 3 || f[2] == cwd || realPath(f[1]) != realPath(f[2]) {
		t.Errorf("cwd line = %q, want one empty directory that is not %s", lines[1], cwd)
	}
}

func TestReadStatsFails(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake opencode is a shell script")
	}
	fakeOpencode(t, fakeAttempt{stdout: "no", stderr: "boom", code: 1})
	if _, err := ReadStats(context.Background(), t.TempDir()); err == nil {
		t.Error("ReadStats() error = nil, want one for a failed opencode")
	}
	fakeOpencode(t, fakeAttempt{stdout: "not json"})
	if _, err := ReadStats(context.Background(), t.TempDir()); err == nil {
		t.Error("ReadStats() error = nil, want one for output that is not JSON")
	}
}
