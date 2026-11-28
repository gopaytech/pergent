// Package sarif holds the small part of SARIF 2.1.0 that pergent writes,
// and a check of a log against the official schema.
package sarif

import (
	_ "embed"
	"fmt"
	"sync"

	"github.com/gopaytech/pergent/internal/jsonschema"
)

// SchemaURL is the value of the "$schema" field.
const SchemaURL = "https://docs.oasis-open.org/sarif/sarif/v2.1.0/errata01/os/schemas/sarif-schema-2.1.0.json"

const (
	maxBytes = 12 << 20
	maxDepth = 64
)

//go:embed sarif-schema-2.1.0.json
var schemaFile []byte

// compiled is built once, on first use.
var compiled = sync.OnceValues(func() (*jsonschema.Schema, error) {
	return jsonschema.Compile(schemaFile)
})

// Log is a SARIF log.
type Log struct {
	Schema  string `json:"$schema"`
	Version string `json:"version"`
	Runs    []Run  `json:"runs"`
}

// Run is one tool run.
type Run struct {
	Tool    Tool     `json:"tool"`
	Results []Result `json:"results"`
}

// Tool names the program that made the results.
type Tool struct {
	Driver Driver `json:"driver"`
}

// Driver describes the tool and its rules.
type Driver struct {
	Name           string `json:"name"`
	Version        string `json:"version,omitempty"`
	InformationURI string `json:"informationUri,omitempty"`
	Rules          []Rule `json:"rules,omitempty"`
}

// Rule is one rule a result can point to.
type Rule struct {
	ID         string          `json:"id"`
	Properties *RuleProperties `json:"properties,omitempty"`
}

// RuleProperties holds the rule tags.
type RuleProperties struct {
	Tags []string `json:"tags,omitempty"`
}

// Result is one finding.
type Result struct {
	RuleID       string            `json:"ruleId,omitempty"`
	Level        string            `json:"level,omitempty"`
	Message      Message           `json:"message"`
	Locations    []Location        `json:"locations,omitempty"`
	Fingerprints map[string]string `json:"fingerprints,omitempty"`
	Properties   map[string]any    `json:"properties,omitempty"`
}

// Message is the text of a result.
type Message struct {
	Text string `json:"text"`
}

// Location says where a result is.
type Location struct {
	PhysicalLocation PhysicalLocation `json:"physicalLocation"`
}

// PhysicalLocation is a file and, when known, a line.
type PhysicalLocation struct {
	ArtifactLocation ArtifactLocation `json:"artifactLocation"`
	Region           *Region          `json:"region,omitempty"`
}

// ArtifactLocation is a file path.
type ArtifactLocation struct {
	URI       string `json:"uri"`
	URIBaseID string `json:"uriBaseId,omitempty"`
}

// Region is a place in a file.
type Region struct {
	StartLine int `json:"startLine"`
}

// Empty returns a valid log with one run and no results.
func Empty(d Driver) Log {
	return Log{
		Schema:  SchemaURL,
		Version: "2.1.0",
		Runs:    []Run{{Tool: Tool{Driver: d}, Results: []Result{}}},
	}
}

// Validate checks data against the SARIF 2.1.0 schema. It returns the
// problems found, or nil when data is valid.
func Validate(data []byte) []string {
	s, err := compiled()
	if err != nil {
		return []string{fmt.Sprintf("schema: %v", err)}
	}
	v, err := jsonschema.Decode(data, maxBytes, maxDepth)
	if err != nil {
		return []string{fmt.Sprintf("decode: %v", err)}
	}
	return s.Validate(v)
}
