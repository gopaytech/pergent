package main

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/gopaytech/pergent/internal/result"
	"github.com/gopaytech/pergent/internal/runner"
)

func TestParseAgent_CheckDeleted(t *testing.T) {
	sha := strings.Repeat("a", 40)
	args := []string{"--repo-path", "/r", "--base", sha, "--head", sha, "--skills-dir", "/s", "--result", "-"}
	tests := []struct {
		name  string
		extra []string
		env   string
		want  bool
	}{
		{"default is off", nil, "", false},
		{"flag off", []string{"--check-deleted=false"}, "", false},
		{"flag on", []string{"--check-deleted"}, "", true},
		{"env on", nil, "true", true},
		{"env off", nil, "false", false},
		{"flag on beats env off", []string{"--check-deleted=true"}, "false", true},
		{"flag off beats env on", []string{"--check-deleted=false"}, "true", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("PERGENT_CHECK_DELETED", tt.env)
			in, e := parseAgent(append(append([]string{}, args...), tt.extra...), agentEnv(nil))
			if e != nil {
				t.Fatalf("parseAgent() error: %+v", e)
			}
			if in.Review.CheckDeleted != tt.want {
				t.Errorf("CheckDeleted = %v, want %v", in.Review.CheckDeleted, tt.want)
			}
		})
	}

	t.Setenv("PERGENT_CHECK_DELETED", "maybe")
	if _, e := parseAgent(args, agentEnv(nil)); e == nil || e.Code != result.CodeUsage || !strings.Contains(e.Message, "PERGENT_CHECK_DELETED") {
		t.Errorf("parseAgent() with a bad PERGENT_CHECK_DELETED = %+v, want a usage error naming the variable", e)
	}
}

// deleteRepo is a checkout whose head commit changes a.go and deletes old.go.
func deleteRepo(t *testing.T) (dir, base, head string) {
	t.Helper()
	dir = t.TempDir()
	gitIn(t, dir, "init", "-q")
	writeFile(t, dir, "a.go", "package a\n")
	writeFile(t, dir, "old.go", "package a\n\nfunc Old() {}\n")
	base = commitAll(t, dir, "base")
	writeFile(t, dir, "a.go", "package a\n\nfunc A() { Old() }\n")
	if err := os.Remove(filepath.Join(dir, "old.go")); err != nil {
		t.Fatal(err)
	}
	return dir, base, commitAll(t, dir, "head")
}

// deletionBatched reports, for each run, whether the diff.patch it got holds
// the deletion of old.go, read while the run was in progress.
func deletionBatched(t *testing.T, args []string, env func(string) string) []bool {
	t.Helper()
	var mu sync.Mutex
	var got []bool
	run := func(ctx context.Context, o runner.Options) runner.RunResult {
		seen := false
		for _, p := range o.Files {
			if filepath.Base(p) == "diff.patch" {
				data, err := os.ReadFile(p)
				if err != nil {
					t.Error(err)
				}
				seen = strings.Contains(string(data), "--- a/old.go\n+++ /dev/null\n")
			}
		}
		mu.Lock()
		got = append(got, seen)
		mu.Unlock()
		return answerRun(t)(ctx, o)
	}
	var stdout, stderr bytes.Buffer
	if code := runAgent(context.Background(), args, env, &stdout, &stderr, run); code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr:\n%s", code, stderr.String())
	}
	return got
}

func TestRunAgent_CheckDeleted(t *testing.T) {
	sandboxEnv(t)
	dir, base, head := deleteRepo(t)
	args := agentArgs(dir, base, head, oneSkill(t))

	t.Setenv("PERGENT_CHECK_DELETED", "")
	if got := deletionBatched(t, args, agentEnv(nil)); len(got) != 1 || got[0] {
		t.Errorf("default: runs batched the deletion %v, want one run without it", got)
	}
	if got := deletionBatched(t, append(args, "--check-deleted"), agentEnv(nil)); len(got) != 1 || !got[0] {
		t.Errorf("--check-deleted: runs batched the deletion %v, want one run with it", got)
	}
	if got := deletionBatched(t, append(args, "--check-deleted=false"), agentEnv(nil)); len(got) != 1 || got[0] {
		t.Errorf("--check-deleted=false: runs batched the deletion %v, want one run without it", got)
	}
	t.Setenv("PERGENT_CHECK_DELETED", "true")
	if got := deletionBatched(t, args, agentEnv(nil)); len(got) != 1 || !got[0] {
		t.Errorf("PERGENT_CHECK_DELETED=true: runs batched the deletion %v, want one run with it", got)
	}
	t.Setenv("PERGENT_CHECK_DELETED", "false")
	if got := deletionBatched(t, args, agentEnv(nil)); len(got) != 1 || got[0] {
		t.Errorf("PERGENT_CHECK_DELETED=false: runs batched the deletion %v, want one run without it", got)
	}
	if got := deletionBatched(t, append(args, "--check-deleted"), agentEnv(nil)); len(got) != 1 || !got[0] {
		t.Errorf("--check-deleted beats the variable: runs batched the deletion %v, want one run with it", got)
	}
}

