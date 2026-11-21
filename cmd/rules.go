package main

import (
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/gopaytech/pergent/internal/config"
	"github.com/gopaytech/pergent/rule"
)

// errRulesUnreadable is baseRules' error when the base branch can't be read
// at all (a shallow clone, no remote, no base branch): CI mode then reviews
// with the built-ins only and says so. CI mode never approves, so this is
// not a false pass.
var errRulesUnreadable = errors.New("repository rules could not be read")

// rulesUnreadableNote is the comment's note for errRulesUnreadable.
const rulesUnreadableNote = "Repository rules could not be read"

// baseRules reads .pergent/rules.yaml at ref (origin/<base branch>), never
// at the head: the change must not edit its own rules. A ref that can't be
// read is errRulesUnreadable; no file there is no entries; a file over the
// limit or malformed is the loader's error, which fails the review.
func baseRules(repoPath, ref string) ([]rule.Entry, error) {
	if ref == "" {
		return nil, fmt.Errorf("%w: no base branch is known", errRulesUnreadable)
	}
	git := func(args ...string) ([]byte, error) {
		return exec.Command("git", append([]string{"-C", repoPath}, args...)...).Output()
	}
	if _, err := git("rev-parse", "--verify", "--quiet", ref+"^{commit}"); err != nil {
		return nil, fmt.Errorf("%w: %s is not in the checkout", errRulesUnreadable, ref)
	}
	obj := ref + ":" + rule.RepoFile
	if _, err := git("cat-file", "-e", obj); err != nil {
		return nil, nil
	}
	out, err := git("cat-file", "-s", obj)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", errRulesUnreadable, err)
	}
	if n, err := strconv.Atoi(strings.TrimSpace(string(out))); err == nil && n > rule.MaxFileBytes {
		return nil, fmt.Errorf("%s: %d bytes, over the limit of %d (64 KB)", rule.RepoFile, n, rule.MaxFileBytes)
	}
	data, err := git("cat-file", "blob", obj)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", errRulesUnreadable, err)
	}
	return rule.Load(data, rule.SourceRepo)
}

// ciRules builds CI and local mode's rule list: the --rules list when one is
// given (rulesPath), else the base branch's .pergent/rules.yaml, then the
// built-ins unless they are off. When the base branch can't be read it logs
// a warning and returns the comment's note; any other error fails the review.
func ciRules(cfg config.Config, rulesPath string, logger *log.Logger) (*rule.Resolver, string, error) {
	builtIn := !cfg.NoBuiltInRules
	if rulesPath != "" {
		team, err := readRulesList(rulesPath)
		if err != nil {
			return nil, "", errors.New(rulesFlagError(rulesPath, err))
		}
		return rule.NewResolver(team, builtIn), "", nil
	}
	var branch string
	switch {
	case cfg.Local:
		branch = cfg.BaseBranch
	case cfg.Platform == "github":
		branch = cfg.GitHub.BaseBranch
	case cfg.Platform == "gitlab":
		branch = cfg.GitLab.BaseBranch
	}
	ref := ""
	if branch != "" {
		ref = "origin/" + branch
	}
	team, err := baseRules(cfg.RepoPath, ref)
	switch {
	case errors.Is(err, errRulesUnreadable):
		logger.Printf("warning: %v; reviewing with built-in rules only", err)
		return rule.NewResolver(nil, builtIn), rulesUnreadableNote, nil
	case err != nil:
		return nil, "", err
	}
	return rule.NewResolver(team, builtIn), "", nil
}

// readRulesList reads the rule list of --rules (agent mode, or CI mode in
// place of the base branch's file): the JSON rule.EncodeList writes. A list
// that can't be read or used is an error, never a review without the rules.
func readRulesList(path string) ([]rule.Entry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	// One byte over the limit is enough for DecodeList to refuse it.
	data, err := io.ReadAll(io.LimitReader(f, rule.MaxListBytes+1))
	if err != nil {
		return nil, err
	}
	return rule.DecodeList(data)
}

// rulesFlagError is how a bad --rules reads.
func rulesFlagError(path string, err error) string {
	return fmt.Sprintf("--rules %s: %v", path, err)
}

func onOff(on bool) string {
	if on {
		return "on"
	}
	return "off"
}
