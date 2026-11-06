package render

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

// textCase is model text and what it must look like in the comment.
type textCase struct{ name, in, want string }

// lineCases are texts for one line (a title, a reason, a note, a cell).
var lineCases = []textCase{
	{"mention", "@bob", "@" + joiner + "bob"},
	{"decimal entity", "&#64;bob", "&" + joiner + "#64;bob"},
	{"hex entity", "&#x40;bob", "&" + joiner + "#x40;bob"},
	{"named entity", "&commat;bob", "&" + joiner + "commat;bob"},
	{"image", "![x](http://e/p.png)", "!" + joiner + "[x]" + joiner + "(http://e/p.png)"},
	{"link", "[a](http://e)", joiner + "[a]" + joiner + "(http://e)"},
	{"reference link", "[a][r]", joiner + "[a]" + joiner + joiner + "[r]" + joiner},
	{"wiki link", "[[wiki]]", joiner + "[" + joiner + "[wiki]" + joiner + "]" + joiner},
	{"html", "<img src=x>", "<" + joiner + "img src=x>"},
	{"pipe", "a|b", "a|" + joiner + "b"},
	{"stray backtick", "a ` b", "a \\` b"},
	{"double backticks are text", "``a`` b", "\\`\\`a\\`\\` b"},
	{"edge space is no span", "` a`", "\\` a\\`"},
	{"escaped backtick", "\\` @bob \\`", "\\` @" + joiner + "bob \\`"},
	{"code span", "use `@Override` and `a<b|c`", "use `@Override` and `a<b|c`"},
	{"code span in parentheses and before punctuation", "(`a`), see `b`.", "(`a`), see `b`."},
	// A span is kept only where it starts after the line, a space, a tab or "(",
	// and ends before the line's end, a space, a tab or one of ").,:;!?";
	// otherwise a URL or math could take its backtick, and every backtick on
	// the line is stray.
	{"span in a url", "https://e.com/`a @bob` x", "https://e.com/\\`a @" + joiner + "bob\\` x"},
	{"span in a word", "x`a`", "x\\`a\\`"},
	{"span in bold", "**`a`**", "**\\`a\\`**"},
	{"span in math", "$`$ @bob`", "$\\`$ @" + joiner + "bob\\`"},
	{"span in inline math", "$x`y$ @bob`z`", "$x\\`y$ @" + joiner + "bob\\`z\\`"},
	{"one span too many", "`a` and x`b`", "\\`a\\` and x\\`b\\`"},
	{"escaped opener", "\\`@bob`", "\\`@" + joiner + "bob\\`"},
	{"backtick run after a span", "`a`` @bob", "\\`a\\`\\` @" + joiner + "bob"},
	{"escaped backslash before a backtick", "\\\\`@bob`", "\\\\\\`@" + joiner + "bob\\`"},
	{"doubled redaction token", "[[REDACTED]]", joiner + "[" + joiner + "[REDACTED]" + joiner + "]" + joiner},
	{"redaction token", "token [REDACTED] here", "token [REDACTED] here"},
	{"redaction token link", "[REDACTED](http://e)", joiner + "[REDACTED]" + joiner + "(http://e)"},
	{"redaction token definition", "[REDACTED]: http://e", joiner + "[REDACTED]" + joiner + ": http://e"},
	{"escaped brackets", "\\[x\\]", "\\[x\\]" + joiner},
	{"url with ampersands", "https://e.com/?a=1&b=2", "https://e.com/?a=1&b=2"},
	{"ampersand in prose", "R&D and a && b", "R&D and a && b"},
	{"plain", "Dropped `Foo.bar` (not real)", "Dropped `Foo.bar` (not real)"},
}

