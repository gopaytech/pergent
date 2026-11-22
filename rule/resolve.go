package rule

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"

	"github.com/gopaytech/pergent/scope"
)

// Label names an entry in a result: "repo:<glob>", "dashboard:<set>:<glob>"
// or "built-in:<name>".
func (e Entry) Label() string {
	if e.Source == SourceBuiltIn {
		return SourceBuiltIn + ":" + e.Name
	}
	return e.Source + ":" + e.Path
}

// Heading names an entry in file-rules.md: "repo · <glob>",
// "dashboard:<set> · <glob>" or "built-in:<name>".
func (e Entry) Heading() string {
	if e.Source == SourceBuiltIn {
		return SourceBuiltIn + ":" + e.Name
	}
	return e.Source + " · " + e.Path
}

// Resolver gives a file its rule: the first entry of one ordered list whose
// path matches, team entries first, then the built-ins when they are on.
type Resolver struct {
	list    []Entry
	builtIn bool
}

// NewResolver is the resolver over team (the repository's entries, then each
// rule set's, in order), followed by Builtin() when builtIn is true.
func NewResolver(team []Entry, builtIn bool) *Resolver {
	var b []Entry
	if builtIn {
		b = Builtin()
	}
	r := newResolver(team, b)
	r.builtIn = builtIn
	return r
}

// NewResolverWith is the resolver over team followed by builtins, such as
// the entries of SelectBuiltin. BuiltIn reports whether builtins has any.
func NewResolverWith(team, builtins []Entry) *Resolver {
	return newResolver(team, builtins)
}

func newResolver(team, builtins []Entry) *Resolver {
	list := make([]Entry, 0, len(team)+len(builtins))
	list = append(append(list, team...), builtins...)
	return &Resolver{list: list, builtIn: len(builtins) > 0}
}

// Resolve returns the first entry whose path matches p. A file that matches
// nothing, or whose first match is an off entry, has no rule. A nil Resolver
// gives no rule.
func (r *Resolver) Resolve(p string) (Entry, bool) {
	if r == nil {
		return Entry{}, false
	}
	for _, e := range r.list {
		if scope.Match(e.Path, p) {
			if e.Off {
				return Entry{}, false
			}
			return e, true
		}
	}
	return Entry{}, false
}

// Entries is the number of entries in use: team entries and, when on, the
// built-ins.
func (r *Resolver) Entries() int {
	if r == nil {
		return 0
	}
	return len(r.list)
}

// BuiltIn reports whether the built-in rules are on.
func (r *Resolver) BuiltIn() bool { return r != nil && r.builtIn }

// Hash is a stable lowercase hex SHA-256 of the list, in order: the same list
// gives the same hash, and any change to an entry or the order changes it.
func Hash(entries []Entry) string {
	h := sha256.New()
	for _, e := range entries {
		line, _ := json.Marshal([]any{e.Source, e.Name, e.Path, e.Rule, e.Off})
		h.Write(line)
		h.Write([]byte{'\n'})
	}
	return hex.EncodeToString(h.Sum(nil))
}

// Hides returns the names of the built-ins whose files e may also match, so e,
// coming first, would hide them there. It is a heuristic for an editor note:
// it builds sample paths from each glob and tries them on the other, so it may
// miss an overlap, but every overlap it reports is real.
func Hides(e Entry, builtins []Entry) []string {
	var names []string
	for _, b := range builtins {
		if b.Name != "" && overlap(e.Path, b.Path) {
			names = append(names, b.Name)
		}
	}
	return names
}

// maxWitnesses bounds the sample paths built from one glob.
const maxWitnesses = 64

func overlap(a, b string) bool {
	for _, w := range witnesses(a, baseWitnesses(b)) {
		if scope.Match(b, w) {
			return true
		}
	}
	for _, w := range witnesses(b, baseWitnesses(a)) {
		if scope.Match(a, w) {
			return true
		}
	}
	return false
}

// baseWitnesses are sample file names for the last segment of pattern.
func baseWitnesses(pattern string) []string {
	segs := strings.Split(pattern, "/")
	if w, ok := segmentWitness(segs[len(segs)-1]); ok && w != "" {
		return []string{w}
	}
	return nil
}

// witnesses are sample paths that match pattern: "*" and "?" become "x", a
// class its first character, and a "**" segment is left out, becomes "x" or
// becomes one of names (so "docs/**" can try "docs/main.go").
func witnesses(pattern string, names []string) []string {
	out := []string{""}
	for _, seg := range strings.Split(pattern, "/") {
		var opts []string
		if seg == "**" {
			opts = append([]string{"", "x"}, names...)
		} else {
			w, ok := segmentWitness(seg)
			if !ok {
				return nil
			}
			opts = []string{w}
		}
		var next []string
		for _, prefix := range out {
			for _, o := range opts {
				switch {
				case o == "":
					next = append(next, prefix)
				case prefix == "":
					next = append(next, o)
				default:
					next = append(next, prefix+"/"+o)
				}
				if len(next) == maxWitnesses {
					break
				}
			}
		}
		out = next
	}
	return out
}

// segmentWitness is one name that the glob segment seg matches; false when it
// can't build one (a negated class).
func segmentWitness(seg string) (string, bool) {
	var b strings.Builder
	for i := 0; i < len(seg); i++ {
		switch c := seg[i]; c {
		case '*', '?':
			b.WriteByte('x')
		case '\\':
			if i+1 < len(seg) {
				i++
				b.WriteByte(seg[i])
			}
		case '[':
			if i+1 >= len(seg) || seg[i+1] == '!' || seg[i+1] == '^' {
				return "", false
			}
			end := strings.IndexByte(seg[i+2:], ']')
			if end < 0 {
				return "", false
			}
			b.WriteByte(seg[i+1])
			i += end + 2
		default:
			b.WriteByte(c)
		}
	}
	return b.String(), true
}
