package scan

import (
	"encoding/json"
	"sort"
	"strings"

	"github.com/gopaytech/pergent/internal/jsonschema"
	"github.com/gopaytech/pergent/internal/sarif"
)

const (
	findingsFile   = "findings.json"
	ledgerFile     = "coverage-ledger.json"
	metadataFile   = "run-metadata.json"
	schemaFile     = "report-schema.json"
	maxFindings    = 5 << 20
	maxLedger      = 5 << 20
	maxMetadata    = 1 << 20
	maxSchema      = 1 << 20
	maxSARIFFile   = 12 << 20
	jsonDepth      = 64
	defaultRuleID  = "security-audit"
	fingerprintKey = "security-audit/v1"
)

// Coverage counts the ledger units. Planned is every unit that is not
// not_applicable or out_of_scope. Done is every unit that is covered or a candidate.
type Coverage struct {
	Done    int `json:"done"`
	Planned int `json:"planned"`
}

// Converted is what Convert makes of a skill's output files.
type Converted struct {
	Results          []sarif.Result
	Rules            []sarif.Rule
	Valid            bool
	Problem          string
	RunStatus        string
	IncompleteReason string
	Coverage         Coverage
}

// Convert turns the output files of a skill into SARIF results. skillDir holds
// the skill (and its report-schema.json), outDir the files the run wrote. Every
// file in outDir is hostile, so none of them is trusted before it is checked.
func Convert(m Manifest, skillDir, outDir string) Converted {
	if m.Output.Format == FormatSARIF {
		return convertSARIF(m, outDir)
	}
	return convertCloudflare(skillDir, outDir)
}

// The only Problem texts Convert gives. The envelope copies them as the reason.

// problemMissing is for a file that is missing, a link, a FIFO, a folder or too big.
func problemMissing(name string) string { return name + " is missing" }

// problemSchema is for a file that is not valid JSON or fails the skill's schema.
func problemSchema(name string) string {
	return name + " did not pass the skill's schema check"
}

// ProblemSARIFSchema is for a sarif-format file that is bad JSON or fails the SARIF schema.
const ProblemSARIFSchema = "the SARIF did not pass the schema check"

func convertSARIF(m Manifest, outDir string) Converted {
	name := m.Output.Path
	data, err := ReadOutFile(outDir, name, maxSARIFFile)
	if err != nil {
		return Converted{Problem: problemMissing(name)}
	}
	if _, err := jsonschema.Decode(data, maxSARIFFile, jsonDepth); err != nil || len(sarif.Validate(data)) > 0 {
		return Converted{Problem: ProblemSARIFSchema}
	}
	var log sarif.Log
	if err := json.Unmarshal(data, &log); err != nil || len(log.Runs) == 0 {
		return Converted{Problem: ProblemSARIFSchema}
	}
	run := log.Runs[0]
	if run.Results == nil {
		run.Results = []sarif.Result{}
	}
	// A sarif-format skill writes no run-metadata.json, so a valid file is a complete run.
	return Converted{Results: run.Results, Rules: run.Tool.Driver.Rules, Valid: true, RunStatus: "complete"}
}

