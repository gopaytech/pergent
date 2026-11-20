package review

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gopaytech/pergent/graph"
	"github.com/gopaytech/pergent/internal/diff"
	"github.com/gopaytech/pergent/internal/finding"
	"github.com/gopaytech/pergent/internal/graphpack"
	"github.com/gopaytech/pergent/internal/runner"
)

const graphCommit = "0123456789abcdef0123456789abcdef01234567"

// testGraph returns a v1 graph where, for each pair in calls, a function in
// the first file (line 1) calls a function in the second (line 1).
func testGraph(t *testing.T, calls ...[2]string) *graph.File {
	t.Helper()
	g := &graph.File{Format: graph.Format, Provider: "graphify", ProviderVersion: "0.9.112", Commit: graphCommit, BuiltAt: "2026-11-19T10:00:00Z"}
	ids := map[string]string{}
	fn := func(file string) string {
		if id, ok := ids[file]; ok {
			return id
		}
		id := fmt.Sprintf("n%d", len(g.Nodes)+1)
		name := "F" + strings.NewReplacer("/", "_", ".", "_").Replace(file)
		g.Nodes = append(g.Nodes, graph.Node{ID: id, Kind: graph.KindFunction, Name: name, File: file, Line: 1})
		ids[file] = id
		return id
	}
	for _, c := range calls {
		from, to := fn(c[0]), fn(c[1])
		g.Edges = append(g.Edges, graph.Edge{From: from, To: to, Kind: graph.EdgeCalls})
	}
	if err := g.Validate(); err != nil {
		t.Fatal(err)
	}
	return g
}

func graphFileContent(c call) (string, bool) { return attached(c, graphpack.FileName) }

func TestGraphPackAttachedToBatch(t *testing.T) {
	diffText := patch("a/x.go", "x")
	g := testGraph(t, [2]string{"b/y.go", "a/x.go"})
	runOnce := func(g *graph.File) call {
		f := &fake{}
		f.respond = func(int, call) runner.RunResult { return completed(answer(t)) }
		req := newRequest(t, f, diffText)
		req.Graph = g
		Run(context.Background(), req)
		if len(f.calls) != 1 {
			t.Fatalf("runner called %d times, want 1", len(f.calls))
		}
		assertRemoved(t, allFiles(f.calls))
		return f.calls[0]
	}
	plain := runOnce(nil)
	with := runOnce(g)

	n := len(with.opts.Files)
	if n != len(plain.opts.Files)+1 || filepath.Base(with.opts.Files[n-1]) != graphpack.FileName {
		t.Fatalf("files = %v, want the plain files then %s", baseNames(with), graphpack.FileName)
	}
	if !slices.Equal(baseNames(with)[:n-1], baseNames(plain)) || !slices.Equal(with.contents[:n-1], plain.contents) {
		t.Errorf("the files before the hint differ from a run without a graph")
	}
	want := graphpack.NewIndex(g).Build([]graphpack.Change{{Path: "a/x.go", Patch: diff.Split(diffText)[0].Patch}}, false).Text
	if want == "" || with.contents[n-1] != want {
		t.Errorf("hint = %q, want %q", with.contents[n-1], want)
	}
	if !strings.Contains(want, "Fb_y_go  b/y.go:1") {
		t.Errorf("the hint does not name the caller:\n%s", want)
	}
	if with.opts.Message != plain.opts.Message+" "+graphMessage {
		t.Errorf("Message = %q, want the plain message and the graph sentence", with.opts.Message)
	}
}

// record is what a run saw, with temp paths made the same across runs.
type record struct {
	message  string
	files    []string
	contents []string
	config   string
}

var tempPathRe = regexp.MustCompile(`(?:/private)?` + regexp.QuoteMeta(strings.TrimSuffix(os.TempDir(), "/")) + `/[^"\s]*`)

func recordingFake(t *testing.T, respond func(c call) runner.RunResult) (*fake, func() []record) {
	var mu sync.Mutex
	var recs []record
	f := &fake{}
	f.respond = func(_ int, c call) runner.RunResult {
		cfg, err := os.ReadFile(c.opts.ConfigPath)
		if err != nil {
			t.Errorf("config: %v", err)
		}
		r := record{message: c.opts.Message, config: tempPathRe.ReplaceAllString(string(cfg), "<tmp>")}
		for i, p := range c.opts.Files {
			r.files = append(r.files, filepath.Base(p))
			r.contents = append(r.contents, c.contents[i])
		}
		mu.Lock()
		recs = append(recs, r)
		mu.Unlock()
		return respond(c)
	}
	return f, func() []record {
		mu.Lock()
		defer mu.Unlock()
		out := slices.Clone(recs)
		slices.SortFunc(out, func(a, b record) int {
			return strings.Compare(a.message+strings.Join(a.contents, "\x00"), b.message+strings.Join(b.contents, "\x00"))
		})
		return out
	}
}

