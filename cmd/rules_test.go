package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/gopaytech/pergent/internal/config"
	"github.com/gopaytech/pergent/rule"
)

// rulesRepo is a repository whose base commit, also refs/remotes/origin/main,
// holds baseRules as .pergent/rules.yaml ("" for no file), and whose head
// commit holds headRules there.
func rulesRepo(t *testing.T, baseRules, headRules string) string {
	t.Helper()
	repo := t.TempDir()
	gitIn(t, repo, "init", "-q")
	writeFile(t, repo, "a.go", "package a\n")
	if baseRules != "" {
		writeFile(t, repo, rule.RepoFile, baseRules)
	}
	base := commitAll(t, repo, "base")
	gitIn(t, repo, "update-ref", "refs/remotes/origin/main", base)
	writeFile(t, repo, "a.go", "package a\n\nfunc A() {}\n")
	if headRules != "" {
		writeFile(t, repo, rule.RepoFile, headRules)
	}
	commitAll(t, repo, "head")
	return repo
}

const baseGoRule = "rules:\n  - path: \"*.go\"\n    rule: BASE RULE\n"
const headGoRule = "rules:\n  - path: \"*.go\"\n    rule: HEAD RULE\n"

func TestBaseRulesRead(t *testing.T) {
	got, err := baseRules(rulesRepo(t, baseGoRule, ""), "origin/main")
	if err != nil {
		t.Fatalf("baseRules: %v", err)
	}
	if len(got) != 1 || got[0] != (rule.Entry{Path: "*.go", Rule: "BASE RULE", Source: rule.SourceRepo}) {
		t.Errorf("baseRules = %+v", got)
	}
}

func TestCIRulesFromBaseNotHead(t *testing.T) {
	got, err := baseRules(rulesRepo(t, baseGoRule, headGoRule), "origin/main")
	if err != nil || len(got) != 1 || got[0].Rule != "BASE RULE" {
		t.Errorf("baseRules = %+v, %v; want the base branch's rule", got, err)
	}
}

func TestBaseRulesMissingFile(t *testing.T) {
	got, err := baseRules(rulesRepo(t, "", headGoRule), "origin/main")
	if err != nil || len(got) != 0 {
		t.Errorf("baseRules = %+v, %v; want no entries and no error", got, err)
	}
}

func TestBaseRulesUnreadable(t *testing.T) {
	repo := rulesRepo(t, baseGoRule, "")
	for _, ref := range []string{"origin/other", ""} {
		if _, err := baseRules(repo, ref); !errors.Is(err, errRulesUnreadable) {
			t.Errorf("baseRules(%q) = %v, want errRulesUnreadable", ref, err)
		}
	}
	if _, err := baseRules(t.TempDir(), "origin/main"); !errors.Is(err, errRulesUnreadable) {
		t.Errorf("not a repository: %v, want errRulesUnreadable", err)
	}
}

func TestBaseRulesMalformed(t *testing.T) {
	_, err := baseRules(rulesRepo(t, "rules:\n  - path: a.go\n", ""), "origin/main")
	if err == nil || errors.Is(err, errRulesUnreadable) || !strings.HasPrefix(err.Error(), ".pergent/rules.yaml: entry 1: ") {
		t.Errorf("baseRules = %v, want the loader's error", err)
	}
	big := "rules: []\n" + strings.Repeat("#", rule.MaxFileBytes)
	_, err = baseRules(rulesRepo(t, big, ""), "origin/main")
	if err == nil || errors.Is(err, errRulesUnreadable) || !strings.Contains(err.Error(), "over the limit of 65536") {
		t.Errorf("big file: %v, want the size error", err)
	}
}

