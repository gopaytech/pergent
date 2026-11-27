// Package scan holds the pieces of "pergent scan": the skill manifest,
// the opencode config, the output converter and the command itself.
package scan

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	// ManifestFile is the manifest's file name inside a skill folder.
	ManifestFile = "pergent-scan.yaml"
	// FormatSARIF and FormatCloudflare are the output formats a skill can write.
	FormatSARIF      = "sarif"
	FormatCloudflare = "cloudflare-findings"
)

const (
	minSteps       = 5
	maxMainSteps   = 1000
	maxSubSteps    = 300
	maxTaskBytes   = 2000
	maxStateFiles  = 8
	maxManifestLen = 64 << 10
)

var (
	nameRe        = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)
	commitRe      = regexp.MustCompile(`^[0-9a-f]{40}$`)
	placeholderRe = regexp.MustCompile(`\{[^{}\s]*\}`)
	knownTools    = map[string]bool{"bash": true, "write": true, "task": true}
	knownHolders  = map[string]bool{"{skill}": true, "{target}": true, "{commit}": true, "{output}": true, "{prior}": true}
)

// Steps are the step limits of the main agent and of each subagent.
type Steps struct {
	Main     int `yaml:"main"`
	Subagent int `yaml:"subagent"`
}

// ManifestOutput names the file the skill writes and its format.
type ManifestOutput struct {
	Path   string `yaml:"path"`
	Format string `yaml:"format"`
}

// Manifest is a parsed and checked pergent-scan.yaml.
type Manifest struct {
	Name   string         `yaml:"name"`
	Commit string         `yaml:"commit"`
	Entry  string         `yaml:"entry"`
	Tools  []string       `yaml:"tools"`
	Steps  Steps          `yaml:"steps"`
	Task   string         `yaml:"task"`
	Output ManifestOutput `yaml:"output"`
	State  []string       `yaml:"state"`
}

// TaskValues fill the placeholders of the task text.
type TaskValues struct {
	Skill, Target, Commit, Output, Prior string
}

// ParseManifest reads a manifest and checks every field. Unknown keys are an error.
func ParseManifest(data []byte) (Manifest, error) {
	var m Manifest
	if len(data) > maxManifestLen {
		return m, errors.New("manifest is too large")
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&m); err != nil {
		return Manifest{}, fmt.Errorf("manifest: %w", err)
	}
	var extra yaml.Node
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		return Manifest{}, errors.New("manifest: more than one YAML document")
	}
	if err := m.validate(); err != nil {
		return Manifest{}, fmt.Errorf("manifest: %w", err)
	}
	return m, nil
}

// LoadManifest reads dir/pergent-scan.yaml. The manifest name must be the
// folder name, and the entry must be a regular file in dir, not a link.
func LoadManifest(dir string) (Manifest, error) {
	file := filepath.Join(dir, ManifestFile)
	if err := regularFile(file); err != nil {
		return Manifest{}, err
	}
	data, err := os.ReadFile(file)
	if err != nil {
		return Manifest{}, err
	}
	m, err := ParseManifest(data)
	if err != nil {
		return Manifest{}, err
	}
	if base := filepath.Base(filepath.Clean(dir)); m.Name != base {
		return Manifest{}, fmt.Errorf("manifest name %q is not the folder name %q", m.Name, base)
	}
	if err := regularFile(filepath.Join(dir, m.Entry)); err != nil {
		return Manifest{}, err
	}
	return m, nil
}

// TaskLine returns the task text with the placeholders filled in.
// An empty Prior becomes "none". Values are not searched for placeholders again.
func (m Manifest) TaskLine(v TaskValues) string {
	prior := v.Prior
	if prior == "" {
		prior = "none"
	}
	return strings.NewReplacer(
		"{skill}", v.Skill,
		"{target}", v.Target,
		"{commit}", v.Commit,
		"{output}", v.Output,
		"{prior}", prior,
	).Replace(m.Task)
}

func regularFile(path string) error {
	fi, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !fi.Mode().IsRegular() {
		return fmt.Errorf("%s is not a regular file", filepath.Base(path))
	}
	return nil
}

func (m Manifest) validate() error {
	if !nameRe.MatchString(m.Name) {
		return fmt.Errorf("name %q must match %s", m.Name, nameRe)
	}
	if !commitRe.MatchString(m.Commit) {
		return errors.New("commit must be 40 lower case hex characters")
	}
	if !plainName(m.Entry) {
		return fmt.Errorf("entry %q must be a plain file name", m.Entry)
	}
	seen := map[string]bool{}
	hasTask := false
	for _, t := range m.Tools {
		if !knownTools[t] {
			return fmt.Errorf("tool %q is not allowed", t)
		}
		if seen[t] {
			return fmt.Errorf("tool %q is listed twice", t)
		}
		seen[t] = true
		hasTask = hasTask || t == "task"
	}
	if m.Steps.Main < minSteps || m.Steps.Main > maxMainSteps {
		return fmt.Errorf("steps.main must be %d to %d", minSteps, maxMainSteps)
	}
	if hasTask && (m.Steps.Subagent < minSteps || m.Steps.Subagent > maxSubSteps) {
		return fmt.Errorf("steps.subagent must be %d to %d", minSteps, maxSubSteps)
	}
	if !hasTask && m.Steps.Subagent != 0 && (m.Steps.Subagent < minSteps || m.Steps.Subagent > maxSubSteps) {
		return fmt.Errorf("steps.subagent must be %d to %d", minSteps, maxSubSteps)
	}
	if len(m.Task) > maxTaskBytes {
		return fmt.Errorf("task is over %d bytes", maxTaskBytes)
	}
	for _, p := range []string{"{target}", "{commit}", "{output}"} {
		if !strings.Contains(m.Task, p) {
			return fmt.Errorf("task must contain %s", p)
		}
	}
	for _, p := range placeholderRe.FindAllString(m.Task, -1) {
		if !knownHolders[p] {
			return fmt.Errorf("task has an unknown placeholder %s", p)
		}
	}
	if !plainName(m.Output.Path) {
		return fmt.Errorf("output.path %q must be a plain file name", m.Output.Path)
	}
	if m.Output.Format != FormatSARIF && m.Output.Format != FormatCloudflare {
		return fmt.Errorf("output.format %q must be %s or %s", m.Output.Format, FormatSARIF, FormatCloudflare)
	}
	if len(m.State) > maxStateFiles {
		return fmt.Errorf("state has more than %d files", maxStateFiles)
	}
	files := map[string]bool{}
	for _, s := range m.State {
		if !plainName(s) {
			return fmt.Errorf("state file %q must be a plain file name", s)
		}
		if files[s] {
			return fmt.Errorf("state file %q is listed twice", s)
		}
		files[s] = true
	}
	return nil
}

// plainName reports whether s is a base name: no slash, not "." or "..".
func plainName(s string) bool {
	return s != "" && s != "." && s != ".." && !strings.ContainsAny(s, `/\`) && !strings.ContainsRune(s, 0)
}
