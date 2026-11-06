package render

import (
	"strings"
	"testing"
)

// These tests do not parse Markdown. They check a structural property that
// holds whatever a platform's parser does: apart from the containers pergent
// writes itself (a fence with no info string, and a code span), model text has
// no live delimiter. Every "@", "<", "]" and "|" is followed by a joiner, every
// "[" is preceded by one, every backtick is backslash-escaped, and every "&"
// that starts an entity is followed by one.

const (
	startMark = "SSTART"
	endMark   = "SEND"
)

// hostileCorpus is model text that has broken a scanner, or could. The first
// group is every input of the review of the first version of the helper.
var hostileCorpus = []string{
	// C1: an indented fence opens a fence at top level.
	"x\n  ```\n```\n@bob ![x](http://e/p.png) [a](http://e/l) <img src=x>",
	"x\n ~~~\n~~~\n@bob",
	// C2: a closer followed by a no-break space does not close.
	"```\nx\n```\u00a0\n```\n@bob ![x](http://e/p.png)\n```",
	"```\nx\n```\u00a0\ny",
	// C3: a table splits a code span at "|".
	"| `a | @bob` |\n|---|---|",
	"| `![x](http://e/p.png) | b` |\n|---|---|",
	"| `<img src=x> | b` |\n|---|---|",
	// C4: a backtick in one text pairs with one in the next.
	"a ` b",
	"`",
	"`x @bob` y",
	"`![x](http://e/p.png)`",
	// C5: reference links and their definitions.
	"[click me][r]\n\n[r]: http://e/l",
	"[click me]\n\n[click me]: http://e/l",
	"[r]: http://e/l",
	"[r]",
	"key [REDACTED]\n\n[redacted]: http://e/l",
	"[REDACTED]: http://e/l",
	"[`]: http://e/l`\n\n[`]",
	// C6: a fence inside a list item.
	"- a\n  ```",
	"```\n@bob ![x](http://e/p.png)\n```",
	"- ```go\n  x := 1\n  ```",
	// I1, M1.
	"&#64;bob ![x](http://e/p.png) [a](http://e/l)",
	"&#64;carol",
	"@bob ![x](http://e/p.png) [a](http://e/l)",
	// I5: closers that must not close.
	"````\n```\n````\n@bob",
	"```\n~~~\n```\n@bob",
	"```\n@a\n``\r`\n@bob",
	// M5: code that is not code.
	"`C:\\` and `@Override`",
	"    indented @bob ![x](u)",
	// GitLab theory: multi-line quotes, math, tables beating a fence.
	">>>\n```\n>>>\n@bob\n```",
	"$`$ @bob `",
	"~~~ `a | @bob`\n  ---|---",
	// The scanner traps.
	"`a\n`p` @bob `q`",
	"- `a\n- `p` @bob `q`",
	"\\` @bob \\`",
	"- ```\n  x\n  ```\n@bob",
	"```\nx\n  ```\n@bob",
	"``` `x\n@bob",
	"~~~\nx\n~~~\n@bob",
	"`ok` @bob `x`\n@carol",
	"`a\n\u00a0\n`x@bob` y",
	// Delimiters on their own.
	"@bob", "&#64;bob", "&#x40;bob", "&commat;bob", "&amp;#64;bob",
	"![x](http://e/p.png)", "[a](http://e)", "[[wiki]]", "[[[wiki]]]", "![[wiki|http://e]]",
	"<img src=x>", "<!-- x -->", "</details>", "<http://e>", "a@b.example",
	"[^1]\n\n[^1]: note", "- [x] task", "\\[a\\](http://e)", "\\@bob",
	"| a | b |\n|---|---|\n| @bob | c |",
	"``a`` @bob ``b``",
	"> ~~~suggestion\nrm -rf\n> ~~~", "- ~~~suggestion:-0+1\n  x\n  ~~~", "1. x\n\n    ~~~suggestion", "~~~~~~", "~~del~~",
	"> ```suggestion\nx\n> ```", "text ```suggestion:-0+3", "```&#115;uggestion\nx\n```", "> ```&#115;uggestion",
	"`a` `b",
	"https://e.com/?a=1&b=2 and https://e.com/@bob",
}

