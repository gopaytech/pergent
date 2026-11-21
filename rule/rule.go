// Package rule holds pergent's path rules: checklists for a kind of file,
// added to the review that already runs on that file. It defines the rule
// file (.pergent/rules.yaml and a pergent.sh rule set share one format), the
// list agent mode takes, the resolver that gives a file its rule, the hash of
// a list and the built-in rules. A rule only adds things to look for: it never
// says what is blocking.
//
// It imports no pergent internal package, so pergent.sh checks rule sets with
// the same loader the CLI uses. Its error strings are a contract.
package rule

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/gopaytech/pergent/scope"
	"gopkg.in/yaml.v3"
)

// Limits, for team rules and built-ins alike.
const (
	// MaxRuleChars is the most characters (runes) one rule's text may have.
	MaxRuleChars = 16000
	// MaxEntries is the most entries one rule file may have, and the most
	// entries of one source in a list.
	MaxEntries = 100
	// MaxFileBytes is the largest rule file.
	MaxFileBytes = 64 << 10
	// MaxListBytes is the largest list agent mode takes.
	MaxListBytes = 1 << 20
)

// RepoFile is the repository's rule file, read from the target branch.
const RepoFile = ".pergent/rules.yaml"

// The sources of an entry. A dashboard rule set's source is
// DashboardSource(name).
const (
	SourceRepo    = "repo"
	SourceBuiltIn = "built-in"
)

const dashboardPrefix = "dashboard:"

// setName is what a pergent.sh rule set may be called, the same pattern as a
// skill name.
var setName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// Entry is one rule: a glob and either the text that files matching it are
// checked with, or Off, which ends the search for those files with no rule.
type Entry struct {
	Path string `json:"path"`
	Rule string `json:"rule,omitempty"`
	Off  bool   `json:"off,omitempty"`
	// Source is SourceRepo, DashboardSource(<set>) or SourceBuiltIn.
	Source string `json:"source"`
	// Name is a built-in's name, such as "go"; "" for a team entry.
	Name string `json:"-"`
}

// DashboardSource is the source of the entries of the pergent.sh rule set
// called set.
func DashboardSource(set string) string { return dashboardPrefix + set }

// Load reads one rule file, YAML or JSON:
//
//	rules:
//	  - path: "internal/api/**/*.go"
//	    rule: |
//	      Every handler that starts a transaction must defer tx.Rollback().
//	  - path: "**/*.pb.go"
//	    off: true
//
// Every entry gets source as its Source. Each error names the file and the
// entry, never the rule text: unknown fields, an entry with both or neither of
// rule and off: true, a path scope.CheckPattern refuses, and the limits
// (MaxFileBytes, checked before parsing, MaxEntries, MaxRuleChars). An empty
// file has no entries.
func Load(data []byte, source string) ([]Entry, error) {
	prefix := sourceName(source)
	if len(data) > MaxFileBytes {
		return nil, fmt.Errorf("%s: %d bytes, over the limit of %d (64 KB)", prefix, len(data), MaxFileBytes)
	}
	root, err := document(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", prefix, err)
	}
	if root == nil || isNull(root) {
		return nil, nil
	}
	if root.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("%s: must be a map with a rules list", prefix)
	}
	var list *yaml.Node
	seen := map[string]bool{}
	for i := 0; i+1 < len(root.Content); i += 2 {
		key := root.Content[i].Value
		if seen[key] {
			return nil, fmt.Errorf("%s: field %q is set twice", prefix, key)
		}
		seen[key] = true
		if key != "rules" {
			return nil, fmt.Errorf("%s: unknown field %q", prefix, key)
		}
		list = root.Content[i+1]
	}
	if list == nil || isNull(list) {
		return nil, nil
	}
	if list.Kind != yaml.SequenceNode {
		return nil, fmt.Errorf("%s: rules must be a list", prefix)
	}
	if n := len(list.Content); n > MaxEntries {
		return nil, fmt.Errorf("%s: %d entries, over the limit of %d", prefix, n, MaxEntries)
	}
	out := make([]Entry, 0, len(list.Content))
	for i, n := range list.Content {
		e, err := entry(n, false)
		if err != nil {
			return nil, fmt.Errorf("%s: entry %d: %w", prefix, i+1, err)
		}
		e.Source = source
		out = append(out, e)
	}
	return out, nil
}

// EncodeList writes the list agent mode takes (--rules, $PERGENT_RULES): a
// JSON array of team entries in order, each with its source. A built-in entry
// is an error: the built-ins come with pergent.
func EncodeList(entries []Entry) ([]byte, error) {
	for i, e := range entries {
		if !teamSource(e.Source) {
			return nil, fmt.Errorf("rules list: entry %d: source %q is not repo or dashboard:<set>", i+1, e.Source)
		}
	}
	if entries == nil {
		entries = []Entry{}
	}
	return json.Marshal(entries)
}

