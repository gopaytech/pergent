package scan

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/gopaytech/pergent/internal/sarif"
)

const testdataDir = "testdata/cloudflare"

func cfManifest() Manifest {
	return Manifest{Name: "security-audit", Output: ManifestOutput{Path: "findings.json", Format: FormatCloudflare}}
}

// copyFixtures copies the testdata files into a fresh folder and returns it.
func copyFixtures(t *testing.T, names ...string) string {
	t.Helper()
	dir := t.TempDir()
	for _, n := range names {
		data, err := os.ReadFile(filepath.Join(testdataDir, n))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, n), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestConvertCloudflare(t *testing.T) {
	t.Parallel()
	c := Convert(cfManifest(), testdataDir, testdataDir)
	if !c.Valid {
		t.Fatalf("not valid: %s", c.Problem)
	}
	if len(c.Results) != 2 {
		t.Fatalf("results = %d, want 2", len(c.Results))
	}
	hi := c.Results[0]
	if hi.Level != "error" || hi.Properties["severity"] != "high" || hi.Properties["security-severity"] != "8.0" {
		t.Errorf("high result = %+v", hi)
	}
	if hi.Fingerprints["security-audit/v1"] != "fp-high-1" {
		t.Errorf("fingerprints = %v", hi.Fingerprints)
	}
	if hi.RuleID != "ATTACK-CLASSES.md#Injection" {
		t.Errorf("ruleId = %q", hi.RuleID)
	}
	if hi.Message.Text != "SQL injection in the user search" {
		t.Errorf("message = %q", hi.Message.Text)
	}
	if len(hi.Locations) != 1 {
		t.Fatalf("locations = %v", hi.Locations)
	}
	loc := hi.Locations[0].PhysicalLocation
	if loc.ArtifactLocation.URI != "src/db.go" || loc.ArtifactLocation.URIBaseID != "%SRCROOT%" || loc.Region == nil || loc.Region.StartLine != 42 {
		t.Errorf("location = %+v", loc)
	}
	if hi.Properties["exploit_scenario"] != "A user sends a quote in the term.\n\nThe query returned all rows." ||
		hi.Properties["fix"] != "Use a bound parameter." || hi.Properties["confidence"] != "high" {
		t.Errorf("properties = %v", hi.Properties)
	}
	nv := c.Results[1]
	if nv.Level != "warning" || nv.Properties["needs_validation"] != true {
		t.Errorf("needs_validation result = %+v", nv)
	}
	if _, ok := nv.Properties["severity"]; ok {
		t.Errorf("needs_validation has a severity: %v", nv.Properties)
	}
	if _, ok := nv.Properties["security-severity"]; ok {
		t.Errorf("needs_validation has a security-severity: %v", nv.Properties)
	}
	if nv.RuleID != "ATTACK-CLASSES.md#Files" {
		t.Errorf("ruleId = %q", nv.RuleID)
	}
	if nv.Properties["fix"] != "Run the export with a dotted name.\n\nCheck the live bucket policy." {
		t.Errorf("fix = %v", nv.Properties["fix"])
	}
	if len(c.Rules) != 2 || c.Rules[0].ID != "ATTACK-CLASSES.md#Injection" ||
		len(c.Rules[0].Properties.Tags) != 2 || c.Rules[0].Properties.Tags[0] != "security" {
		t.Errorf("rules = %+v", c.Rules)
	}
	if c.Coverage != (Coverage{Done: 2, Planned: 3}) {
		t.Errorf("coverage = %+v", c.Coverage)
	}
	if c.RunStatus != "complete" {
		t.Errorf("run status = %q", c.RunStatus)
	}
}

// oneConfirmed builds a findings.json with a single confirmed record.
func oneConfirmed(fp, file, severity string) string {
	return `[{"verdict":"confirmed","fingerprint":"` + fp + `","title":"T ` + fp + `","description":"d",` +
		`"trace":[{"kind":"sink","file":` + file + `,"line":3}],` +
		`"execution":{"attacker_perspective":"a","observed_result":"o"},"remediation":{"strategy":"s"},` +
		`"severity":{"overall_severity":"` + severity + `"},"confidence":{"score":"low"}}]`
}

func writeFindings(t *testing.T, dir, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "findings.json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestConvertSeverityMap(t *testing.T) {
	t.Parallel()
	dir := copyFixtures(t, "report-schema.json")
	tests := []struct{ in, level, sev, score string }{
		{"critical", "error", "critical", "9.5"},
		{"high", "error", "high", "8.0"},
		{"medium", "warning", "medium", "5.5"},
		{"low", "note", "low", "3.0"},
		{"informational", "note", "low", "1.0"},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			writeFindings(t, dir, oneConfirmed("fp1", `"a.go"`, tt.in))
			c := Convert(cfManifest(), dir, dir)
			if !c.Valid || len(c.Results) != 1 {
				t.Fatalf("valid=%v problem=%q results=%d", c.Valid, c.Problem, len(c.Results))
			}
			r := c.Results[0]
			if r.Level != tt.level || r.Properties["severity"] != tt.sev || r.Properties["security-severity"] != tt.score {
				t.Errorf("got %s %v %v", r.Level, r.Properties["severity"], r.Properties["security-severity"])
			}
			if r.RuleID != "security-audit" {
				t.Errorf("ruleId without a ledger = %q", r.RuleID)
			}
		})
	}
}

