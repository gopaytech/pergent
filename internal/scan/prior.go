package scan

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/gopaytech/pergent/internal/jsonschema"
)

const (
	// MaxPrior is the size limit of the prior file.
	MaxPrior = 800 << 10

	priorRunDir     = "run-1"
	maxCommentBytes = 1000
	maxRuleBytes    = 200
)

// fingerprintRe is the fingerprint pattern of the skill's report schema.
var fingerprintRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/@+-]*$`)

// dismissReasons maps a reason of the prior file to the words in the record.
var dismissReasons = map[string]string{
	"false_positive":  "a false positive",
	"wont_fix":        "won't fix",
	"used_in_tests":   "used in tests",
	"mitigated":       "mitigated",
	"acceptable_risk": "an acceptable risk",
}

// Dismissed is a finding that a maintainer dismissed. Rule is accepted and not used.
type Dismissed struct {
	Fingerprint string `json:"fingerprint"`
	Title       string `json:"title"`
	File        string `json:"file"`
	Line        int    `json:"line"`
	Reason      string `json:"reason"`
	Comment     string `json:"comment"`
	Rule        string `json:"rule"`
}

// Prior is the last complete scan's skill state and the dismissed findings.
type Prior struct {
	State     map[string]string `json:"state"`
	Dismissed []Dismissed       `json:"dismissed"`
}

// ReadPrior reads the prior file. The file is trusted, but it is still parsed
// strictly: a duplicate or unknown key, bad JSON or a file over MaxPrior is an
// error. A dismissal that is not valid is dropped with a log line.
func ReadPrior(path string) (Prior, error) {
	data, err := ReadOutFile(filepath.Dir(path), filepath.Base(path), MaxPrior)
	if err != nil {
		return Prior{}, err
	}
	if _, err := jsonschema.Decode(data, MaxPrior, jsonDepth); err != nil {
		return Prior{}, fmt.Errorf("prior: %w", err)
	}
	var p Prior
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&p); err != nil {
		return Prior{}, fmt.Errorf("prior: %w", err)
	}
	kept := p.Dismissed[:0]
	for i, d := range p.Dismissed {
		if why := checkDismissed(d); why != "" {
			log.Printf("warning: prior: dropped dismissal %d: %s", i+1, why)
			continue
		}
		kept = append(kept, d)
	}
	p.Dismissed = kept
	return p, nil
}

func checkDismissed(d Dismissed) string {
	switch {
	case !fingerprintRe.MatchString(d.Fingerprint):
		return "the fingerprint is not valid"
	case dismissReasons[d.Reason] == "":
		return "the reason is not known"
	case len(d.Comment) > maxCommentBytes:
		return "the comment is too long"
	case len(d.Rule) > maxRuleBytes:
		return "the rule is too long"
	}
	return ""
}

// WritePrior writes the prior run folder the skill reads: dir/run-1 holds the
// state files the manifest lists, and each dismissal is a rejected record in
// findings.json. It returns the folder, or "" when there is nothing to write.
// A dismissal that would break the skill's report-schema.json (in skillDir)
// is dropped with a log line, and with no usable schema none is written.
func WritePrior(p Prior, m Manifest, skillDir, dir string) (string, error) {
	files := map[string][]byte{}
	for _, name := range m.State {
		if text, ok := p.State[name]; ok {
			files[name] = []byte(text)
		}
	}
	if merged, ok := mergeDismissals(files[findingsFile], p.Dismissed, skillDir); ok {
		files[findingsFile] = merged
	}
	if len(files) == 0 {
		return "", nil
	}
	run := filepath.Join(dir, priorRunDir)
	if err := os.MkdirAll(run, 0o700); err != nil {
		return "", err
	}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(run, name), data, 0o600); err != nil {
			return "", err
		}
	}
	return run, nil
}

// mergeDismissals puts the dismissals into the findings text as rejected
// records. The bool is false when nothing changed.
func mergeDismissals(findings []byte, dismissed []Dismissed, skillDir string) ([]byte, bool) {
	if len(dismissed) == 0 {
		return nil, false
	}
	schemaData, err := ReadOutFile(skillDir, schemaFile, maxSchema)
	if err != nil {
		log.Printf("warning: prior: no dismissal written, the skill's schema is not readable")
		return nil, false
	}
	schema, err := jsonschema.Compile(schemaData)
	if err != nil {
		log.Printf("warning: prior: no dismissal written, the skill's schema is not valid")
		return nil, false
	}
	var records []json.RawMessage
	if len(bytes.TrimSpace(findings)) > 0 {
		if err := json.Unmarshal(findings, &records); err != nil {
			log.Printf("warning: prior: no dismissal written, findings.json is not a list of records")
			return nil, false
		}
	}
	added := false
	done := map[string]bool{}
	for _, d := range dismissed {
		if done[d.Fingerprint] {
			continue // the first dismissal of a fingerprint wins
		}
		rec, err := json.Marshal(rejectedRecord(d))
		if err != nil || !passes(schema, []json.RawMessage{rec}) {
			log.Printf("warning: prior: dropped the dismissal of %s, it does not pass the skill's schema", d.Fingerprint)
			continue
		}
		done[d.Fingerprint] = true
		records = replaceRecord(records, d.Fingerprint, rec)
		added = true
	}
	if !added {
		return nil, false
	}
	out, err := json.Marshal(records)
	if err != nil || !passes(schema, records) {
		log.Printf("warning: prior: no dismissal written, the merged findings.json does not pass the skill's schema")
		return nil, false
	}
	return out, true
}

// replaceRecord puts rec where the first record of the fingerprint was and
// removes the other records of it. With no such record it adds rec at the end.
func replaceRecord(records []json.RawMessage, fingerprint string, rec json.RawMessage) []json.RawMessage {
	out := make([]json.RawMessage, 0, len(records)+1)
	placed := false
	for _, r := range records {
		var head struct {
			Fingerprint string `json:"fingerprint"`
		}
		if json.Unmarshal(r, &head) == nil && head.Fingerprint == fingerprint {
			if !placed {
				out = append(out, rec)
				placed = true
			}
			continue
		}
		out = append(out, r)
	}
	if !placed {
		out = append(out, rec)
	}
	return out
}

func passes(schema *jsonschema.Schema, records []json.RawMessage) bool {
	data, err := json.Marshal(records)
	if err != nil {
		return false
	}
	v, err := jsonschema.Decode(data, maxFindings, jsonDepth)
	return err == nil && len(schema.Validate(v)) == 0
}

type rejectedPlace struct {
	Kind        string `json:"kind,omitempty"`
	File        string `json:"file"`
	Line        int    `json:"line"`
	Scope       string `json:"scope,omitempty"`
	Description string `json:"description"`
}

type rejected struct {
	Verdict          string          `json:"verdict"`
	Fingerprint      string          `json:"fingerprint"`
	Title            string          `json:"title"`
	Description      string          `json:"description"`
	ClaimedRootCause string          `json:"claimed_root_cause"`
	Trace            []rejectedPlace `json:"trace"`
	Evidence         []rejectedPlace `json:"evidence"`
	Reason           string          `json:"reason"`
}

func rejectedRecord(d Dismissed) rejected {
	line := d.Line
	if line < 1 {
		line = 1 // the schema wants a line; a finding without one is dismissed at line 1
	}
	reason := "A maintainer dismissed this finding as " + dismissReasons[d.Reason] + "."
	if c := strings.TrimSpace(d.Comment); c != "" {
		reason += " " + c
	}
	return rejected{
		Verdict:          "rejected",
		Fingerprint:      d.Fingerprint,
		Title:            d.Title,
		Description:      d.Title,
		ClaimedRootCause: d.Title,
		Trace:            []rejectedPlace{{Kind: "sink", File: d.File, Line: line, Scope: d.File, Description: d.Title}},
		Evidence:         []rejectedPlace{{File: d.File, Line: line, Description: d.Title}},
		Reason:           reason,
	}
}
