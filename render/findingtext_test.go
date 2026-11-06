package render

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

const joiner = "\u200d" // what the escaping puts inside a sequence that does something

// textCase is model text and what it must look like in the comment.
type textCase struct {
	name, in, want string
	// closed is what closeFences adds after the text, for a fence the scanner
	// and closeFences read differently.
	closed string
}

// defusedCases are single-line texts that must come out inert on every
// surface that shows model text.
var defusedCases = []textCase{
	{"mention", "@bob", "@" + joiner + "bob", ""},
	{"decimal entity", "&#64;bob", "&" + joiner + "#64;bob", ""},
	{"hex entity", "&#x40;bob", "&" + joiner + "#x40;bob", ""},
	{"named entity", "&commat;bob", "&" + joiner + "commat;bob", ""},
	{"image", "![x](http://e/p.png)", "!" + joiner + "[x]" + joiner + "(http://e/p.png)", ""},
	{"link", "[a](http://e)", "[a]" + joiner + "(http://e)", ""},
	{"wiki link", "[[wiki]]", "[" + joiner + "[wiki]]", ""},
	{"wiki link run", "[[[wiki]]]", "[" + joiner + "[" + joiner + "[wiki]]]", ""},
	{"html", "<img src=x>", "<" + joiner + "img src=x>", ""},
}

// trapCases are multi-line texts where a simpler scanner would take live text
// for code and leave "@bob" a mention.
var trapCases = []textCase{
	{"unclosed span shifts the pairing", "`a\n`p` @bob `q`", "`a\n`p` @" + joiner + "bob `q`", ""},
	{"unclosed span in a list", "- `a\n- `p` @bob `q`", "- `a\n- `p` @" + joiner + "bob `q`", ""},
	{"escaped backticks", "\\` @bob \\`", "\\` @" + joiner + "bob \\`", ""},
	{"indented line closes a list fence", "- ```\n  x\n  ```\n@bob", "- ```\n  x\n  ```\n@" + joiner + "bob", "\n```"},
	{"indented closer", "```\nx\n  ```\n@bob", "```\nx\n  ```\n@" + joiner + "bob", ""},
	{"backtick in the info string", "``` `x\n@bob", "``` `x\n@" + joiner + "bob", ""},
	{"tilde fence", "~~~\nx\n~~~\n@bob", "~~~\nx\n~~~\n@" + joiner + "bob", ""},
	{"text after a span", "`ok` @bob `x`\n@carol", "`ok` @" + joiner + "bob `x`\n@" + joiner + "carol", ""},
	{"text after a paragraph break", "`a\n\n`p` @bob `q`", "`a\n\n`p` @" + joiner + "bob `q`", ""},
	// Markdown ends a paragraph only at a line of spaces and tabs; a line
	// with a no-break space continues it, so the spans pair across it.
	{"no-break space line", "`a\n\u00a0\n`x@bob` y", "`a\n\u00a0\n`x@" + joiner + "bob` y", ""},
	{"image after a fence", "```\nx\n```\n![x](http://e/p.png)", "```\nx\n```\n!" + joiner + "[x]" + joiner + "(http://e/p.png)", ""},
}

// keptCases are texts that stay byte for byte: code, formatting and links.
var keptCases = []textCase{
	{"fence", "```go\n@Override\nMap<String>\n```", "```go\n@Override\nMap<String>\n```", ""},
	{"tilde fence", "~~~\n@x\n~~~", "~~~\n@x\n~~~", ""},
	{"code span", "`@Override` and `a<b`", "`@Override` and `a<b`", ""},
	{"code span image", "`![x](u)`", "`![x](u)`", ""},
	{"formatting", "**bold** and\n- a list\n> quote", "**bold** and\n- a list\n> quote", ""},
	{"bare url", "see https://example.com/a?b=1", "see https://example.com/a?b=1", ""},
	{"url with ampersands", "https://e.com/?a=1&b=2&c=x_y", "https://e.com/?a=1&b=2&c=x_y", ""},
	{"ampersand in prose", "R&D and a && b", "R&D and a && b", ""},
	{"entity after a url", "https://e.com/?a=1&b=2 &#64;bob", "https://e.com/?a=1&b=2 &" + joiner + "#64;bob", ""},
	{"double backtick span", "``a ` b`` @x", "``a ` b`` @" + joiner + "x", ""},
	{"span and text", "use `@Override` and ping @bob", "use `@Override` and ping @" + joiner + "bob", ""},
	{"fence then span", "```\n@a\n```\n`@b` @c", "```\n@a\n```\n`@b` @" + joiner + "c", ""},
}