// surface is one place model text goes in the comment. out puts a and b in it
// (b only for a surface that takes two texts), with the marks around them.
type surface struct {
	name  string
	pairs bool
	cell  bool
	out   func(a, b string) string
}

func hostileSkill(f Finding) []Skill {
	f.File, f.Line, f.Severity, f.Fingerprint = "a.go", 1, Blocking, "fp1"
	if f.Title == "" {
		f.Title = "T"
	}
	return []Skill{{Name: "s", Verdict: VerdictHasBlockingIssues, Findings: []Finding{f}}}
}

var surfaces = []surface{
	{name: "summary explanation", out: func(a, _ string) string {
		return Comment(hostileSkill(Finding{Explanation: startMark + "\n" + a + "\n" + endMark}), Options{})
	}},
	{name: "summary fix", out: func(a, _ string) string {
		return Comment(hostileSkill(Finding{SuggestedFix: startMark + "\n" + a + "\n" + endMark}), Options{})
	}},
	{name: "summary explanation and fix", pairs: true, out: func(a, b string) string {
		return Comment(hostileSkill(Finding{Explanation: startMark + "\n" + a, SuggestedFix: b + "\n" + endMark}), Options{})
	}},
	{name: "summary note and explanation", pairs: true, out: func(a, b string) string {
		return Comment(hostileSkill(Finding{Explanation: b + "\n" + endMark}),
			Options{FindingNote: map[string]string{"fp1": startMark + " " + a}})
	}},
	{name: "inline explanation", out: func(a, _ string) string {
		return InlineComment(Finding{Fingerprint: "fp1", Explanation: startMark + "\n" + a + "\n" + endMark}, InlineOptions{})
	}},
	{name: "inline fix", out: func(a, _ string) string {
		return InlineComment(Finding{Fingerprint: "fp1", SuggestedFix: startMark + "\n" + a + "\n" + endMark}, InlineOptions{})
	}},
	{name: "inline title and explanation", pairs: true, out: func(a, b string) string {
		return InlineComment(Finding{Fingerprint: "fp1", Title: startMark + " " + a, Explanation: b + "\n" + endMark}, InlineOptions{})
	}},
	{name: "inline explanation and fix", pairs: true, out: func(a, b string) string {
		return InlineComment(Finding{Fingerprint: "fp1", Explanation: startMark + "\n" + a, SuggestedFix: b + "\n" + endMark}, InlineOptions{})
	}},
	{name: "dismissed title, by and reason", pairs: true, out: func(a, b string) string {
		s := Skill{Name: "s", Verdict: VerdictNoBlockingIssues, Dismissed: []DismissedFinding{
			{Finding: Finding{ID: "abc123", Severity: Suggestion, Title: startMark + " " + a}, By: "eve", Reason: b + " " + endMark},
		}}
		return Comment([]Skill{s}, Options{})
	}},
	{name: "dismissed by and reason", pairs: true, out: func(a, b string) string {
		s := Skill{Name: "s", Verdict: VerdictNoBlockingIssues, Dismissed: []DismissedFinding{
			{Finding: Finding{ID: "abc123", Severity: Suggestion, Title: "T"}, By: startMark + " " + a, Reason: b + " " + endMark},
		}}
		return Comment([]Skill{s}, Options{})
	}},
	{name: "confidence reason", out: func(a, _ string) string {
		s := Skill{Name: "s", Verdict: VerdictNoBlockingIssues, Confidence: 4, ConfidenceReason: startMark + " " + a + " " + endMark}
		return Comment([]Skill{s}, Options{})
	}},
	{name: "note", out: func(a, _ string) string {
		s := Skill{Name: "s", Verdict: VerdictNoBlockingIssues, Notes: []string{startMark + " " + a + " " + endMark}}
		return Comment([]Skill{s}, Options{})
	}},
	{name: "check section", cell: true, out: func(a, _ string) string { return checkCommentWith(startMark+" "+a+" "+endMark, "n", "d") }},
	{name: "check name", cell: true, out: func(a, _ string) string { return checkCommentWith("s", startMark+" "+a+" "+endMark, "d") }},
	{name: "check detail", cell: true, out: func(a, _ string) string { return checkCommentWith("s", "n", startMark+" "+a+" "+endMark) }},
	{name: "failure message", out: func(a, _ string) string {
		s := Skill{Name: "s", Verdict: VerdictFailed, Failures: []Failure{{Message: startMark + " " + a + " " + endMark}}}
		return Comment([]Skill{s}, Options{})
	}},
	{name: "failure lead", out: func(a, _ string) string {
		s := Skill{Name: "s", Verdict: VerdictFailed, Failures: []Failure{{Lead: startMark + "\n" + a + "\n" + endMark}}}
		return Comment([]Skill{s}, Options{})
	}},
}

