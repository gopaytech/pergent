package diff

import (
	"path"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// fileOfTokens returns a FileDiff whose Patch estimates to exactly tokens.
func fileOfTokens(p string, tokens int) FileDiff {
	return FileDiff{Path: p, Patch: strings.Repeat("x", tokens*4)}
}

func batchPaths(batches [][]FileDiff) [][]string {
	out := make([][]string, 0, len(batches))
	for _, b := range batches {
		var ps []string
		for _, f := range b {
			ps = append(ps, f.Path)
		}
		out = append(out, ps)
	}
	return out
}

func TestBatch_AllFitInOne(t *testing.T) {
	files := []FileDiff{
		fileOfTokens("a/one.go", 10),
		fileOfTokens("b/two.go", 10),
		fileOfTokens("c/three.go", 10),
	}
	batches, oversize := Batch(files, 100)
	if len(batches) != 1 {
		t.Fatalf("got %d batches, want 1: %v", len(batches), batchPaths(batches))
	}
	if len(batches[0]) != 3 {
		t.Errorf("batch has %d files, want 3", len(batches[0]))
	}
	if len(oversize) != 0 {
		t.Errorf("oversize = %v, want none", oversize)
	}
}

func TestBatch_KeepsDirectoryTogether(t *testing.T) {
	files := []FileDiff{
		fileOfTokens("pkg/a/one.go", 30),
		fileOfTokens("pkg/b/three.go", 30),
		fileOfTokens("pkg/a/two.go", 30),
		fileOfTokens("pkg/b/four.go", 30),
	}
	batches, oversize := Batch(files, 70)
	if len(oversize) != 0 {
		t.Errorf("oversize = %v, want none", oversize)
	}
	want := [][]string{
		{"pkg/a/one.go", "pkg/a/two.go"},
		{"pkg/b/four.go", "pkg/b/three.go"},
	}
	if got := batchPaths(batches); !reflect.DeepEqual(got, want) {
		t.Errorf("batches = %v, want %v", got, want)
	}
}

func TestBatch_SplitsLargeDirectory(t *testing.T) {
	var files []FileDiff
	for _, n := range []string{"a.go", "b.go", "c.go", "d.go", "e.go"} {
		files = append(files, fileOfTokens(path.Join("big", n), 30))
	}
	batches, oversize := Batch(files, 70)
	if len(oversize) != 0 {
		t.Errorf("oversize = %v, want none", oversize)
	}
	if len(batches) < 2 {
		t.Fatalf("got %d batches, want the directory split into several", len(batches))
	}
	var seen []string
	for _, b := range batches {
		total := 0
		for _, f := range b {
			total += EstimateTokens(f.Patch)
			seen = append(seen, f.Path)
		}
		if total > 70 {
			t.Errorf("batch %v has %d tokens, over budget 70", batchPaths([][]FileDiff{b}), total)
		}
	}
	sort.Strings(seen)
	want := []string{"big/a.go", "big/b.go", "big/c.go", "big/d.go", "big/e.go"}
	if !reflect.DeepEqual(seen, want) {
		t.Errorf("files across batches = %v, want each exactly once: %v", seen, want)
	}
}

func TestBatch_Oversize(t *testing.T) {
	files := []FileDiff{
		fileOfTokens("src/small.go", 10),
		fileOfTokens("src/huge.go", 500),
		fileOfTokens("src/other.go", 10),
	}
	batches, oversize := Batch(files, 100)
	if len(oversize) != 1 || oversize[0].Path != "src/huge.go" {
		t.Fatalf("oversize = %v, want only src/huge.go", oversize)
	}
	want := [][]string{{"src/other.go", "src/small.go"}}
	if got := batchPaths(batches); !reflect.DeepEqual(got, want) {
		t.Errorf("batches = %v, want %v", got, want)
	}
}

func TestBatch_Deterministic(t *testing.T) {
	a := []FileDiff{fileOfTokens("x/b.go", 20), fileOfTokens("y/a.go", 20), fileOfTokens("x/a.go", 20)}
	b := []FileDiff{fileOfTokens("x/a.go", 20), fileOfTokens("x/b.go", 20), fileOfTokens("y/a.go", 20)}
	ba, _ := Batch(a, 50)
	bb, _ := Batch(b, 50)
	if !reflect.DeepEqual(batchPaths(ba), batchPaths(bb)) {
		t.Errorf("order-dependent output: %v vs %v", batchPaths(ba), batchPaths(bb))
	}
}

func TestBatch_Empty(t *testing.T) {
	batches, oversize := Batch(nil, 100)
	if len(batches) != 0 || len(oversize) != 0 {
		t.Errorf("got %v / %v, want nothing", batches, oversize)
	}
}

func TestBatchBudget(t *testing.T) {
	if got := BatchBudget(0); got != 40000 {
		t.Errorf("BatchBudget(0) = %d, want 40000", got)
	}
	if got := BatchBudget(100000); got != 25000 {
		t.Errorf("BatchBudget(100000) = %d, want 25000", got)
	}
}
