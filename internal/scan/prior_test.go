package scan

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gopaytech/pergent/internal/jsonschema"
)

func priorManifest() Manifest {
	m := cfManifest()
	m.State = []string{"findings.json", "coverage-ledger.json"}
	return m
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestWritePriorStateFiles(t *testing.T) {
	t.Parallel()
	skill := copyFixtures(t, "report-schema.json")
	out := t.TempDir()
	p := Prior{State: map[string]string{
		"findings.json":        "[ ]\n",
		"coverage-ledger.json": `{"a":  1}`,
		"other.json":           "no",
	}}
	dir, err := WritePrior(p, priorManifest(), skill, out)
	if err != nil {
		t.Fatal(err)
	}
	if dir != filepath.Join(out, "run-1") {
		t.Fatalf("dir = %q", dir)
	}
	if got := readFile(t, filepath.Join(dir, "findings.json")); got != "[ ]\n" {
		t.Errorf("findings = %q", got)
	}
	if got := readFile(t, filepath.Join(dir, "coverage-ledger.json")); got != `{"a":  1}` {
		t.Errorf("ledger = %q", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "other.json")); err == nil {
		t.Error("a key not in the manifest was written")
	}
}

func decodeRecords(t *testing.T, text string) []map[string]any {
	t.Helper()
	var recs []map[string]any
	if err := json.Unmarshal([]byte(text), &recs); err != nil {
		t.Fatal(err)
	}
	return recs
}

func TestWritePriorDismissalReplaces(t *testing.T) {
	t.Parallel()
	skill := copyFixtures(t, "report-schema.json")
	state := `[{"verdict":"confirmed","fingerprint":"f1","title":"old","description":"d","trace":[{"kind":"sink","file":"a.go","line":3}],"execution":{"attacker_perspective":"a","observed_result":"o"},"remediation":{"strategy":"s"},"severity":{"overall_severity":"high"},"confidence":{"score":"high"}},
{"verdict":"needs_validation","fingerprint":"f2","title":"keep","description":"d","trace":[{"kind":"entrypoint","file":"b.go","line":1}],"validation_plan":{"local":"l","deployment":"d"}}]`
	p := Prior{
		State:     map[string]string{"findings.json": state},
		Dismissed: []Dismissed{{Fingerprint: "f1", Title: "Weak hash", File: "a/b.go", Line: 12, Reason: "false_positive", Comment: "It is a test key."}},
	}
	dir, err := WritePrior(p, priorManifest(), skill, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	recs := decodeRecords(t, readFile(t, filepath.Join(dir, "findings.json")))
	if len(recs) != 2 {
		t.Fatalf("records = %d, want 2", len(recs))
	}
	r := recs[0]
	if r["verdict"] != "rejected" || r["fingerprint"] != "f1" || r["title"] != "Weak hash" {
		t.Errorf("record = %v", r)
	}
	if r["reason"] != "A maintainer dismissed this finding as a false positive. It is a test key." {
		t.Errorf("reason = %q", r["reason"])
	}
	if r["description"] != "Weak hash" || r["claimed_root_cause"] != "Weak hash" {
		t.Errorf("text fields = %v", r)
	}
	if recs[1]["fingerprint"] != "f2" || recs[1]["verdict"] != "needs_validation" {
		t.Errorf("other record = %v", recs[1])
	}
}

func TestWritePriorDismissalAdds(t *testing.T) {
	t.Parallel()
	skill := copyFixtures(t, "report-schema.json")
	reasons := map[string]string{
		"false_positive":  "a false positive",
		"wont_fix":        "won't fix",
		"used_in_tests":   "used in tests",
		"mitigated":       "mitigated",
		"acceptable_risk": "an acceptable risk",
	}
	for reason, words := range reasons {
		p := Prior{Dismissed: []Dismissed{{Fingerprint: "f9", Title: "T", File: "x.go", Line: 0, Reason: reason}}}
		dir, err := WritePrior(p, priorManifest(), skill, t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		text := readFile(t, filepath.Join(dir, "findings.json"))
		recs := decodeRecords(t, text)
		if len(recs) != 1 || recs[0]["verdict"] != "rejected" || recs[0]["fingerprint"] != "f9" {
			t.Fatalf("records = %v", recs)
		}
		if want := "A maintainer dismissed this finding as " + words + "."; recs[0]["reason"] != want {
			t.Errorf("reason = %q, want %q", recs[0]["reason"], want)
		}
		raw, err := jsonschema.Decode([]byte(text), 1<<20, 64)
		if err != nil {
			t.Fatal(err)
		}
		schema, err := jsonschema.Compile([]byte(readFile(t, filepath.Join(skill, "report-schema.json"))))
		if err != nil {
			t.Fatal(err)
		}
		if errs := schema.Validate(raw); len(errs) > 0 {
			t.Fatalf("schema: %v", errs)
		}
		trace := recs[0]["trace"].([]any)[0].(map[string]any)
		if trace["kind"] != "sink" || trace["file"] != "x.go" || trace["scope"] != "x.go" || trace["line"].(float64) != 1 {
			t.Errorf("trace = %v", trace)
		}
	}
}

func TestWritePriorDropsBadDismissals(t *testing.T) {
	t.Parallel()
	skill := copyFixtures(t, "report-schema.json")
	p := Prior{Dismissed: []Dismissed{
		{Fingerprint: "ok", Title: "Fine", File: "a.go", Line: 2, Reason: "mitigated"},
		{Fingerprint: "blank", Title: "  ​ ", File: "a.go", Line: 2, Reason: "mitigated"},
		{Fingerprint: "nofile", Title: "T", File: "", Line: 2, Reason: "mitigated"},
		{Fingerprint: "ok", Title: "Second", File: "a.go", Line: 2, Reason: "wont_fix"},
	}}
	dir, err := WritePrior(p, priorManifest(), skill, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	recs := decodeRecords(t, readFile(t, filepath.Join(dir, "findings.json")))
	if len(recs) != 1 || recs[0]["title"] != "Fine" {
		t.Fatalf("records = %v", recs)
	}
	// With no usable schema, no dismissal is written (fail closed).
	dir, err = WritePrior(p, priorManifest(), t.TempDir(), t.TempDir())
	if err != nil || dir != "" {
		t.Errorf("no schema: dir = %q, err = %v", dir, err)
	}
	// A state findings.json that is not a record list drops the dismissals, keeps the state.
	p.State = map[string]string{"findings.json": `{"x":1}`}
	dir, err = WritePrior(p, priorManifest(), skill, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, filepath.Join(dir, "findings.json")); got != `{"x":1}` {
		t.Errorf("findings = %q", got)
	}
}

func TestWritePriorNone(t *testing.T) {
	t.Parallel()
	skill := copyFixtures(t, "report-schema.json")
	for name, p := range map[string]Prior{
		"empty":           {},
		"only other keys": {State: map[string]string{"other.json": "x"}},
	} {
		out := t.TempDir()
		dir, err := WritePrior(p, priorManifest(), skill, out)
		if err != nil || dir != "" {
			t.Errorf("%s: dir = %q, err = %v", name, dir, err)
		}
		if _, err := os.Stat(filepath.Join(out, "run-1")); err == nil {
			t.Errorf("%s: run-1 was made", name)
		}
	}
}

func TestReadPriorRefuses(t *testing.T) {
	t.Parallel()
	write := func(t *testing.T, s string) string {
		path := filepath.Join(t.TempDir(), "prior.json")
		if err := os.WriteFile(path, []byte(s), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	entry := func(k, v string) string {
		m := map[string]any{"fingerprint": "f1", "title": "t", "file": "a.go", "line": 1, "reason": "false_positive"}
		if k != "" {
			m[k] = v
		}
		b, _ := json.Marshal(m)
		return string(b)
	}

	t.Run("good", func(t *testing.T) {
		p, err := ReadPrior(write(t, `{"state":{"findings.json":"[]"},"dismissed":[`+entry("rule", "go-sql")+`]}`))
		if err != nil {
			t.Fatal(err)
		}
		if p.State["findings.json"] != "[]" || len(p.Dismissed) != 1 || p.Dismissed[0].Rule != "go-sql" {
			t.Errorf("prior = %+v", p)
		}
	})
	t.Run("file level", func(t *testing.T) {
		for name, content := range map[string]string{
			"unknown key":       `{"state":{},"extra":1}`,
			"unknown entry key": `{"dismissed":[{"fingerprint":"f1","title":"t","file":"a","line":1,"reason":"mitigated","x":1}]}`,
			"duplicate key":     `{"state":{},"state":{}}`,
			"not json":          `nope`,
			"state not text":    `{"state":{"findings.json":1}}`,
			"too big":           `{"state":{"findings.json":"` + strings.Repeat("a", MaxPrior) + `"}}`,
		} {
			if _, err := ReadPrior(write(t, content)); err == nil {
				t.Errorf("%s: no error", name)
			}
		}
		if _, err := ReadPrior(filepath.Join(t.TempDir(), "none.json")); err == nil {
			t.Error("missing file: no error")
		}
	})
	t.Run("bad entries dropped", func(t *testing.T) {
		bad := []string{
			entry("reason", "because"),
			entry("fingerprint", "-x"),
			entry("comment", strings.Repeat("c", 1001)),
			entry("rule", strings.Repeat("r", 201)),
		}
		p, err := ReadPrior(write(t, `{"dismissed":[`+strings.Join(bad, ",")+`,`+entry("comment", strings.Repeat("c", 1000))+`]}`))
		if err != nil {
			t.Fatal(err)
		}
		if len(p.Dismissed) != 1 || len(p.Dismissed[0].Comment) != 1000 {
			t.Errorf("dismissed = %d entries, want the one good entry", len(p.Dismissed))
		}
	})
}