func checkCommentWith(section, name, detail string) string {
	s := Skill{Name: "s", Verdict: VerdictHasBlockingIssues, Checks: []BatchChecks{{Batch: 1, Checks: []Check{
		{Section: section, Name: name, Status: CheckFail, Detail: detail},
	}}}}
	return Comment([]Skill{s}, Options{})
}

// between returns the part of out from the start mark to the end mark.
func between(out string) (string, bool) {
	i := strings.Index(out, startMark)
	j := strings.LastIndex(out, endMark)
	if i < 0 || j < i {
		return "", false
	}
	return out[i+len(startMark) : j], true
}

// isEscaped reports whether the byte at i is escaped by an odd number of
// backslashes before it.
func isEscaped(s string, i int) bool {
	n := 0
	for i-n-1 >= 0 && s[i-n-1] == '\\' {
		n++
	}
	return n%2 == 1
}

// entityStartAt reports whether an entity starts at the "&" at s[i]: "&#" or
// "&" and a name up to ";".
func entityStartAt(s string, i int) bool {
	rest := s[i+1:]
	if strings.HasPrefix(rest, "#") {
		return true
	}
	n := 0
	for n < len(rest) && (rest[n] >= 'a' && rest[n] <= 'z' || rest[n] >= 'A' && rest[n] <= 'Z' || rest[n] >= '0' && rest[n] <= '9') {
		n++
	}
	return n > 0 && n < len(rest) && rest[n] == ';'
}

// liveDelimiters returns what is live in region once pergent's own fences
// and the code spans are taken out, and how many of each it took out. A cell
// escapes "|" with a backslash instead of a joiner.
func liveDelimiters(region string, cell bool) (problems []string, fences, spans int) {
	fenceLen := 0
	for _, line := range strings.Split(region, "\n") {
		if fenceLen > 0 {
			if line == strings.Repeat("`", fenceLen) {
				fenceLen = 0
			}
			continue
		}
		// pergent's fence: only backticks, at least 3, at column 0. Text can't
		// make one, because its backticks are escaped.
		if len(line) >= 3 && strings.Trim(line, "`") == "" {
			fenceLen = len(line)
			fences++
			continue
		}
		for i := 0; i < len(line); {
			c := line[i]
			rest := line[i+1:]
			bad := func(why string) {
				problems = append(problems, why+" in "+line)
			}
			switch c {
			case '`':
				if isEscaped(line, i) {
					break
				}
				j := strings.IndexByte(rest, '`')
				switch {
				case i > 0 && line[i-1] == '`':
					bad("backtick run")
				case j <= 0:
					bad("stray backtick")
				case j+1 < len(rest) && rest[j+1] == '`':
					bad("backtick run")
				case rest[0] == ' ' || rest[j-1] == ' ':
					bad("span with an edge space")
				default:
					spans++
					i += j + 1 // to the closer
				}
			case '~':
				if strings.HasPrefix(line[i:], "~~~") {
					bad("tilde run")
				}
			case '@', '<':
				if !strings.HasPrefix(rest, joiner) {
					bad("live " + string(c))
				}
			case '[':
				if tok := "[REDACTED]"; strings.HasPrefix(line[i:], tok) && !strings.ContainsAny(line[i+len(tok):][:min(1, len(line[i+len(tok):]))], "([:") {
					i += len(tok) - 1
					break
				}
				if !strings.HasSuffix(line[:i], joiner) && !isEscaped(line, i) {
					bad("live [")
				}
			case ']':
				if !strings.HasPrefix(rest, joiner) {
					bad("live ]")
				}
			case '|':
				switch {
				case cell && !isEscaped(line, i):
					bad("live | in a cell")
				case !cell && !strings.HasPrefix(rest, joiner):
					bad("live |")
				}
			case '&':
				if entityStartAt(line, i) && !strings.HasPrefix(rest, joiner) {
					bad("live entity")
				}
			}
			i++
		}
	}
	return problems, fences, spans
}