// blockingAnswerer answers every batch with one blocking finding on its first
// file, the cross-batch run with nothing, and verification with all real.
func blockingAnswerer(t *testing.T) func(c call) runner.RunResult {
	return func(c call) runner.RunResult {
		switch {
		case isVerifyRun(c):
			return completed(confirmAll(t, c))
		case isCrossBatchCall(c):
			return completed(answer(t))
		}
		file := "d0/f.go"
		for _, s := range c.contents {
			if m := regexp.MustCompile(`(?m)^diff --git a/(\S+)`).FindStringSubmatch(s); m != nil {
				file = m[1]
				break
			}
		}
		return completed(answer(t, finding.Finding{File: file, Line: 1, Severity: finding.Blocking, Title: "Bug in " + file, Explanation: "e"}))
	}
}

func clearDurations(rs []SkillResult) []SkillResult {
	out := slices.Clone(rs)
	for i := range out {
		out[i].Duration = 0
	}
	return out
}

func TestGraphOffUnchanged(t *testing.T) {
	graphs := map[string]*graph.File{
		"nil":         nil,
		"no nodes":    testGraph(t),
		"other files": testGraph(t, [2]string{"z/a.go", "z/b.go"}),
	}
	var base []record
	var baseResults []SkillResult
	for _, name := range []string{"nil", "no nodes", "other files"} {
		t.Run(name, func(t *testing.T) {
			f, recs := recordingFake(t, blockingAnswerer(t))
			req, _ := crossBatchRequest(t, f, true)
			req.Graph = graphs[name]
			rs := clearDurations(Run(context.Background(), req))
			got := recs()
			if len(got) != 5 {
				t.Fatalf("%d runs, want 3 batches, cross-batch and verify", len(got))
			}
			if rs[0].Verdict != HasBlockingIssues {
				t.Fatalf("verdict = %s", rs[0].Verdict)
			}
			if base == nil {
				base, baseResults = got, rs
				return
			}
			if !reflect.DeepEqual(got, base) {
				for i := range got {
					if !reflect.DeepEqual(got[i], base[i]) {
						t.Errorf("run %d differs:\n%+v\nwant\n%+v", i, got[i], base[i])
					}
				}
			}
			if !reflect.DeepEqual(rs, baseResults) {
				t.Errorf("results differ:\n%+v\nwant\n%+v", rs, baseResults)
			}
		})
	}
}

func isBatchRun(c call) bool {
	return strings.HasPrefix(c.opts.Message, "Review the attached diff. It is batch") ||
		strings.HasPrefix(c.opts.Message, singleBatchMessage)
}

func TestGraphPackOnlyOnBatchRuns(t *testing.T) {
	assertNone := func(t *testing.T, calls []call, wantBatches int) {
		t.Helper()
		batches := 0
		for i, c := range calls {
			_, has := graphFileContent(c)
			hasMsg := strings.Contains(c.opts.Message, graphMessage)
			if isBatchRun(c) {
				batches++
				if !has || !hasMsg {
					t.Errorf("batch run %d has no hint (file %v, message %v)", i, has, hasMsg)
				}
				continue
			}
			if has || hasMsg {
				t.Errorf("run %d (%q) has the hint", i, c.opts.Message)
			}
		}
		if batches != wantBatches {
			t.Errorf("%d batch runs, want %d", batches, wantBatches)
		}
	}

	t.Run("cross-batch, verify and repair", func(t *testing.T) {
		f := &fake{}
		var once sync.Once
		answerer := blockingAnswerer(t)
		f.respond = func(_ int, c call) runner.RunResult {
			bad := false
			if strings.Contains(c.opts.Message, "batch 1 of 3") {
				once.Do(func() { bad = true })
			}
			if bad {
				return completed("no json here")
			}
			return answerer(c)
		}
		req, paths := crossBatchRequest(t, f, true)
		var calls [][2]string
		for _, p := range paths {
			calls = append(calls, [2]string{"z/caller.go", p})
		}
		req.Graph = testGraph(t, calls...)
		Run(context.Background(), req)
		var repair, cross, verify int
		for _, c := range f.calls {
			switch {
			case strings.HasPrefix(c.opts.Message, "The attached review is not in the required output format"):
				repair++
			case isCrossBatchCall(c):
				cross++
			case isVerifyRun(c):
				verify++
			}
		}
		if repair != 1 || cross != 1 || verify != 1 {
			t.Fatalf("repair %d, cross %d, verify %d; want 1 each", repair, cross, verify)
		}
		assertNone(t, f.calls, 3)
	})

	t.Run("references", func(t *testing.T) {
		f := &fake{}
		f.respond = func(int, call) runner.RunResult { return completed(answer(t)) }
		req := deletedReq(t, f, pureRenamePatch("a.go", "b.go"))
		req.Graph = testGraph(t, [2]string{"c.go", "a.go"})
		Run(context.Background(), req)
		if len(f.calls) != 1 {
			t.Fatalf("runner called %d times, want 1", len(f.calls))
		}
		assertNone(t, f.calls, 0)
	})

	t.Run("impact", func(t *testing.T) {
		f := impactFake(t, answer(t), nil)
		req := impactRequest(t, f, patch("render/r.go", "x"), []LinkedRepo{{Name: "billing", Path: billingClone(t), CanBlock: true}})
		req.Graph = testGraph(t, [2]string{"c.go", "render/r.go"})
		Run(context.Background(), req)
		checks, _, others := impactCalls(f)
		if len(checks) != 1 || len(others) != 1 {
			t.Fatalf("%d checks, %d others", len(checks), len(others))
		}
		assertNone(t, f.calls, 1)
	})
}

