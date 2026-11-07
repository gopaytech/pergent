package review

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gopaytech/pergent/internal/diff"
	"github.com/gopaytech/pergent/internal/finding"
	"github.com/gopaytech/pergent/internal/runner"
	"github.com/gopaytech/pergent/internal/skill"
)

// Log prefixes of the runs, as a hardened review writes them.
const (
	prefixCheck       = "[impact check]"
	prefixCheckRepair = "[impact check repair]"
	prefixVerify      = "[impact verify]"
)

// clonesIn returns the clone directories a message of the check lists as
// `"name" at "/path"`.
func clonesIn(msg string) []string {
	var out []string
	for _, m := range regexp.MustCompile(`" at ("(?:[^"\\]|\\.)*")`).FindAllStringSubmatch(msg, -1) {
		if p, err := strconv.Unquote(m[1]); err == nil {
			out = append(out, p)
		}
	}
	return out
}

// checked is the result of a check run that read the clones it was given.
func checked(c call, output string) runner.RunResult {
	res := completed(output)
	for _, d := range clonesIn(c.opts.Message) {
		res.Opened = append(res.Opened, filepath.Join(d, "svc", "client.go"))
	}
	return res
}

func isCheck(c call) bool       { return c.opts.LogPrefix == prefixCheck }
func isCheckVerify(c call) bool { return c.opts.LogPrefix == prefixVerify }