func TestLiveDelimitersChecker(t *testing.T) {
	for _, bad := range []string{
		"@bob", "<img>", "[a]" + joiner + "(b)", joiner + "[a](b)", "`x", "`a` `b", "``a``", "` a`", "&#64;", "&commat;", "a|b",
		"x\n```\n@bob", // not our fence: it never closes, but the line before is text
	} {
		if p, _, _ := liveDelimiters(bad, false); len(p) == 0 && bad != "x\n```\n@bob" {
			t.Errorf("checker accepts %q", bad)
		}
	}
	for _, good := range []string{
		"@" + joiner + "bob", "<" + joiner + "img>", joiner + "[a]" + joiner + "(b)", "`@bob` and `a<b`", "\\`", "R&D & a&b=1",
		"[REDACTED] ok", joiner + "[REDACTED]" + joiner + ": x", "\\[x", "a|" + joiner + "b",
		"x\n```\n@bob ![x](u)\n```\ny",
	} {
		if p, _, _ := liveDelimiters(good, false); len(p) != 0 {
			t.Errorf("checker flags %q: %v", good, p)
		}
	}
	if p, _, _ := liveDelimiters("a\\|b", true); len(p) != 0 {
		t.Errorf("checker flags an escaped pipe in a cell: %v", p)
	}
	if p, _, _ := liveDelimiters("a|b", true); len(p) == 0 {
		t.Error("checker accepts a live pipe in a cell")
	}
	if _, f, s := liveDelimiters("`a` b\n```\n@x\n```\n`c`", false); f != 1 || s != 2 {
		t.Errorf("checker counts %d fences and %d spans, want 1 and 2", f, s)
	}
}

func TestNoLiveDelimitersOutsideOurContainers(t *testing.T) {
	var fences, spans, texts int
	bad := 0
	for _, sf := range surfaces {
		for _, a := range hostileCorpus {
			bs := []string{""}
			if sf.pairs {
				bs = hostileCorpus
			}
			for _, b := range bs {
				out := sf.out(a, b)
				region, ok := between(out)
				if !ok {
					t.Fatalf("%s: marks missing for %q, %q:\n%s", sf.name, a, b, out)
				}
				problems, f, s := liveDelimiters(region, sf.cell)
				fences, spans, texts = fences+f, spans+s, texts+1
				if len(problems) > 0 && bad < 12 {
					bad++
					t.Errorf("%s: %q, %q: %v\nregion: %q", sf.name, a, b, problems[:min(3, len(problems))], region)
				}
				if sf.cell {
					continue
				}
				// The layout is not swallowed by an open fence or a block.
				if o, c := strings.Count(out, "<details"), strings.Count(out, "</details>"); o != c {
					if bad < 12 {
						bad++
						t.Errorf("%s: %q, %q: %d <details, %d </details>", sf.name, a, b, o, c)
					}
				}
			}
		}
	}
	// The check is not vacuous: it saw fences, spans and a lot of text.
	if fences < 100 || spans < 100 || texts < 10000 {
		t.Errorf("only %d fences, %d spans and %d texts were checked", fences, spans, texts)
	}
}
