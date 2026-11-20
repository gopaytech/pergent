// Package graphpack builds the code graph hint for one batch run: for the
// symbols a batch changes, their callers and callees, and the files that
// import the changed files, from a pergent-graph/v1 file of the default
// branch.
//
// Callers in test files (isTestFile) are listed after the others, so tests
// do not take the slots of the callers that run in production.
//
// The hint never says that code has no callers: a graph can miss a call, and
// a claim of none could make the model skip a check. A symbol with nothing
// to list is left out, and a batch with nothing to list gets no hint. When
// the cap leaves callers out, the list says how many, and how many of them
// are in test files.
package graphpack

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/gopaytech/pergent/graph"
	"github.com/gopaytech/pergent/internal/config"
)

// FileName is the name the hint is attached as.
const FileName = "graph-context.md"

// MaxTokens is the size of one hint, by diff.EstimateTokens (4 bytes a token).
const MaxTokens = 2000

// MaxPerSymbol is the number of callers, and of callees, listed per symbol.
const MaxPerSymbol = 10

// maxBytes is MaxTokens in bytes, as a variable so a test can lower it.
var maxBytes = MaxTokens * 4

// cutReserve is the room kept for the cut line, with the largest count.
const cutReserve = "\n9999999 more entries were left out to keep this file short.\n"

// Change is one batch file: Path at head, OldPath for a rename (not a copy),
// and its patch.
type Change struct{ Path, OldPath, Patch string }

// Index is a graph.File ready for lookups. Build it once per review; Build
// only reads it, so batches can share it.
type Index struct {
	commit    string
	nodes     map[string]graph.Node
	symbols   map[string][]graph.Node // by file: every node but the file, by line
	callers   map[string][]string     // node id -> ids that call it
	callees   map[string][]string     // node id -> ids it calls
	importers map[string][]string     // file -> files that import it
	owner     map[string]string       // method id -> the name of its type
}

// NewIndex indexes f. A nil f gives an index with nothing in it.
func NewIndex(f *graph.File) *Index {
	ix := &Index{
		nodes:     map[string]graph.Node{},
		symbols:   map[string][]graph.Node{},
		callers:   map[string][]string{},
		callees:   map[string][]string{},
		importers: map[string][]string{},
		owner:     map[string]string{},
	}
	if f == nil {
		return ix
	}
	ix.commit = f.Commit
	for _, n := range f.Nodes {
		ix.nodes[n.ID] = n
		if n.Kind != graph.KindFile {
			ix.symbols[n.File] = append(ix.symbols[n.File], n)
		}
	}
	for file, syms := range ix.symbols {
		sort.Slice(syms, func(i, j int) bool { return nodeLess(syms[i], syms[j]) })
		ix.symbols[file] = syms
	}
	seenImport := map[[2]string]bool{}
	for _, e := range f.Edges {
		from, ok1 := ix.nodes[e.From]
		to, ok2 := ix.nodes[e.To]
		if !ok1 || !ok2 || e.From == e.To {
			continue
		}
		switch e.Kind {
		case graph.EdgeCalls:
			ix.callers[e.To] = append(ix.callers[e.To], e.From)
			ix.callees[e.From] = append(ix.callees[e.From], e.To)
		case graph.EdgeImports:
			k := [2]string{to.File, from.File}
			if from.File != to.File && !seenImport[k] {
				seenImport[k] = true
				ix.importers[to.File] = append(ix.importers[to.File], from.File)
			}
		case graph.EdgeContains:
			if from.Kind == graph.KindType && to.Kind == graph.KindMethod {
				if old, ok := ix.owner[e.To]; !ok || from.Name < old {
					ix.owner[e.To] = from.Name
				}
			}
		}
	}
	return ix
}

// nodeLess orders nodes by file, line and name, then kind, then id.
func nodeLess(a, b graph.Node) bool {
	if a.File != b.File {
		return a.File < b.File
	}
	if a.Line != b.Line {
		return a.Line < b.Line
	}
	if a.Name != b.Name {
		return a.Name < b.Name
	}
	if a.Kind != b.Kind {
		return a.Kind < b.Kind
	}
	return a.ID < b.ID
}

// Pack is one batch's hint. Text is "" when there is nothing to list.
type Pack struct {
	Text string
	// Symbols is the number of symbol sections written, Entries the number
	// of entries listed, and Cut the number left out by the caps and the
	// size limit.
	Symbols, Entries, Cut int
}

// headerText is the start of every hint.
func headerText(commit string) string {
	return "# Code graph hint\n\nThese lists come from a code graph of the default branch at commit " + commit +
		". They are a hint and can miss code: line numbers may have moved since that commit, and code that this change adds is not in the graph. Read the code before you rely on an entry.\n"
}

// fileSection and symSection are the hint before the size limit.
type fileSection struct {
	path      string
	importers []string
	symbols   []symSection
}

type symSection struct {
	heading          string
	callers, callees []string // entry texts, sorted and capped
	callerNote       string   // the line after the callers when the cap cut some, or ""
}