// makeClone writes a clone with the given files and returns its directory.
func makeClone(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for rel, content := range files {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// billingClone is a clone with a three-line caller.
func billingClone(t *testing.T) string {
	return makeClone(t, map[string]string{"svc/client.go": "line 1\nline 2\nline 3\n"})
}

// impactRequest is a hardened request with links. The budget leaves room for
// verification.
func impactRequest(t *testing.T, f *fake, diffText string, links []LinkedRepo, skills ...skill.Skill) Request {
	t.Helper()
	req := newRequest(t, f, diffText, skills...)
	req.Sandbox = Sandbox{Hardened: true}
	req.Linked = links
	req.Budget = 5 * time.Minute
	return req
}

func callerFinding(sev finding.Severity, caller *finding.Caller) finding.Finding {
	return finding.Finding{File: "render/r.go", Line: 1, Severity: sev, Title: "Renamed " + string(sev), Explanation: "e", Linked: caller}
}

func realCallerAt(line int) *finding.Caller {
	return &finding.Caller{Repo: "billing", File: "svc/client.go", Line: line}
}

// confirm answers a verification run: every id real.
func confirmAll(t *testing.T, c call) string {
	t.Helper()
	var sent struct {
		Findings []struct {
			ID string `json:"id"`
		} `json:"findings"`
	}
	if err := json.Unmarshal([]byte(c.contents[0]), &sent); err != nil {
		t.Fatalf("verification file %q: %v", c.contents[0], err)
	}
	var results []map[string]any
	for _, f := range sent.Findings {
		results = append(results, map[string]any{"id": f.ID, "real": true, "reason": "ok"})
	}
	return verifyAnswer(t, results...)
}

// impactFake answers the skills with no findings, the check with check, and
// its verification with verify (all real when nil).
func impactFake(t *testing.T, check string, verify func(call) string) *fake {
	f := &fake{}
	f.respond = func(_ int, c call) runner.RunResult {
		switch {
		case isCheck(c):
			return checked(c, check)
		case isCheckVerify(c):
			if verify == nil {
				return completed(confirmAll(t, c))
			}
			return completed(verify(c))
		}
		return completed(answer(t))
	}
	return f
}

func impactCalls(f *fake) (checks, verifies, others []call) {
	for _, c := range f.calls {
		switch {
		case isCheck(c):
			checks = append(checks, c)
		case isCheckVerify(c):
			verifies = append(verifies, c)
		default:
			others = append(others, c)
		}
	}
	return
}

func resultFor(t *testing.T, rs []SkillResult, name string) SkillResult {
	t.Helper()
	for _, r := range rs {
		if r.Skill == name {
			return r
		}
	}
	t.Fatalf("no result for %q in %+v", name, rs)
	return SkillResult{}
}

func TestNoLinkedReposIsUnchanged(t *testing.T) {
	for name, linked := range map[string][]LinkedRepo{"nil": nil, "empty": {}} {
		t.Run(name, func(t *testing.T) {
			f := impactFake(t, answer(t), nil)
			req := impactRequest(t, f, patch("render/r.go", "x"), linked)
			rs := Run(context.Background(), req)
			if len(rs) != 1 || rs[0].Skill != "s" {
				t.Fatalf("results = %+v, want the skill's only", rs)
			}
			if len(f.calls) != 1 {
				t.Errorf("runner called %d times, want 1", len(f.calls))
			}
		})
	}
}

func TestImpactRunsAfterSkills(t *testing.T) {
	f := impactFake(t, answer(t), nil)
	clone := billingClone(t)
	req := impactRequest(t, f, patch("render/r.go", "x"), []LinkedRepo{{Name: "billing", Path: clone, CanBlock: true}},
		skill.Skill{Name: "one", Body: "Review it."}, skill.Skill{Name: "two", Body: "Review it."})

	rs := Run(context.Background(), req)

	if got := []string{rs[0].Skill, rs[1].Skill, rs[2].Skill}; !slices.Equal(got, []string{"one", "two", ImpactSkill}) || len(rs) != 3 {
		t.Fatalf("results = %v, want the skills then impact", got)
	}
	if len(f.calls) != 3 || !isCheck(f.calls[2]) || isCheck(f.calls[0]) || isCheck(f.calls[1]) {
		t.Errorf("run order = %v, want the check last", prefixes(f))
	}
	r := rs[2]
	if r.Verdict != NoBlockingIssues || len(r.Reviewed) != 0 || r.Confidence != 0 {
		t.Errorf("impact = %+v, want clean, with no reviewed files and no score", r)
	}
	assertRemoved(t, allFiles(f.calls))
}

func prefixes(f *fake) []string {
	var out []string
	for _, c := range f.calls {
		out = append(out, c.opts.LogPrefix)
	}
	return out
}

func TestImpactDiffHoldsOnlyWatchedFiles(t *testing.T) {
	d := patch("a/x.go", "AAA") + patch("render/r.go", "RRR") + patch("scope/s.go", "SSS") + patch("render/.env", "ENVV") +
		patch("web/package-lock.json", "LOCK") + deletedPatch("render/gone.go")
	billing := LinkedRepo{Name: "billing", Path: billingClone(t), Watched: []string{"render/**"}, CanBlock: true}
	web := LinkedRepo{Name: "web", Path: makeClone(t, nil), Watched: []string{"scope/**"}}
	other := LinkedRepo{Name: "other", Path: makeClone(t, nil), Watched: []string{"nothing/**"}}

	var ext map[string]string
	f := &fake{}
	f.respond = func(_ int, c call) runner.RunResult {
		if isCheck(c) {
			ext = readExternal(t, c.opts.ConfigPath)
			return checked(c, answer(t))
		}
		return completed(answer(t))
	}
	Run(context.Background(), impactRequest(t, f, d, []LinkedRepo{billing, web, other}))
	checks, _, _ := impactCalls(f)
	if len(checks) != 1 {
		t.Fatalf("%d checks, want 1", len(checks))
	}
	got := checks[0].contents[0]
	for _, want := range []string{"render/r.go", "scope/s.go", "render/gone.go"} {
		if !strings.Contains(got, want) {
			t.Errorf("diff misses %s:\n%s", want, got)
		}
	}
	for _, not := range []string{"a/x.go", "render/.env", "package-lock.json"} {
		if strings.Contains(got, not) {
			t.Errorf("diff holds %s:\n%s", not, got)
		}
	}
	// The link that matched nothing is neither read nor named.
	if ext[other.Path] != "" || ext[billing.Path] != "allow" || ext[web.Path] != "allow" {
		t.Errorf("external_directory = %v, want the two matched clones only", ext)
	}
	if strings.Contains(checks[0].opts.Message, other.Path) {
		t.Errorf("message names the unmatched link: %s", checks[0].opts.Message)
	}

	// A link with no watched paths matches every change.
	f = impactFake(t, answer(t), nil)
	Run(context.Background(), impactRequest(t, f, d, []LinkedRepo{{Name: "all", Path: makeClone(t, nil)}}))
	checks, _, _ = impactCalls(f)
	if len(checks) != 1 || !strings.Contains(checks[0].contents[0], "a/x.go") || !strings.Contains(checks[0].contents[0], "render/r.go") {
		t.Errorf("a link with no watched paths: diff = %v", checks)
	}
}

// A request for the check always gets an answer. When no changed file matches
// a watched path (a later push that re-runs an open check), the whole change
// is checked against every link.
func TestImpactNoMatchUsesTheWholeChange(t *testing.T) {
	f := impactFake(t, answer(t), nil)
	link := LinkedRepo{Name: "billing", Path: billingClone(t), Watched: []string{"render/**"}}
	rs := Run(context.Background(), impactRequest(t, f, patch("a/x.go", "AAA"), []LinkedRepo{link}))
	r := resultFor(t, rs, ImpactSkill)
	checks, _, _ := impactCalls(f)
	if len(checks) != 1 || !strings.Contains(checks[0].contents[0], "a/x.go") || !strings.Contains(checks[0].opts.Message, link.Path) {
		t.Fatalf("checks = %+v, want one over the whole change and the link", checks)
	}
	if r.Verdict != NoBlockingIssues || !slices.Contains(r.Notes, "Impact: checked billing, nothing found") {
		t.Errorf("impact = %+v", r)
	}
}

func TestNoLinkedEntriesNoImpactResult(t *testing.T) {
	f := impactFake(t, answer(t), nil)
	rs := Run(context.Background(), impactRequest(t, f, patch("a/x.go", "x"), []LinkedRepo{}))
	if len(rs) != 1 || len(f.calls) != 1 {
		t.Errorf("results = %d, runs = %d, want the skill only", len(rs), len(f.calls))
	}
}

func pureRenamePatch(from, to string) string {
	return "diff --git a/" + from + " b/" + to + "\nsimilarity index 100%\nrename from " + from + "\nrename to " + to + "\n"
}

// Every kind of change reaches the check: a moved package or a regenerated
// client is what breaks callers.
func TestImpactCountsEveryKindOfChange(t *testing.T) {
	generated := "diff --git a/gen/client.go b/gen/client.go\n--- a/gen/client.go\n+++ b/gen/client.go\n@@ -1,2 +1,2 @@\n+// Code generated by a tool. DO NOT EDIT.\n+package gen\n-old\n"
	for name, c := range map[string]struct {
		diff, want string
		watched    []string
	}{
		"a lockfile only":       {diff: patch("web/package-lock.json", "LOCK"), want: "package-lock.json"},
		"a vendored file only":  {diff: patch("vendor/x/y.go", "VEND"), want: "vendor/x/y.go"},
		"a generated file only": {diff: generated, want: "gen/client.go"},
		"a rename only":         {diff: pureRenamePatch("old/pkg.go", "new/pkg.go"), want: "new/pkg.go"},
		"a rename out of a watched directory": {diff: pureRenamePatch("render/pkg.go", "elsewhere/pkg.go") + patch("a/x.go", "AAA"),
			want: "elsewhere/pkg.go", watched: []string{"render/**"}},
	} {
		t.Run(name, func(t *testing.T) {
			f := impactFake(t, answer(t), nil)
			link := LinkedRepo{Name: "billing", Path: billingClone(t), Watched: c.watched}
			r := resultFor(t, Run(context.Background(), impactRequest(t, f, c.diff, []LinkedRepo{link})), ImpactSkill)
			checks, _, _ := impactCalls(f)
			if len(checks) != 1 || !strings.Contains(checks[0].contents[0], c.want) {
				t.Fatalf("checks = %+v, want one that holds %s", checks, c.want)
			}
			if c.watched != nil && strings.Contains(checks[0].contents[0], "a/x.go") {
				t.Errorf("the watched path did not narrow the diff:\n%s", checks[0].contents[0])
			}
			if r.Verdict != NoBlockingIssues {
				t.Errorf("impact = %+v", r)
			}
		})
	}
}

// A check that has nothing to look at is failed, not closed.
func TestImpactWithNothingToShowFails(t *testing.T) {
	for name, d := range map[string]string{
		"only denied files": patch("config/prod.env", "x") + patch("keys/a.pem", "y"),
		"an empty change":   "",
	} {
		t.Run(name, func(t *testing.T) {
			f := impactFake(t, answer(t), nil)
			rs := Run(context.Background(), impactRequest(t, f, d, []LinkedRepo{{Name: "billing", Path: billingClone(t)}}))
			r := resultFor(t, rs, ImpactSkill)
			if r.Verdict != Failed || len(r.Failures) != 1 || len(r.Failures[0].Files) != 0 {
				t.Errorf("impact = %+v, want failed with one failure", r)
			}
			if checks, _, _ := impactCalls(f); len(checks) != 0 {
				t.Error("the check ran with nothing to show it")
			}
		})
	}
}

// A run that opened no clone checked nothing.
func TestImpactThatReadNoCloneIsFailed(t *testing.T) {
	clone := billingClone(t)
	for name, opened := range map[string][]string{
		"nothing":                  nil,
		"a relative path":          {"svc/client.go"},
		"a path in the repository": {"/repo/svc/client.go"},
		"a path beside the clone":  {clone + "-other/svc/client.go"},
		"a path that climbs out":   {clone + "/../elsewhere/x.go"},
	} {
		t.Run(name, func(t *testing.T) {
			f := &fake{}
			f.respond = func(_ int, c call) runner.RunResult {
				if isCheck(c) {
					res := completed(answer(t))
					res.Opened = opened
					return res
				}
				return completed(answer(t))
			}
			r := resultFor(t, Run(context.Background(), impactRequest(t, f, patch("render/r.go", "x"), []LinkedRepo{{Name: "billing", Path: clone}})), ImpactSkill)
			if r.Verdict != Failed || len(r.Failures) != 1 || !strings.Contains(r.Failures[0].Message, "billing") {
				t.Errorf("impact = %+v, want failed naming billing", r)
			}
			for _, n := range r.Notes {
				if strings.HasPrefix(n, "Impact: checked") {
					t.Errorf("a run that read nothing says it found nothing: %q", n)
				}
			}
		})
	}
	t.Run("a directory of the clone counts", func(t *testing.T) {
		f := &fake{}
		f.respond = func(_ int, c call) runner.RunResult {
			res := completed(answer(t))
			if isCheck(c) {
				res.Opened = []string{clone + "/svc/"}
			}
			return res
		}
		r := resultFor(t, Run(context.Background(), impactRequest(t, f, patch("render/r.go", "x"), []LinkedRepo{{Name: "billing", Path: clone}})), ImpactSkill)
		if r.Verdict != NoBlockingIssues {
			t.Errorf("impact = %+v, want clean", r)
		}
	})
}

// A stop of the review during verification is not a verifier's doubt.
func TestImpactStoppedDuringVerificationFails(t *testing.T) {
	link := LinkedRepo{Name: "billing", Path: billingClone(t), CanBlock: true}
	for name, res := range map[string]runner.RunResult{
		"cancelled": {Status: runner.StatusFailed, Cause: &runner.Cause{Kind: runner.CauseCancelled, StopReason: "the review was cancelled"}},
		"timed out": {Status: runner.StatusFailed, Cause: &runner.Cause{Kind: runner.CauseTimeout, LastToolTarget: "/work/CLONE-SECRET/x.go"}},
	} {
		t.Run(name, func(t *testing.T) {
			f := &fake{}
			f.respond = func(_ int, c call) runner.RunResult {
				switch {
				case isCheck(c):
					return checked(c, answer(t, blockingWithRealCaller()))
				case isCheckVerify(c):
					return res
				}
				return completed(answer(t))
			}
			r := resultFor(t, Run(context.Background(), impactRequest(t, f, patch("render/r.go", "x"), []LinkedRepo{link})), ImpactSkill)
			if r.Verdict != Failed || len(r.Failures) != 1 {
				t.Fatalf("impact = %+v, want failed", r)
			}
			if len(r.Findings) != 1 || r.Findings[0].Severity != finding.Suggestion {
				t.Errorf("findings = %+v, want the finding kept as a suggestion", r.Findings)
			}
			if strings.Contains(r.Failures[0].Message, "CLONE-SECRET") {
				t.Errorf("failure = %q, holds a clone path", r.Failures[0].Message)
			}
		})
	}
	t.Run("the review ends before verification can start", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		f := &fake{}
		f.respond = func(_ int, c call) runner.RunResult {
			if isCheck(c) {
				cancel()
				return checked(c, answer(t, blockingWithRealCaller()))
			}
			return completed(answer(t))
		}
		r := resultFor(t, Run(ctx, impactRequest(t, f, patch("render/r.go", "x"), []LinkedRepo{link})), ImpactSkill)
		if r.Verdict != Failed {
			t.Errorf("impact = %+v, want failed", r)
		}
	})
}

func TestImpactVerifierThatContradictsItselfDoesNotConfirm(t *testing.T) {
	link := LinkedRepo{Name: "billing", Path: billingClone(t), CanBlock: true}
	for name, results := range map[string][]map[string]any{
		"false then true": {{"id": "1", "real": false}, {"id": "1", "real": true}},
		"true then false": {{"id": "1", "real": true}, {"id": "1", "real": false}},
	} {
		t.Run(name, func(t *testing.T) {
			f := impactFake(t, answer(t, blockingWithRealCaller()), func(call) string { return verifyAnswer(t, results...) })
			r := resultFor(t, Run(context.Background(), impactRequest(t, f, patch("render/r.go", "x"), []LinkedRepo{link})), ImpactSkill)
			assertDowngraded(t, r, "could not confirm")
		})
	}
}

// seenRun is what the config of one run held.
type seenRun struct {
	cfg  seenConfig
	ext  map[string]string
	call call
}

func readExternal(t *testing.T, configPath string) map[string]string {
	t.Helper()
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Errorf("reading config: %v", err)
		return nil
	}
	var cfg struct {
		Permission struct {
			External map[string]string `json:"external_directory"`
		} `json:"permission"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Errorf("config: %v", err)
	}
	return cfg.Permission.External
}

func TestImpactPromptIsFixed(t *testing.T) {
	instr := filepath.Join(t.TempDir(), "i.md")
	if err := os.WriteFile(instr, []byte("INSTRUCTION-TEXT"), 0o600); err != nil {
		t.Fatal(err)
	}
	clone := billingClone(t)
	var mu sync.Mutex
	runs := map[string]seenRun{}
	f := &fake{}
	f.respond = func(_ int, c call) runner.RunResult {
		cfg := readConfig(t, c.opts.ConfigPath)
		mu.Lock()
		runs[c.opts.LogPrefix] = seenRun{cfg: cfg, ext: readExternal(t, c.opts.ConfigPath), call: c}
		mu.Unlock()
		if isCheck(c) {
			return checked(c, answer(t))
		}
		return completed(answer(t))
	}
	req := impactRequest(t, f, patch("render/r.go", "x"), []LinkedRepo{{Name: "billing", Path: clone, CanBlock: true}},
		skill.Skill{Name: "s", Body: "SKILL-BODY-TEXT quiet"})
	req.Sandbox.Instructions = []string{instr}
	req.Description = "DESCRIPTION-TEXT"

	Run(context.Background(), req)

	skillRun, ok1 := runs["[s 1/1]"]
	check, ok2 := runs[prefixCheck]
	if !ok1 || !ok2 {
		t.Fatalf("runs = %v", runs)
	}
	// The skill is as before: its text, the instructions and the description.
	if !strings.Contains(skillRun.cfg.prompt, "SKILL-BODY-TEXT") || !slices.Equal(skillRun.cfg.instructions, []string{instr}) ||
		!strings.Contains(skillRun.call.opts.Message, "change-description") {
		t.Fatalf("the skill's run changed: %+v", skillRun)
	}
	want := strings.TrimRight(impactPrompt, "\n") + "\n\n" + untrustedSection + "\n"
	if check.cfg.prompt != want {
		t.Errorf("impact prompt =\n%s\nwant the fixed prompt", check.cfg.prompt)
	}
	for _, not := range []string{"SKILL-BODY-TEXT", "INSTRUCTION-TEXT", "DESCRIPTION-TEXT", "quiet", "Quiet"} {
		everything := check.cfg.raw + check.cfg.prompt + check.call.opts.Message + strings.Join(check.call.contents, "\n")
		if strings.Contains(everything, not) {
			t.Errorf("the impact run holds %q", not)
		}
	}
	if len(check.cfg.instructions) != 0 || strings.Contains(check.cfg.raw, instr) {
		t.Errorf("instructions = %v, want none for the impact run", check.cfg.instructions)
	}
	// It reads the clone, and the places every run reads, and nothing else.
	if len(check.ext) != 5 || check.ext[clone] != "allow" || check.ext[filepath.Join(clone, "*")] != "allow" || check.ext["*"] != "deny" {
		t.Errorf("external_directory = %v, want the clone and the usual three", check.ext)
	}
	if len(skillRun.ext) != 3 {
		t.Errorf("the skill's external_directory = %v, want the usual three", skillRun.ext)
	}
	if !strings.Contains(check.call.opts.Message, clone) {
		t.Errorf("message = %q, want it to say where the clone is", check.call.opts.Message)
	}
}

func TestImpactGetsItsPreviousFindings(t *testing.T) {
	const fp = "abcdefabcdef"
	prev := []finding.Finding{{File: "render/r.go", Line: 1, Severity: finding.Suggestion, Title: "Old title", Explanation: "e", Fingerprint: fp}}
	reworded := finding.Finding{File: "render/r.go", Line: 1, Severity: finding.Suggestion, Title: "Reworded title", Explanation: "e", Fingerprint: fp}
	rejected := []finding.Rejected{{File: "render/r.go", Title: "Dismissed", Severity: "blocking", Reason: "pinned"}}

	f := impactFake(t, answer(t, reworded), nil)
	req := impactRequest(t, f, patch("render/r.go", "x"), []LinkedRepo{{Name: "billing", Path: billingClone(t), CanBlock: true}})
	req.Previous = map[string][]finding.Finding{ImpactSkill: prev, "s": {{File: "z.go", Severity: finding.Suggestion, Title: "Skill's own"}}}
	req.Dismissals = map[string][]finding.Rejected{ImpactSkill: rejected}

	rs := Run(context.Background(), req)
	checks, _, _ := impactCalls(f)
	c := checks[0]
	if len(c.contents) != 3 || !strings.HasSuffix(c.opts.Files[1], "previous-findings.json") || !strings.HasSuffix(c.opts.Files[2], rejectedFile) {
		t.Fatalf("files = %v, want the diff, the previous findings and the rejected ones", c.opts.Files)
	}
	if strings.Contains(c.contents[1], "Skill's own") || !strings.Contains(c.contents[1], "Old title") {
		t.Errorf("previous findings = %s, want impact's own only", c.contents[1])
	}
	if !strings.Contains(c.contents[2], "Dismissed") {
		t.Errorf("rejected = %s", c.contents[2])
	}
	if !strings.Contains(c.opts.Message, previousMessage) || !strings.Contains(c.opts.Message, rejectedMessage) {
		t.Errorf("message = %q, want both sentences", c.opts.Message)
	}
	got := resultFor(t, rs, ImpactSkill)
	if len(got.Findings) != 1 || got.Findings[0].Fingerprint != fp {
		t.Errorf("findings = %+v, want the reused fingerprint kept", got.Findings)
	}
	// The skill's run gets nothing of impact's.
	for _, c := range f.calls {
		if !isCheck(c) && strings.Contains(strings.Join(c.contents, ""), "Old title") {
			t.Errorf("a run besides the check got impact's previous findings")
		}
	}
}

func TestBlockingNeedsARealCaller(t *testing.T) {
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("a\nb\nc\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	clone := makeClone(t, map[string]string{
		"svc/client.go": "line 1\nline 2\nline 3\n",
		"svc/short.go":  "no newline at the end",
		".git/config":   "a\nb\nc\n",
		"empty.go":      "",
	})
	if err := os.Symlink(filepath.Join(outside, "secret.txt"), filepath.Join(clone, "svc", "out.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(clone, "dir.go"), 0o755); err != nil {
		t.Fatal(err)
	}
	link := LinkedRepo{Name: "billing", Path: clone, CanBlock: true}

	for _, c := range []struct {
		name         string
		caller       *finding.Caller
		wantBlocking bool
		wantCaller   bool
	}{
		{"a real caller", realCallerAt(3), true, true},
		{"the first line", realCallerAt(1), true, true},
		{"a last line without a newline", &finding.Caller{Repo: "billing", File: "svc/short.go", Line: 1}, true, true},
		{"no caller", nil, false, false},
		{"a line past the end", realCallerAt(4), false, false},
		{"a line past the end of a file with no newline", &finding.Caller{Repo: "billing", File: "svc/short.go", Line: 2}, false, false},
		{"an empty file", &finding.Caller{Repo: "billing", File: "empty.go", Line: 1}, false, false},
		{"a missing file", &finding.Caller{Repo: "billing", File: "svc/nope.go", Line: 1}, false, false},
		{"a directory", &finding.Caller{Repo: "billing", File: "dir.go", Line: 1}, false, false},
		{"an unknown repository", &finding.Caller{Repo: "elsewhere", File: "svc/client.go", Line: 1}, false, false},
		{"a path out of the clone", &finding.Caller{Repo: "billing", File: "../../etc/passwd", Line: 1}, false, false},
		{"a path that goes up and back", &finding.Caller{Repo: "billing", File: "svc/../svc/client.go", Line: 1}, false, false},
		{"an absolute path", &finding.Caller{Repo: "billing", File: filepath.Join(clone, "svc/client.go"), Line: 1}, false, false},
		{"a file in .git", &finding.Caller{Repo: "billing", File: ".git/config", Line: 1}, false, false},
		{"a link out of the clone", &finding.Caller{Repo: "billing", File: "svc/out.txt", Line: 1}, false, false},
		{"line zero", &finding.Caller{Repo: "billing", File: "svc/client.go", Line: 0}, false, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := impactFake(t, answer(t, callerFinding(finding.Blocking, c.caller)), nil)
			rs := Run(context.Background(), impactRequest(t, f, patch("render/r.go", "x"), []LinkedRepo{link}))
			r := resultFor(t, rs, ImpactSkill)
			if len(r.Findings) != 1 {
				t.Fatalf("findings = %+v", r.Findings)
			}
			got := r.Findings[0]
			if (got.Severity == finding.Blocking) != c.wantBlocking {
				t.Errorf("severity = %s, want blocking = %v", got.Severity, c.wantBlocking)
			}
			if (got.Linked != nil) != c.wantCaller {
				t.Errorf("Linked = %+v, want kept = %v", got.Linked, c.wantCaller)
			}
			if (r.Verdict == HasBlockingIssues) != c.wantBlocking {
				t.Errorf("verdict = %s", r.Verdict)
			}
			if !c.wantBlocking {
				// Nothing blocking is left to verify.
				if _, verifies, _ := impactCalls(f); len(verifies) != 0 {
					t.Error("a finding made a suggestion by the code was sent to verification")
				}
			}
		})
	}
}

func TestCanBlockOffMeansSuggestion(t *testing.T) {
	f := impactFake(t, answer(t, callerFinding(finding.Blocking, realCallerAt(2))), nil)
	rs := Run(context.Background(), impactRequest(t, f, patch("render/r.go", "x"), []LinkedRepo{{Name: "billing", Path: billingClone(t), CanBlock: false}}))
	r := resultFor(t, rs, ImpactSkill)
	if len(r.Findings) != 1 || r.Findings[0].Severity != finding.Suggestion || r.Findings[0].Linked == nil {
		t.Errorf("findings = %+v, want a suggestion that keeps its real caller", r.Findings)
	}
	if r.Verdict != NoBlockingIssues {
		t.Errorf("verdict = %s", r.Verdict)
	}
}

func blockingWithRealCaller() finding.Finding {
	return callerFinding(finding.Blocking, realCallerAt(2))
}

func TestVerificationConfirmsOrDowngrades(t *testing.T) {
	link := LinkedRepo{Name: "billing", Path: billingClone(t), CanBlock: true}
	t.Run("confirmed stays blocking and the verification reads the clone", func(t *testing.T) {
		instr := filepath.Join(t.TempDir(), "i.md")
		os.WriteFile(instr, []byte("x"), 0o600)
		var cfg seenConfig
		var ext map[string]string
		f := impactFake(t, answer(t, blockingWithRealCaller()), func(c call) string {
			cfg, ext = readConfig(t, c.opts.ConfigPath), readExternal(t, c.opts.ConfigPath)
			return confirmAll(t, c)
		})
		req := impactRequest(t, f, patch("render/r.go", "x"), []LinkedRepo{link})
		req.Sandbox.Instructions = []string{instr}
		rs := Run(context.Background(), req)
		r := resultFor(t, rs, ImpactSkill)
		if len(r.Findings) != 1 || r.Findings[0].Severity != finding.Blocking || r.Verdict != HasBlockingIssues {
			t.Fatalf("impact = %+v, want it blocking", r)
		}
		_, verifies, _ := impactCalls(f)
		if len(verifies) != 1 {
			t.Fatalf("%d verifications, want 1", len(verifies))
		}
		v := verifies[0]
		if ext[link.Path] != "allow" || len(cfg.instructions) != 0 || cfg.prompt != strings.TrimRight(impactVerifyPrompt, "\n")+"\n\n"+untrustedSection+"\n" {
			t.Errorf("verification config: external %v, instructions %v, prompt %q", ext, cfg.instructions, cfg.prompt)
		}
		if len(v.contents) != 2 || !strings.Contains(v.contents[1], "render/r.go") || !strings.Contains(v.opts.Message, link.Path) {
			t.Errorf("verification gets %v and %q, want the findings, the diff and where the clone is", v.opts.Files, v.opts.Message)
		}
	})
	t.Run("not real", func(t *testing.T) {
		f := impactFake(t, answer(t, blockingWithRealCaller()), func(call) string {
			return verifyAnswer(t, map[string]any{"id": "1", "real": false, "reason": "still compiles"})
		})
		r := resultFor(t, Run(context.Background(), impactRequest(t, f, patch("render/r.go", "x"), []LinkedRepo{link})), ImpactSkill)
		assertDowngraded(t, r, "could not confirm")
		// The verifier's words may quote a clone: they stay out of the comment.
		if strings.Contains(strings.Join(r.Notes, "\n"), "still compiles") {
			t.Errorf("notes = %q, want the verifier's reason left out", r.Notes)
		}
	})
}

func assertDowngraded(t *testing.T, r SkillResult, noteHas string) {
	t.Helper()
	if len(r.Findings) != 1 || r.Findings[0].Severity != finding.Suggestion || r.Verdict != NoBlockingIssues {
		t.Errorf("impact = %+v, want the finding kept as a suggestion and no blocking verdict", r)
	}
	if !strings.Contains(strings.Join(r.Notes, "\n"), noteHas) {
		t.Errorf("notes = %q, want one with %q", r.Notes, noteHas)
	}
}

func TestUnconfirmedDowngrades(t *testing.T) {
	link := LinkedRepo{Name: "billing", Path: billingClone(t), CanBlock: true}
	for name, answerText := range map[string]string{
		"the id is missing":            verifyAnswer(t),
		"another id only":              verifyAnswer(t, map[string]any{"id": "9", "real": true}),
		"real is missing":              verifyAnswer(t, map[string]any{"id": "1"}),
		"real is a string":             verifyAnswer(t, map[string]any{"id": "1", "real": "true"}),
		"an example before the answer": "For example ```json\n{\"results\": [{\"id\": \"1\", \"real\": true}]}\n``` and then ```json\n{\"results\": 5}\n```",
	} {
		t.Run(name, func(t *testing.T) {
			f := impactFake(t, answer(t, blockingWithRealCaller()), func(call) string { return answerText })
			r := resultFor(t, Run(context.Background(), impactRequest(t, f, patch("render/r.go", "x"), []LinkedRepo{link})), ImpactSkill)
			assertDowngraded(t, r, "Impact verification")
		})
	}
	t.Run("one of two is confirmed", func(t *testing.T) {
		second := blockingWithRealCaller()
		second.Title = "Second"
		second.Line = 2
		f := impactFake(t, answer(t, blockingWithRealCaller(), second), func(call) string {
			return verifyAnswer(t, map[string]any{"id": "2", "real": true})
		})
		r := resultFor(t, Run(context.Background(), impactRequest(t, f, patch("render/r.go", "x"), []LinkedRepo{link})), ImpactSkill)
		if len(r.Findings) != 2 || r.Findings[0].Severity != finding.Suggestion || r.Findings[1].Severity != finding.Blocking {
			t.Errorf("findings = %+v, want the first a suggestion and the second blocking", r.Findings)
		}
	})
}

func TestVerificationFailureDowngrades(t *testing.T) {
	link := LinkedRepo{Name: "billing", Path: billingClone(t), CanBlock: true}
	cause := &runner.Cause{Kind: runner.CauseExitError, ExitCode: 1}
	for name, res := range map[string]runner.RunResult{
		"the run failed":           {Status: runner.StatusFailed, Cause: cause},
		"the run failed, no cause": {Status: runner.StatusFailed},
		"no json block":            completed("I looked and it is fine."),
		"a block without results":  completed("```json\n{\"findings\": []}\n```"),
		"unreadable json":          completed("```json\n{\"results\": [\n```"),
	} {
		t.Run(name, func(t *testing.T) {
			f := &fake{}
			f.respond = func(_ int, c call) runner.RunResult {
				switch {
				case isCheck(c):
					return checked(c, answer(t, blockingWithRealCaller()))
				case isCheckVerify(c):
					return res
				}
				return completed(answer(t))
			}
			r := resultFor(t, Run(context.Background(), impactRequest(t, f, patch("render/r.go", "x"), []LinkedRepo{link})), ImpactSkill)
			assertDowngraded(t, r, "Impact verification")
			if len(r.Failures) != 0 {
				t.Errorf("failures = %+v, a verification failure must not fail the check", r.Failures)
			}
		})
	}
	t.Run("little budget is left", func(t *testing.T) {
		f := impactFake(t, answer(t, blockingWithRealCaller()), nil)
		req := impactRequest(t, f, patch("render/r.go", "x"), []LinkedRepo{link})
		req.Budget = time.Minute // under verifyMinRemaining
		r := resultFor(t, Run(context.Background(), req), ImpactSkill)
		// A budget that is too short is a stop of the check, not a verifier's
		// doubt: the check is failed and the blocking finding is not left standing.
		if r.Verdict != Failed || len(r.Failures) != 1 || !strings.Contains(r.Failures[0].Message, "billing") {
			t.Errorf("impact = %+v, want failed naming billing", r)
		}
		if len(r.Findings) != 1 || r.Findings[0].Severity != finding.Suggestion {
			t.Errorf("findings = %+v, want the finding kept as a suggestion", r.Findings)
		}
		if _, verifies, _ := impactCalls(f); len(verifies) != 0 {
			t.Error("verification ran with no time for it")
		}
	})
}

// A normal skill is unchanged: its verification keeps a finding on a failure.
func TestSkillVerificationStillKeepsOnFailure(t *testing.T) {
	f := &fake{}
	f.respond = func(_ int, c call) runner.RunResult {
		if isVerifyCall(c) {
			return runner.RunResult{Status: runner.StatusFailed, Cause: &runner.Cause{Kind: runner.CauseTimeout, Timeout: time.Minute}}
		}
		return completed(answer(t, finding.Finding{File: "a/x.go", Line: 1, Severity: finding.Blocking, Title: "Bug", Explanation: "e"}))
	}
	req := impactRequest(t, f, patch("a/x.go", "x"), nil)
	r := Run(context.Background(), req)[0]
	if len(r.Findings) != 1 || r.Findings[0].Severity != finding.Blocking {
		t.Errorf("findings = %+v, want the finding kept as blocking", r.Findings)
	}
}

func TestImpactFailure(t *testing.T) {
	good := func(t *testing.T) LinkedRepo {
		return LinkedRepo{Name: "billing", Path: billingClone(t), CanBlock: true}
	}
	cause := &runner.Cause{Kind: runner.CauseTimeout, Timeout: time.Minute}
	const secret = "/work/SECRET-CLONE-NAME"
	for _, c := range []struct {
		name      string
		links     func(t *testing.T) []LinkedRepo
		diffText  string
		check     runner.RunResult
		limit     int
		wantCheck bool // whether the check runs
		wantText  string
	}{
		{name: "an entry with an error",
			links:    func(t *testing.T) []LinkedRepo { return []LinkedRepo{{Name: "web", Error: "clone failed: " + secret}} },
			diffText: patch("render/r.go", "x"), wantText: "`web`"},
		{name: "an entry with no name",
			links:    func(t *testing.T) []LinkedRepo { return []LinkedRepo{{Error: "bad file " + secret}} },
			diffText: patch("a/x.go", "x"), wantText: "could not be read"},
		{name: "an error on one entry, a good one beside it",
			links:    func(t *testing.T) []LinkedRepo { return []LinkedRepo{{Name: "web", Error: secret}, good(t)} },
			diffText: patch("render/r.go", "x"), check: completed(answer(t)), wantCheck: true, wantText: "`web`"},
		{name: "a run failure",
			links:    func(t *testing.T) []LinkedRepo { return []LinkedRepo{good(t)} },
			diffText: patch("render/r.go", "x"), check: runner.RunResult{Status: runner.StatusFailed, Cause: cause}, wantCheck: true, wantText: "billing"},
		{name: "a timeout that names a clone file",
			links:    func(t *testing.T) []LinkedRepo { return []LinkedRepo{good(t)} },
			diffText: patch("render/r.go", "x"), check: runner.RunResult{Status: runner.StatusFailed, Cause: &runner.Cause{
				Kind: runner.CauseTimeout, Timeout: time.Minute, LastTool: "grep", LastToolTarget: "/work/CLONE-SECRET/x.go", ErrorMessage: "CLONE-SECRET"}},
			wantCheck: true, wantText: "billing"},
		{name: "a run failure with no cause",
			links:    func(t *testing.T) []LinkedRepo { return []LinkedRepo{good(t)} },
			diffText: patch("render/r.go", "x"), check: runner.RunResult{Status: runner.StatusFailed}, wantCheck: true, wantText: "billing"},
		{name: "an answer that is not in the format",
			links:    func(t *testing.T) []LinkedRepo { return []LinkedRepo{good(t)} },
			diffText: patch("render/r.go", "x"), check: completed("CLONE-SECRET is all good"), wantCheck: true, wantText: "billing"},
		{name: "a finding with a severity the model made up",
			links:    func(t *testing.T) []LinkedRepo { return []LinkedRepo{good(t)} },
			diffText: patch("render/r.go", "x"), check: completed("```json\n{\"findings\": [{\"file\": \"a\", \"title\": \"t\", \"severity\": \"CLONE-SECRET\"}]}\n```"), wantCheck: true, wantText: "billing"},
		{name: "a diff too big for one run",
			links:    func(t *testing.T) []LinkedRepo { return []LinkedRepo{good(t)} },
			diffText: patch("a/x.go", "x") + patch("render/r.go", strings.Repeat("r", 800)), limit: 400, wantText: "more than"},
		{name: "a matching file with no diff",
			links:    func(t *testing.T) []LinkedRepo { return []LinkedRepo{good(t)} },
			diffText: "diff --git a/render/r.go b/render/r.go\n" + diff.NotReviewedHeader + "too large\n", wantText: "`render/r.go`"},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := &fake{}
			f.respond = func(_ int, cl call) runner.RunResult {
				if isCheck(cl) || strings.HasPrefix(cl.opts.LogPrefix, prefixCheckRepair) {
					if c.check.Status == runner.StatusCompleted {
						return checked(cl, c.check.Output)
					}
					return c.check
				}
				return completed(answer(t))
			}
			// The skill reviews a small file only, so the diff that is too big is
			// the impact run's alone.
			req := impactRequest(t, f, c.diffText, c.links(t), skill.Skill{Name: "s", Body: "Review it.", Paths: []string{"a/**"}})
			req.ContextLimit = c.limit
			rs := Run(context.Background(), req)

			r := resultFor(t, rs, ImpactSkill)
			if r.Verdict != Failed {
				t.Errorf("verdict = %s, want failed", r.Verdict)
			}
			if got := resultFor(t, rs, "s"); got.Verdict == Failed {
				t.Errorf("the skill's result = %+v, want it to stand", got)
			}
			if checks, _, _ := impactCalls(f); (len(checks) > 0) != c.wantCheck {
				t.Errorf("the check ran = %v, want %v", len(checks) > 0, c.wantCheck)
			}
			var text strings.Builder
			for _, fl := range r.Failures {
				if len(fl.Files) != 0 {
					t.Errorf("failure %+v names files: a repository must not be listed as an unreviewed file", fl)
				}
				text.WriteString(fl.Message + "\n")
			}
			for _, n := range r.Notes {
				text.WriteString(n + "\n")
			}
			if !strings.Contains(text.String(), c.wantText) {
				t.Errorf("failure text = %q, want it to hold %q", text.String(), c.wantText)
			}
			if strings.Contains(text.String(), "CLONE-SECRET") || strings.Contains(text.String(), "/work/") {
				t.Errorf("clone or model text reached the failure: %q", text.String())
			}
			if strings.Contains(text.String(), "SECRET-CLONE-NAME") || strings.Contains(text.String(), "clone failed") {
				t.Errorf("the caller's own error text reached the failure: %q", text.String())
			}
			for _, n := range r.Notes {
				if strings.HasPrefix(n, "Impact: checked") {
					t.Errorf("a failed check says it found nothing: %q", n)
				}
			}
		})
	}
}