func allTextCases() []textCase {
	var all []textCase
	for _, cs := range [][]textCase{defusedCases, trapCases, keptCases} {
		all = append(all, cs...)
	}
	return all
}

// fixAfter is what follows "**Fix:**" for a fix that is want.
func fixAfter(want string) string {
	if strings.Contains(want, "\n") {
		return "\n\n" + want
	}
	return " " + want
}

func TestModelTextDefusesLiveText(t *testing.T) {
	for _, tc := range allTextCases() {
		t.Run(tc.name, func(t *testing.T) {
			if got := modelText(tc.in); got != tc.want+tc.closed {
				t.Errorf("modelText(%q)\n got %q\nwant %q", tc.in, got, tc.want+tc.closed)
			}
		})
	}
}

func TestFindingTextInSummaryComment(t *testing.T) {
	for _, tc := range allTextCases() {
		t.Run(tc.name, func(t *testing.T) {
			s := func(f Finding) []Skill {
				f.File, f.Line, f.Severity, f.Title = "a.go", 1, Blocking, "T"
				return []Skill{{Name: "s", Verdict: VerdictHasBlockingIssues, Findings: []Finding{f}}}
			}
			got := Comment(s(Finding{Explanation: tc.in}), Options{})
			if want := "</summary>\n\n" + tc.want + tc.closed + "\n\n"; !strings.Contains(got, want) {
				t.Errorf("explanation: comment lacks %q:\n%s", want, got)
			}
			got = Comment(s(Finding{SuggestedFix: tc.in}), Options{})
			if want := "**Fix:**" + fixAfter(tc.want+tc.closed) + "\n\n"; !strings.Contains(got, want) {
				t.Errorf("fix: comment lacks %q:\n%s", want, got)
			}
		})
	}
}

func TestFindingTextInInlineComment(t *testing.T) {
	for _, tc := range allTextCases() {
		t.Run(tc.name, func(t *testing.T) {
			f := Finding{Fingerprint: "fp1", Severity: Blocking, Title: "T"}
			f.Explanation = tc.in
			got := InlineComment(f, InlineOptions{})
			if want := "**\n" + tc.want + tc.closed + "\n\n"; !strings.Contains(got, want) {
				t.Errorf("explanation: comment lacks %q:\n%s", want, got)
			}
			f.Explanation, f.SuggestedFix = "", tc.in
			got = InlineComment(f, InlineOptions{})
			if want := "**Fix:**" + fixAfter(tc.want+tc.closed) + "\n\n"; !strings.Contains(got, want) {
				t.Errorf("fix: comment lacks %q:\n%s", want, got)
			}
		})
	}
}

func TestFindingTitleInInlineCommentIsText(t *testing.T) {
	for _, tc := range defusedCases {
		t.Run(tc.name, func(t *testing.T) {
			f := Finding{Fingerprint: "fp1", Severity: Blocking, Title: tc.in}
			if got, want := InlineComment(f, InlineOptions{}), "**❌ Blocking: "+tc.want+"**\n"; !strings.HasPrefix(got, want) {
				t.Errorf("comment starts %q, want prefix %q", head(got), want)
			}
		})
	}
}

func TestDismissedTitleIsText(t *testing.T) {
	for _, tc := range defusedCases {
		t.Run(tc.name, func(t *testing.T) {
			s := Skill{Name: "s", Verdict: VerdictNoBlockingIssues, Dismissed: []DismissedFinding{
				{Finding: Finding{ID: "abc123", Severity: Suggestion, Title: tc.in}, By: "eve"},
			}}
			got := Comment([]Skill{s}, Options{})
			if want := "- **" + tc.want + "** id `abc123` — dismissed by eve\n"; !strings.Contains(got, want) {
				t.Errorf("comment lacks %q:\n%s", want, got)
			}
		})
	}
}

func TestConfidenceReasonIsText(t *testing.T) {
	for _, tc := range defusedCases {
		t.Run(tc.name, func(t *testing.T) {
			s := Skill{Name: "s", Verdict: VerdictNoBlockingIssues, Confidence: 4, ConfidenceReason: tc.in}
			got := Comment([]Skill{s}, Options{})
			if want := "\n> " + tc.want + "\n\n"; !strings.Contains(got, want) {
				t.Errorf("comment lacks %q:\n%s", want, got)
			}
		})
	}
}

