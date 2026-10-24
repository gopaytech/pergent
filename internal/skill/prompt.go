package skill

const defaultBlockingSection = "## Blocking\n\n" +
	"Treat these as blocking: bugs, security vulnerabilities, and broken configuration. Everything else is a suggestion."

const outputFormatSection = "## Output format\n\n" +
	"End your final answer with exactly one fenced code block tagged `json`, with nothing before or after it, in this shape:\n\n" +
	"```json\n" +
	`{"summary": "What you checked and what you concluded, in 1-3 sentences.", "findings": [{"file": "path/from/repo/root", "line": 42, "severity": "blocking", "title": "One-line summary", "explanation": "Why it matters", "suggested_fix": "Optional fix"}]}` + "\n" +
	"```\n\n" +
	"- `summary` is 1-3 sentences on what you checked and what you concluded, including when you found nothing. Name the files or resources you looked at.\n" +
	"- `severity` is `blocking` or `suggestion`. Only problems described under Blocking are `blocking`; everything else is a `suggestion`.\n" +
	"- `line` is the line number in the new version of the file. Omit it when the finding is not about one line.\n" +
	"- `suggested_fix` is optional.\n" +
	"- If you find nothing, answer with your summary and `\"findings\": []`."

// Prompt returns the agent prompt: the skill body, the default Blocking
// section (only when the skill does not define its own), and pergent's fixed
// output-format instructions.
func (s Skill) Prompt() string {
	p := s.Body
	if !s.HasBlocking {
		p += "\n\n" + defaultBlockingSection
	}
	return p + "\n\n" + outputFormatSection + "\n"
}
