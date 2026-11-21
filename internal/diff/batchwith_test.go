package diff

import (
	"reflect"
	"testing"
)

// extraOf gives each listed path a key and a token count; other paths get
// nothing.
func extraOf(m map[string]struct {
	key    string
	tokens int
}) Extra {
	return func(p string) (string, int) {
		e := m[p]
		return e.key, e.tokens
	}
}

type ex = struct {
	key    string
	tokens int
}

func TestBatchWithNilIsBatch(t *testing.T) {
	files := []FileDiff{
		fileOfTokens("pkg/a/one.go", 30), fileOfTokens("pkg/b/three.go", 30),
		fileOfTokens("pkg/a/two.go", 30), fileOfTokens("big.go", 200), fileOfTokens("c.go", 5),
	}
	b1, o1 := Batch(files, 70)
	b2, o2 := BatchWith(files, 70, nil)
	if !reflect.DeepEqual(b1, b2) || !reflect.DeepEqual(o1, o2) {
		t.Errorf("BatchWith(nil) = %v %v, Batch = %v %v", batchPaths(b2), o2, batchPaths(b1), o1)
	}
}

func TestBatchWithCountsRuleOncePerBatch(t *testing.T) {
	files := []FileDiff{fileOfTokens("a/1.go", 100), fileOfTokens("a/2.go", 100), fileOfTokens("a/3.go", 100)}
	extra := extraOf(map[string]ex{"a/1.go": {"go", 200}, "a/2.go": {"go", 200}, "a/3.go": {"go", 200}})
	batches, oversize := BatchWith(files, 500, extra)
	if len(oversize) != 0 || len(batches) != 1 || len(batches[0]) != 3 {
		t.Errorf("got %v, oversize %v; want one batch of 3 (300 + 200 tokens)", batchPaths(batches), oversize)
	}
}

func TestBatchWithSplitsWhenRuleWouldOverflow(t *testing.T) {
	files := []FileDiff{fileOfTokens("a/x.go", 100), fileOfTokens("b/y.py", 100)}
	extra := extraOf(map[string]ex{"a/x.go": {"go", 200}, "b/y.py": {"py", 200}})
	batches, _ := BatchWith(files, 500, extra)
	want := [][]string{{"a/x.go"}, {"b/y.py"}}
	if got := batchPaths(batches); !reflect.DeepEqual(got, want) {
		t.Errorf("batches = %v, want %v (100+200+100+200 > 500)", got, want)
	}
	// Without the rules both fit in one.
	if b, _ := BatchWith(files, 500, nil); len(b) != 1 {
		t.Errorf("without rules got %d batches, want 1", len(b))
	}
}

// A directory too big with its rules is split file by file, and each file's
// rule counts in the batch it lands in.
func TestBatchWithSplitsDirectory(t *testing.T) {
	files := []FileDiff{fileOfTokens("a/1.go", 150), fileOfTokens("a/2.go", 150)}
	extra := extraOf(map[string]ex{"a/1.go": {"go", 150}, "a/2.go": {"go", 150}})
	batches, oversize := BatchWith(files, 400, extra)
	if len(oversize) != 0 || len(batches) != 2 {
		t.Errorf("got %v, oversize %v; want two batches", batchPaths(batches), oversize)
	}
}

func TestBatchWithRuleMakesFileOversize(t *testing.T) {
	files := []FileDiff{fileOfTokens("a.go", 300)}
	batches, oversize := BatchWith(files, 500, extraOf(map[string]ex{"a.go": {"go", 300}}))
	if len(batches) != 0 || len(oversize) != 1 {
		t.Errorf("got %v, oversize %v; want a.go oversize", batchPaths(batches), oversize)
	}
}

func TestBatchWithOrderIndependent(t *testing.T) {
	files := []FileDiff{fileOfTokens("b/y.py", 100), fileOfTokens("a/x.go", 100), fileOfTokens("a/z.go", 50)}
	extra := extraOf(map[string]ex{"a/x.go": {"go", 200}, "b/y.py": {"py", 200}, "a/z.go": {"go", 200}})
	b1, o1 := BatchWith(files, 500, extra)
	rev := []FileDiff{files[2], files[0], files[1]}
	b2, o2 := BatchWith(rev, 500, extra)
	if !reflect.DeepEqual(batchPaths(b1), batchPaths(b2)) || len(o1) != len(o2) {
		t.Errorf("order changed the result: %v vs %v", batchPaths(b1), batchPaths(b2))
	}
}
