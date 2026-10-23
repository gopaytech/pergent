// Package redact removes credentials from text before pergent posts or logs it.
package redact

import (
	"os"
	"regexp"
	"strings"
)

const placeholder = "[REDACTED]"

// secretEnvVars hold credentials whose values must never reach a comment or log.
var secretEnvVars = []string{"OPENCODE_API_KEY", "GITHUB_TOKEN", "GITLAB_TOKEN"}

// minSecretLen keeps very short values (e.g. a placeholder like "x" in a local
// run) from blanking out ordinary words. Real keys and tokens are far longer.
const minSecretLen = 8

// bearerRe matches "Bearer <credential>". Only candidates that look like a
// credential (see looksLikeToken) are replaced, so prose such as "uses a bearer
// token" in a review is left alone.
var bearerRe = regexp.MustCompile(`(?i)(\bbearer\s+)([A-Za-z0-9\-._~+/]+=*)`)

var patterns = []struct {
	re   *regexp.Regexp
	repl string
}{
	// Credential headers echoed back in provider error bodies. "Authorization"
	// is left to the bearer rule: the word alone is common in review text.
	{regexp.MustCompile(`(?i)(\b(?:x-api-key|api-key|private-token)["']?\s*[:=]\s*["']?)[^\s"',;}]+`), "${1}" + placeholder},
	// Keys passed in URLs, e.g. ...?key=AIza... or &access_token=...
	{regexp.MustCompile(`(?i)([?&](?:key|api_key|apikey|access_token|token)=)[^&\s"']+`), "${1}" + placeholder},
}

// String returns s with the values of the credential environment variables,
// bearer tokens and similar credentials replaced by [REDACTED].
func String(s string) string {
	for _, name := range secretEnvVars {
		if v := os.Getenv(name); len(v) >= minSecretLen {
			s = strings.ReplaceAll(s, v, placeholder)
		}
	}
	s = bearerRe.ReplaceAllStringFunc(s, func(m string) string {
		sub := bearerRe.FindStringSubmatch(m)
		if !looksLikeToken(sub[2]) {
			return m
		}
		return sub[1] + placeholder
	})
	for _, p := range patterns {
		s = p.re.ReplaceAllString(s, p.repl)
	}
	return s
}

func looksLikeToken(s string) bool {
	return len(s) >= 12 && strings.ContainsAny(s, "0123456789")
}
