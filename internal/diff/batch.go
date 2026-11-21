package diff

import (
	"path"
	"sort"
)

const defaultBatchBudget = 40000

// BatchBudget returns the per-batch token budget for a model with the given
// context limit: a quarter of the limit, or a default when it is unknown.
func BatchBudget(contextLimit int) int {
	if contextLimit > 0 {
		return contextLimit / 4
	}
	return defaultBatchBudget
}

// Extra is text a run gets for some of its files besides their diffs, such as
// a path rule: for a path, a key naming the text ("" for none) and its tokens.
// Files with the same key share one copy of the text in a batch.
type Extra func(path string) (key string, tokens int)

// Batch groups files into batches of at most budget estimated tokens each.
// Files in the same directory are kept together when the directory fits the
// budget; a larger directory is split file by file. A single file over budget
// goes to oversize instead. Every input file lands in exactly one batch or in
// oversize, and the output does not depend on the input order.
func Batch(files []FileDiff, budget int) (batches [][]FileDiff, oversize []FileDiff) {
	return BatchWith(files, budget, nil)
}

// BatchWith is Batch where each batch also holds the extra text of its files:
// each distinct key counts its tokens once per batch, and a file whose diff
// plus its own extra is over budget goes to oversize. A nil extra is Batch.
func BatchWith(files []FileDiff, budget int, extra Extra) (batches [][]FileDiff, oversize []FileDiff) {
	sorted := append([]FileDiff(nil), files...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Path < sorted[j].Path })

	keyOf := func(f FileDiff) (string, int) {
		if extra == nil {
			return "", 0
		}
		return extra(f.Path)
	}
	type group struct {
		dir    string
		files  []FileDiff
		tokens int
		keys   map[string]int
	}
	var groups []*group
	byDir := map[string]*group{}
	for _, f := range sorted {
		key, extraTokens := keyOf(f)
		if EstimateTokens(f.Patch)+extraTokens > budget {
			oversize = append(oversize, f)
			continue
		}
		dir := path.Dir(f.Path)
		g := byDir[dir]
		if g == nil {
			g = &group{dir: dir, keys: map[string]int{}}
			byDir[dir] = g
			groups = append(groups, g)
		}
		g.files = append(g.files, f)
		g.tokens += EstimateTokens(f.Patch)
		if key != "" {
			g.keys[key] = extraTokens
		}
	}
	sort.Slice(groups, func(i, j int) bool { return groups[i].dir < groups[j].dir })

	var cur []FileDiff
	curTokens := 0
	curKeys := map[string]bool{}
	// cost is what fs adds to the current batch: its diffs and the extra text
	// the batch does not hold yet.
	cost := func(patch int, keys map[string]int) int {
		n := patch
		for k, t := range keys {
			if !curKeys[k] {
				n += t
			}
		}
		return n
	}
	place := func(fs []FileDiff, patch int, keys map[string]int) {
		add := cost(patch, keys)
		if len(cur) > 0 && curTokens+add > budget {
			batches = append(batches, cur)
			cur, curTokens, curKeys = nil, 0, map[string]bool{}
			add = cost(patch, keys)
		}
		cur = append(cur, fs...)
		curTokens += add
		for k := range keys {
			curKeys[k] = true
		}
	}
	for _, g := range groups {
		total := g.tokens
		for _, t := range g.keys {
			total += t
		}
		if total <= budget {
			place(g.files, g.tokens, g.keys)
			continue
		}
		for _, f := range g.files {
			keys := map[string]int{}
			if key, t := keyOf(f); key != "" {
				keys[key] = t
			}
			place([]FileDiff{f}, EstimateTokens(f.Patch), keys)
		}
	}
	if len(cur) > 0 {
		batches = append(batches, cur)
	}
	return batches, oversize
}
