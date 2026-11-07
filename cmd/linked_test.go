package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gopaytech/pergent/internal/config"
	"github.com/gopaytech/pergent/internal/result"
	"github.com/gopaytech/pergent/internal/review"
)

func writeLinkedFile(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "linked-repos.json")
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestReadLinkedRepos(t *testing.T) {
	t.Run("a valid file", func(t *testing.T) {
		p := writeLinkedFile(t, `[
		  {"name":"billing","path":"/work/linked-1","watched":["api/","render/*.go"],"can_block":true},
		  {"name":"web","path":"","watched":["api/"],"can_block":false,"error":"clone failed: timed out"}
		]`)
		logger, logs := bufferLog()
		got, err := readLinkedRepos(p, logger)
		if err != nil {
			t.Fatalf("readLinkedRepos() error: %v", err)
		}
		want := []review.LinkedRepo{
			{Name: "billing", Path: "/work/linked-1", Watched: []string{"api/", "render/*.go"}, CanBlock: true},
			{Name: "web", Watched: []string{"api/"}, Error: "clone failed: timed out"},
		}
		if len(got) != 2 || got[0].Name != want[0].Name || got[0].Path != want[0].Path || !got[0].CanBlock ||
			strings.Join(got[0].Watched, ",") != "api/,render/*.go" || got[0].Error != "" ||
			got[1].Name != "web" || got[1].Error != want[1].Error || got[1].CanBlock {
			t.Errorf("got %+v, want %+v", got, want)
		}
		if !strings.Contains(logs.String(), "linked repositories: 2") {
			t.Errorf("logged %q, want the count", logs.String())
		}
	})
	t.Run("an empty list is no links", func(t *testing.T) {
		got, err := readLinkedRepos(writeLinkedFile(t, `[]`), quietLog)
		if err != nil || len(got) != 0 {
			t.Errorf("got %+v, %v, want none", got, err)
		}
	})
	t.Run("a missing file is an error", func(t *testing.T) {
		if _, err := readLinkedRepos(filepath.Join(t.TempDir(), "nope.json"), quietLog); err == nil {
			t.Error("want an error")
		}
	})
	t.Run("more than 3 entries is refused", func(t *testing.T) {
		var entries []map[string]any
		for _, n := range []string{"a", "b", "c", "d"} {
			entries = append(entries, map[string]any{"name": n, "path": "/work/linked-" + n})
		}
		data, _ := json.Marshal(entries)
		if _, err := readLinkedRepos(writeLinkedFile(t, string(data)), quietLog); err == nil || !strings.Contains(err.Error(), "3") {
			t.Errorf("error = %v, want a refusal naming the limit of 3", err)
		}
	})
	// Whatever makes the whole file unusable is an error. Nothing is guessed
	// from a file like that.
	for name, content := range map[string]string{
		"not JSON":              `{"name":`,
		"not a list":            `{"name":"a","path":"/x"}`,
		"an entry with no name": `[{"path":"/work/linked-1"}]`,
		"a duplicate name":      `[{"name":"a","path":"/work/linked-1"},{"name":"a","path":"/work/linked-2"}]`,
	} {
		t.Run("refuses "+name, func(t *testing.T) {
			if _, err := readLinkedRepos(writeLinkedFile(t, content), quietLog); err == nil {
				t.Errorf("readLinkedRepos(%s) = nil error, want one", content)
			}
		})
	}
	// A bad path spoils its own entry only: the entry is kept, with an Error,
	// so the repository it names can be told apart.
	for name, path := range map[string]string{
		"no path":                   ``,
		"a relative path":           `work/linked-1`,
		"a path with a wildcard":    `/work/*`,
		"a path that is the root":   `/`,
		"a path with a bracket":     `/work/linked-[1]`,
		"a path with a brace":       `/work/{x,y}`,
		"a path with a question":    `/work/linked-?`,
		"a path with a parent part": `/work/../etc`,
	} {
		t.Run("marks the entry for "+name, func(t *testing.T) {
			content := `[{"name":"good","path":"/work/linked-1"},{"name":"a","path":` + jsonString(path) + `}]`
			got, err := readLinkedRepos(writeLinkedFile(t, content), quietLog)
			if err != nil || len(got) != 2 {
				t.Fatalf("readLinkedRepos(%s) = %+v, %v, want both entries and no error", content, got, err)
			}
			if got[0].Error != "" {
				t.Errorf("the good entry got Error %q", got[0].Error)
			}
			if got[1].Name != "a" || got[1].Error == "" {
				t.Errorf("entry = %+v, want it named and marked", got[1])
			}
		})
	}
	t.Run("an entry with an error needs no path", func(t *testing.T) {
		got, err := readLinkedRepos(writeLinkedFile(t, `[{"name":"a","error":"not cloned"}]`), quietLog)
		if err != nil || len(got) != 1 || got[0].Error != "not cloned" {
			t.Errorf("got %+v, %v", got, err)
		}
	})
}

