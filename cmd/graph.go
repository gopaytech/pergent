package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"sort"
	"strings"

	"github.com/gopaytech/pergent/graph"
	"github.com/gopaytech/pergent/internal/graphify"
)

// converters are the providers "pergent graph convert" knows: each turns a
// provider's output folder into a pergent-graph/v1 file. commit overrides
// the commit the provider recorded.
var converters = map[string]func(dir, commit string) (*graph.File, graphify.Stats, error){
	graphify.Provider: func(dir, commit string) (*graph.File, graphify.Stats, error) {
		return graphify.Convert(dir, graphify.Options{Commit: commit})
	},
}

const graphUsage = "usage: pergent graph convert [--provider graphify] --in <dir> --out <file> [--commit <sha>]"

// graphMain runs "pergent graph <command>"; only "convert" exists. It
// returns the exit code: 0 written, 1 the convert failed, 2 usage. It reads
// no opencode setting, LLM key or platform token.
func graphMain(args []string, stderr io.Writer) int {
	if len(args) == 0 || args[0] != "convert" {
		fmt.Fprintln(stderr, graphUsage)
		return 2
	}
	fs := flag.NewFlagSet("pergent graph convert", flag.ContinueOnError)
	fs.SetOutput(stderr)
	provider := fs.String("provider", graphify.Provider, "The graph provider")
	in := fs.String("in", "", "The provider's output folder (graphify: the folder that holds graph.json, graphify-out by default)")
	out := fs.String("out", "", "The pergent-graph/v1 file to write")
	commit := fs.String("commit", "", "The full commit SHA the graph was built from (needed when the provider did not record it)")
	if err := fs.Parse(args[1:]); err != nil {
		return 2
	}
	if fs.NArg() > 0 || *in == "" || *out == "" {
		fmt.Fprintln(stderr, graphUsage)
		return 2
	}
	conv, ok := converters[*provider]
	if !ok {
		known := make([]string, 0, len(converters))
		for k := range converters {
			known = append(known, k)
		}
		sort.Strings(known)
		fmt.Fprintf(stderr, "graph: unknown provider %q; known: %s\n", *provider, strings.Join(known, ", "))
		return 2
	}
	f, st, err := conv(*in, *commit)
	if err != nil {
		fmt.Fprintf(stderr, "graph: convert failed: %v\n", err)
		return 1
	}
	b, err := f.Encode()
	if err != nil {
		fmt.Fprintf(stderr, "graph: convert failed: %v\n", err)
		return 1
	}
	if err := writeFileAtomic(*out, b); err != nil {
		fmt.Fprintf(stderr, "graph: write failed: %v\n", err)
		return 1
	}
	fmt.Fprintf(stderr, "graph: wrote %s: %d nodes, %d edges (dropped %d denied, %d other, %d bad nodes, %d edges)\n",
		*out, st.Nodes, st.Edges, st.DeniedNodes, st.OtherNodes, st.BadNodes, st.DroppedEdges)
	return 0
}

// writeFileAtomic writes b to name.tmp (mode 0600), then renames it to name,
// so a reader never sees half a file.
func writeFileAtomic(name string, b []byte) error {
	tmp := name + ".tmp"
	fh, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	_, werr := fh.Write(b)
	if err := errors.Join(werr, fh.Chmod(0o600), fh.Close()); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, name); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

// readGraph reads a pergent-graph/v1 file. "" gives nil and logs nothing. A
// file that cannot be used logs "graph: skipped: <reason>" and gives nil: the
// review then runs as if no graph was given.
func readGraph(path string, logger *log.Logger) *graph.File {
	if path == "" {
		return nil
	}
	f, err := graph.ReadFile(path)
	if err != nil {
		logger.Printf("graph: skipped: %s", strings.TrimPrefix(err.Error(), "graph: "))
		return nil
	}
	short := f.Commit
	if len(short) > 12 {
		short = short[:12]
	}
	logger.Printf("graph: commit %s, %d nodes, %d edges", short, len(f.Nodes), len(f.Edges))
	return f
}