func TestImpactRepairRun(t *testing.T) {
	var repairs []call
	var repairExt map[string]string
	f := &fake{}
	f.respond = func(_ int, c call) runner.RunResult {
		switch c.opts.LogPrefix {
		case prefixCheck:
			res := checked(c, "no json here")
			return res
		case prefixCheckRepair:
			repairs = append(repairs, c)
			repairExt = readExternal(t, c.opts.ConfigPath)
			return completed(answer(t))
		}
		return completed(answer(t))
	}
	req := impactRequest(t, f, patch("render/r.go", "x"), []LinkedRepo{{Name: "billing", Path: billingClone(t)}})
	r := resultFor(t, Run(context.Background(), req), ImpactSkill)
	if len(repairs) != 1 || r.Verdict != NoBlockingIssues {
		t.Fatalf("repairs = %d, impact = %+v, want one repair and a clean result", len(repairs), r)
	}
	// The repair rewrites text: it reads no clone and gets no instructions.
	if len(repairExt) != 3 {
		t.Errorf("the repair run's external_directory = %v, want the usual three", repairExt)
	}
}

func TestImpactFindingOutsideTheChange(t *testing.T) {
	outside := finding.Finding{File: "docs/other.md", Line: 7, EndLine: 9, Suggestion: "replace", Severity: finding.Suggestion,
		Title: "Elsewhere", Explanation: "e", Linked: realCallerAt(1)}
	inside := finding.Finding{File: "render/r.go", Line: 1, EndLine: 1, Suggestion: "fixed", Severity: finding.Suggestion,
		Title: "Here", Explanation: "e", Linked: realCallerAt(1)}
	f := impactFake(t, answer(t, outside, inside), nil)
	r := resultFor(t, Run(context.Background(), impactRequest(t, f, patch("render/r.go", "x"),
		[]LinkedRepo{{Name: "billing", Path: billingClone(t), CanBlock: true}})), ImpactSkill)
	if len(r.Findings) != 2 {
		t.Fatalf("findings = %+v", r.Findings)
	}
	for _, g := range r.Findings {
		switch g.Title {
		case "Elsewhere":
			if g.Line != 0 || g.EndLine != 0 || g.Suggestion != "" || g.File != "docs/other.md" {
				t.Errorf("a finding outside the change = %+v, want line 0 and no suggestion", g)
			}
		case "Here":
			if g.Line != 1 || g.Suggestion != "" {
				t.Errorf("a finding on changed code = %+v, want its line kept and no suggestion", g)
			}
		}
	}
}

