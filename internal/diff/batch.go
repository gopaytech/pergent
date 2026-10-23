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

// Batch groups files into batches of at most budget estimated tokens each.
// Files in the same directory are kept together when the directory fits the
// budget; a larger directory is split file by file. A single file over budget
// goes to oversize instead. Every input file lands in exactly one batch or in
// oversize, and the output does not depend on the input order.
func Batch(files []FileDiff, budget int) (batches [][]FileDiff, oversize []FileDiff) {
	sorted := append([]FileDiff(nil), files...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Path < sorted[j].Path })

	type group struct {
		dir    string
		files  []FileDiff
		tokens int
	}
	var groups []*group
	byDir := map[string]*group{}
	for _, f := range sorted {
		if EstimateTokens(f.Patch) > budget {
			oversize = append(oversize, f)
			continue
		}
		dir := path.Dir(f.Path)
		g := byDir[dir]
		if g == nil {
			g = &group{dir: dir}
			byDir[dir] = g
			groups = append(groups, g)
		}
		g.files = append(g.files, f)
		g.tokens += EstimateTokens(f.Patch)
	}
	sort.Slice(groups, func(i, j int) bool { return groups[i].dir < groups[j].dir })

	var cur []FileDiff
	curTokens := 0
	place := func(fs []FileDiff, tokens int) {
		if len(cur) > 0 && curTokens+tokens > budget {
			batches = append(batches, cur)
			cur, curTokens = nil, 0
		}
		cur = append(cur, fs...)
		curTokens += tokens
	}
	for _, g := range groups {
		if g.tokens <= budget {
			place(g.files, g.tokens)
			continue
		}
		for _, f := range g.files {
			place([]FileDiff{f}, EstimateTokens(f.Patch))
		}
	}
	if len(cur) > 0 {
		batches = append(batches, cur)
	}
	return batches, oversize
}
