package runner

import "regexp"

// The context-overflow phrases below are copied from opencode v2.3.2,
// packages/ai/src/provider-error.ts (`patterns`, `exclusions` and the
// no-body status line in `isContextOverflow`). They are JS regexps with the
// `i` flag; each is written here as an RE2 expression with (?i), and none
// uses JS-only syntax. Keep them in step with that file when opencode is
// upgraded.
var overflowPatterns = compileAll(
	`prompt is too long`,
	`input is too long for (?:the )?(?:requested|selected) model`,
	// OpenCode Go
	`too large for the model'?s configured input limit`,
	// Cloudflare Workers AI reports this as HTTP 413.
	`exceeded this model context window limit`,
	`exceeds the context window`,
	`exceeds (?:the )?(?:model'?s )?maximum context length(?: of [\d,]+ tokens?|\s*\([\d,]+\))`,
	`input token count.*exceeds the maximum`,
	// Amazon Nova on Bedrock reports this as a mid-stream validationException.
	`number of input tokens exceeds maximum length`,
	`tokens in request more than max tokens allowed`,
	`maximum prompt length is \d+`,
	`reduce the length of the messages`,
	// DeepInfra
	`requested input length \d+ exceeds maximum input length`,
	`maximum context length is \d+ tokens`,
	`exceeds (?:the )?maximum allowed input length of [\d,]+ tokens?`,
	// Novita omits the token counts.
	`input(?: \(\d+ tokens\))? is longer than the model'?s context length`,
	`exceeds the limit of \d+`,
	`exceeds the available context size`,
	`greater than the context length`,
	// Hugging Face Text Generation Inference, e.g. Together
	"`inputs` tokens \\+ `max_new_tokens` must be <= \\d+",
	`context window exceeds limit`,
	// LiteLLM's ContextWindowExceededError
	`context window exceeded`,
	`exceeded model token limit`,
	`context[_ ]length[_ ]exceeded`,
	`context length is only \d+ tokens`,
	`input length.*exceeds.*context length`,
	// Z.ai code 1261
	`prompt (?:too long|exceeds max length|超长)`,
	`too large for model with \d+ maximum context length`,
	`prompt has [\d,]+ tokens?, but the configured context size is [\d,]+ tokens?`,
	`model_context_window_exceeded`,
	`range of input length should be`,
	`too many tokens`,
	`token limit exceeded`,
	`request_too_large`,
	// A bare 400 or 413 with no body.
	`^4(?:00|13)\s*(status code)?\s*\(no body\)`,
)

// overflowExclusions are throttling and output-limit messages that look like
// an overflow but are not (opencode v2.3.2 `exclusions`).
var overflowExclusions = compileAll(
	`^(throttling error|service unavailable):`,
	`rate limit`,
	`too many requests`,
	// Cohere reports an output limit above the model maximum as "too many tokens".
	`max[_ ]tokens must be less than`,
	// Bedrock phrases throttling as "Too many tokens[, per day], please wait...".
	`too many tokens(?: per day)?, please wait`,
)

func compileAll(exprs ...string) []*regexp.Regexp {
	out := make([]*regexp.Regexp, len(exprs))
	for i, e := range exprs {
		out[i] = regexp.MustCompile(`(?i)` + e)
	}
	return out
}

// ContextOverflow reports whether this error says the request was larger
// than the model's context window. opencode 2 gives such an error no type of
// its own: it is a provider.invalid-request (HTTP 400, or 413) whose message
// or response body says so. Throttling (429) and server errors are never an
// overflow, whatever their text says.
func (e EventError) ContextOverflow() bool {
	if e.StatusCode == 429 || (e.StatusCode != 0 && (e.StatusCode < 400 || e.StatusCode > 499)) {
		return false
	}
	text := e.Message + "\n" + e.Body
	for _, p := range overflowExclusions {
		if p.MatchString(text) {
			return false
		}
	}
	for _, p := range overflowPatterns {
		if p.MatchString(text) {
			return true
		}
	}
	return false
}
