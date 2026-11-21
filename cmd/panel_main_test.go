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
	panelKeyA = "key-a-0123456789"
	panelKeyB = "key-b-0123456789"
	// panelCloudA is a cloud credential model A could sign in with.
	panelCloudA = "cloud-a-0123456789"
)

// fakePanelOpencode is a stub opencode for a panel merge. For --version it
// prints a supported version. For a run it saves its whole environment as
// <pid>.env and its config as <pid>.config in dir (the run's environment
// keeps no variable of ours, so the path is in the script), then answers
// as the judge of the findings in the --file that ends in
// their-findings.json.
const fakePanelOpencode = `#!/bin/sh
if [ "$1" = "--version" ]; then echo "opencode 2.3.2"; exit 0; fi
env > "DIR/$$.env"
cp "$OPENCODE_CONFIG" "DIR/$$.config"
their=""
prev=""
for a in "$@"; do
	if [ "$prev" = "--file" ]; then
		case "$a" in *their-findings.json) their="$a" ;; esac
	fi
	prev="$a"
done
if grep -q '"a1"' "$their"; then cat "DIR/judge-of-a.ndjson"; else cat "DIR/judge-of-b.ndjson"; fi
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

// main runs `pergent agent --panel` in a process of its own, against a stub
// opencode: each judge run must get only its own model's key.
func TestMain_PanelKeysStayApart(t *testing.T) {
	if args := os.Getenv("PERGENT_TEST_PANEL_ARGS"); args != "" {
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
	script := strings.ReplaceAll(fakePanelOpencode, "DIR", dumps)
	if err := os.WriteFile(filepath.Join(bin, "opencode"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	judgeRun(t, filepath.Join(dumps, "judge-of-a.ndjson"), checkAnswer(t, map[string]any{"id": "a1", "real": true, "reason": "yes", "same_as": "b1"}))
	judgeRun(t, filepath.Join(dumps, "judge-of-b.ndjson"), checkAnswer(t, map[string]any{"id": "b1", "real": true, "reason": "yes", "same_as": "a1"}))

	dir, base, head := agentRepo(t, nil)
	a := member("a.json", memberCompleted(reviewSkill(panelFindA)))(t, base, head)
	b := member("b.json", memberCompleted(reviewSkill(panelFindB)))(t, base, head)
	out := filepath.Join(t.TempDir(), "r.json")
	args, err := json.Marshal([]string{"--repo-path", dir, "--base", base, "--head", head, "--skills-dir", oneSkill(t),
		"--result", out, "--panel", "--panel-a", a, "--panel-b", b})
	if err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(os.Args[0], "-test.run=^TestMain_PanelKeysStayApart$")
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "OPENCODE_") && !strings.HasPrefix(kv, "PERGENT_") && !strings.HasPrefix(kv, "PATH=") && !strings.HasPrefix(kv, "AWS_") {
			cmd.Env = append(cmd.Env, kv)
		}
	}
	cmd.Env = append(cmd.Env,
		"PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"),
		"PERGENT_TEST_PANEL_ARGS="+string(args),
		"PERGENT_TEST_REAL_VERSION_CHECK=1",
		"OPENCODE_PROVIDER=anthropic",
		"OPENCODE_MODEL=model-a",
		"OPENCODE_API_KEY="+panelKeyA,
		"PERGENT_PANEL_B_PROVIDER=openai",
		"PERGENT_PANEL_B_MODEL=model-b",
		"PERGENT_PANEL_B_API_KEY="+panelKeyB,
		"AWS_SECRET_ACCESS_KEY="+panelCloudA,
	)
	logs, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("main() err = %v\n%s", err, logs)
	}
	for _, key := range []string{panelKeyA, panelKeyB} {
		if strings.Contains(string(logs), key) {
			t.Errorf("the log holds %s", key)
		}
	}

	envs, err := filepath.Glob(filepath.Join(dumps, "*.env"))
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]int{}
	for _, p := range envs {
		env := readEnvDump(t, p)
		cfg, err := os.ReadFile(strings.TrimSuffix(p, ".env") + ".config")
		if err != nil {
			t.Fatal(err)
		}
		var c struct {
			Model string `json:"model"`
		}
		if err := json.Unmarshal(cfg, &c); err != nil {
			t.Fatalf("%s: config: %v", p, err)
		}
		seen[c.Model]++
		own, other := panelKeyA, panelKeyB
		if c.Model == panelModelB {
			own, other = panelKeyB, panelKeyA
		}
		if env["OPENCODE_API_KEY"] != own {
			t.Errorf("run on %s: OPENCODE_API_KEY is not its own key", c.Model)
		}
		raw, _ := os.ReadFile(p)
		if strings.Contains(string(raw), other) || strings.Contains(string(cfg), other) {
			t.Errorf("run on %s holds the other model's key", c.Model)
		}
		if strings.Contains(string(raw), "PERGENT_PANEL_B_API_KEY") {
			t.Errorf("run on %s has PERGENT_PANEL_B_API_KEY", c.Model)
		}
		// Model A's cloud credentials reach A's runs only.
		if got, ok := env["AWS_SECRET_ACCESS_KEY"]; c.Model == panelModelB && (ok || strings.Contains(string(raw), panelCloudA)) {
			t.Errorf("run on %s holds model A's cloud credential", c.Model)
		} else if c.Model == panelModelA && got != panelCloudA {
			t.Errorf("run on %s: AWS_SECRET_ACCESS_KEY = %q, want model A's", c.Model, got)
		}
	}
	if len(envs) != 2 || seen[panelModelA] != 1 || seen[panelModelB] != 1 {
		t.Errorf("runs by model = %v, want one judge run on each\n%s", seen, logs)
	}

	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("no result: %v\n%s", err, logs)
	}
	r := decodeOnly(t, data)
	if r.Status != result.StatusCompleted || r.Panel == nil || r.Panel.Mode != "both" {
		t.Fatalf("result status %q, panel %+v\n%s", r.Status, r.Panel, logs)
	}
	s, ok := skillNamed(r, "review")
	if !ok || s.Panel == nil || len(s.Panel.Judges) != 2 {
		t.Fatalf("skill = %+v", s)
	}
	for _, j := range s.Panel.Judges {
		if j.Status != "ok" {
			t.Errorf("judge %s status %q, want ok", j.Role, j.Status)
		}
	}
	if len(s.Findings) != 1 || s.Findings[0].Panel == nil || s.Findings[0].Panel.FoundBy != "both" {
		t.Errorf("findings = %+v", s.Findings)
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