// The fields of the skill's records that the converter reads. The schema check
// runs first, so a record with other fields has already been refused.
type cfRecord struct {
	Verdict     string    `json:"verdict"`
	Fingerprint string    `json:"fingerprint"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	Trace       []cfPlace `json:"trace"`
	Evidence    []cfPlace `json:"evidence"`
	Execution   *struct {
		AttackerPerspective string `json:"attacker_perspective"`
		ObservedResult      string `json:"observed_result"`
	} `json:"execution"`
	Remediation *struct {
		Strategy string `json:"strategy"`
	} `json:"remediation"`
	ValidationPlan *struct {
		Local      string `json:"local"`
		Deployment string `json:"deployment"`
	} `json:"validation_plan"`
	Severity *struct {
		Overall string `json:"overall_severity"`
	} `json:"severity"`
	Confidence *struct {
		Score string `json:"score"`
	} `json:"confidence"`
}

type cfPlace struct {
	Kind string `json:"kind"`
	File string `json:"file"`
	Line int    `json:"line"`
}

type cfUnit struct {
	CoverageID         string   `json:"coverage_id"`
	AttackClass        string   `json:"attack_class"`
	Status             string   `json:"status"`
	ResultFingerprints []string `json:"result_fingerprints"`
}

var severityMap = map[string]struct{ level, severity, score string }{
	"critical":      {"error", "critical", "9.5"},
	"high":          {"error", "high", "8.0"},
	"medium":        {"warning", "medium", "5.5"},
	"low":           {"note", "low", "3.0"},
	"informational": {"note", "low", "1.0"},
}

func convertCloudflare(skillDir, outDir string) Converted {
	// The ledger's coverage is kept with no valid findings.json too: a
	// partial scan still reports how far it came.
	units, coverage := readLedger(outDir)
	bad := Converted{Problem: problemSchema(findingsFile), Coverage: coverage}
	data, err := ReadOutFile(outDir, findingsFile, maxFindings)
	if err != nil {
		return Converted{Problem: problemMissing(findingsFile), Coverage: coverage}
	}
	raw, err := jsonschema.Decode(data, maxFindings, jsonDepth)
	if err != nil {
		return bad
	}
	// A missing or broken skill schema fails closed.
	schemaData, err := ReadOutFile(skillDir, schemaFile, maxSchema)
	if err != nil {
		return bad
	}
	schema, err := jsonschema.Compile(schemaData)
	if err != nil || len(schema.Validate(raw)) > 0 {
		return bad
	}
	var records []cfRecord
	if err := json.Unmarshal(data, &records); err != nil {
		return bad
	}

	out := Converted{Valid: true, Coverage: coverage, Results: []sarif.Result{}}
	out.RunStatus, out.IncompleteReason = readRunStatus(outDir)

	seen := map[string]bool{}
	ruleSeen := map[string]bool{}
	for _, r := range records {
		if r.Verdict != "confirmed" && r.Verdict != "needs_validation" {
			continue // a rejected record is not a result
		}
		if seen[r.Fingerprint] {
			continue // keep the first record of a fingerprint
		}
		seen[r.Fingerprint] = true
		ruleID := ruleFor(units, r.Fingerprint)
		if !ruleSeen[ruleID] {
			ruleSeen[ruleID] = true
			out.Rules = append(out.Rules, sarif.Rule{ID: ruleID, Properties: &sarif.RuleProperties{Tags: []string{"security", ruleID}}})
		}
		out.Results = append(out.Results, toResult(r, ruleID))
	}
	return out
}

func toResult(r cfRecord, ruleID string) sarif.Result {
	text := r.Title
	if strings.TrimSpace(text) == "" {
		text = r.Fingerprint
	}
	res := sarif.Result{
		RuleID:       ruleID,
		Level:        "warning",
		Message:      sarif.Message{Text: text},
		Fingerprints: map[string]string{fingerprintKey: r.Fingerprint},
		Properties:   map[string]any{},
	}
	setIf := func(k, v string) {
		if v != "" {
			res.Properties[k] = v
		}
	}
	setIf("description", r.Description)
	if r.Verdict == "needs_validation" {
		res.Properties["needs_validation"] = true
		if p := r.ValidationPlan; p != nil {
			setIf("fix", joinNonEmpty(p.Local, p.Deployment))
		}
	} else {
		if s, ok := severityMap[severityOf(r)]; ok {
			res.Level = s.level
			res.Properties["severity"] = s.severity
			res.Properties["security-severity"] = s.score
		}
		if e := r.Execution; e != nil {
			setIf("exploit_scenario", joinNonEmpty(e.AttackerPerspective, e.ObservedResult))
		}
		if rem := r.Remediation; rem != nil {
			setIf("fix", rem.Strategy)
		}
	}
	if r.Confidence != nil {
		setIf("confidence", r.Confidence.Score)
	}
	if p, ok := placeOf(r); ok && safePath(p.File) {
		loc := sarif.Location{PhysicalLocation: sarif.PhysicalLocation{
			ArtifactLocation: sarif.ArtifactLocation{URI: p.File, URIBaseID: "%SRCROOT%"},
		}}
		if p.Line >= 1 {
			loc.PhysicalLocation.Region = &sarif.Region{StartLine: p.Line}
		}
		res.Locations = []sarif.Location{loc}
	}
	return res
}

func severityOf(r cfRecord) string {
	if r.Severity == nil {
		return ""
	}
	return r.Severity.Overall
}

func joinNonEmpty(parts ...string) string {
	var keep []string
	for _, p := range parts {
		if p != "" {
			keep = append(keep, p)
		}
	}
	return strings.Join(keep, "\n\n")
}

// placeOf picks the sink of the trace, else the last trace entry, else the first evidence.
func placeOf(r cfRecord) (cfPlace, bool) {
	for _, p := range r.Trace {
		if p.Kind == "sink" {
			return p, true
		}
	}
	if n := len(r.Trace); n > 0 {
		return r.Trace[n-1], true
	}
	if len(r.Evidence) > 0 {
		return r.Evidence[0], true
	}
	return cfPlace{}, false
}

// safePath is false for a file the SARIF reader must not get: empty, absolute,
// with ".." or with a backslash. The result stays, without a location.
func safePath(p string) bool {
	return p != "" && !strings.HasPrefix(p, "/") && !strings.Contains(p, "..") &&
		!strings.Contains(p, `\`) && !strings.ContainsRune(p, 0)
}

// ruleFor returns the attack class of the first unit (by coverage_id) that lists the fingerprint.
func ruleFor(units []cfUnit, fp string) string {
	for _, u := range units {
		if u.AttackClass == "" {
			continue
		}
		for _, f := range u.ResultFingerprints {
			if f == fp {
				return u.AttackClass
			}
		}
	}
	return defaultRuleID
}

// readLedger reads the coverage ledger. A missing or bad ledger gives no units and a zero Coverage.
func readLedger(dir string) ([]cfUnit, Coverage) {
	data, err := ReadOutFile(dir, ledgerFile, maxLedger)
	if err != nil {
		return nil, Coverage{}
	}
	if _, err := jsonschema.Decode(data, maxLedger, jsonDepth); err != nil {
		return nil, Coverage{}
	}
	var units []cfUnit
	if err := json.Unmarshal(data, &units); err != nil {
		return nil, Coverage{}
	}
	sort.SliceStable(units, func(i, j int) bool { return units[i].CoverageID < units[j].CoverageID })
	var c Coverage
	for _, u := range units {
		switch u.Status {
		case "not_applicable", "out_of_scope":
			continue
		case "covered", "candidate":
			c.Done++
		}
		c.Planned++
	}
	return units, c
}

// readRunStatus reads run_status and incomplete_reason. A missing or bad file gives "".
func readRunStatus(dir string) (status, reason string) {
	data, err := ReadOutFile(dir, metadataFile, maxMetadata)
	if err != nil {
		return "", ""
	}
	if _, err := jsonschema.Decode(data, maxMetadata, jsonDepth); err != nil {
		return "", ""
	}
	var md struct {
		RunStatus        string `json:"run_status"`
		IncompleteReason string `json:"incomplete_reason"`
	}
	if err := json.Unmarshal(data, &md); err != nil {
		return "", ""
	}
	return md.RunStatus, md.IncompleteReason
}
