package sarif

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"

	"github.com/gopaytech/pergent/internal/jsonschema"
)

const schemaDigest = "c3b4bb2d6093897483348925aaa73af03b3e3f4bd4ca38cef26dcb4212a2682e"

func TestSchemaFileDigest(t *testing.T) {
	t.Parallel()
	sum := sha256.Sum256(schemaFile)
	if got := hex.EncodeToString(sum[:]); got != schemaDigest {
		t.Fatalf("schema digest = %s, want %s", got, schemaDigest)
	}
}

func TestSchemaCompiles(t *testing.T) {
	t.Parallel()
	if _, err := jsonschema.Compile(schemaFile); err != nil {
		t.Fatalf("Compile: %v", err)
	}
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestEmptyLogPasses(t *testing.T) {
	t.Parallel()
	l := Empty(Driver{Name: "security-audit"})
	if l.Schema != SchemaURL || l.Version != "2.1.0" || len(l.Runs) != 1 {
		t.Fatalf("unexpected empty log: %+v", l)
	}
	if errs := Validate(mustJSON(t, l)); len(errs) != 0 {
		t.Fatalf("errors: %v", errs)
	}
	if !strings.Contains(string(mustJSON(t, l)), `"results":[]`) {
		t.Fatalf("results must be an empty array, got %s", mustJSON(t, l))
	}
}

func fullLog() Log {
	l := Empty(Driver{
		Name: "security-audit", Version: "1.0", InformationURI: "https://example.com/x",
		Rules: []Rule{{ID: "sql-injection", Properties: &RuleProperties{Tags: []string{"security", "CWE-89"}}}},
	})
	l.Runs[0].Results = []Result{{
		RuleID:  "sql-injection",
		Level:   "error",
		Message: Message{Text: "User input reaches a query."},
		Locations: []Location{{PhysicalLocation: PhysicalLocation{
			ArtifactLocation: ArtifactLocation{URI: "src/db.go", URIBaseID: "%SRCROOT%"},
			Region:           &Region{StartLine: 12},
		}}},
		Fingerprints: map[string]string{"pergent/v1": "abc123"},
		Properties:   map[string]any{"confidence": "high"},
	}}
	return l
}

func TestFullResultPasses(t *testing.T) {
	t.Parallel()
	if errs := Validate(mustJSON(t, fullLog())); len(errs) != 0 {
		t.Fatalf("errors: %v", errs)
	}
}

func TestValidateRefuses(t *testing.T) {
	t.Parallel()
	cases := map[string]func(l *Log){
		"bad level":  func(l *Log) { l.Runs[0].Results[0].Level = "fatal" },
		"no message": func(l *Log) { l.Runs[0].Results[0].Message = Message{} },
		"version":    func(l *Log) { l.Version = "2.0.0" },
	}
	for name, mut := range cases {
		t.Run(name, func(t *testing.T) {
			l := fullLog()
			mut(&l)
			data := mustJSON(t, l)
			if name == "no message" {
				// A Go struct cannot drop the key, so remove it from the JSON.
				var m map[string]any
				_ = json.Unmarshal(data, &m)
				r := m["runs"].([]any)[0].(map[string]any)["results"].([]any)[0].(map[string]any)
				delete(r, "message")
				data = mustJSON(t, m)
			}
			if errs := Validate(data); len(errs) == 0 {
				t.Fatal("want errors, got none")
			}
		})
	}
	if errs := Validate([]byte("not json")); len(errs) == 0 {
		t.Fatal("bad JSON must give an error")
	}
}
