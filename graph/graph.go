// Package graph is the pergent-graph/v1 format: a code graph of one commit of
// a repository, with only names, paths, lines and edges in it. A provider
// (graphify first) writes it through a converter; pergent reads it to give
// each batch run the callers and callees of the code it changes.
//
// The format holds no free text. Every string is a fixed header value, an id,
// a kind, one identifier or a clean relative path, and Validate checks each
// one, so a file that passes holds no comment, docstring or value from the
// code. The package imports no pergent internal package: pergent.sh uses it
// to check an upload.
package graph

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

// Format is the value of the format field of every v1 file.
const Format = "pergent-graph/v1"

// Node kinds.
const (
	KindFile      = "file"
	KindFunction  = "function"
	KindMethod    = "method"
	KindType      = "type"
	KindConfigKey = "config_key"
)

// Edge kinds.
const (
	EdgeCalls    = "calls"
	EdgeImports  = "imports"
	EdgeInherits = "inherits"
	EdgeContains = "contains"
)

// MaxBytes is the largest v1 file pergent reads or writes.
const MaxBytes = 64 << 20

// maxBytes is MaxBytes, as a variable so the size tests can lower it.
var maxBytes int64 = MaxBytes

// File is one pergent-graph/v1 file.
type File struct {
	Format          string `json:"format"`
	Provider        string `json:"provider"`
	ProviderVersion string `json:"provider_version"`
	Commit          string `json:"commit"`
	BuiltAt         string `json:"built_at"`
	Nodes           []Node `json:"nodes"`
	Edges           []Edge `json:"edges"`
}

// Node is one file or symbol. Line is where it starts (1 for a file);
// EndLine is where it ends, 0 when the provider does not know.
type Node struct {
	ID      string `json:"id"`
	Kind    string `json:"kind"`
	Name    string `json:"name"`
	File    string `json:"file"`
	Line    int    `json:"line"`
	EndLine int    `json:"end_line,omitempty"`
}

// Edge points from one node to another: From calls, imports, inherits from
// or contains To.
type Edge struct {
	From string `json:"from"`
	To   string `json:"to"`
	Kind string `json:"kind"`
}