func TestImpactNameReservedOnlyWithLinks(t *testing.T) {
	named := skill.Skill{Name: ImpactSkill, Body: "Review it."}
	t.Run("with links the skill is not run and the check does not run", func(t *testing.T) {
		f := impactFake(t, answer(t), nil)
		rs := Run(context.Background(), impactRequest(t, f, patch("render/r.go", "x"), []LinkedRepo{{Name: "billing", Path: billingClone(t)}}, named))
		if len(rs) != 1 || rs[0].Verdict != Failed || len(f.calls) != 0 {
			t.Errorf("results = %+v, calls = %d, want one failed result and no run", rs, len(f.calls))
		}
	})
	t.Run("without links it is an ordinary skill", func(t *testing.T) {
		f := &fake{respond: func(int, call) runner.RunResult { return completed(answer(t)) }}
		req := impactRequest(t, f, patch("render/r.go", "x"), nil, named)
		rs := Run(context.Background(), req)
		if len(rs) != 1 || rs[0].Skill != ImpactSkill || rs[0].Verdict != NoBlockingIssues || len(f.calls) != 1 {
			t.Errorf("results = %+v, calls = %d, want the skill reviewed as any other", rs, len(f.calls))
		}
	})
}

// Only the impact run and its verification read the clones. A batch run, its
// repair, the cross-batch run and a skill's verification do not.
func TestLinkedOnlyForImpactRuns(t *testing.T) {
	clone := billingClone(t)
	var mu sync.Mutex
	reads := map[string]bool{}
	f := &fake{}
	repaired := false
	f.respond = func(_ int, c call) runner.RunResult {
		ext := readExternal(t, c.opts.ConfigPath)
		mu.Lock()
		reads[c.opts.LogPrefix] = ext[clone] == "allow"
		first := !repaired
		if strings.HasPrefix(c.opts.LogPrefix, "[s 1/2]") && first {
			repaired = true
		}
		mu.Unlock()
		switch {
		case isCheck(c):
			return checked(c, answer(t, blockingWithRealCaller()))
		case isCheckVerify(c):
			return completed(confirmAll(t, c))
		case isVerifyCall(c):
			return completed(verifyAnswer(t))
		case c.opts.LogPrefix == "[s 1/2]" && first:
			return completed("not json") // gets a repair run
		case strings.HasPrefix(c.opts.LogPrefix, "[s 1/2"), strings.HasPrefix(c.opts.LogPrefix, "[s 2/2"):
			return completed(answer(t, finding.Finding{File: "render/r.go", Line: 1, Severity: finding.Blocking, Title: "Bug", Explanation: "e"}))
		}
		return completed(answer(t))
	}
	req := impactRequest(t, f, twoBatchDiff()+patch("render/r.go", "x"), []LinkedRepo{{Name: "billing", Path: clone, CanBlock: true}},
		skill.Skill{Name: "s", Body: "Review it.", CrossBatch: true, Paths: []string{"a/**", "b/**"}})
	req.ContextLimit = twoBatchContextLimit
	Run(context.Background(), req)

	want := map[string]bool{prefixCheck: true, prefixVerify: true}
	for _, p := range []string{"[s 1/2]", "[s 2/2]", "[s 1/2 repair]", "[s cross]", "[s verify]"} {
		if _, ok := reads[p]; !ok {
			t.Errorf("no run with the prefix %s: saw %v", p, reads)
		}
		want[p] = false
	}
	for p, got := range reads {
		if got != want[p] {
			t.Errorf("run %s can read the clone = %v, want %v", p, got, want[p])
		}
	}
}

