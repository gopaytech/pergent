// Package graphify converts the graph.json that graphify writes into a
// pergent-graph/v1 file.
//
// It is the security gate between graphify and a review. It reads only the
// graphify fields it needs (every other field, docstrings and labels included,
// is never decoded, so it can never be copied), keeps only code nodes whose
// name is one identifier, drops every node in a file the hardened read rules
// deny, and drops every edge with a dropped end. It accepts one graphify
// output only: schema_version 1 written by graphify 0.9.112, in its default
// form (networkx "links"; a run with --no-cluster writes another shape).
package graphify

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gopaytech/pergent/graph"
	"github.com/gopaytech/pergent/internal/config"
)

// SchemaVersion and Version are the one graphify output this converter reads.
const (
	SchemaVersion = 1
	Version       = "0.9.112"
	Provider      = "graphify"
)

// maxInput is the largest graph.json read, graphify's own default read cap.
var maxInput int64 = 512 << 20

// Options changes a convert.
type Options struct {
	// Commit overrides built_at_commit when set. graphify writes
	// built_at_commit from the git repository that holds its output folder,
	// and leaves it out when there is none.
	Commit string
	// Now is the clock for built_at; nil means time.Now.
	Now func() time.Time
}

// Stats counts what a convert kept and dropped. Counts only, no names.
type Stats struct {
	// Nodes and Edges are the counts written.
	Nodes, Edges int
	// DeniedNodes are in a file the read rules deny. OtherNodes are not code,
	// not in a code file or not a known shape. BadNodes have a bad path, line
	// or name.
	DeniedNodes, OtherNodes, BadNodes int
	// DroppedEdges are the graphify links not written: an unknown relation,
	// AMBIGUOUS, a dropped end, ends of the wrong kind, a self loop or a
	// duplicate.
	DroppedEdges int
}

// codeExtensions are the files whose symbols are kept. In any other file
// (YAML, JSON, Markdown, ...) only the file node is kept: graphify names
// config keys and documents there by their text, which can hold data.
var codeExtensions = map[string]bool{}

func init() {
	for _, e := range strings.Fields(".go .py .pyi .js .jsx .mjs .cjs .ts .tsx .java .kt .kts .scala .rs .rb .php .cs .swift .c .h .cc .cpp .cxx .hpp .hh .m .mm .lua .dart .ex .exs .erl .hs .ml .jl .sh .zig .sol .vue") {
		codeExtensions[e] = true
	}
}

// text is a JSON string field read loosely: any value that is not a string
// (null, a number, an object) reads as "", so one odd node is dropped
// instead of failing the whole convert.
type text string

func (t *text) UnmarshalJSON(b []byte) error {
	var s string
	if json.Unmarshal(b, &s) == nil {
		*t = text(s)
	} else {
		*t = ""
	}
	return nil
}

type inNode struct {
	ID             text `json:"id"`
	Label          text `json:"label"`
	FileType       text `json:"file_type"`
	SourceFile     text `json:"source_file"`
	SourceLocation text `json:"source_location"`
}

type inLink struct {
	Source     text `json:"source"`
	Target     text `json:"target"`
	Src        text `json:"_src"`
	Tgt        text `json:"_tgt"`
	Relation   text `json:"relation"`
	Confidence text `json:"confidence"`
}

type inFile struct {
	Graph         map[string]json.RawMessage `json:"graph"`
	BuiltAtCommit text                       `json:"built_at_commit"`
	Nodes         []inNode                   `json:"nodes"`
	Links         *[]inLink                  `json:"links"`
	Edges         json.RawMessage            `json:"edges"`
}

// ends returns the link's direction: _src/_tgt win when both are set, as
// graphify's own reader does.
func (l inLink) ends() (string, string) {
	if l.Src != "" && l.Tgt != "" {
		return string(l.Src), string(l.Tgt)
	}
	return string(l.Source), string(l.Target)
}

var (
	commitRe   = regexp.MustCompile(`^(?:[0-9a-f]{40}|[0-9a-f]{64})$`)
	locationRe = regexp.MustCompile(`^L?([0-9]{1,9})$`)
	driveRe    = regexp.MustCompile(`^[A-Za-z]:`)
)

func fail(format string, a ...any) error {
	return fmt.Errorf("graphify: graph.json: "+format, a...)
}