func TestCIModeRulesFlagOverrides(t *testing.T) {
	repo := rulesRepo(t, "rules:\n  - path: a.go\n", "") // a broken base file is never read
	list := rulesFileOutside(t, `[{"source":"repo","path":"*.go","rule":"FROM THE FLAG"}]`)
	cfg := config.Config{Local: true, BaseBranch: "main", RepoPath: repo}
	r, note, err := ciRules(cfg, list, quietLog)
	if err != nil || note != "" {
		t.Fatalf("ciRules = %v, %q", err, note)
	}
	if e, ok := r.Resolve("a.go"); !ok || e.Rule != "FROM THE FLAG" {
		t.Errorf("a.go got %+v, %v; want the flag's rule", e, ok)
	}
	if _, _, err := ciRules(cfg, filepath.Join(t.TempDir(), "missing"), quietLog); err == nil || !strings.HasPrefix(err.Error(), "--rules ") {
		t.Errorf("an unreadable --rules = %v, want an error", err)
	}
}

func TestCIRulesBaseRef(t *testing.T) {
	repo := rulesRepo(t, baseGoRule, "")
	logger, logs := log.New(io.Discard, "", 0), &bytes.Buffer{}
	logger.SetOutput(logs)
	for name, cfg := range map[string]config.Config{
		"local":  {Local: true, BaseBranch: "main", RepoPath: repo},
		"github": {Platform: "github", GitHub: config.GitHubConfig{BaseBranch: "main"}, RepoPath: repo},
		"gitlab": {Platform: "gitlab", GitLab: config.GitLabConfig{BaseBranch: "main"}, RepoPath: repo},
	} {
		r, note, err := ciRules(cfg, "", logger)
		if e, ok := r.Resolve("a.go"); err != nil || note != "" || !ok || e.Rule != "BASE RULE" {
			t.Errorf("%s: %+v %v %q %v", name, e, ok, note, err)
		}
	}
	r, note, err := ciRules(config.Config{Platform: "gitlab", RepoPath: repo}, "", logger)
	if err != nil || note != rulesUnreadableNote || r.Entries() != len(rule.Builtin()) {
		t.Errorf("no base branch: %v %q %d", err, note, r.Entries())
	}
	if !strings.Contains(logs.String(), "warning: repository rules could not be read: ") ||
		!strings.Contains(logs.String(), "; reviewing with built-in rules only") {
		t.Errorf("log = %q, want the warning", logs.String())
	}
}

// TestMain_RulesInLocalMode runs main() in a child process in local mode with
// a stub opencode that keeps the file-rules.md it gets: the base branch's
// rule reaches the run, the head's never does, and the comment counts it. A
// broken base file stops pergent with a config error.
func TestMain_RulesInLocalMode(t *testing.T) {
	if root := os.Getenv("PERGENT_TEST_RULES_REPO"); root != "" {
		os.Args = []string{"pergent", "--local", "--skill", "code-review", "--repo-path", root}
		main()
		return
	}
	bin, capDir := rulesStub(t)
	run := func(t *testing.T, repo string) (string, string, error) {
		os.Remove(filepath.Join(capDir, "file-rules.md"))
		cmd := exec.Command(os.Args[0], "-test.run=^TestMain_RulesInLocalMode$")
		cmd.Env = append(os.Environ(), "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"),
			"PERGENT_TEST_RULES_REPO="+repo, "OPENCODE_PROVIDER=anthropic", "OPENCODE_MODEL=claude-sonnet-4",
			"PERGENT_RULES=", "PERGENT_NO_BUILT_IN_RULES=")
		var so, se bytes.Buffer
		cmd.Stdout, cmd.Stderr = &so, &se
		err := cmd.Run()
		got, _ := os.ReadFile(filepath.Join(capDir, "file-rules.md"))
		return so.String() + se.String(), string(got), err
	}

	out, got, err := run(t, rulesRepo(t, baseGoRule, headGoRule))
	if err != nil {
		t.Fatalf("main() failed: %v\n%s", err, out)
	}
	if !strings.Contains(got, "BASE RULE") || strings.Contains(got, "HEAD RULE") {
		t.Errorf("file-rules.md = %q, want the base branch's rule only", got)
	}
	if !strings.Contains(out, "Rules: repo `*.go` (1 file)") {
		t.Errorf("the comment has no Rules line:\n%s", out)
	}

	out, _, err = run(t, rulesRepo(t, "rules:\n  - path: a.go\n", ""))
	if err == nil || !strings.Contains(out, "loading rules: .pergent/rules.yaml: entry 1: needs a rule or off: true") {
		t.Errorf("a broken base file: err %v, output:\n%s", err, out)
	}
}