// blockCases are multi-line texts and what modelText makes of them. A fence
// comes out as pergent's own, which may differ from the one that went in: no
// info string, backticks, longer than any backtick run inside. Its content,
// and a code span, are byte for byte.
var blockCases = []textCase{
	{"fence", "```go\n@Override\nMap<String>\n```", "```\n@Override\nMap<String>\n```"},
	{"tilde fence", "~~~\n@x\n~~~", "```\n@x\n```"},
	{"tilde fence with an info string", "~~~ `a`\nx\n~~~", "```\nx\n```"},
	{"backticks in a tilde fence", "~~~\n```\n~~~", "````\n```\n````"},
	{"empty fence", "```\n```", "```\n```"},
	{"unclosed fence", "```\n@bob", "```\n@bob\n```"},
	{"indented fence", "x\n  ```\ny\n  ```\n@bob", "x\n\n```\ny\n```\n\n@" + joiner + "bob"},
	{"indent of the content", "x\n  ~~~\n    y\n  z\n  ~~~", "x\n\n```\n  y\nz\n```"},
	{"closer of another char", "```\n~~~\n```\n@bob", "```\n~~~\n```\n\n@" + joiner + "bob"},
	{"shorter closer", "````\n```\n````\n@bob", "````\n```\n````\n\n@" + joiner + "bob"},
	{"carriage return in a closer", "```\n@a\n``\r`\n@bob", "```\n@a\n```\n\n@" + joiner + "bob"},
	{"closer with a no-break space", "```\nx\n```\u00a0\n```\n@bob", "````\nx\n```\u00a0\n````\n\n@" + joiner + "bob"},
	{"info string with a backtick", "``` `x\n@bob", "\\`\\`\\` \\`x\n@" + joiner + "bob"},
	{"indent of the first line", "  ```\n  a\n  ```", "```\na\n```"},
	{"info string of two words", "~~~ not a fence in prose", "~" + joiner + "~" + joiner + "~" + joiner + " not a fence in prose"},
	{"info string of two words, backticks", "```go title=x\n@bob\n```", "\\`\\`\\`go title=x\n@" + joiner + "bob\n\n```\n```"},
	{"indented quote marker", "x\n  >>>", "x\n  >" + joiner + ">>"},
	{"indented four", "x\n    ```\n@bob", "x\n    \\`\\`\\`\n@" + joiner + "bob"},
	{"fence after text", "see\n```\ncode\n```\nand", "see\n\n```\ncode\n```\n\nand"},
	{"fence between blank lines", "see\n\n```\ncode\n```\n\nand", "see\n\n```\ncode\n```\n\nand"},
	{"quote lines", ">>>\n```\n>>>\n@bob\n```", ">" + joiner + ">>\n\n```\n>>>\n@bob\n```"},
	{"stray backticks", "- `a\n- `p` @bob `q`", "- \\`a\n- `p` @" + joiner + "bob `q`"},
	{"no-break space line", "`a\n\u00a0\n`x@bob` y", "\\`a\n\u00a0\n`x@bob` y"},
	{"code span", "`@Override` and `a<b`", "`@Override` and `a<b`"},
	{"code span image", "`![x](u)`", "`![x](u)`"},
	{"formatting", "**bold** and\n- a list\n> quote\n# heading\n1. one", "**bold** and\n- a list\n> quote\n# heading\n1. one"},
	{"bare url", "see https://example.com/a?b=1", "see https://example.com/a?b=1"},
	{"url with ampersands", "https://e.com/?a=1&b=2&c=x_y", "https://e.com/?a=1&b=2&c=x_y"},
	// An accepted cost: the joiner ends the autolink there.
	{"url with an at sign", "https://medium.com/@user/post", "https://medium.com/@" + joiner + "user/post"},
	{"entity after a url", "https://e.com/?a=1&b=2 &#64;bob", "https://e.com/?a=1&b=2 &" + joiner + "#64;bob"},
	{"definition", "[r]: http://e/l", joiner + "[r]" + joiner + ": http://e/l"},
	{"table", "| a | b |\n|---|---|", "|" + joiner + " a |" + joiner + " b |" + joiner + "\n|" + joiner + "---|" + joiner + "---|" + joiner},
}

func TestLineText(t *testing.T) {
	for _, tc := range lineCases {
		t.Run(tc.name, func(t *testing.T) {
			if got := lineText(tc.in); got != tc.want {
				t.Errorf("lineText(%q)\n got %q\nwant %q", tc.in, got, tc.want)
			}
		})
	}
	// Line breaks collapse.
	if got := lineText("a\n\n`b\nc"); got != "a \\`b c" {
		t.Errorf("lineText over lines = %q", got)
	}
}