func TestParseAgent_LinkedReposFromEnv(t *testing.T) {
	sha := strings.Repeat("a", 40)
	args := []string{"--repo-path", "/r", "--base", sha, "--head", sha, "--skills-dir", "/s", "--result", "-"}
	env := agentEnv(map[string]string{"PERGENT_LINKED_REPOS": "/env/linked.json"})

	in, e := parseAgent(args, env)
	if e != nil {
		t.Fatalf("parseAgent() error: %+v", e)
	}
	if in.linkedPath != "/env/linked.json" {
		t.Errorf("linkedPath = %q, want the PERGENT_LINKED_REPOS path", in.linkedPath)
	}
	in, e = parseAgent(append(args, "--linked-repos", "/flag/linked.json"), env)
	if e != nil {
		t.Fatalf("parseAgent() error: %+v", e)
	}
	if in.linkedPath != "/flag/linked.json" {
		t.Errorf("linkedPath = %q, want the flag to win", in.linkedPath)
	}
	in, e = parseAgent(args, agentEnv(nil))
	if e != nil || in.linkedPath != "" {
		t.Errorf("linkedPath = %q, %+v, want none", in.linkedPath, e)
	}
}

func TestPrepareAgent_LinkedRepos(t *testing.T) {
	repo, base, head := agentRepo(t, nil)

	t.Run("a clone outside the checkout is read and scrubbed", func(t *testing.T) {
		clone := t.TempDir()
		writeFile(t, clone, "main.go", "package main\n")
		writeFile(t, clone, ".env", "KEY=1\n")
		p := writeLinkedFile(t, `[{"name":"billing","path":`+jsonString(clone)+`,"watched":["api/"],"can_block":true}]`)
		in := mustParse(t, agentArgs(repo, base, head, oneSkill(t), "--linked-repos", p))
		logger, logs := bufferLog()
		if e := prepareAgent(&in, logger); e != nil {
			t.Fatalf("prepareAgent() error: %+v", e)
		}
		if len(in.Linked) != 1 || in.Linked[0].Name != "billing" || in.Linked[0].Error != "" {
			t.Fatalf("Linked = %+v", in.Linked)
		}
		if _, err := os.Stat(filepath.Join(clone, ".env")); !os.IsNotExist(err) {
			t.Errorf(".env still in the clone after prepareAgent: %v", err)
		}
		if _, err := os.Stat(filepath.Join(clone, "main.go")); err != nil {
			t.Errorf("main.go: %v", err)
		}
		if !strings.Contains(logs.String(), "linked repositories: 1") {
			t.Errorf("logged %q", logs.String())
		}
	})
	t.Run("usage for a clone path inside the checkout", func(t *testing.T) {
		writeFile(t, repo, "inside/main.go", "package x\n")
		defer os.RemoveAll(filepath.Join(repo, "inside"))
		p := writeLinkedFile(t, `[{"name":"billing","path":`+jsonString(filepath.Join(repo, "inside"))+`}]`)
		in := mustParse(t, agentArgs(repo, base, head, oneSkill(t), "--linked-repos", p))
		e := prepareAgent(&in, quietLog)
		if e == nil || e.Code != result.CodeUsage || !strings.Contains(e.Message, "billing") {
			t.Errorf("prepareAgent() error = %+v, want %s naming the link", e, result.CodeUsage)
		}
		if _, err := os.Stat(filepath.Join(repo, "inside", "main.go")); err != nil {
			t.Errorf("the checkout was scrubbed: %v", err)
		}
	})
	t.Run("usage for a clone path that holds the checkout", func(t *testing.T) {
		p := writeLinkedFile(t, `[{"name":"billing","path":`+jsonString(filepath.Dir(repo))+`}]`)
		in := mustParse(t, agentArgs(repo, base, head, oneSkill(t), "--linked-repos", p))
		if e := prepareAgent(&in, quietLog); e == nil || e.Code != result.CodeUsage {
			t.Errorf("prepareAgent() error = %+v, want %s", e, result.CodeUsage)
		}
	})
	// A file that can't be used must not fail the review: the skills run and
	// the impact check is failed. The details go to the log, not into the
	// entry, which reaches the comment.
	for name, content := range map[string]string{
		"a missing file":        "",
		"a malformed file":      `{"name":`,
		"more than 3 entries":   `[{"name":"a","path":"/x/1"},{"name":"b","path":"/x/2"},{"name":"c","path":"/x/3"},{"name":"d","path":"/x/4"}]`,
		"a duplicate name":      `[{"name":"a","path":"/x/1"},{"name":"a","path":"/x/2"}]`,
		"an entry with no name": `[{"path":"/x/1"}]`,
	} {
		t.Run("a failed link, not usage, for "+name, func(t *testing.T) {
			p := filepath.Join(t.TempDir(), "nope.json")
			if content != "" {
				p = writeLinkedFile(t, content)
			}
			in := mustParse(t, agentArgs(repo, base, head, oneSkill(t), "--linked-repos", p))
			logger, logs := bufferLog()
			if e := prepareAgent(&in, logger); e != nil {
				t.Fatalf("prepareAgent() error: %+v, want the review to go on", e)
			}
			if len(in.Linked) != 1 || in.Linked[0].Error == "" {
				t.Fatalf("Linked = %+v, want one failed entry", in.Linked)
			}
			if !strings.Contains(logs.String(), "--linked-repos") {
				t.Errorf("logged %q, want the reason", logs.String())
			}
		})
	}
	t.Run("a bad path fails its entry only", func(t *testing.T) {
		clone := t.TempDir()
		p := writeLinkedFile(t, `[{"name":"billing","path":`+jsonString(clone)+`},{"name":"web","path":"relative"}]`)
		in := mustParse(t, agentArgs(repo, base, head, oneSkill(t), "--linked-repos", p))
		if e := prepareAgent(&in, quietLog); e != nil {
			t.Fatalf("prepareAgent() error: %+v", e)
		}
		if len(in.Linked) != 2 || in.Linked[0].Error != "" || in.Linked[1].Error == "" {
			t.Errorf("Linked = %+v, want only web failed", in.Linked)
		}
	})
	t.Run("a skill named impact is refused when there are links", func(t *testing.T) {
		clone := t.TempDir()
		p := writeLinkedFile(t, `[{"name":"billing","path":`+jsonString(clone)+`}]`)
		in := mustParse(t, agentArgs(repo, base, head, impactSkill(t), "--linked-repos", p))
		e := prepareAgent(&in, quietLog)
		if e == nil || e.Code != result.CodeUsage || !strings.Contains(e.Message, review.ImpactSkill) {
			t.Errorf("prepareAgent() error = %+v, want %s naming the reserved skill", e, result.CodeUsage)
		}
	})
	t.Run("a skill named impact is fine without links", func(t *testing.T) {
		in := mustParse(t, agentArgs(repo, base, head, impactSkill(t)))
		if e := prepareAgent(&in, quietLog); e != nil {
			t.Errorf("prepareAgent() error = %+v, want none", e)
		}
	})
	t.Run("a link that failed to clone is not scrubbed and stays failed", func(t *testing.T) {
		clone := t.TempDir()
		writeFile(t, clone, ".env", "KEY=1\n")
		p := writeLinkedFile(t, `[{"name":"billing","path":`+jsonString(clone)+`,"error":"clone failed"}]`)
		in := mustParse(t, agentArgs(repo, base, head, oneSkill(t), "--linked-repos", p))
		if e := prepareAgent(&in, quietLog); e != nil {
			t.Fatalf("prepareAgent() error: %+v", e)
		}
		if len(in.Linked) != 1 || in.Linked[0].Error != "clone failed" {
			t.Errorf("Linked = %+v, want the caller's error kept", in.Linked)
		}
		if _, err := os.Stat(filepath.Join(clone, ".env")); err != nil {
			t.Errorf("an entry with an error was scrubbed: %v", err)
		}
	})
	t.Run("no links changes nothing", func(t *testing.T) {
		in := mustParse(t, agentArgs(repo, base, head, oneSkill(t)))
		if e := prepareAgent(&in, quietLog); e != nil {
			t.Fatalf("prepareAgent() error: %+v", e)
		}
		if in.Linked != nil {
			t.Errorf("Linked = %+v, want nil", in.Linked)
		}
	})
}