func TestSummaryRowTitleBreaksMentions(t *testing.T) {
	title := "ping @bob &#64;bob <img src=x> ![x](u)"
	want := "ping @" + joiner + "bob &amp;#64;bob &lt;img src=x&gt; ![x](u)"
	for _, name := range []string{"details row", "title-only row"} {
		t.Run(name, func(t *testing.T) {
			s := Skill{Name: "s", Verdict: VerdictNoBlockingIssues, Findings: []Finding{
				{ID: "abc123", File: "a.go", Line: 3, Severity: Suggestion, Title: title, Explanation: strings.Repeat("e", 500)},
			}}
			skills := []Skill{s}
			l := layout{noHidden: true, listLimit: -1}
			opening := "<details><summary>💡 <b>"
			if name == "title-only row" {
				l.suggestionTitles = true
				opening = "<li>💡 <b>"
			}
			got := render(skills, Options{}, l)
			if !strings.Contains(got, opening+want+"</b>") {
				t.Errorf("title not escaped and mention-safe:\n%s", got)
			}
			if strings.Contains(got, "@bob") {
				t.Errorf("a mention survived:\n%s", got)
			}
		})
	}
	// Ordinary titles read as written.
	if got := escapeSummary("Write to a nil map"); got != "Write to a nil map" {
		t.Errorf("escapeSummary changed a plain title: %q", got)
	}
}

func TestOrdinaryFindingTextIsUnchanged(t *testing.T) {
	text := "The map is never initialised, so `seen[k]++` panics.\n\n- first: use `make(map[string]int)`\n- second: see https://example.com/docs?a=1\n\n> note\n\n```go\nseen := make(map[string]int)\n```"
	s := Skill{Name: "s", Verdict: VerdictHasBlockingIssues, Findings: []Finding{
		{File: "a.go", Line: 1, Severity: Blocking, Title: "Write to a nil map", Explanation: text, SuggestedFix: text},
	}}
	got := Comment([]Skill{s}, Options{})
	for _, want := range []string{"</summary>\n\n" + text + "\n\n**Fix:**\n\n" + text + "\n\n"} {
		if !strings.Contains(got, want) {
			t.Errorf("comment changed ordinary text:\n%s", got)
		}
	}
	f := Finding{Fingerprint: "fp1", Severity: Blocking, Title: "Write to a nil map", Explanation: text, SuggestedFix: text}
	if got := InlineComment(f, InlineOptions{}); !strings.Contains(got, "**\n"+text+"\n\n**Fix:**\n\n"+text+"\n\n") {
		t.Errorf("inline comment changed ordinary text:\n%s", got)
	}
}

func TestHiddenBlockKeepsOriginalText(t *testing.T) {
	f := Finding{
		Fingerprint: "fp1", File: "a.go", Line: 1, Severity: Blocking,
		Title:        "ping @bob <img src=x> ![x](u)",
		Explanation:  "cc @bob &#64;bob [[wiki]] <!-- x -->\n`a\n`p` @carol `q`",
		SuggestedFix: "[a](http://e) @dave",
	}
	got := Comment([]Skill{{Name: "s", Verdict: VerdictHasBlockingIssues, Findings: []Finding{f}}}, Options{})
	line := strings.TrimSpace(got[strings.LastIndex(got, "<!-- pergent:findings ")+len("<!-- pergent:findings "):])
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSuffix(line, " -->"))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	var block struct {
		Skills map[string][]blockFinding `json:"skills"`
	}
	if err := json.Unmarshal(raw, &block); err != nil || len(block.Skills["s"]) != 1 {
		t.Fatalf("block = %s (%v)", raw, err)
	}
	b := block.Skills["s"][0]
	if b.Title != f.Title || b.Explanation != f.Explanation || b.SuggestedFix != f.SuggestedFix {
		t.Errorf("the hidden block changed the text: %+v", b)
	}
	if strings.Contains(string(raw), joiner) {
		t.Errorf("the hidden block carries a joiner: %s", raw)
	}
}

// cellCases are texts for a check table cell. "<" is escaped the way the cell
// always did, and "|" and an unbalanced backtick can't leave the cell.
var cellCases = []textCase{
	{"mention", "@bob", "@" + joiner + "bob", ""},
	{"decimal entity", "&#64;bob", "&" + joiner + "#64;bob", ""},
	{"hex entity", "&#x40;bob", "&" + joiner + "#x40;bob", ""},
	{"named entity", "&commat;bob", "&" + joiner + "commat;bob", ""},
	{"image", "![x](u)", "!" + joiner + "[x]" + joiner + "(u)", ""},
	{"link", "[a](u)", "[a]" + joiner + "(u)", ""},
	{"wiki link", "[[w]]", "[" + joiner + "[w]]", ""},
	{"html", "<img src=x>", "&lt;img src=x>", ""},
	{"pipe", "a|b", "a\\|b", ""},
	{"backtick", "a`b", "a\\`b", ""},
	{"code", "`ok()` here", "`ok()` here", ""},
	{"line breaks", "one\ntwo", "one two", ""},
}