// Convert reads dir/graph.json and returns it as a pergent-graph/v1 file.
// dir is the folder that holds graph.json (graphify-out), or the folder
// given to graphify extract --out, which holds graphify-out/graph.json.
func Convert(dir string, opts Options) (*graph.File, Stats, error) {
	var st Stats
	name := filepath.Join(dir, "graph.json")
	if _, err := os.Stat(name); errors.Is(err, fs.ErrNotExist) {
		if _, err := os.Stat(filepath.Join(dir, "graphify-out", "graph.json")); err == nil {
			name = filepath.Join(dir, "graphify-out", "graph.json")
		}
	}
	in, err := read(name)
	if err != nil {
		return nil, st, err
	}
	if err := checkVersion(in); err != nil {
		return nil, st, err
	}
	commit := opts.Commit
	if commit == "" {
		commit = string(in.BuiltAtCommit)
	}
	if commit == "" {
		return nil, st, fail("no commit: pass --commit")
	}
	if !commitRe.MatchString(commit) {
		return nil, st, fail("commit is not a full SHA")
	}
	now := time.Now
	if opts.Now != nil {
		now = opts.Now
	}

	links := *in.Links
	nodes, ids := keepNodes(in.Nodes, links, &st)
	edges := keepEdges(links, nodes, ids)
	st.DroppedEdges = len(links) - len(edges)

	out := &graph.File{
		Format:          graph.Format,
		Provider:        Provider,
		ProviderVersion: Version,
		Commit:          commit,
		BuiltAt:         now().UTC().Format(time.RFC3339),
		Nodes:           nodes,
		Edges:           edges,
	}
	st.Nodes, st.Edges = len(out.Nodes), len(out.Edges)
	if err := out.Validate(); err != nil {
		return nil, st, fmt.Errorf("graphify: %w", err)
	}
	return out, st, nil
}

func read(name string) (*inFile, error) {
	fh, err := os.Open(name)
	if err != nil {
		return nil, fmt.Errorf("graphify: %w", err)
	}
	defer fh.Close()
	tooBig := fail("over the %d MiB limit", maxInput>>20)
	if maxInput%(1<<20) != 0 {
		tooBig = fail("over the %d byte limit", maxInput)
	}
	if fi, err := fh.Stat(); err == nil && fi.Size() > maxInput {
		return nil, tooBig
	}
	b, err := io.ReadAll(io.LimitReader(fh, maxInput+1))
	if err != nil {
		return nil, fmt.Errorf("graphify: %w", err)
	}
	if int64(len(b)) > maxInput {
		return nil, tooBig
	}
	var in inFile
	if err := json.Unmarshal(b, &in); err != nil {
		return nil, fail("not a graphify graph")
	}
	if in.Links == nil {
		if len(in.Edges) > 0 {
			return nil, fail("this is graphify's raw output (edges, no links): run graphify extract without --no-cluster")
		}
		return nil, fail("not a graphify graph")
	}
	return &in, nil
}

func checkVersion(in *inFile) error {
	raw, ok := in.Graph["schema_version"]
	if !ok || string(raw) == "null" {
		return fail("schema_version is missing")
	}
	var schema int
	if err := json.Unmarshal(raw, &schema); err != nil {
		return fail("schema_version is not a number")
	}
	if schema != SchemaVersion {
		return fail("schema_version %d is not supported; pergent reads %d", schema, SchemaVersion)
	}
	raw, ok = in.Graph["graphify_version"]
	if !ok || string(raw) == "null" {
		return fail("graphify_version is missing")
	}
	var v string
	if err := json.Unmarshal(raw, &v); err != nil {
		return fail("graphify_version is not a string")
	}
	if v != Version {
		if len(v) > 32 {
			v = v[:32]
		}
		return fail("graphify_version %q is not supported; pergent reads %s", v, Version)
	}
	return nil
}

// sortKey orders nodes by file, line, kind and name. Two graphify nodes with
// the same key are one output node.
func sortKey(n graph.Node) string {
	return fmt.Sprintf("%s\x00%010d\x00%s\x00%s", n.File, n.Line, n.Kind, n.Name)
}

// keepNodes classifies every graphify node and returns the kept nodes,
// sorted and numbered, and a map from graphify id to output id. Nodes that
// map to the same kind, name, file and line are merged into one.
func keepNodes(in []inNode, links []inLink, st *Stats) ([]graph.Node, map[string]string) {
	shape := map[string]string{} // graphify id -> "file" for file-shaped nodes
	for _, n := range in {
		if p, line, ok := location(n); ok && isFile(string(n.Label), p, line) {
			shape[string(n.ID)] = graph.KindFile
		}
	}
	methodFrom, methodTo, typeEnd := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, l := range links {
		if l.Confidence == "AMBIGUOUS" {
			continue
		}
		s, t := l.ends()
		switch l.Relation {
		case "method":
			methodFrom[s], methodTo[t] = true, true
		case "contains":
			if shape[s] == graph.KindFile {
				typeEnd[t] = true
			}
		case "inherits", "extends", "implements":
			typeEnd[s], typeEnd[t] = true, true
		}
	}

	byKey := map[string]graph.Node{}
	gid := map[string]string{} // graphify id -> key
	for _, n := range in {
		node, why := classify(n, methodFrom[string(n.ID)] || typeEnd[string(n.ID)], methodTo[string(n.ID)])
		switch why {
		case "denied":
			st.DeniedNodes++
			continue
		case "other":
			st.OtherNodes++
			continue
		case "bad":
			st.BadNodes++
			continue
		}
		key := sortKey(node)
		if _, ok := byKey[key]; !ok {
			byKey[key] = node
		}
		if _, ok := gid[string(n.ID)]; !ok && n.ID != "" {
			gid[string(n.ID)] = key
		}
	}
	keys := make([]string, 0, len(byKey))
	for k := range byKey {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]graph.Node, len(keys))
	num := map[string]string{}
	for i, k := range keys {
		out[i] = byKey[k]
		out[i].ID = "n" + strconv.Itoa(i+1)
		num[k] = out[i].ID
	}
	ids := make(map[string]string, len(gid))
	for g, k := range gid {
		ids[g] = num[k]
	}
	return out, ids
}