func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func deniedForLinked(rel string) bool { return config.ReadDenied(rel, true) }

func mustWrite(t *testing.T, dir, rel, content string) {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestScrubLinkedRemovesDeniedFiles(t *testing.T) {
	outside := t.TempDir()
	mustWrite(t, outside, "secret.txt", "s3cret\n")
	clone := t.TempDir()
	for _, rel := range []string{
		"main.go", "pkg/util.go", "prod.env.example", "docs/deep/notes.md",
		".env", "a.pem", "deep/er/est/server.key", "infra/prod.tfvars", "config/app.env.local", "config/.env",
		"AGENTS.md", "docs/CLAUDE.md", "docs/deep/Context.md", "x/agents.md",
		".git/config", ".git/hooks/AGENTS.md",
	} {
		mustWrite(t, clone, rel, "x\n")
	}
	if err := os.Symlink(filepath.Join(outside, "secret.txt"), filepath.Join(clone, "out.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(clone, "outdir")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("prod.tfvars", filepath.Join(clone, "infra", "safe.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("pkg/util.go", filepath.Join(clone, "ok-link.go")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("..", filepath.Join(clone, "pkg", "self")); err != nil {
		t.Fatal(err)
	}

	got := scrubLinked([]review.LinkedRepo{{Name: "billing", Path: clone, Watched: []string{"api/"}, CanBlock: true}}, deniedForLinked, quietLog)

	if len(got) != 1 || got[0].Error != "" || got[0].Name != "billing" || got[0].Path != clone || !got[0].CanBlock {
		t.Fatalf("scrubLinked() = %+v, want the entry back unchanged with no error", got)
	}
	for _, rel := range []string{
		".env", "a.pem", "deep/er/est/server.key", "infra/prod.tfvars", "config/app.env.local", "config/.env",
		"AGENTS.md", "docs/CLAUDE.md", "docs/deep/Context.md", "x/agents.md", "out.txt", "outdir", "infra/safe.txt", "pkg/self",
	} {
		if exists(clone, rel) {
			t.Errorf("%s is still in the clone", rel)
		}
	}
	for _, rel := range []string{"main.go", "pkg/util.go", "prod.env.example", "docs/deep/notes.md", "ok-link.go", ".git/config"} {
		if !exists(clone, rel) {
			t.Errorf("%s was deleted", rel)
		}
	}
	// Nothing outside the clone was touched.
	if data, err := os.ReadFile(filepath.Join(outside, "secret.txt")); err != nil || string(data) != "s3cret\n" {
		t.Errorf("a file outside the clone changed: %q, %v", data, err)
	}
}

func TestScrubLinkedLeavesErroredEntriesAlone(t *testing.T) {
	clone := t.TempDir()
	mustWrite(t, clone, ".env", "x\n")
	in := []review.LinkedRepo{{Name: "a", Path: clone, Error: "clone failed"}}
	got := scrubLinked(in, deniedForLinked, quietLog)
	if len(got) != 1 || got[0].Error != "clone failed" {
		t.Errorf("got %+v, want the entry and its error kept", got)
	}
	if !exists(clone, ".env") {
		t.Error("an entry with an error was scrubbed")
	}
}

func TestScrubLinkedFailureMarksTheEntry(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads every directory")
	}
	bad := t.TempDir()
	mustWrite(t, bad, "locked/inside.go", "x\n")
	mustWrite(t, bad, ".env", "x\n")
	if err := os.Chmod(filepath.Join(bad, "locked"), 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(filepath.Join(bad, "locked"), 0o755) })
	good := t.TempDir()
	mustWrite(t, good, ".env", "x\n")
	mustWrite(t, good, "main.go", "x\n")

	logger, logs := bufferLog()
	got := scrubLinked([]review.LinkedRepo{
		{Name: "bad", Path: bad, CanBlock: true},
		{Name: "good", Path: good},
	}, deniedForLinked, logger)

	if len(got) != 2 {
		t.Fatalf("got %d entries, want 2", len(got))
	}
	// The entry's text can reach the comment: it holds no name from the clone.
	// The log holds the details.
	if got[0].Error == "" || strings.Contains(got[0].Error, "locked") {
		t.Errorf("bad.Error = %q, want a fixed text without the directory's name", got[0].Error)
	}
	if !strings.Contains(logs.String(), "locked") || !strings.Contains(logs.String(), `"bad"`) {
		t.Errorf("logged %q, want the repository and the directory", logs.String())
	}
	if got[1].Error != "" {
		t.Errorf("good.Error = %q, want none", got[1].Error)
	}
	if exists(good, ".env") || !exists(good, "main.go") {
		t.Error("the other entry was not scrubbed as usual")
	}
}

func TestScrubLinkedRefusesAPathThatIsNotAPlainDirectory(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "real")
	mustWrite(t, real, ".env", "x\n")
	link := filepath.Join(dir, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "file")
	mustWrite(t, dir, "file", "x\n")
	got := scrubLinked([]review.LinkedRepo{
		{Name: "link", Path: link},
		{Name: "missing", Path: filepath.Join(dir, "missing")},
		{Name: "file", Path: file},
	}, deniedForLinked, quietLog)
	for _, r := range got {
		if r.Error == "" {
			t.Errorf("%s: want an error", r.Name)
		}
	}
	if !exists(real, ".env") {
		t.Error("a symlinked clone path was followed and scrubbed")
	}
}

func TestScrubLinkedDoesNotChangeItsInput(t *testing.T) {
	in := []review.LinkedRepo{{Name: "missing", Path: filepath.Join(t.TempDir(), "missing")}}
	got := scrubLinked(in, deniedForLinked, quietLog)
	if in[0].Error != "" {
		t.Errorf("input entry changed: %+v", in[0])
	}
	if got[0].Error == "" {
		t.Error("want an error on the returned entry")
	}
}

// impactSkill is a skills directory with one skill named like the reserved one.
func impactSkill(t *testing.T) string {
	t.Helper()
	return skillsDir(t, map[string]string{"impact.md": "---\nname: impact\n---\nReview it.\n"})
}

// The read rules are lower case and case-sensitive; a name in another case is
// still the denied file on a case-insensitive file system. Each name below is
// written once, so the test does not depend on the file system's case rules.
func TestScrubLinkedRemovesCaseVariantsOfDeniedFiles(t *testing.T) {
	clone := t.TempDir()
	for _, rel := range []string{".ENV", "KEY.PEM", "infra/prod.TFVARS", "cfg/App.Env.Local", "Agents.md", "main.go", "Prod.ENV.EXAMPLE"} {
		mustWrite(t, clone, rel, "x\n")
	}
	// A link to a denied file in another case.
	mustWrite(t, clone, "secret.PEM", "x\n")
	if err := os.Symlink("secret.PEM", filepath.Join(clone, "safe.txt")); err != nil {
		t.Fatal(err)
	}
	got := scrubLinked([]review.LinkedRepo{{Name: "a", Path: clone}}, deniedForLinked, quietLog)
	if got[0].Error != "" {
		t.Fatalf("Error = %q", got[0].Error)
	}
	for _, rel := range []string{".ENV", "KEY.PEM", "infra/prod.TFVARS", "cfg/App.Env.Local", "Agents.md", "secret.PEM", "safe.txt"} {
		if exists(clone, rel) {
			t.Errorf("%s is still in the clone", rel)
		}
	}
	// *.env.example is allowed in any case, as it is in lower case.
	for _, rel := range []string{"main.go", "Prod.ENV.EXAMPLE"} {
		if !exists(clone, rel) {
			t.Errorf("%s was deleted", rel)
		}
	}
}

// Only the clone's own .git is left alone.
func TestScrubLinkedSkipsOnlyTheRootGitDirectory(t *testing.T) {
	clone := t.TempDir()
	mustWrite(t, clone, ".git/config", "x\n")
	mustWrite(t, clone, ".git/secret.env", "x\n")
	mustWrite(t, clone, "sub/.git/prod.env", "x\n")
	mustWrite(t, clone, "sub/.git/CLAUDE.md", "x\n")
	mustWrite(t, clone, "sub/.git/keep.txt", "x\n")
	got := scrubLinked([]review.LinkedRepo{{Name: "a", Path: clone}}, deniedForLinked, quietLog)
	if got[0].Error != "" {
		t.Fatalf("Error = %q", got[0].Error)
	}
	for _, rel := range []string{"sub/.git/prod.env", "sub/.git/CLAUDE.md"} {
		if exists(clone, rel) {
			t.Errorf("%s is still in the clone", rel)
		}
	}
	for _, rel := range []string{".git/config", ".git/secret.env", "sub/.git/keep.txt"} {
		if !exists(clone, rel) {
			t.Errorf("%s was deleted", rel)
		}
	}
}

// A file name with a line break must not start a line in the log.
func TestScrubLinkedLogsNoRawPaths(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads every directory")
	}
	bad := t.TempDir()
	mustWrite(t, bad, "evil\nINJECTED log line/inside.go", "x\n")
	dir := filepath.Join(bad, "evil\nINJECTED log line")
	if err := os.Chmod(dir, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0o755) })
	logger, logs := bufferLog()
	got := scrubLinked([]review.LinkedRepo{{Name: "bad", Path: bad}}, deniedForLinked, logger)
	if got[0].Error == "" {
		t.Fatal("want the entry failed")
	}
	for _, line := range strings.Split(logs.String(), "\n") {
		if strings.HasPrefix(line, "INJECTED") {
			t.Errorf("a name from the clone started a log line: %q", logs.String())
		}
	}
}