// TestMain_RulesUnreadableInPlatformMode runs main() against a fake GitLab in
// a folder that is not a git checkout: the rules can't be read, the review
// goes on, and the posted comment says so.
func TestMain_RulesUnreadableInPlatformMode(t *testing.T) {
	if root := os.Getenv("PERGENT_TEST_RULES_PLATFORM_REPO"); root != "" {
		os.Args = []string{"pergent", "--platform", "gitlab", "--skill", "code-review", "--repo-path", root}
		main()
		return
	}
	var mu sync.Mutex
	var posted string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/user"):
			fmt.Fprint(w, `{"id":1}`)
		case strings.HasSuffix(r.URL.Path, "/diffs"):
			fmt.Fprint(w, `[{"old_path":"a.go","new_path":"a.go","diff":"@@ -1 +1 @@\n-a\n+b\n"}]`)
		case r.Method == http.MethodPost:
			body, _ := io.ReadAll(r.Body)
			mu.Lock()
			posted = string(body)
			mu.Unlock()
			w.WriteHeader(http.StatusCreated)
			fmt.Fprint(w, `{"id":2}`)
		default:
			fmt.Fprint(w, `[]`)
		}
	}))
	defer srv.Close()
	bin, _ := rulesStub(t)
	cmd := exec.Command(os.Args[0], "-test.run=^TestMain_RulesUnreadableInPlatformMode$")
	cmd.Env = append(os.Environ(), "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"),
		"PERGENT_TEST_RULES_PLATFORM_REPO="+t.TempDir(), "GITLAB_TOKEN=test-token", "CI_SERVER_URL="+srv.URL,
		"CI_PROJECT_ID=1", "CI_MERGE_REQUEST_IID=1", "CI_MERGE_REQUEST_TARGET_BRANCH_NAME=main",
		"OPENCODE_PROVIDER=anthropic", "OPENCODE_MODEL=claude-sonnet-4", "PERGENT_RULES=")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("main() failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "warning: repository rules could not be read: ") {
		t.Errorf("no warning in the log:\n%s", out)
	}
	mu.Lock()
	defer mu.Unlock()
	if !strings.Contains(posted, "Repository rules could not be read") {
		t.Errorf("the posted comment has no note: %s", posted)
	}
}

// rulesStub is a stub opencode that answers with no findings and copies the
// file-rules.md it gets into capDir.
func rulesStub(t *testing.T) (bin, capDir string) {
	t.Helper()
	capDir = t.TempDir()
	ndjson := `{"type":"step_start","part":{"type":"step-start"}}
{"type":"text","part":{"type":"text","text":"` + "```json\\n{\\\"findings\\\": []}\\n```" + `"}}
{"type":"step_finish","part":{"type":"step-finish","reason":"stop","tokens":{"total":24,"input":20,"output":4}}}
`
	script := "#!/bin/sh\nprev=\n" +
		"for a in \"$@\"; do\n" +
		"  if [ \"$prev\" = --file ] && [ \"$(basename \"$a\")\" = file-rules.md ]; then cp \"$a\" \"" + capDir + "/file-rules.md\"; fi\n" +
		"  prev=$a\n" +
		"done\n" +
		"cat <<'EOF'\n" + ndjson + "EOF\n"
	bin = filepath.Join(t.TempDir(), "bin")
	if err := os.Mkdir(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "opencode"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return bin, capDir
}