// Text between pergent's own characters keeps a span only away from the edges
// of the line: "**`a`**" would put a backtick next to a star.
func TestWrappedText(t *testing.T) {
	for _, tc := range []textCase{
		{"span at the start", "`a` b", "\\`a\\` b"},
		{"span at the end", "x `a`", "x \\`a\\`"},
		{"span inside", "x `a` y", "x `a` y"},
		{"span before punctuation", "x `a`, y", "x `a`, y"},
		{"mention", "@bob", "@" + joiner + "bob"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := wrappedText(tc.in); got != tc.want {
				t.Errorf("wrappedText(%q)\n got %q\nwant %q", tc.in, got, tc.want)
			}
		})
	}
	f := Finding{Fingerprint: "fp1", Severity: Blocking, Title: "use `a`"}
	if got, want := InlineComment(f, InlineOptions{}), "**❌ Blocking: use \\`a\\`**\n\n"; !strings.HasPrefix(got, want) {
		t.Errorf("comment starts %q, want prefix %q", head(got), want)
	}
	s := hostileSkill(Finding{Explanation: "e"})
	if got, want := Comment(s, Options{FindingNote: map[string]string{"fp1": "see `a`"}}), "_see \\`a\\`_\n\ne"; !strings.Contains(got, want) {
		t.Errorf("comment lacks %q:\n%s", want, got)
	}
}