func TestConvertBadPathKeepsResult(t *testing.T) {
	t.Parallel()
	dir := copyFixtures(t, "report-schema.json")
	for name, file := range map[string]string{
		"absolute":  `"/etc/x"`,
		"parent":    `"../x"`,
		"inner":     `"a/../x"`,
		"backslash": `"a\\b"`,
	} {
		t.Run(name, func(t *testing.T) {
			writeFindings(t, dir, oneConfirmed("fp1", file, "high"))
			c := Convert(cfManifest(), dir, dir)
			if !c.Valid || len(c.Results) != 1 {
				t.Fatalf("valid=%v problem=%q results=%d", c.Valid, c.Problem, len(c.Results))
			}
			if len(c.Results[0].Locations) != 0 {
				t.Errorf("locations = %v", c.Results[0].Locations)
			}
		})
	}
}

func TestConvertRepeatedFingerprint(t *testing.T) {
	t.Parallel()
	dir := copyFixtures(t, "report-schema.json")
	one := oneConfirmed("same", `"a.go"`, "high")
	two := oneConfirmed("same", `"b.go"`, "low")
	writeFindings(t, dir, one[:len(one)-1]+","+two[1:])
	c := Convert(cfManifest(), dir, dir)
	if !c.Valid || len(c.Results) != 1 {
		t.Fatalf("valid=%v problem=%q results=%d", c.Valid, c.Problem, len(c.Results))
	}
	if c.Results[0].Level != "error" || c.Results[0].Locations[0].PhysicalLocation.ArtifactLocation.URI != "a.go" {
		t.Errorf("kept the wrong record: %+v", c.Results[0])
	}
}

func TestConvertInvalid(t *testing.T) {
	t.Parallel()
	dir := copyFixtures(t, "report-schema.json")
	c := Convert(cfManifest(), dir, dir)
	if c.Valid || c.Problem != "findings.json is missing" || len(c.Results) != 0 {
		t.Errorf("missing: %+v", c)
	}
	for name, body := range map[string]string{
		"schema":  `[{"verdict":"confirmed"}]`,
		"unknown": `[{"verdict":"maybe","fingerprint":"x","title":"t"}]`,
	} {
		writeFindings(t, dir, body)
		c = Convert(cfManifest(), dir, dir)
		if c.Valid || c.Problem != "findings.json did not pass the skill's schema check" || len(c.Results) != 0 {
			t.Errorf("%s: %+v", name, c)
		}
	}
	writeFindings(t, dir, `[{"a":1,"a":2}]`)
	if c = Convert(cfManifest(), dir, dir); c.Valid || c.Problem != "findings.json did not pass the skill's schema check" {
		t.Errorf("bad json: %+v", c)
	}
	writeFindings(t, dir, `[]`)
	if c = Convert(cfManifest(), t.TempDir(), dir); c.Valid || c.Problem != "findings.json did not pass the skill's schema check" {
		t.Errorf("no schema: %+v", c)
	}
	writeFindings(t, dir, `[]`)
	if c = Convert(cfManifest(), dir, dir); !c.Valid || len(c.Results) != 0 {
		t.Errorf("empty list: %+v", c)
	}
}

// A run stopped before findings.json is valid still reports the ledger's
// coverage: the envelope of a partial scan carries it.
func TestConvertCoverageWithoutFindings(t *testing.T) {
	t.Parallel()
	dir := copyFixtures(t, "report-schema.json", "coverage-ledger.json")
	if c := Convert(cfManifest(), dir, dir); c.Valid || c.Coverage != (Coverage{Done: 2, Planned: 3}) {
		t.Errorf("missing findings: %+v", c)
	}
	writeFindings(t, dir, `[{"verdict":"confirmed"}]`)
	if c := Convert(cfManifest(), dir, dir); c.Valid || c.Coverage != (Coverage{Done: 2, Planned: 3}) {
		t.Errorf("bad findings: %+v", c)
	}
}