var (
	providerRe = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)
	versionRe  = regexp.MustCompile(`^[0-9A-Za-z][0-9A-Za-z.+-]{0,63}$`)
	commitRe   = regexp.MustCompile(`^(?:[0-9a-f]{40}|[0-9a-f]{64})$`)
	idRe       = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
	nameRe     = regexp.MustCompile(`^[\p{L}_$][\p{L}\p{N}_$]{0,127}$`)
	configRe   = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,128}$`)
)

// sizeText names a size limit for an error.
func sizeText(n int64) string {
	if n%(1<<20) == 0 {
		return fmt.Sprintf("%d MiB", n>>20)
	}
	return fmt.Sprintf("%d bytes", n)
}

func errTooBig() error {
	return fmt.Errorf("graph: the file is over the %s limit", sizeText(maxBytes))
}

// Decode reads one v1 file: at most MaxBytes, unknown fields refused, nothing
// after the object, then Validate. No error names a value from the input.
func Decode(r io.Reader) (*File, error) {
	b, err := io.ReadAll(io.LimitReader(r, maxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("graph: read: %w", err)
	}
	if int64(len(b)) > maxBytes {
		return nil, errTooBig()
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	var f File
	if err := dec.Decode(&f); err != nil {
		return nil, decodeError(err)
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, errors.New("graph: more data after the JSON object")
	}
	if err := f.Validate(); err != nil {
		return nil, err
	}
	return &f, nil
}

// decodeError turns a JSON error into one that names no input text: the
// encoding/json errors quote unknown field names and bad characters.
func decodeError(err error) error {
	var syntax *json.SyntaxError
	var typ *json.UnmarshalTypeError
	switch {
	case errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF):
		return errors.New("graph: the JSON ends too early")
	case errors.As(err, &syntax):
		return fmt.Errorf("graph: not valid JSON at byte %d", syntax.Offset)
	case errors.As(err, &typ):
		return fmt.Errorf("graph: field %s has the wrong type", typ.Field)
	case strings.HasPrefix(err.Error(), "json: unknown field"):
		return errors.New("graph: a field that is not in pergent-graph/v1")
	default:
		return errors.New("graph: not a pergent-graph/v1 file")
	}
}

// ReadFile opens path, refuses a file over MaxBytes before reading it, and
// Decodes it.
func ReadFile(name string) (*File, error) {
	fh, err := os.Open(name)
	if err != nil {
		return nil, fmt.Errorf("graph: %w", err)
	}
	defer fh.Close()
	st, err := fh.Stat()
	if err != nil {
		return nil, fmt.Errorf("graph: %w", err)
	}
	if !st.Mode().IsRegular() {
		return nil, errors.New("graph: not a regular file")
	}
	if st.Size() > maxBytes {
		return nil, errTooBig()
	}
	return Decode(fh)
}

// Encode validates f and returns compact JSON with a trailing newline. It is
// an error when the result is over MaxBytes.
func (f *File) Encode() ([]byte, error) {
	if err := f.Validate(); err != nil {
		return nil, err
	}
	out := *f
	if out.Nodes == nil {
		out.Nodes = []Node{}
	}
	if out.Edges == nil {
		out.Edges = []Edge{}
	}
	b, err := json.Marshal(&out)
	if err != nil {
		return nil, fmt.Errorf("graph: encode: %w", err)
	}
	b = append(b, '\n')
	if int64(len(b)) > maxBytes {
		return nil, errTooBig()
	}
	return b, nil
}

// Validate checks every field of f. An error names the field and the index,
// never the value: a caller may store it.
func (f *File) Validate() error {
	switch {
	case f.Format != Format:
		return errors.New("graph: bad format")
	case !providerRe.MatchString(f.Provider):
		return errors.New("graph: bad provider")
	case !versionRe.MatchString(f.ProviderVersion):
		return errors.New("graph: bad provider_version")
	case !commitRe.MatchString(f.Commit):
		return errors.New("graph: bad commit")
	}
	if _, err := time.Parse(time.RFC3339, f.BuiltAt); err != nil {
		return errors.New("graph: bad built_at")
	}
	kinds := make(map[string]string, len(f.Nodes))
	for i, n := range f.Nodes {
		if !idRe.MatchString(n.ID) {
			return fmt.Errorf("graph: node %d: bad id", i)
		}
		if _, dup := kinds[n.ID]; dup {
			return fmt.Errorf("graph: node %d: duplicate id", i)
		}
		switch n.Kind {
		case KindFile, KindFunction, KindMethod, KindType, KindConfigKey:
		default:
			return fmt.Errorf("graph: node %d: bad kind", i)
		}
		if !ValidPath(n.File) {
			return fmt.Errorf("graph: node %d: bad file", i)
		}
		if !ValidName(n.Kind, n.Name, n.File) {
			return fmt.Errorf("graph: node %d: bad name", i)
		}
		if n.Line < 1 {
			return fmt.Errorf("graph: node %d: bad line", i)
		}
		if n.EndLine != 0 && n.EndLine < n.Line {
			return fmt.Errorf("graph: node %d: bad end_line", i)
		}
		kinds[n.ID] = n.Kind
	}
	for i, e := range f.Edges {
		from, okFrom := kinds[e.From]
		to, okTo := kinds[e.To]
		if !okFrom {
			return fmt.Errorf("graph: edge %d: bad from", i)
		}
		if !okTo {
			return fmt.Errorf("graph: edge %d: bad to", i)
		}
		if !EdgeAllowed(e.Kind, from, to) {
			return fmt.Errorf("graph: edge %d: bad kind for its ends", i)
		}
	}
	return nil
}

// EdgeAllowed reports whether an edge of kind may go from a node of kind
// from to a node of kind to.
func EdgeAllowed(kind, from, to string) bool {
	switch kind {
	case EdgeCalls:
		return (from == KindFunction || from == KindMethod) &&
			(to == KindFunction || to == KindMethod || to == KindType)
	case EdgeImports:
		return from == KindFile && to == KindFile
	case EdgeContains:
		return (from == KindFile || from == KindType || from == KindConfigKey) &&
			to != KindFile && to != ""
	case EdgeInherits:
		return from == KindType && to == KindType
	}
	return false
}

// ValidName reports whether name may stand for a node of kind in file. A file
// is named by its base name, a config key by a dotted key, and every other
// kind by one identifier.
func ValidName(kind, name, file string) bool {
	switch kind {
	case KindFile:
		return name == path.Base(file)
	case KindConfigKey:
		return configRe.MatchString(name)
	case KindFunction, KindMethod, KindType:
		return nameRe.MatchString(name)
	}
	return false
}

// ValidPath reports whether p is a clean, relative, slash-separated path
// inside the repository: 1 to 4096 bytes of valid UTF-8, no control byte, no
// backslash, and no "." or ".." part.
func ValidPath(p string) bool {
	if p == "" || len(p) > 4096 || !utf8.ValidString(p) {
		return false
	}
	for i := 0; i < len(p); i++ {
		if c := p[i]; c < 0x20 || c == 0x7f || c == '\\' {
			return false
		}
	}
	if path.IsAbs(p) || path.Clean(p) != p || p == "." || p == ".." || strings.HasPrefix(p, "../") {
		return false
	}
	return true
}