func TestImpactCleanRunNote(t *testing.T) {
	a := LinkedRepo{Name: "gopaytech/pergent.sh", Path: billingClone(t), CanBlock: true}
	b := LinkedRepo{Name: "web", Path: makeClone(t, nil), Watched: []string{"render/**"}}
	skipped := LinkedRepo{Name: "unmatched", Path: makeClone(t, nil), Watched: []string{"zzz/**"}}
	f := impactFake(t, answer(t), nil)
	r := resultFor(t, Run(context.Background(), impactRequest(t, f, patch("render/r.go", "x"), []LinkedRepo{a, b, skipped})), ImpactSkill)
	if want := "Impact: checked gopaytech/pergent.sh, web, nothing found"; !slices.Contains(r.Notes, want) {
		t.Errorf("notes = %q, want %q", r.Notes, want)
	}
	if r.Verdict != NoBlockingIssues || r.Batches != 1 || r.BatchesFinished != 1 || len(r.Reviewed) != 0 {
		t.Errorf("impact = %+v", r)
	}

	f = impactFake(t, answer(t, callerFinding(finding.Suggestion, nil)), nil)
	r = resultFor(t, Run(context.Background(), impactRequest(t, f, patch("render/r.go", "x"), []LinkedRepo{a})), ImpactSkill)
	for _, n := range r.Notes {
		if strings.HasPrefix(n, "Impact: checked") {
			t.Errorf("a result with a finding says nothing was found: %q", n)
		}
	}
}

