package scan

import (
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

const repoManifest = "../../security-skills/security-audit/pergent-scan.yaml"

const goodYAML = `name: security-audit
commit: c1c8a8c1471069fb0e188eeaff69b8e8db6564a8
entry: SKILL.md
tools: [bash, write, task]
steps:
  main: 600
  subagent: 100
task: "Skill {skill}. Target {target}. Commit {commit}. Output {output}. Prior {prior}."
output:
  path: findings.json
  format: cloudflare-findings
state: [findings.json, coverage-ledger.json]
`

func TestParseManifestRepoFile(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile(repoManifest)
	if err != nil {
		t.Fatal(err)
	}
	got, err := ParseManifest(data)
	if err != nil {
		t.Fatal(err)
	}
	want := Manifest{
		Name:   "security-audit",
		Commit: "c1c8a8c1471069fb0e188eeaff69b8e8db6564a8",
		Entry:  "SKILL.md",
		Tools:  []string{"bash", "write", "task"},
		Steps:  Steps{Main: 600, Subagent: 100},
		Task: "Full audit mode. Skill directory: {skill}. Target: {target}. " +
			"Source commit: {commit}. Output directory: {output}. Prior run: {prior}. Do not ask questions. " +
			"No OS sandbox is available: do not build, test or run the target's code.",
		Output: ManifestOutput{Path: "findings.json", Format: FormatCloudflare},
		State:  []string{"findings.json", "coverage-ledger.json"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v\nwant %+v", got, want)
	}
}

func TestParseManifestRefuses(t *testing.T) {
	t.Parallel()
	if _, err := ParseManifest([]byte(goodYAML)); err != nil {
		t.Fatalf("the base manifest must parse: %v", err)
	}
	nine := make([]string, 9)
	for i := range nine {
		nine[i] = "s" + strconv.Itoa(i)
	}
	long := strings.Repeat("a", 2000)
	cases := []struct{ name, from, to string }{
		{"unknown key", "entry: SKILL.md\n", "entry: SKILL.md\nextra: 1\n"},
		{"upper case name", "name: security-audit", "name: Security"},
		{"short commit", "c1c8a8c1471069fb0e188eeaff69b8e8db6564a8", "c1c8a8c1471069fb0e188eeaff69b8e8db6564a"},
		{"upper case commit", "c1c8a8c1471069fb0e188eeaff69b8e8db6564a8", "C1C8A8C1471069FB0E188EEAFF69B8E8DB6564A8"},
		{"unknown tool", "[bash, write, task]", "[bash, webfetch]"},
		{"tool twice", "[bash, write, task]", "[bash, bash, task]"},
		{"main 4", "main: 600", "main: 4"},
		{"main 1001", "main: 600", "main: 1001"},
		{"subagent 4", "subagent: 100", "subagent: 4"},
		{"subagent 301", "subagent: 100", "subagent: 301"},
		{"subagent missing", "  subagent: 100\n", ""},
		{"task without target", "Target {target}. ", ""},
		{"task without commit", "Commit {commit}. ", ""},
		{"task without output", "Output {output}. ", ""},
		{"task with home", "Prior {prior}.", "Prior {prior}. {home}"},
		{"task too long", "Prior {prior}.", "Prior {prior}. " + long},
		{"path with slash", "path: findings.json", "path: a/b"},
		{"path dotdot", "path: findings.json", "path: .."},
		{"unknown format", "cloudflare-findings", "csv"},
		{"state dotdot", "state: [findings.json, coverage-ledger.json]", "state: [../x]"},
		{"state repeat", "state: [findings.json, coverage-ledger.json]", "state: [a, a]"},
		{"state nine", "state: [findings.json, coverage-ledger.json]", "state: [" + strings.Join(nine, ", ") + "]"},
		{"entry with slash", "entry: SKILL.md", "entry: a/SKILL.md"},
		{"two documents", "state: [findings.json, coverage-ledger.json]\n", "state: [a]\n---\nname: x\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if !strings.Contains(goodYAML, c.from) {
				t.Fatalf("bad case: %q not in base", c.from)
			}
			if _, err := ParseManifest([]byte(strings.Replace(goodYAML, c.from, c.to, 1))); err == nil {
				t.Fatal("want an error")
			}
		})
	}
	t.Run("no task tool, no subagent steps", func(t *testing.T) {
		t.Parallel()
		y := strings.Replace(strings.Replace(goodYAML, "[bash, write, task]", "[bash]", 1), "  subagent: 100\n", "", 1)
		if _, err := ParseManifest([]byte(y)); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("empty", func(t *testing.T) {
		t.Parallel()
		if _, err := ParseManifest(nil); err == nil {
			t.Fatal("want an error")
		}
	})
}

func TestLoadManifestChecksTheFolder(t *testing.T) {
	t.Parallel()
	write := func(t *testing.T, dir string) {
		t.Helper()
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, ManifestFile), []byte(goodYAML), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	t.Run("good", func(t *testing.T) {
		t.Parallel()
		dir := filepath.Join(t.TempDir(), "security-audit")
		write(t, dir)
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		m, err := LoadManifest(dir)
		if err != nil {
			t.Fatal(err)
		}
		if m.Name != "security-audit" || m.Entry != "SKILL.md" {
			t.Fatalf("got %+v", m)
		}
	})
	t.Run("folder name differs", func(t *testing.T) {
		t.Parallel()
		dir := filepath.Join(t.TempDir(), "other")
		write(t, dir)
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadManifest(dir); err == nil {
			t.Fatal("want an error")
		}
	})
	t.Run("entry is a link", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		dir := filepath.Join(root, "security-audit")
		write(t, dir)
		real := filepath.Join(root, "real.md")
		if err := os.WriteFile(real, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(real, filepath.Join(dir, "SKILL.md")); err != nil {
			t.Skip("no symlinks here")
		}
		if _, err := LoadManifest(dir); err == nil {
			t.Fatal("want an error")
		}
	})
	t.Run("manifest too large", func(t *testing.T) {
		t.Parallel()
		dir := filepath.Join(t.TempDir(), "security-audit")
		write(t, dir)
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		big := goodYAML + "# " + strings.Repeat("a", 65<<10) + "\n"
		if err := os.WriteFile(filepath.Join(dir, ManifestFile), []byte(big), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadManifest(dir); err == nil {
			t.Fatal("want an error")
		}
	})
	t.Run("manifest is a link", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		dir := filepath.Join(root, "security-audit")
		write(t, dir)
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		real := filepath.Join(root, "real.yaml")
		if err := os.WriteFile(real, []byte(goodYAML), 0o644); err != nil {
			t.Fatal(err)
		}
		os.Remove(filepath.Join(dir, ManifestFile))
		if err := os.Symlink(real, filepath.Join(dir, ManifestFile)); err != nil {
			t.Skip("no symlinks here")
		}
		if _, err := LoadManifest(dir); err == nil {
			t.Fatal("want an error")
		}
	})
	t.Run("entry missing", func(t *testing.T) {
		t.Parallel()
		dir := filepath.Join(t.TempDir(), "security-audit")
		write(t, dir)
		if _, err := LoadManifest(dir); err == nil {
			t.Fatal("want an error")
		}
	})
	t.Run("manifest missing", func(t *testing.T) {
		t.Parallel()
		if _, err := LoadManifest(filepath.Join(t.TempDir(), "security-audit")); err == nil {
			t.Fatal("want an error")
		}
	})
}

func TestTaskLine(t *testing.T) {
	t.Parallel()
	m, err := ParseManifest([]byte(goodYAML))
	if err != nil {
		t.Fatal(err)
	}
	v := TaskValues{Skill: "/s", Target: "/t", Commit: "abc", Output: "/o", Prior: "/p"}
	if got, want := m.TaskLine(v), "Skill /s. Target /t. Commit abc. Output /o. Prior /p."; got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	v.Prior = ""
	if got, want := m.TaskLine(v), "Skill /s. Target /t. Commit abc. Output /o. Prior none."; got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	// A value that looks like a placeholder is not replaced again.
	v.Target = "{output}"
	if got := m.TaskLine(v); !strings.Contains(got, "Target {output}.") {
		t.Fatalf("value was replaced twice: %q", got)
	}
}

// TestManifestCommitMatchesDockerfile keeps the image's pinned skill commit
// and the manifest's commit the same: one is what the image fetches, the
// other is what a scan reports.
func TestManifestCommitMatchesDockerfile(t *testing.T) {
	t.Parallel()
	dockerfile, err := os.ReadFile("../../Dockerfile")
	if err != nil {
		t.Fatal(err)
	}
	var arg string
	for _, line := range strings.Split(string(dockerfile), "\n") {
		if v, ok := strings.CutPrefix(strings.TrimSpace(line), "ARG SECURITY_AUDIT_COMMIT="); ok {
			arg = v
		}
	}
	if arg == "" {
		t.Fatal("the Dockerfile has no ARG SECURITY_AUDIT_COMMIT with a default")
	}
	data, err := os.ReadFile(repoManifest)
	if err != nil {
		t.Fatal(err)
	}
	m, err := ParseManifest(data)
	if err != nil {
		t.Fatal(err)
	}
	if arg != m.Commit {
		t.Errorf("Dockerfile SECURITY_AUDIT_COMMIT is %q, the manifest's commit is %q", arg, m.Commit)
	}
}