// location returns a node's path and line when both are usable.
func location(n inNode) (string, int, bool) {
	p := filepath.ToSlash(string(n.SourceFile))
	if p == "" || strings.HasPrefix(p, "/") || driveRe.MatchString(p) || !graph.ValidPath(p) {
		return "", 0, false
	}
	m := locationRe.FindStringSubmatch(string(n.SourceLocation))
	if m == nil {
		return p, 0, true
	}
	line, _ := strconv.Atoi(m[1])
	return p, line, true
}

// isFile reports whether a node is graphify's node for the file itself: line
// 1, labelled by its base name, or by a path suffix when two files share a
// base name ("result/usage.go").
func isFile(label, p string, line int) bool {
	if line != 1 || label == "" {
		return false
	}
	return label == path.Base(p) || (strings.Contains(label, "/") && (label == p || strings.HasSuffix(p, "/"+label)))
}

// classify returns the node to keep, or why it is dropped: "denied",
// "other" or "bad". typeHint is set when an edge marks the node as a type,
// methodHint when a "method" edge points at it.
func classify(n inNode, typeHint, methodHint bool) (graph.Node, string) {
	if n.FileType != "code" {
		return graph.Node{}, "other"
	}
	p, line, ok := location(n)
	if !ok {
		return graph.Node{}, "bad"
	}
	if config.ReadDenied(p, true) || config.ReadDenied(strings.ToLower(p), true) {
		return graph.Node{}, "denied"
	}
	if line < 1 {
		return graph.Node{}, "bad"
	}
	label := string(n.Label)
	node := graph.Node{File: p, Line: line}
	switch {
	case isFile(label, p, line):
		node.Kind, node.Name = graph.KindFile, path.Base(p)
		return node, ""
	case !codeExtensions[strings.ToLower(path.Ext(p))]:
		return graph.Node{}, "other"
	case strings.HasSuffix(label, "()"):
		name := strings.TrimSuffix(label, "()")
		switch {
		case strings.HasPrefix(name, "."):
			node.Kind, node.Name = graph.KindMethod, name[1:]
		case methodHint:
			node.Kind, node.Name = graph.KindMethod, name
		default:
			node.Kind, node.Name = graph.KindFunction, name
		}
	case typeHint:
		node.Kind, node.Name = graph.KindType, label
	default:
		return graph.Node{}, "other"
	}
	if !graph.ValidName(node.Kind, node.Name, node.File) {
		return graph.Node{}, "bad"
	}
	return node, ""
}

// relations maps a graphify relation to a v1 edge kind. Any other relation
// is dropped.
var relations = map[string]string{
	"calls":        graph.EdgeCalls,
	"imports":      graph.EdgeImports,
	"imports_from": graph.EdgeImports,
	"contains":     graph.EdgeContains,
	"method":       graph.EdgeContains,
	"inherits":     graph.EdgeInherits,
	"extends":      graph.EdgeInherits,
	"implements":   graph.EdgeInherits,
}

// keepEdges maps graphify links to v1 edges, sorted and without duplicates.
// EXTRACTED and INFERRED links are kept; AMBIGUOUS and any other confidence
// are dropped.
func keepEdges(links []inLink, nodes []graph.Node, ids map[string]string) []graph.Edge {
	kinds := map[string]string{}
	order := map[string]int{}
	for i, n := range nodes {
		kinds[n.ID] = n.Kind
		order[n.ID] = i
	}
	seen := map[graph.Edge]bool{}
	var out []graph.Edge
	for _, l := range links {
		if l.Confidence != "EXTRACTED" && l.Confidence != "INFERRED" {
			continue
		}
		kind, ok := relations[string(l.Relation)]
		if !ok {
			continue
		}
		s, t := l.ends()
		from, to := ids[s], ids[t]
		if from == "" || to == "" || from == to {
			continue
		}
		if !graph.EdgeAllowed(kind, kinds[from], kinds[to]) {
			continue
		}
		e := graph.Edge{From: from, To: to, Kind: kind}
		if seen[e] {
			continue
		}
		seen[e] = true
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if order[a.From] != order[b.From] {
			return order[a.From] < order[b.From]
		}
		if order[a.To] != order[b.To] {
			return order[a.To] < order[b.To]
		}
		return a.Kind < b.Kind
	})
	return out
}