func TestReadOutFileRefusesLinksAndFIFOs(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	real := filepath.Join(dir, "real.json")
	if err := os.WriteFile(real, []byte(`[]`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, filepath.Join(dir, "link.json")); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(filepath.Join(dir, "fifo.json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "dir.json"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "big.json"), make([]byte, 100), 0o600); err != nil {
		t.Fatal(err)
	}
	if data, err := ReadOutFile(dir, "real.json", 10); err != nil || string(data) != "[]" {
		t.Fatalf("real file: %q %v", data, err)
	}
	for name, want := range map[string]error{
		"link.json": ErrNotRegular, "fifo.json": ErrNotRegular, "dir.json": ErrNotRegular, "big.json": ErrTooBig,
	} {
		if _, err := ReadOutFile(dir, name, 10); !errors.Is(err, want) {
			t.Errorf("%s: err = %v, want %v", name, err, want)
		}
	}
	for _, name := range []string{"", "..", "a/b", `a\b`, "missing.json"} {
		if _, err := ReadOutFile(dir, name, 10); err == nil {
			t.Errorf("%q: no error", name)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "findings.json"), make([]byte, 6<<20), 0o600); err != nil {
		t.Fatal(err)
	}
	// Convert reports each of them as a missing findings.json.
	schema := copyFixtures(t, "report-schema.json")
	for _, mk := range []func(string){
		func(p string) { _ = os.Symlink(real, p) },
		func(p string) { _ = syscall.Mkfifo(p, 0o600) },
		func(p string) { _ = os.Mkdir(p, 0o700) },
		func(p string) { _ = os.WriteFile(p, make([]byte, 6<<20), 0o600) },
	} {
		out := t.TempDir()
		mk(filepath.Join(out, "findings.json"))
		if c := Convert(cfManifest(), schema, out); c.Valid || c.Problem != "findings.json is missing" {
			t.Errorf("Convert = %+v", c)
		}
	}
}

func TestConvertOutputPassesSARIFSchema(t *testing.T) {
	t.Parallel()
	c := Convert(cfManifest(), testdataDir, testdataDir)
	if !c.Valid {
		t.Fatal(c.Problem)
	}
	log := sarif.Empty(sarif.Driver{Name: "security-audit", Rules: c.Rules})
	log.Runs[0].Results = c.Results
	data, err := json.Marshal(log)
	if err != nil {
		t.Fatal(err)
	}
	if problems := sarif.Validate(data); len(problems) != 0 {
		t.Errorf("SARIF problems: %v", problems)
	}
}

func TestConvertSARIFFormat(t *testing.T) {
	t.Parallel()
	m := Manifest{Output: ManifestOutput{Path: "out.sarif", Format: FormatSARIF}}
	dir := t.TempDir()
	log := sarif.Empty(sarif.Driver{Name: "x", Rules: []sarif.Rule{{ID: "r1"}}})
	log.Runs[0].Results = []sarif.Result{{RuleID: "r1", Message: sarif.Message{Text: "m"}}}
	data, _ := json.Marshal(log)
	if err := os.WriteFile(filepath.Join(dir, "out.sarif"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	c := Convert(m, dir, dir)
	if !c.Valid || len(c.Results) != 1 || len(c.Rules) != 1 || c.RunStatus != "complete" {
		t.Errorf("good: %+v", c)
	}
	if err := os.WriteFile(filepath.Join(dir, "out.sarif"), []byte(`{"version":"2.1.0"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if c = Convert(m, dir, dir); c.Valid || c.Problem != "the SARIF did not pass the schema check" {
		t.Errorf("bad: %+v", c)
	}
	if err := os.WriteFile(filepath.Join(dir, "out.sarif"), []byte(`{`), 0o600); err != nil {
		t.Fatal(err)
	}
	if c = Convert(m, dir, dir); c.Valid || c.Problem != ProblemSARIFSchema {
		t.Errorf("bad json: %+v", c)
	}
	if c = Convert(m, dir, t.TempDir()); c.Valid || c.Problem != "out.sarif is missing" {
		t.Errorf("missing: %+v", c)
	}
}
