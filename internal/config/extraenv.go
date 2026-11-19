package config

import (
	"fmt"
	"regexp"
	"strings"
)

// ExtraEnvVar names the variable that lists extra environment variables
// opencode may inherit in a run with a generated config.
const ExtraEnvVar = "PERGENT_OPENCODE_ENV"

var envName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// neverExtraEnv are names the list may not carry: pergent removes or sets
// them itself, and the list must not reopen that.
var neverExtraEnv = map[string]bool{
	"GITHUB_TOKEN": true, "GITLAB_TOKEN": true, "PWD": true,
	"XDG_DATA_HOME": true, "XDG_STATE_HOME": true,
}

// ExtraEnvNames parses the value of PERGENT_OPENCODE_ENV: exact variable
// names separated by commas, spaces around a name trimmed, empty items
// skipped. A name that is not a plain variable name, starts with OPENCODE_ or
// PERGENT_, or is one pergent strips or sets itself is an error.
func ExtraEnvNames(value string) ([]string, error) {
	var names []string
	for _, item := range strings.Split(value, ",") {
		name := strings.TrimSpace(item)
		if name == "" {
			continue
		}
		upper := strings.ToUpper(name) // refused in any letter case
		switch {
		case !envName.MatchString(name):
			return nil, fmt.Errorf("%s: %q is not a variable name", ExtraEnvVar, name)
		case strings.HasPrefix(upper, "OPENCODE_") || strings.HasPrefix(upper, "PERGENT_"):
			return nil, fmt.Errorf("%s: %q is pergent's or opencode's own and cannot be passed through", ExtraEnvVar, name)
		case neverExtraEnv[upper]:
			return nil, fmt.Errorf("%s: %q is removed or set by pergent and cannot be passed through", ExtraEnvVar, name)
		}
		names = append(names, name)
	}
	return names, nil
}
