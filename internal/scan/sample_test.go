package scan

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// schemaWith writes the testdata report schema into a new skill folder after
// change edits its decoded form.
func schemaWith(t *testing.T, change func(s map[string]any)) string {
	t.Helper()
	dir := copyFixtures(t, "report-schema.json")
	var s map[string]any
	if err := json.Unmarshal([]byte(readFile(t, filepath.Join(dir, "report-schema.json"))), &s); err != nil {
		t.Fatal(err)
	}
	change(s)
	data, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "report-schema.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func branches(s map[string]any) []any { return s["items"].(map[string]any)["oneOf"].([]any) }

func TestCheckSample(t *testing.T) {
	t.Parallel()
	t.Run("the testdata schema takes the sample", func(t *testing.T) {
		t.Parallel()
		if err := CheckSample(copyFixtures(t, "report-schema.json")); err != nil {
			t.Fatalf("CheckSample() error: %v", err)
		}
	})
	cases := []struct {
		name, want string
		skill      func(t *testing.T) string
	}{
		{"a schema that takes no record", "sample", func(t *testing.T) string {
			return schemaWith(t, func(s map[string]any) { s["items"] = map[string]any{"type": "string"} })
		}},
		{"a schema that wants a field the sample lacks", "sample", func(t *testing.T) string {
			return schemaWith(t, func(s map[string]any) {
				b := branches(s)[0].(map[string]any)
				b["properties"].(map[string]any)["cwe"] = map[string]any{"type": "string"}
				b["required"] = append(b["required"].([]any), "cwe")
			})
		}},
		{"a schema with no rejected record", "dismissal", func(t *testing.T) string {
			return schemaWith(t, func(s map[string]any) {
				s["items"].(map[string]any)["oneOf"] = branches(s)[:2]
			})
		}},
		{"no schema", "report-schema.json", func(t *testing.T) string { return t.TempDir() }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			err := CheckSample(c.skill(t))
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("CheckSample() error = %v, want one naming %q", err, c.want)
			}
		})
	}
}