func TestCheckCellIsText(t *testing.T) {
	for _, tc := range cellCases {
		t.Run(tc.name, func(t *testing.T) {
			s := Skill{Name: "s", Verdict: VerdictHasBlockingIssues, Checks: []BatchChecks{{Batch: 1, Checks: []Check{
				{Section: tc.in, Name: tc.in, Status: CheckFail, Detail: tc.in},
			}}}}
			got := Comment([]Skill{s}, Options{})
			if want := "| " + tc.want + " | " + tc.want + " | ❌ | " + tc.want + " |\n"; !strings.Contains(got, want) {
				t.Errorf("comment lacks %q:\n%s", want, got)
			}
			// The same cell in the collapsed table.
			s.Checks[0].Checks[0].Status = CheckPass
			got = Comment([]Skill{s}, Options{})
			if want := "| " + tc.want + " | " + tc.want + " | ✅ | " + tc.want + " |\n"; !strings.Contains(got, want) {
				t.Errorf("collapsed: comment lacks %q:\n%s", want, got)
			}
		})
	}
}

func TestFailureTextIsInert(t *testing.T) {
	const prefix = "⚠️ **Review failed:** "
	for _, tc := range []struct{ name, message, lead, want string }{
		{"message", "ping @bob &#64;bob ![x](u) [a](u) [[w]] <img src=x>", "",
			prefix + "ping @" + joiner + "bob &" + joiner + "#64;bob !" + joiner + "[x]" + joiner + "(u) [a]" + joiner + "(u) [" + joiner + "[w]] <" + joiner + "img src=x>\n\n"},
		{"message comment", "<!-- pergent:findings x -->", "", prefix + "<" + joiner + "!-- pergent:findings x -->\n\n"},
		{"lead", "", "Not reviewed: @bob &commat;bob ![x](u) [a](u) [[w]] <img src=x>",
			prefix + "Not reviewed: @" + joiner + "bob &" + joiner + "commat;bob !" + joiner + "[x]" + joiner + "(u) [a]" + joiner + "(u) [" + joiner + "[w]] <" + joiner + "img src=x>\n\n"},
		{"lead keeps code", "", "Not reviewed: `@bob.go` and `a<b`, ping @bob",
			prefix + "Not reviewed: `@bob.go` and `a<b`, ping @" + joiner + "bob\n\n"},
		{"lead keeps a url", "", "see https://e.com/?a=1&b=2", prefix + "see https://e.com/?a=1&b=2\n\n"},
		{"lead stderr block", "", "Failed @bob\n\nLast lines of opencode's stderr:\n\n```text\n@bob <b> ![x](u)\n```\n",
			prefix + "Failed @" + joiner + "bob\n\nLast lines of opencode's stderr:\n\n```text\n@bob <b> ![x](u)\n```\n\n"},
		// The fence in the lead's first line follows the prefix, so it opens nothing.
		{"lead fence after the prefix", "", "```\n@bob", prefix + "```\n@" + joiner + "bob\n\n"},
		{"lead carriage return", "", "``\r`\n@bob", prefix + "```\n@" + joiner + "bob\n\n"},
		{"lead unclosed fence", "", "x\n\n```text\n@bob", prefix + "x\n\n```text\n@bob\n```\n\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := Skill{Name: "s", Verdict: VerdictFailed, Failures: []Failure{{Message: tc.message, Lead: tc.lead}}}
			got := Comment([]Skill{s}, Options{})
			if !strings.Contains(got, tc.want) {
				t.Errorf("comment lacks %q:\n%s", tc.want, got)
			}
		})
	}
}

func TestFailureLeadWithSeveralSkillsKeepsTheirNames(t *testing.T) {
	s := Skill{Name: "s", Verdict: VerdictFailed, Batches: 2, Failures: []Failure{{Files: []string{"@bob.go"}, Lead: "no @bob"}}}
	got := Comment([]Skill{s, cleanSkill("t", 0)}, Options{})
	if want := "⚠️ `s`: Files `@bob.go`: **Review failed:** no @" + joiner + "bob\n\n"; !strings.Contains(got, want) {
		t.Errorf("comment lacks %q:\n%s", want, got)
	}
}
