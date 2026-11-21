package main

import (
	"fmt"
	"io"
	"os"

	"github.com/gopaytech/pergent/rule"
)

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
