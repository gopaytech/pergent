package rule

import (
	"embed"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"strings"

	"gopkg.in/yaml.v3"
)

// builtinFS holds builtin/builtin.yaml, the ordered glob to file map, and one
// .md per rule. The directory is embedded whole: a builtin/*.md pattern would
// not compile while there is no text.
//
//go:embed builtin
var builtinFS embed.FS

// builtins is the embedded list, read once.
var builtins = mustLoadBuiltin(builtinFS)

// Builtin returns the built-in rules, in the order of builtin.yaml, each with
// Source SourceBuiltIn and Name its file name without .md. The caller may
// change the slice it gets.
func Builtin() []Entry {
	return append([]Entry(nil), builtins...)
}

func mustLoadBuiltin(fsys fs.FS) []Entry {
	list, err := loadBuiltin(fsys)
	if err != nil {
		panic(err)
	}
	return list
}

// loadBuiltin reads builtin/builtin.yaml from fsys: the rule file format with
// rule_file (a .md file beside it) in place of rule, and the same checks and
// limits as Load.
func loadBuiltin(fsys fs.FS) ([]Entry, error) {
	const prefix = "builtin.yaml"
	data, err := fs.ReadFile(fsys, "builtin/builtin.yaml")
	if err != nil {
		return nil, fmt.Errorf("%s: %w", prefix, err)
	}
	if len(data) > MaxFileBytes {
		return nil, fmt.Errorf("%s: %d bytes, over the limit of %d (64 KB)", prefix, len(data), MaxFileBytes)
	}
	var doc struct {
		Rules []struct {
			Path     string `yaml:"path"`
			RuleFile string `yaml:"rule_file"`
			Off      bool   `yaml:"off"`
		} `yaml:"rules"`
	}
	dec := yaml.NewDecoder(strings.NewReader(string(data)))
	dec.KnownFields(true)
	if err := dec.Decode(&doc); err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("%s: %w", prefix, err)
	}
	if n := len(doc.Rules); n > MaxEntries {
		return nil, fmt.Errorf("%s: %d entries, over the limit of %d", prefix, n, MaxEntries)
	}
	out := make([]Entry, 0, len(doc.Rules))
	for i, r := range doc.Rules {
		e := Entry{Path: r.Path, Off: r.Off, Source: SourceBuiltIn}
		if r.RuleFile != "" {
			if path.Ext(r.RuleFile) != ".md" || path.Base(r.RuleFile) != r.RuleFile {
				return nil, fmt.Errorf("%s: entry %d: rule_file must be a .md file name", prefix, i+1)
			}
			text, err := fs.ReadFile(fsys, "builtin/"+r.RuleFile)
			if err != nil {
				return nil, fmt.Errorf("%s: entry %d: %w", prefix, i+1, err)
			}
			e.Rule, e.Name = string(text), strings.TrimSuffix(r.RuleFile, ".md")
		}
		if err := check(e, r.RuleFile != ""); err != nil {
			return nil, fmt.Errorf("%s: entry %d: %w", prefix, i+1, err)
		}
		out = append(out, e)
	}
	return out, nil
}