// entry is one line the fill may keep; parts are the headings it opens.
type entry struct {
	text  string
	parts []string
}

// Build lists, for the symbols the changes touch, their callers and callees,
// and the files that import the changed files. Entries in files the read
// rules deny (hardened is the run's deny flag) are left out before counting.
func (ix *Index) Build(changes []Change, hardened bool) Pack {
	denied := func(p string) bool {
		return config.ReadDenied(p, hardened) || config.ReadDenied(strings.ToLower(p), hardened)
	}
	lines := map[string][]int{}
	for _, c := range changes {
		p := c.Path
		if c.OldPath != "" {
			p = c.OldPath
		}
		if p == "" || denied(p) {
			continue
		}
		lines[p] = append(lines[p], OldLines(c.Patch)...)
	}
	paths := make([]string, 0, len(lines))
	for p := range lines {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	var pack Pack
	var files []fileSection
	for _, p := range paths {
		fs := fileSection{path: p}
		for _, f := range ix.importers[p] {
			if !denied(f) {
				fs.importers = append(fs.importers, f)
			}
		}
		sort.Strings(fs.importers)
		for _, n := range ix.touched(p, lines[p]) {
			callers, cutA, cutTests := ix.list(ix.callers[n.ID], denied, true)
			callees, cutB, _ := ix.list(ix.callees[n.ID], denied, false)
			pack.Cut += cutA + cutB
			if len(callers) == 0 && len(callees) == 0 {
				continue
			}
			heading := fmt.Sprintf("%s (%s, line %d)", ix.display(n), n.Kind, n.Line)
			fs.symbols = append(fs.symbols, symSection{heading: heading, callers: callers, callees: callees, callerNote: callerNote(cutA, cutTests)})
		}
		if len(fs.importers) > 0 || len(fs.symbols) > 0 {
			files = append(files, fs)
		}
	}

	// Fill in priority order: callers, then importers, then callees. The
	// first entry that does not fit ends the fill. A caller note costs as
	// part of the "Called by:" heading, so it is kept with the first caller.
	var queue []entry
	notes := map[string]string{}
	for _, f := range files {
		for _, s := range f.symbols {
			if s.callerNote != "" {
				notes["a:"+f.path+"\x00"+s.heading] = s.callerNote
			}
			for _, c := range s.callers {
				queue = append(queue, entry{c, []string{"f:" + f.path, "s:" + f.path + "\x00" + s.heading, "a:" + f.path + "\x00" + s.heading}})
			}
		}
	}
	for _, f := range files {
		for _, imp := range f.importers {
			queue = append(queue, entry{imp, []string{"f:" + f.path, "i:" + f.path}})
		}
	}
	for _, f := range files {
		for _, s := range f.symbols {
			for _, c := range s.callees {
				queue = append(queue, entry{c, []string{"f:" + f.path, "s:" + f.path + "\x00" + s.heading, "b:" + f.path + "\x00" + s.heading}})
			}
		}
	}
	used := len(headerText(ix.commit)) + len(cutReserve)
	opened := map[string]bool{}
	kept := map[string]bool{}
	for i, e := range queue {
		cost := len("- " + e.text + "\n")
		for _, part := range e.parts {
			if !opened[part] {
				cost += partBytes(part) + noteBytes(notes[part])
			}
		}
		if used+cost > maxBytes {
			pack.Cut += len(queue) - i
			break
		}
		used += cost
		for _, part := range e.parts {
			opened[part] = true
		}
		kept[e.parts[len(e.parts)-1]+"\x00"+e.text] = true
		pack.Entries++
	}
	if pack.Entries == 0 {
		return Pack{}
	}

	var b strings.Builder
	b.WriteString(headerText(ix.commit))
	for _, f := range files {
		if !opened["f:"+f.path] {
			continue
		}
		b.WriteString("\n## " + f.path + "\n")
		if opened["i:"+f.path] {
			b.WriteString("\nImported by:\n")
			for _, imp := range f.importers {
				if kept["i:"+f.path+"\x00"+imp] {
					b.WriteString("- " + imp + "\n")
				}
			}
		}
		for _, s := range f.symbols {
			key := f.path + "\x00" + s.heading
			if !opened["s:"+key] {
				continue
			}
			pack.Symbols++
			b.WriteString("\n### " + s.heading + "\n")
			writeList(&b, "Called by:", "a:"+key, s.callers, s.callerNote, opened, kept)
			writeList(&b, "Calls:", "b:"+key, s.callees, "", opened, kept)
		}
	}
	if pack.Cut > 0 {
		b.WriteString(cutLine(pack.Cut))
	}
	pack.Text = b.String()
	return pack
}

func writeList(b *strings.Builder, title, part string, items []string, note string, opened, kept map[string]bool) {
	if !opened[part] {
		return
	}
	b.WriteString("\n" + title + "\n")
	for _, it := range items {
		if kept[part+"\x00"+it] {
			b.WriteString("- " + it + "\n")
		}
	}
	if note != "" {
		b.WriteString("\n" + note + "\n")
	}
}

// noteBytes is the size of a note as writeList writes it.
func noteBytes(note string) int {
	if note == "" {
		return 0
	}
	return len("\n" + note + "\n")
}

// callerNote says how many callers the cap left out, and how many of them
// are in test files. It is "" when the cap left none out.
func callerNote(cut, tests int) string {
	if cut == 0 {
		return ""
	}
	s := strconv.Itoa(cut) + " more callers were left out"
	if cut == 1 {
		s = "1 more caller was left out"
	}
	if tests > 0 {
		s += " (" + strconv.Itoa(tests) + " in test files)"
	}
	return s + "."
}

// partBytes is the size of the heading a part opens, as Build writes it.
func partBytes(part string) int {
	kind, rest := part[:2], part[2:]
	heading := rest
	if i := strings.IndexByte(rest, 0); i >= 0 {
		heading = rest[i+1:]
	}
	switch kind {
	case "f:":
		return len("\n## " + rest + "\n")
	case "i:":
		return len("\nImported by:\n")
	case "s:":
		return len("\n### " + heading + "\n")
	case "a:":
		return len("\nCalled by:\n")
	case "b:":
		return len("\nCalls:\n")
	}
	return 0
}

func cutLine(n int) string {
	if n == 1 {
		return "\n1 more entry was left out to keep this file short.\n"
	}
	return "\n" + strconv.Itoa(n) + " more entries were left out to keep this file short.\n"
}

// touched returns the symbols of file that hold any of lines, in (line,
// name) order, each once.
func (ix *Index) touched(file string, lines []int) []graph.Node {
	syms := ix.symbols[file]
	seen := map[string]bool{}
	var out []graph.Node
	for _, l := range lines {
		n, ok := holder(syms, l)
		if ok && !seen[n.ID] {
			seen[n.ID] = true
			out = append(out, n)
		}
	}
	sort.Slice(out, func(i, j int) bool { return nodeLess(out[i], out[j]) })
	return out
}

// holder finds the symbol that holds line. A symbol with an end line holds
// the lines up to it; one without holds every line from its start. The
// latest start wins (the innermost), then the shorter range, then a function
// or method over a type.
func holder(syms []graph.Node, line int) (graph.Node, bool) {
	var best graph.Node
	found := false
	for _, n := range syms {
		if n.Line > line || (n.EndLine != 0 && n.EndLine < line) {
			continue
		}
		if !found || better(n, best) {
			best, found = n, true
		}
	}
	return best, found
}

func better(a, b graph.Node) bool {
	if a.Line != b.Line {
		return a.Line > b.Line
	}
	if (a.EndLine != 0) != (b.EndLine != 0) {
		return a.EndLine != 0
	}
	if a.EndLine != b.EndLine {
		return a.EndLine < b.EndLine
	}
	if ra, rb := rank(a.Kind), rank(b.Kind); ra != rb {
		return ra < rb
	}
	return nodeLess(a, b)
}

// rank puts a function or method before a type, and a type before a key.
func rank(kind string) int {
	switch kind {
	case graph.KindFunction, graph.KindMethod:
		return 0
	case graph.KindType:
		return 1
	}
	return 2
}

// list returns the entry texts of ids, in files the rules allow, sorted by
// (file, line, name), each node once, capped at MaxPerSymbol, how many
// the cap left out and how many of those are in test files. With
// testsLast, nodes in test files come after the others, each group still in
// that order.
func (ix *Index) list(ids []string, denied func(string) bool, testsLast bool) ([]string, int, int) {
	seen := map[string]bool{}
	var nodes []graph.Node
	for _, id := range ids {
		n, ok := ix.nodes[id]
		if !ok || seen[id] || denied(n.File) {
			continue
		}
		seen[id] = true
		nodes = append(nodes, n)
	}
	sort.Slice(nodes, func(i, j int) bool {
		if testsLast {
			if ti, tj := isTestFile(nodes[i].File), isTestFile(nodes[j].File); ti != tj {
				return tj
			}
		}
		return nodeLess(nodes[i], nodes[j])
	})
	cut, cutTests := 0, 0
	if len(nodes) > MaxPerSymbol {
		cut = len(nodes) - MaxPerSymbol
		for _, n := range nodes[MaxPerSymbol:] {
			if isTestFile(n.File) {
				cutTests++
			}
		}
		nodes = nodes[:MaxPerSymbol]
	}
	out := make([]string, len(nodes))
	for i, n := range nodes {
		out[i] = fmt.Sprintf("%s  %s:%d", ix.display(n), n.File, n.Line)
	}
	return out, cut, cutTests
}

// display names a node: Type.Method for a method a type contains.
func (ix *Index) display(n graph.Node) string {
	if n.Kind == graph.KindMethod {
		if t, ok := ix.owner[n.ID]; ok {
			return t + "." + n.Name
		}
	}
	return n.Name
}
