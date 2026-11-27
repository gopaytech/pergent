package scan

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"

	"github.com/gopaytech/pergent/internal/jsonschema"
	"github.com/gopaytech/pergent/internal/sarif"
)

// sampleFindings is one confirmed record in the shape of the skill's
// findings.json, written for pergent (not copied from the skill).
//
//go:embed sample-findings.json
var sampleFindings []byte

// sampleDismissal is the dismissal CheckSample turns into a rejected record.
var sampleDismissal = Dismissed{Fingerprint: "pergent-sample/dismissed", Title: "Sample dismissal",
	File: "app/db.go", Line: 42, Reason: "false_positive", Comment: "A sample."}

// sampleResult is the SARIF result the converter must make of the sample.
func sampleResult() sarif.Result {
	return sarif.Result{
		RuleID:       defaultRuleID,
		Level:        "error",
		Message:      sarif.Message{Text: "Sample: a search term reaches a SQL query"},
		Fingerprints: map[string]string{fingerprintKey: "pergent-sample/search-query"},
		Properties: map[string]any{
			"description":       "pergent's own sample record, never a real finding.",
			"severity":          "high",
			"security-severity": "8.0",
			"exploit_scenario":  "A signed-in user sends a quote in the term.\n\nEvery row comes back.",
			"fix":               "Send the term as a bound parameter.",
			"confidence":        "high",
		},
		Locations: []sarif.Location{{PhysicalLocation: sarif.PhysicalLocation{
			ArtifactLocation: sarif.ArtifactLocation{URI: "app/db.go", URIBaseID: "%SRCROOT%"},
			Region:           &sarif.Region{StartLine: 42},
		}}},
	}
}

// CheckSample proves that a cloudflare-findings skill's report-schema.json
// (in skillDir) and pergent agree on the record shape: the sample record
// passes the schema and the converter makes the expected SARIF result of
// it, and the rejected record a dismissal writes passes the schema too. The
// image build runs it on the pinned skill, so a skill whose records changed
// shape fails the build instead of every scan.
func CheckSample(skillDir string) error {
	schemaData, err := ReadOutFile(skillDir, schemaFile, maxSchema)
	if err != nil {
		return fmt.Errorf("%s: %w", schemaFile, err)
	}
	schema, err := jsonschema.Compile(schemaData)
	if err != nil {
		return fmt.Errorf("%s: %w", schemaFile, err)
	}

	out, err := os.MkdirTemp("", "pergent-sample-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(out)
	if err := os.WriteFile(filepath.Join(out, findingsFile), sampleFindings, 0o600); err != nil {
		return err
	}
	c := convertCloudflare(skillDir, out)
	if !c.Valid {
		return errors.New("the sample findings.json record does not pass the skill's report-schema.json")
	}
	if len(c.Results) != 1 || !reflect.DeepEqual(c.Results[0], sampleResult()) {
		return errors.New("the converter did not make the expected SARIF result of the sample record")
	}

	rec, err := json.Marshal(rejectedRecord(sampleDismissal))
	if err != nil {
		return err
	}
	if !passes(schema, []json.RawMessage{rec}) {
		return errors.New("the record a dismissal writes does not pass the skill's report-schema.json")
	}
	return nil
}