func TestImpactNeedsAHardenedReview(t *testing.T) {
	f := impactFake(t, answer(t), nil)
	req := impactRequest(t, f, patch("render/r.go", "x"), []LinkedRepo{{Name: "billing", Path: billingClone(t)}})
	req.Sandbox = Sandbox{}
	r := resultFor(t, Run(context.Background(), req), ImpactSkill)
	if r.Verdict != Failed {
		t.Errorf("verdict = %s, want failed", r.Verdict)
	}
	if checks, _, _ := impactCalls(f); len(checks) != 0 {
		t.Error("the check ran without the sandbox")
	}
}

func TestImpactUnsplittableDiffFails(t *testing.T) {
	f := impactFake(t, answer(t), nil)
	req := impactRequest(t, f, "this is not a diff\n", []LinkedRepo{{Name: "billing", Path: billingClone(t)}})
	rs := Run(context.Background(), req)
	if len(rs) != 2 || rs[1].Skill != ImpactSkill || rs[1].Verdict != Failed {
		t.Errorf("results = %+v, want impact failed beside the skill", rs)
	}
}

// Findings an ordinary skill's model writes carry no caller.
func TestSkillFindingsCarryNoCaller(t *testing.T) {
	f := &fake{}
	f.respond = func(int, call) runner.RunResult {
		return completed("```json\n{\"findings\": [{\"file\": \"a/x.go\", \"severity\": \"suggestion\", \"title\": \"T\", \"explanation\": \"E\", \"linked\": {\"repo\": \"billing\", \"file\": \"svc/client.go\", \"line\": 1}}]}\n```")
	}
	r := Run(context.Background(), newRequest(t, f, patch("a/x.go", "x")))[0]
	if len(r.Findings) != 1 || r.Findings[0].Linked != nil || r.Verdict == Failed {
		t.Errorf("findings = %+v, want the caller dropped", r.Findings)
	}
}