func TestGraphPackFailureKeepsTheVerdict(t *testing.T) {
	orig := writeGraphPack
	writeGraphPack = func(string, []byte, os.FileMode) error { return errors.New("disk full") }
	t.Cleanup(func() { writeGraphPack = orig })
	var mu sync.Mutex
	var warnings []string

	for _, blocking := range []bool{false, true} {
		t.Run(fmt.Sprintf("blocking=%v", blocking), func(t *testing.T) {
			mu.Lock()
			warnings = nil
			mu.Unlock()
			f := &fake{}
			f.respond = func(_ int, c call) runner.RunResult {
				if isVerifyRun(c) {
					return completed(confirmAll(t, c))
				}
				if blocking {
					return blockingAnswerer(t)(c)
				}
				return completed(answer(t))
			}
			req := newRequest(t, f, twoBatchDiff())
			req.ContextLimit = twoBatchContextLimit
			req.Budget = 5 * time.Minute
			req.Graph = testGraph(t, [2]string{"z/c.go", "a/x.go"}, [2]string{"z/c.go", "b/y.go"})
			// After newRequest, which sets its own warnf.
			origWarn := warnf
			warnf = func(format string, args ...any) {
				mu.Lock()
				defer mu.Unlock()
				warnings = append(warnings, fmt.Sprintf(format, args...))
			}
			t.Cleanup(func() { warnf = origWarn })
			r := Run(context.Background(), req)[0]
			want := NoBlockingIssues
			if blocking {
				want = HasBlockingIssues
			}
			if r.Verdict != want || len(r.Failures) != 0 {
				t.Errorf("verdict = %s, failures = %+v; want %s and none", r.Verdict, r.Failures, want)
			}
			if !slices.Equal(r.Notes, []string{graphNote}) {
				t.Errorf("Notes = %q, want one %q", r.Notes, graphNote)
			}
			for i, c := range f.calls {
				if _, ok := graphFileContent(c); ok || strings.Contains(c.opts.Message, graphMessage) {
					t.Errorf("run %d has the hint", i)
				}
			}
			mu.Lock()
			defer mu.Unlock()
			found := false
			for _, w := range warnings {
				found = found || strings.HasPrefix(w, "graph: skipped for s 1/2: ")
			}
			if !found {
				t.Errorf("warnings = %q, want graph: skipped for s 1/2: ...", warnings)
			}
		})
	}
}

func TestGraphKeepsBatching(t *testing.T) {
	patches := func(g *graph.File) []string {
		f := &fake{}
		f.respond = func(int, call) runner.RunResult { return completed(answer(t)) }
		req := newRequest(t, f, twoBatchDiff())
		req.ContextLimit = twoBatchContextLimit
		req.Graph = g
		Run(context.Background(), req)
		var out []string
		for _, c := range f.calls {
			p, _ := attached(c, "diff.patch")
			out = append(out, p)
		}
		slices.Sort(out)
		return out
	}
	without := patches(nil)
	with := patches(testGraph(t, [2]string{"z/c.go", "a/x.go"}, [2]string{"z/c.go", "b/y.go"}))
	if len(without) != 2 || !slices.Equal(with, without) {
		t.Errorf("batches with a graph %q, without %q", with, without)
	}
}

func TestGraphPackHardened(t *testing.T) {
	for _, hardened := range []bool{false, true} {
		f := &fake{}
		f.respond = func(int, call) runner.RunResult { return completed(answer(t)) }
		req := newRequest(t, f, patch("a/x.go", "x"))
		req.Sandbox.Hardened = hardened
		req.Graph = testGraph(t, [2]string{"k.pem", "a/x.go"}, [2]string{"b/y.go", "a/x.go"})
		Run(context.Background(), req)
		got, ok := graphFileContent(f.calls[0])
		if !ok {
			t.Fatalf("hardened=%v: no hint", hardened)
		}
		if strings.Contains(got, "k.pem") == hardened {
			t.Errorf("hardened=%v: hint lists k.pem = %v:\n%s", hardened, !hardened, got)
		}
	}
}

func TestGraphPackRenameUsesOldPath(t *testing.T) {
	f := &fake{}
	f.respond = func(int, call) runner.RunResult { return completed(answer(t)) }
	req := newRequest(t, f, renamePatch("a/old.go", "a/new.go"))
	req.Graph = testGraph(t, [2]string{"b/y.go", "a/old.go"})
	Run(context.Background(), req)
	got, ok := graphFileContent(f.calls[0])
	if !ok || !strings.Contains(got, "## a/old.go") || !strings.Contains(got, "Fb_y_go  b/y.go:1") {
		t.Errorf("hint = %q (attached %v), want the callers of a/old.go", got, ok)
	}
}
