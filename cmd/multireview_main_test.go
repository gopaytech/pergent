package main

import (
	"bufio"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gopaytech/pergent/internal/result"
)

const (
	multiJudgeKey = "key-judge-0123456789"
	multiOldKey   = "key-old-0123456789"
)

// fakeMultiOpencode is a stub opencode for a multi review merge. For
// --version it prints a supported version. For a run it saves its whole
// environment as <pid>.env and its config as <pid>.config in dir (the run's
// environment keeps no variable of ours, so the path is in the script), then
// prints a completed run from dir.
const fakeMultiOpencode = `#!/bin/sh
if [ "$1" = "--version" ]; then echo "opencode 2.3.2"; exit 0; fi
env > "DIR/$$.env"
cp "$OPENCODE_CONFIG" "DIR/$$.config"
cat "DIR/judge.ndjson"
`

// judgeRun writes the event lines of a completed run whose final text is
// answer: completed.ndjson with its last text replaced.
func judgeRun(t *testing.T, path, answer string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "internal", "runner", "testdata", "completed.ndjson"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	last := -1
	for i, l := range lines {
		if strings.Contains(l, `"type":"text"`) {
			last = i
		}
	}
	var ev map[string]any
	if err := json.Unmarshal([]byte(lines[last]), &ev); err != nil {
		t.Fatal(err)
	}
	ev["part"].(map[string]any)["text"] = answer
	b, err := json.Marshal(ev)
	if err != nil {
		t.Fatal(err)
	}
	lines[last] = string(b)
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

// The judge answers 1.1 is not real with its key in the reason, so the reason
// reaches the result and must be redacted.
//
// main runs `pergent agent --multi-review` in a process of its own, against
// a stub opencode: the judge run must get only the judge's key.
func TestMain_MultiReviewKeys(t *testing.T) {
	if args := os.Getenv("PERGENT_TEST_MULTI_ARGS"); args != "" {
		var a []string
		if err := json.Unmarshal([]byte(args), &a); err != nil {
			t.Fatal(err)
		}
		os.Args = append([]string{"pergent", "agent"}, a...)
		main()
		return
	}
	dumps := t.TempDir()
	bin := t.TempDir()
	script := strings.ReplaceAll(fakeMultiOpencode, "DIR", dumps)
	if err := os.WriteFile(filepath.Join(bin, "opencode"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	judgeRun(t, filepath.Join(dumps, "judge.ndjson"), checkAnswer(t,
		map[string]any{"id": "1.1", "real": false, "reason": "not real " + multiJudgeKey, "same_as": []string{"2.1"}},
		map[string]any{"id": "2.1", "real": true, "reason": "yes", "same_as": []string{"1.1"}}))

	dir, base, head := agentRepo(t, nil)
	other := reviewerFind
	other.Fingerprint, other.Title, other.Severity = "fb1", "Nil map write on reload", "suggestion"
	var files []string
	for i, f := range []result.Finding{reviewerFind, other} {
		r := reviewerCompleted(reviewSkill(f))
		r.Base, r.Head = base, head
		files = append(files, writeReviewer(t, "r"+string(rune('1'+i))+".json", r))
	}
	out := filepath.Join(t.TempDir(), "r.json")
	args, err := json.Marshal([]string{"--repo-path", dir, "--base", base, "--head", head, "--skills-dir", oneSkill(t),
		"--result", out, "--multi-review", "--reviewer", files[0], "--reviewer", files[1], "--mr-dismissals", noMRDismissals(t)})
	if err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(os.Args[0], "-test.run=^TestMain_MultiReviewKeys$")
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "OPENCODE_") && !strings.HasPrefix(kv, "PERGENT_") && !strings.HasPrefix(kv, "PATH=") {
			cmd.Env = append(cmd.Env, kv)
		}
	}
	cmd.Env = append(cmd.Env,
		"PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"),
		"PERGENT_TEST_MULTI_ARGS="+string(args),
		"PERGENT_TEST_REAL_VERSION_CHECK=1",
		"OPENCODE_PROVIDER=anthropic",
		"OPENCODE_MODEL=model-j",
		"OPENCODE_API_KEY="+multiJudgeKey,
		"PERGENT_PANEL_B_API_KEY="+multiOldKey,
	)
	logs, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("main() err = %v\n%s", err, logs)
	}
	for _, key := range []string{multiJudgeKey, multiOldKey} {
		if strings.Contains(string(logs), key) {
			t.Errorf("the log holds %s", key)
		}
	}

	envs, err := filepath.Glob(filepath.Join(dumps, "*.env"))
	if err != nil {
		t.Fatal(err)
	}
	if len(envs) != 1 {
		t.Fatalf("opencode runs = %d, want 1 judge run\n%s", len(envs), logs)
	}
	env := readEnvDump(t, envs[0])
	if env["OPENCODE_API_KEY"] != multiJudgeKey {
		t.Error("the judge run does not hold the judge key")
	}
	raw, _ := os.ReadFile(envs[0])
	if strings.Contains(string(raw), multiOldKey) || strings.Contains(string(raw), "PERGENT_PANEL_B_API_KEY") {
		t.Error("the judge run holds the old panel key")
	}
	cfg, err := os.ReadFile(strings.TrimSuffix(envs[0], ".env") + ".config")
	if err != nil {
		t.Fatal(err)
	}
	var c struct {
		Model string `json:"model"`
	}
	if err := json.Unmarshal(cfg, &c); err != nil {
		t.Fatalf("config: %v", err)
	}
	if c.Model != "anthropic/model-j" {
		t.Errorf("config model = %q, want anthropic/model-j", c.Model)
	}
	if strings.Contains(string(cfg), multiOldKey) {
		t.Error("the config holds the old panel key")
	}

	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("no result: %v\n%s", err, logs)
	}
	if strings.Contains(string(data), multiJudgeKey) {
		t.Error("the result holds the judge key")
	}
	if !strings.Contains(string(data), "not real [REDACTED]") {
		t.Errorf("the result has no redacted judge reason\n%s", data)
	}
	r := decodeOnly(t, data)
	if r.Status != result.StatusCompleted || r.MultiReview == nil || !r.MultiReview.Judge.Judged {
		t.Fatalf("result status %q, multi review %+v\n%s", r.Status, r.MultiReview, logs)
	}
	s, ok := skillNamed(r, "review")
	if !ok || s.MultiReview == nil || s.MultiReview.Judge != "ok" {
		t.Fatalf("skill = %+v", s)
	}
}

// readEnvDump reads an `env` dump into a map, one NAME=value per line.
func readEnvDump(t *testing.T, path string) map[string]string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	env := map[string]string{}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		if k, v, ok := strings.Cut(sc.Text(), "="); ok {
			env[k] = v
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return env
}