// A change that only deletes a file gets one batch, and its result lists the
// deleted file as reviewed.
func TestRunAgent_CheckDeletedDeleteOnly(t *testing.T) {
	sandboxEnv(t)
	t.Setenv("PERGENT_CHECK_DELETED", "true")
	dir := t.TempDir()
	gitIn(t, dir, "init", "-q")
	writeFile(t, dir, "old.go", "package a\n")
	base := commitAll(t, dir, "base")
	if err := os.Remove(filepath.Join(dir, "old.go")); err != nil {
		t.Fatal(err)
	}
	head := commitAll(t, dir, "head")
	var stdout, stderr bytes.Buffer
	seen := 0
	run := func(ctx context.Context, o runner.Options) runner.RunResult {
		seen++
		return answerRun(t)(ctx, o)
	}

	code := runAgent(context.Background(), agentArgs(dir, base, head, oneSkill(t)), agentEnv(nil), &stdout, &stderr, run)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr:\n%s", code, stderr.String())
	}
	r := decodeOnly(t, stdout.Bytes())
	if seen != 1 || len(r.Skills) != 1 {
		t.Fatalf("runs = %d, skills = %d, want 1 and 1", seen, len(r.Skills))
	}
	sk := r.Skills[0]
	if len(sk.Files.Reviewed) != 1 || sk.Files.Reviewed[0] != "old.go" || len(sk.Files.Skipped) != 0 {
		t.Errorf("files = %+v, want old.go reviewed and nothing skipped", sk.Files)
	}
}

// main passes the setting to the review in CI mode. It runs in a process of
// its own against a fake GitLab and a stub opencode that copies the diff.patch
// it is given.
func TestMain_CheckDeletedInPlatformMode(t *testing.T) {
	if root := os.Getenv("PERGENT_TEST_CHECKDELETED_REPO"); root != "" {
		os.Args = append([]string{"pergent", "--platform", "gitlab", "--skill", "code-review", "--repo-path", root},
			strings.Fields(os.Getenv("PERGENT_TEST_CHECKDELETED_ARGS"))...)
		main()
		return
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/user"):
			fmt.Fprint(w, `{"id":1}`)
		case strings.HasSuffix(r.URL.Path, "/diffs"):
			fmt.Fprint(w, `[{"old_path":"a.go","new_path":"a.go","diff":"@@ -1 +1 @@\n-a\n+b\n"},`+
				`{"old_path":"gone.go","new_path":"gone.go","deleted_file":true,"diff":"@@ -1 +0,0 @@\n-x\n"}]`)
		case r.Method == http.MethodPost:
			w.WriteHeader(http.StatusCreated)
			fmt.Fprint(w, `{"id":2}`)
		default:
			fmt.Fprint(w, `[]`)
		}
	}))
	defer srv.Close()

	stubDir := t.TempDir()
	record := filepath.Join(stubDir, "diff.txt")
	script := "#!/bin/sh\nprev=\n" +
		"for a in \"$@\"; do\n" +
		"  if [ \"$prev\" = --file ] && [ \"$(basename \"$a\")\" = diff.patch ]; then cat \"$a\" >> \"" + record + "\"; fi\n" +
		"  prev=$a\n" +
		"done\n"
	bin := filepath.Join(stubDir, "bin")
	if err := os.Mkdir(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "opencode"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	for _, tt := range []struct {
		name, args, env string
		want            bool
	}{
		{"default", "", "", false},
		{"flag on", "--check-deleted", "", true},
		{"env on", "", "PERGENT_CHECK_DELETED=true", true},
		{"flag off beats env on", "--check-deleted=false", "PERGENT_CHECK_DELETED=true", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			os.Remove(record)
			cmd := exec.Command(os.Args[0], "-test.run=^TestMain_CheckDeletedInPlatformMode$")
			cmd.Env = append(os.Environ(),
				"PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"),
				"PERGENT_TEST_CHECKDELETED_REPO="+t.TempDir(),
				"PERGENT_TEST_CHECKDELETED_ARGS="+tt.args,
				"PERGENT_CHECK_DELETED=",
				"GITLAB_TOKEN=test-token",
				"CI_SERVER_URL="+srv.URL,
				"CI_PROJECT_ID=1",
				"CI_MERGE_REQUEST_IID=1",
				"OPENCODE_PROVIDER=anthropic",
				"OPENCODE_MODEL=claude-sonnet-4",
			)
			if tt.env != "" {
				cmd.Env = append(cmd.Env, tt.env)
			}
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("main() failed: %v\n%s", err, out)
			}
			got, _ := os.ReadFile(record)
			if !strings.Contains(string(got), "+++ b/a.go") {
				t.Fatalf("the run got diff.patch %q, want a.go in it", got)
			}
			if batched := strings.Contains(string(got), "+++ /dev/null"); batched != tt.want {
				t.Errorf("the run got diff.patch %q; the deletion of gone.go in it = %v, want %v", got, batched, tt.want)
			}
		})
	}
}