// DecodeList reads the list EncodeList writes, with the checks of Load on
// every entry; each source may have at most MaxEntries entries, and the list
// at most MaxListBytes.
func DecodeList(data []byte) ([]Entry, error) {
	const prefix = "rules list"
	if len(data) > MaxListBytes {
		return nil, fmt.Errorf("%s: %d bytes, over the limit of %d (1 MiB)", prefix, len(data), MaxListBytes)
	}
	root, err := document(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", prefix, err)
	}
	if root == nil || root.Kind != yaml.SequenceNode {
		return nil, fmt.Errorf("%s: must be a JSON list of entries", prefix)
	}
	out := make([]Entry, 0, len(root.Content))
	count := map[string]int{}
	for i, n := range root.Content {
		e, err := entry(n, true)
		if err != nil {
			return nil, fmt.Errorf("%s: entry %d: %w", prefix, i+1, err)
		}
		if !teamSource(e.Source) {
			return nil, fmt.Errorf("%s: entry %d: source %q is not repo or dashboard:<set>", prefix, i+1, e.Source)
		}
		if count[e.Source]++; count[e.Source] > MaxEntries {
			return nil, fmt.Errorf("%s: %d entries from %s, over the limit of %d", prefix, count[e.Source], e.Source, MaxEntries)
		}
		out = append(out, e)
	}
	return out, nil
}

// teamSource reports whether s is the source of a team entry: SourceRepo or
// DashboardSource of a valid set name.
func teamSource(s string) bool {
	if s == SourceRepo {
		return true
	}
	set, ok := strings.CutPrefix(s, dashboardPrefix)
	return ok && setName.MatchString(set)
}

// sourceName is how an error names where an entry came from.
func sourceName(source string) string {
	switch {
	case source == SourceRepo:
		return RepoFile
	case source == SourceBuiltIn:
		return "builtin.yaml"
	case strings.HasPrefix(source, dashboardPrefix):
		return fmt.Sprintf("rule set %q", strings.TrimPrefix(source, dashboardPrefix))
	}
	return "rules"
}

// document parses data into its root node; nil for an empty document. An
// alias is refused where a value is read, so nothing is expanded.
func document(data []byte) (*yaml.Node, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, errors.New(strings.TrimPrefix(err.Error(), "yaml: "))
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 {
		return nil, nil
	}
	return doc.Content[0], nil
}

func isNull(n *yaml.Node) bool { return n.Kind == yaml.ScalarNode && n.Tag == "!!null" }

// entry reads one entry's fields; withSource takes the source field of a
// list entry. The source is not checked here.
func entry(n *yaml.Node, withSource bool) (Entry, error) {
	if n.Kind != yaml.MappingNode {
		return Entry{}, errors.New("must be a map with path and rule or off")
	}
	var e Entry
	seen := map[string]bool{}
	var hasRule bool
	for i := 0; i+1 < len(n.Content); i += 2 {
		key, v := n.Content[i].Value, n.Content[i+1]
		if seen[key] {
			return Entry{}, fmt.Errorf("field %q is set twice", key)
		}
		seen[key] = true
		switch {
		case key == "path":
			if !isString(v) {
				return Entry{}, errors.New("path must be a string")
			}
			e.Path = v.Value
		case key == "rule":
			if !isString(v) {
				return Entry{}, errors.New("rule must be a string")
			}
			e.Rule, hasRule = v.Value, true
		case key == "off":
			if v.Kind != yaml.ScalarNode || v.Tag != "!!bool" {
				return Entry{}, errors.New("off must be true or false")
			}
			e.Off = v.Value == "true"
		case key == "source" && withSource:
			if !isString(v) {
				return Entry{}, errors.New("source must be a string")
			}
			e.Source = v.Value
		default:
			return Entry{}, fmt.Errorf("unknown field %q", key)
		}
	}
	if err := check(e, hasRule); err != nil {
		return Entry{}, err
	}
	return e, nil
}

// check applies the entry rules every source shares: a path
// scope.CheckPattern takes, exactly one of a rule and off: true, and
// MaxRuleChars. hasRule says the entry set its rule field.
func check(e Entry, hasRule bool) error {
	if err := scope.CheckPattern(e.Path); err != nil {
		return fmt.Errorf("path: %w", err)
	}
	switch {
	case e.Off && hasRule:
		return errors.New("has both rule and off: true; use one")
	case e.Off:
	case strings.TrimSpace(e.Rule) == "":
		return errors.New("needs a rule or off: true")
	}
	if n := utf8.RuneCountInString(e.Rule); n > MaxRuleChars {
		return fmt.Errorf("rule is %d characters, over the limit of %d", n, MaxRuleChars)
	}
	return nil
}

func isString(n *yaml.Node) bool { return n.Kind == yaml.ScalarNode && n.Tag == "!!str" }