func TestCellText(t *testing.T) {
	for _, tc := range []textCase{
		{"pipe", "a|b", "a&#124;b"},
		{"pipe in a span", "`a|b`", "\\`a&#124;b\\`"},
		{"escaped pipe", "a\\|b", "a\\\\&#124;b"},
		{"escaped pipe in a span", "`a\\| @bob`", "\\`a\\\\&#124; @" + joiner + "bob\\`"},
		{"html", "<img src=x>", "<" + joiner + "img src=x>"},
		{"mention", "@bob", "@" + joiner + "bob"},
		{"entity", "&#124;", "&" + joiner + "#124;"},
		{"stray backtick", "a`b", "a\\`b"},
		{"no span", "`ok()` here", "\\`ok()\\` here"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := cellText(tc.in); got != tc.want {
				t.Errorf("cellText(%q)\n got %q\nwant %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestModelText(t *testing.T) {
	for _, tc := range append(append([]textCase{}, blockCases...), lineCases...) {
		t.Run(tc.name, func(t *testing.T) {
			if got := modelText(tc.in); got != tc.want {
				t.Errorf("modelText(%q)\n got %q\nwant %q", tc.in, got, tc.want)
			}
		})
	}
}

// fixAfter is what follows "**Fix:**" for a fix that is want.
func fixAfter(want string) string {
	if strings.Contains(want, "\n") {
		return "\n\n" + want
	}
	return " " + want
}

func TestFindingTextInSummaryComment(t *testing.T) {
	for _, tc := range append(append([]textCase{}, blockCases...), lineCases...) {
		t.Run(tc.name, func(t *testing.T) {
			got := Comment(hostileSkill(Finding{Explanation: tc.in}), Options{})
			if want := "</summary>\n\n" + tc.want + "\n\n"; !strings.Contains(got, want) {
				t.Errorf("explanation: comment lacks %q:\n%s", want, got)
			}
			got = Comment(hostileSkill(Finding{SuggestedFix: tc.in}), Options{})
			if want := "**Fix:**" + fixAfter(tc.want) + "\n\n"; !strings.Contains(got, want) {
				t.Errorf("fix: comment lacks %q:\n%s", want, got)
			}
		})
	}
}

func TestFindingTextInInlineComment(t *testing.T) {
	for _, tc := range append(append([]textCase{}, blockCases...), lineCases...) {
		t.Run(tc.name, func(t *testing.T) {
			f := Finding{Fingerprint: "fp1", Severity: Blocking, Title: "T", Explanation: tc.in}
			if got, want := InlineComment(f, InlineOptions{}), "**❌ Blocking: T**\n\n"+tc.want+"\n\n"; !strings.Contains(got, want) {
				t.Errorf("explanation: comment lacks %q:\n%s", want, got)
			}
			f.Explanation, f.SuggestedFix = "", tc.in
			if got, want := InlineComment(f, InlineOptions{}), "**Fix:**"+fixAfter(tc.want)+"\n\n"; !strings.Contains(got, want) {
				t.Errorf("fix: comment lacks %q:\n%s", want, got)
			}
		})
	}
}

func TestFindingTitleInInlineCommentIsText(t *testing.T) {
	for _, tc := range lineCases {
		if strings.Contains(tc.in, "`") {
			continue // a title is wrapped: see TestWrappedText
		}
		t.Run(tc.name, func(t *testing.T) {
			f := Finding{Fingerprint: "fp1", Severity: Blocking, Title: tc.in}
			if got, want := InlineComment(f, InlineOptions{}), "**❌ Blocking: "+tc.want+"**\n\n"; !strings.HasPrefix(got, want) {
				t.Errorf("comment starts %q, want prefix %q", head(got), want)
			}
		})
	}
}

func TestInlineTitleAndExplanationAreSeparateParagraphs(t *testing.T) {
	f := Finding{Fingerprint: "fp1", Severity: Blocking, Title: "a ` b", Explanation: "`x @bob` y"}
	got := InlineComment(f, InlineOptions{})
	if want := "**❌ Blocking: a \\` b**\n\n`x @bob` y\n\n"; !strings.HasPrefix(got, want) {
		t.Errorf("comment starts %q, want prefix %q", head(got), want)
	}
}

func TestFindingNoteAndExplanationAreSeparateParagraphs(t *testing.T) {
	s := hostileSkill(Finding{Explanation: "`x @bob` y"})
	got := Comment(s, Options{FindingNote: map[string]string{"fp1": "a ` b"}})
	if want := "</summary>\n\n_a \\` b_\n\n`x @bob` y\n\n"; !strings.Contains(got, want) {
		t.Errorf("comment lacks %q:\n%s", want, got)
	}
}

func TestDismissedTextIsInert(t *testing.T) {
	for _, tc := range lineCases {
		t.Run(tc.name, func(t *testing.T) {
			title, titleWant := tc.in, tc.want
			if strings.Contains(tc.in, "`") {
				title, titleWant = "T", "T" // a title is wrapped: see TestWrappedText
			}
			s := Skill{Name: "s", Verdict: VerdictNoBlockingIssues, Dismissed: []DismissedFinding{
				{Finding: Finding{ID: "abc123", Severity: Suggestion, Title: title}, By: tc.in, Reason: tc.in},
			}}
			got := Comment([]Skill{s}, Options{})
			want := "- **" + titleWant + "** id `abc123` — dismissed by " + tc.want + ": " + tc.want + "\n"
			if !strings.Contains(got, want) {
				t.Errorf("comment lacks %q:\n%s", want, got)
			}
		})
	}
}

func TestConfidenceReasonIsText(t *testing.T) {
	for _, tc := range lineCases {
		t.Run(tc.name, func(t *testing.T) {
			s := Skill{Name: "s", Verdict: VerdictNoBlockingIssues, Confidence: 4, ConfidenceReason: tc.in}
			got := Comment([]Skill{s}, Options{})
			if want := "\n> " + tc.want + "\n\n"; !strings.Contains(got, want) {
				t.Errorf("comment lacks %q:\n%s", want, got)
			}
		})
	}
}

func TestNoteIsText(t *testing.T) {
	for _, tc := range lineCases {
		t.Run(tc.name, func(t *testing.T) {
			s := Skill{Name: "s", Verdict: VerdictNoBlockingIssues, Notes: []string{tc.in}}
			got := Comment([]Skill{s}, Options{})
			if want := "\n> " + tc.want + "\n"; !strings.Contains(got, want) {
				t.Errorf("comment lacks %q:\n%s", want, got)
			}
		})
	}
}

func TestCheckCellIsText(t *testing.T) {
	for _, tc := range lineCases {
		t.Run(tc.name, func(t *testing.T) {
			want := cellText(tc.in)
			s := Skill{Name: "s", Verdict: VerdictHasBlockingIssues, Checks: []BatchChecks{{Batch: 1, Checks: []Check{
				{Section: tc.in, Name: tc.in, Status: CheckFail, Detail: tc.in},
			}}}}
			got := Comment([]Skill{s}, Options{})
			if row := "| " + want + " | " + want + " | ❌ | " + want + " |\n"; !strings.Contains(got, row) {
				t.Errorf("comment lacks %q:\n%s", row, got)
			}
			// Every pipe of the text is an entity, so the row has five pipes of
			// its own, whatever the text holds.
			for _, line := range strings.Split(got, "\n") {
				if strings.HasPrefix(line, "| "+want) && strings.Count(line, "|") != 5 {
					t.Errorf("row has a stray pipe: %q", line)
				}
			}
		})
	}
}

// A bare URL keeps its link where its characters are plain ("&" in a query
// too), and loses it where one of them is a delimiter pergent breaks. That
// cost is accepted: a link in model text is a link someone else chose.
func TestBareUrlsKeepOrLoseTheirLink(t *testing.T) {
	for _, in := range []string{
		"https://e.com/path?a=1&b=2", "https://e.com/a/b-c_d.html#frag", "http://e.com:8080/a~b(c)", "www.e.com/a",
	} {
		if got := modelText("see " + in + " now"); got != "see "+in+" now" {
			t.Errorf("%q changed: %q", in, got)
		}
	}
	for _, in := range []string{
		"https://medium.com/@user/post", "https://e.com/a?x[]=1", "http://[::1]:8080/", "https://e.com/a|b", "https://e.com/x?a=1&#frag",
	} {
		got := modelText("see " + in + " now")
		if !strings.Contains(got, joiner) || strings.ReplaceAll(got, joiner, "") != "see "+in+" now" {
			t.Errorf("%q: got %q, want it with joiners only", in, got)
		}
	}
}

func TestCodeSpan(t *testing.T) {
	for in, want := range map[string]string{
		"a.go": "`a.go`", "": "` `", " \u00a0 ": "` `", "a`b": "`` a`b ``", "`": "`` ` ``", "``a": "``` ``a ```",
	} {
		if got := codeSpan(in); got != want {
			t.Errorf("codeSpan(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestFailureTextIsInert(t *testing.T) {
	const prefix = "⚠️ **Review failed:** "
	for _, tc := range []struct{ name, message, lead, want string }{
		{"message", "ping @bob &#64;bob ![x](u) [a](u) [[w]] <img src=x>", "",
			prefix + "ping @" + joiner + "bob &" + joiner + "#64;bob !" + joiner + "[x]" + joiner + "(u) " + joiner + "[a]" + joiner + "(u) " + joiner + "[" + joiner + "[w]" + joiner + "]" + joiner + " <" + joiner + "img src=x>\n\n"},
		{"message comment", "<!-- pergent:findings x -->", "", prefix + "<" + joiner + "!-- pergent:findings x -->\n\n"},
		{"lead", "", "Not reviewed: @bob &commat;bob <img src=x>",
			prefix + "Not reviewed: @" + joiner + "bob &" + joiner + "commat;bob <" + joiner + "img src=x>\n\n"},
		{"lead keeps code", "", "Not reviewed: `@bob.go` and `a<b`, ping @bob",
			prefix + "Not reviewed: `@bob.go` and `a<b`, ping @" + joiner + "bob\n\n"},
		{"lead keeps a url", "", "see https://e.com/?a=1&b=2", prefix + "see https://e.com/?a=1&b=2\n\n"},
		// The fence follows the prefix on its line in the lead, so pergent starts it below.
		{"lead stderr block", "", "Failed @bob\n\nLast lines of opencode's stderr:\n\n```text\n@bob <b> ![x](u)\n```\n",
			prefix + "Failed @" + joiner + "bob\n\nLast lines of opencode's stderr:\n\n```\n@bob <b> ![x](u)\n```\n\n"},
		{"lead fence first", "", "```\n@bob", "⚠️ **Review failed:**\n\n```\n@bob\n```\n\n"},
		{"lead carriage return", "", "`a\r`b`\n@bob", prefix + "\\`a\\`b\\`\n@" + joiner + "bob\n\n"},
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

func TestSummaryRowTitleBreaksMentions(t *testing.T) {
	title := "ping @bob &#64;bob <img src=x> ![x](u) `code`"
	want := "ping @" + joiner + "bob &amp;#64;bob &lt;img src=x&gt; ![x](u) `code`"
	for _, name := range []string{"details row", "title-only row"} {
		t.Run(name, func(t *testing.T) {
			s := Skill{Name: "s", Verdict: VerdictNoBlockingIssues, Findings: []Finding{
				{ID: "abc123", File: "a.go", Line: 3, Severity: Suggestion, Title: title, Explanation: strings.Repeat("e", 500)},
			}}
			l := layout{noHidden: true, listLimit: -1}
			opening := "<details><summary>💡 <b>"
			if name == "title-only row" {
				l.suggestionTitles = true
				opening = "<li>💡 <b>"
			}
			got := render([]Skill{s}, Options{}, l)
			if !strings.Contains(got, opening+want+"</b>") {
				t.Errorf("title not escaped and mention-safe:\n%s", got)
			}
			if strings.Contains(got, "@bob") {
				t.Errorf("a mention survived:\n%s", got)
			}
		})
	}
	// An ordinary title reads as written.
	if got := findingTitleHTML("Write to a nil map"); got != "Write to a nil map" {
		t.Errorf("findingTitleHTML changed a plain title: %q", got)
	}
}

func TestOrdinaryFindingTextIsUnchanged(t *testing.T) {
	text := "The map is never initialised, so `seen[k]++` panics.\n\n- first: use `make(map[string]int)`\n- second: see https://example.com/docs?a=1&b=2\n\n> note\n\n```\nseen := make(map[string]int)\n```"
	s := hostileSkill(Finding{Title: "Write to a nil map", Explanation: text, SuggestedFix: text})
	got := Comment(s, Options{})
	if want := "</summary>\n\n" + text + "\n\n**Fix:**\n\n" + text + "\n\n"; !strings.Contains(got, want) {
		t.Errorf("comment changed ordinary text:\n%s", got)
	}
	f := Finding{Fingerprint: "fp1", Severity: Blocking, Title: "Write to a nil map", Explanation: text, SuggestedFix: text}
	if got := InlineComment(f, InlineOptions{}); !strings.Contains(got, "**\n\n"+text+"\n\n**Fix:**\n\n"+text+"\n\n") {
		t.Errorf("inline comment changed ordinary text:\n%s", got)
	}
}

// A fence's content is byte for byte whatever the fence was, and its info
// string is dropped. The fence pergent writes is not the one that went in.
func TestFenceContentIsKept(t *testing.T) {
	content := "@Override\n\t![x](u)\n[a](u)\n`stray\n||\n&#64;\n  indented"
	for _, fence := range [][2]string{
		{"```", "```"}, {"````mermaid", "````"}, {"~~~suggestion:-0+1", "~~~"}, {"~~~~ plantuml", "~~~~"},
	} {
		got := modelText("x\n" + fence[0] + "\n" + content + "\n" + fence[1])
		if want := "x\n\n```\n" + content + "\n```"; got != want {
			t.Errorf("fence %q:\n got %q\nwant %q", fence[0], got, want)
		}
	}
	// <details> and a comment are neutralized in code as well.
	if got, want := modelText("```\n<details><!-- x -->\n```"), "```\n&lt;details>&lt;!-- x -->\n```"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestHiddenBlockKeepsOriginalText(t *testing.T) {
	f := Finding{
		Fingerprint: "fp1", File: "a.go", Line: 1, Severity: Blocking,
		Title:        "ping @bob <img src=x> ![x](u) `",
		Explanation:  "cc @bob &#64;bob [[wiki]] <!-- x -->\n`a\n`p` @carol `q`\n```go\nx\n```",
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