func TestImpactLinkedAllowsOnlyUsableEntries(t *testing.T) {
	bad := makeClone(t, nil)
	good := billingClone(t)
	var ext map[string]string
	f := &fake{}
	f.respond = func(_ int, c call) runner.RunResult {
		if isCheck(c) {
			ext = readExternal(t, c.opts.ConfigPath)
		}
		return completed(answer(t))
	}
	Run(context.Background(), impactRequest(t, f, patch("render/r.go", "x"),
		[]LinkedRepo{{Name: "broken", Path: bad, Error: "x"}, {Name: "billing", Path: good}}))
	if ext[bad] != "" || ext[filepath.Join(bad, "*")] != "" || ext[good] != "allow" {
		t.Errorf("external_directory = %v, want only the usable clone", ext)
	}
}

// The prompt forbids a code suggestion and a fix, and the code does not rely
// on the model obeying it: a steered run must not post a one-click change.
func TestImpactFindingsKeepNoSuggestionOrFix(t *testing.T) {
	steered := finding.Finding{File: "render/r.go", Line: 1, EndLine: 2, Severity: finding.Suggestion, Title: "Steered", Explanation: "e",
		Suggestion: "rm -rf /", SuggestedFix: "run this", Linked: realCallerAt(1)}
	f := impactFake(t, answer(t, steered), nil)
	r := resultFor(t, Run(context.Background(), impactRequest(t, f, patch("render/r.go", "x"),
		[]LinkedRepo{{Name: "billing", Path: billingClone(t), CanBlock: true}})), ImpactSkill)
	if len(r.Findings) != 1 {
		t.Fatalf("findings = %+v", r.Findings)
	}
	g := r.Findings[0]
	if g.Suggestion != "" || g.SuggestedFix != "" || g.EndLine != 2 || g.Line != 1 {
		t.Errorf("finding = %+v, want no suggestion or fix, and its lines kept", g)
	}
}