func TestPrepareAgent_LinkedPathsThatWidenTheSandbox(t *testing.T) {
	repo, base, head := agentRepo(t, nil)
	// One parent of everything, so "a directory holding" is not the test's
	// own temp directory, which holds the checkout too.
	parent := t.TempDir()
	home := filepath.Join(parent, "home")
	mustWrite(t, parent, "home/.keep", "x\n")
	mustWrite(t, parent, "skills/review.md", "---\nname: review\n---\nReview the change.\n")
	skills := filepath.Join(parent, "skills")
	mustWrite(t, parent, "inputs/previous.json", "{}")
	previous := filepath.Join(parent, "inputs", "previous.json")

	cases := map[string]struct {
		path string
		args []string
	}{
		"the temp directory":                 {path: os.TempDir()},
		"the home directory":                 {path: home},
		"a directory holding home":           {path: parent},
		"a directory of the skills":          {path: skills},
		"a directory below the skills":       {path: filepath.Join(skills, "clone")},
		"a directory holding a trusted file": {path: filepath.Dir(previous), args: []string{"--previous", previous}},
		"a system directory":                 {path: "/etc/pergent-test-clone"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			p := writeLinkedFile(t, `[{"name":"billing","path":`+jsonString(c.path)+`},{"name":"ok","path":`+jsonString(t.TempDir())+`}]`)
			args := agentArgs(repo, base, head, skills, "--linked-repos", p)
			args = append(args, c.args...)
			in := mustParse(t, args)
			in.home = home
			logger, logs := bufferLog()
			if e := prepareAgent(&in, logger); e != nil {
				t.Fatalf("prepareAgent() error: %+v, want the review to go on with the check failed", e)
			}
			if len(in.Linked) != 2 || in.Linked[0].Error == "" || in.Linked[1].Error != "" {
				t.Fatalf("Linked = %+v, want only billing failed", in.Linked)
			}
			if !strings.Contains(logs.String(), "billing") {
				t.Errorf("logged %q, want the reason", logs.String())
			}
		})
	}

	t.Run("nothing is scrubbed in a refused path", func(t *testing.T) {
		mustWrite(t, home, "AGENTS.md", "x\n")
		mustWrite(t, home, ".env", "x\n")
		p := writeLinkedFile(t, `[{"name":"billing","path":`+jsonString(home)+`}]`)
		in := mustParse(t, agentArgs(repo, base, head, skills, "--linked-repos", p))
		in.home = home
		if e := prepareAgent(&in, quietLog); e != nil {
			t.Fatalf("prepareAgent() error: %+v", e)
		}
		if in.Linked[0].Error == "" || !exists(home, "AGENTS.md") || !exists(home, ".env") {
			t.Errorf("Linked = %+v: the refused path was scrubbed", in.Linked)
		}
	})
	t.Run("a directory below the temp directory is fine", func(t *testing.T) {
		p := writeLinkedFile(t, `[{"name":"billing","path":`+jsonString(t.TempDir())+`}]`)
		in := mustParse(t, agentArgs(repo, base, head, skills, "--linked-repos", p))
		if e := prepareAgent(&in, quietLog); e != nil || in.Linked[0].Error != "" {
			t.Errorf("prepareAgent() = %+v, Linked = %+v", e, in.Linked)
		}
	})
}
