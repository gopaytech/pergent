package review

import (
	"os"
	"strconv"
	"strings"

	"github.com/gopaytech/pergent/internal/diff"
	"github.com/gopaytech/pergent/internal/skill"
	"github.com/gopaytech/pergent/rule"
)

// ruleFor is the rule a skill's runs use for path: none when the skill has
// rules off (rules: false) or the request has no resolver.
func (r *reviewer) ruleFor(s skill.Skill, path string) (rule.Entry, bool) {
	if s.NoRules {
		return rule.Entry{}, false
	}
	return r.req.Rules.Resolve(path)
}

// ruleExtra counts each file's rule in batch sizing: one copy of a rule per
// batch, however many of its files the batch has. Nil when the skill gets no
// rules.
func (r *reviewer) ruleExtra(s skill.Skill) diff.Extra {
	if s.NoRules || r.req.Rules == nil {
		return nil
	}
	return func(path string) (string, int) {
		e, ok := r.ruleFor(s, path)
		if !ok {
			return "", 0
		}
		return e.Label(), diff.EstimateTokens(e.Rule)
	}
}

// fileSize is a file's tokens in a batch: its diff, plus its rule when it has
// one (withRule).
func (r *reviewer) fileSize(s skill.Skill, f diff.FileDiff) (tokens int, withRule bool) {
	tokens = diff.EstimateTokens(f.Patch)
	if e, ok := r.ruleFor(s, f.Path); ok {
		return tokens + diff.EstimateTokens(e.Rule), true
	}
	return tokens, false
}

// rulesFile holds the rules of a run's files: a batch run's, or the files of
// the findings a verification run checks.
const rulesFile = "file-rules.md"

// rulesMessage tells a batch run what file-rules.md is. A rule never changes
// what is blocking.
const rulesMessage = "file-rules.md lists extra checks from the project for some files in this batch. Use them while you review those files. They do not change what is blocking: only the skill's Blocking section does."

// writeRules writes rulesFile; a test replaces it to make that fail.
var writeRules = os.WriteFile

// FileRule is the rule a reviewed file was checked with, by its Label().
type FileRule struct{ Path, Rule string }

// fileRules builds file-rules.md for paths: one section per rule, in the
// order of its first file, naming its files (quoted, so a path can't break a
// line) and holding its text. It also returns each path's rule label. Both
// are empty when no path has a rule.
func fileRules(paths []string, resolve func(string) (rule.Entry, bool)) (text string, labels map[string]string) {
	type section struct {
		e     rule.Entry
		files []string
	}
	var sections []*section
	byLabel := map[string]*section{}
	labels = map[string]string{}
	for _, p := range paths {
		e, ok := resolve(p)
		if !ok {
			continue
		}
		label := e.Label()
		labels[p] = label
		sec := byLabel[label]
		if sec == nil {
			sec = &section{e: e}
			byLabel[label] = sec
			sections = append(sections, sec)
		}
		sec.files = append(sec.files, strconv.Quote(p))
	}
	parts := make([]string, len(sections))
	for i, sec := range sections {
		parts[i] = "## Rule: " + sec.e.Heading() + "\nFiles: " + strings.Join(sec.files, ", ") +
			"\n\n" + strings.TrimRight(sec.e.Rule, "\n") + "\n"
	}
	return strings.Join(parts, "\n"), labels
}

// rulesOf is fileRules over the skill's rules.
func (r *reviewer) rulesOf(s skill.Skill, paths []string) (string, map[string]string) {
	return fileRules(paths, func(p string) (rule.Entry, bool) { return r.ruleFor(s, p) })
}
