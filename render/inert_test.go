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
	// N2, N3: a URL or math may take a span's backtick.
	"https://e.com/`a @bob` x", "www.e.com/`a ![x](http://e/p.png) b` c", "$`$ @bob`", "$x`y$ @bob`z`",
	// N4: a backslash before a pipe in a cell, alone and in a span.
	"a\\|b", "`a\\| @bob`",
	// N6: spans next to backslashes and other backticks, an indented ">>>".
	"\\`@bob`", "`a`` @bob", "\\\\`@bob`", "  >>>", "x\n  >>>\n```\n>>>\n```",
	// N7, N10: a doubled redaction token, an info string that is not a word.
	"[[REDACTED]]", "![REDACTED](http://e)", "~~~ not a fence in prose", "```go title=x\n@bob ![x](u)\n```", "  ```\n  a\n  ```\n@bob",
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

// between returns the part of out from the start mark to the end mark, and
// the line after it when that is the closing line of a fence the end mark is in.
func between(out string) (string, bool) {
	i := strings.Index(out, startMark)
	j := strings.LastIndex(out, endMark)
	if i < 0 || j < i {
		return "", false
	}
	end := j
	if rest := out[j+len(endMark):]; strings.HasPrefix(rest, "\n") {
		line := rest[1:]
		if k := strings.IndexByte(line, '\n'); k >= 0 {
			line = line[:k]
		}
		if len(line) >= 3 && strings.Trim(line, "`") == "" {
			end = j + len(endMark) + 1 + len(line)
		}
	}
	return out[i+len(startMark) : end], true
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

// isFenceLine reports whether line is a line of only backticks, 3 or more.
func isFenceLine(line string) bool { return len(line) >= 3 && strings.Trim(line, "`") == "" }

// liveDelimiters returns what is live in region once pergent's own fences and
// the code spans text may keep are taken out: the problems found, the number
// of fences taken out and the content of the spans. A fence still open at the
// end is a problem. A cell keeps no span and has no raw "|": its pipes are the
// entity "&#124;", which pergent writes.
func liveDelimiters(region string, cell bool) (problems []string, fences int, spans []string) {
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
		if isFenceLine(line) {
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
				case cell:
					bad("backtick in a cell")
				case i > 0 && line[i-1] == '`':
					bad("backtick run")
				case j <= 0:
					bad("stray backtick")
				case j+1 < len(rest) && rest[j+1] == '`':
					bad("backtick run")
				case rest[0] == ' ' || rest[j-1] == ' ':
					bad("span with an edge space")
				case i > 0 && !strings.ContainsRune(" \t(", rune(line[i-1])):
					bad("span opens inside a word")
				case j+1 < len(rest) && !strings.ContainsRune(" \t).,:;!?", rune(rest[j+1])):
					bad("span closes inside a word")
				default:
					spans = append(spans, rest[:j])
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
				if tok := "[REDACTED]"; strings.HasPrefix(line[i:], tok) && (i == 0 || line[i-1] != '[') &&
					!strings.ContainsAny(line[i+len(tok):][:min(1, len(line[i+len(tok):]))], "([:") {
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
				if cell || !strings.HasPrefix(rest, joiner) {
					bad("live |")
				}
			case '&':
				switch {
				case cell && strings.HasPrefix(rest, "#124;") && !isEscaped(line, i):
					i += len("#124;")
				case entityStartAt(line, i) && !strings.HasPrefix(rest, joiner):
					bad("live entity")
				}
			}
			i++
		}
	}
	if fenceLen > 0 {
		problems = append(problems, "a fence is still open at the end")
	}
	return problems, fences, spans
}

// layoutProblems checks the whole comment: every fence closes, the layout's
// tags and markers are outside the fences, and none is inside one.
func layoutProblems(out string) []string {
	var problems []string
	fenceLen := 0
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		switch {
		case fenceLen > 0 && line == strings.Repeat("`", fenceLen):
			fenceLen = 0
		case fenceLen > 0:
			for _, tag := range []string{"<details", "</details", "<summary", "</summary", "<!--", "<sub>"} {
				if strings.Contains(strings.ToLower(line), tag) {
					problems = append(problems, tag+" inside a fence: "+line)
				}
			}
		default:
			// The real suggestion fence has an info string, so it is not all backticks.
			if n := len(line) - len(strings.TrimLeft(line, "`")); n >= 3 && !strings.Contains(line[n:], "`") {
				fenceLen = n
			}
		}
	}
	if fenceLen > 0 {
		problems = append(problems, "a fence is open at the end of the comment")
	}
	return problems
}

func TestLiveDelimitersChecker(t *testing.T) {
	for _, bad := range []string{
		"@bob", "<img>", "[a]" + joiner + "(b)", joiner + "[a](b)", "`x", "`a` `b", "``a``", "` a`", "&#64;", "&commat;", "a|b",
		"x\n```\n@bob ![x](http://e/p.png)", // an unclosed fence
		"https://e.com/`a @bob`", "x`a`", "`a`x", "$`a`$ y", "~~~", joiner + "[[REDACTED]]", "[REDACTED](u)",
	} {
		if p, _, _ := liveDelimiters(bad, false); len(p) == 0 {
			t.Errorf("checker accepts %q", bad)
		}
	}
	for _, good := range []string{
		"@" + joiner + "bob", "<" + joiner + "img>", joiner + "[a]" + joiner + "(b)", "`@bob` and `a<b`", "(`a`)", "`a`, `b`.", "\\`", "R&D & a&b=1",
		"[REDACTED] ok", joiner + "[REDACTED]" + joiner + ": x", "\\[x", "a|" + joiner + "b",
		"x\n```\n@bob ![x](u)\n```\ny",
	} {
		if p, _, _ := liveDelimiters(good, false); len(p) != 0 {
			t.Errorf("checker flags %q: %v", good, p)
		}
	}
	for _, bad := range []string{"a|b", "a\\|b", "`a`", "a\\&#124;b"} {
		if p, _, _ := liveDelimiters(bad, true); len(p) == 0 {
			t.Errorf("checker accepts %q in a cell", bad)
		}
	}
	if p, _, _ := liveDelimiters("a&#124;b \\` c", true); len(p) != 0 {
		t.Errorf("checker flags a cell's own pipe: %v", p)
	}
	if _, f, s := liveDelimiters("`a` b\n```\n@x\n```\n`c`", false); f != 1 || len(s) != 2 {
		t.Errorf("checker counts %d fences and %v spans, want 1 and 2", f, s)
	}
	for _, bad := range []string{"```\n</details>\n```", "```\nx", "a\n```\n<!-- x -->\n```"} {
		if len(layoutProblems(bad)) == 0 {
			t.Errorf("layout check accepts %q", bad)
		}
	}
	if p := layoutProblems("<details>\n\n```\n&lt;/details>\n```\n\n</details>\n<!-- x -->"); len(p) != 0 {
		t.Errorf("layout check flags a good comment: %v", p)
	}
}

func TestNoLiveDelimitersOutsideOurContainers(t *testing.T) {
	var fences, modelSpans, texts int
	bad := 0
	report := func(format string, args ...any) {
		if bad < 12 {
			bad++
			t.Errorf(format, args...)
		}
	}
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
				problems, f, spans := liveDelimiters(region, sf.cell)
				fences, texts = fences+f, texts+1
				for _, content := range spans {
					if content != "abc123" { // pergent's own: the ID of a dismissed finding
						modelSpans++
					}
				}
				if len(problems) > 0 {
					report("%s: %q, %q: %v\nregion: %q", sf.name, a, b, problems[:min(3, len(problems))], region)
				}
				if p := layoutProblems(out); len(p) > 0 {
					report("%s: %q, %q: %v", sf.name, a, b, p[:min(3, len(p))])
				}
			}
		}
	}
	// The check is not vacuous: it saw fences, spans of the model's and a lot of text.
	if fences < 100 || modelSpans < 100 || texts < 10000 {
		t.Errorf("only %d fences, %d model spans and %d texts were checked", fences, modelSpans, texts)
	}
}

// parseSpans reads s the way CommonMark reads code spans: a run of n backticks
// is closed by the next run of exactly n. It returns the text outside the
// spans, the content of each span (one space off each end when both have one,
// unless it is all spaces) and whether every run was matched.
func parseSpans(s string) (outside string, spans []string, ok bool) {
	var b strings.Builder
	for i := 0; i < len(s); {
		if s[i] != '`' {
			b.WriteByte(s[i])
			i++
			continue
		}
		n := 0
		for i+n < len(s) && s[i+n] == '`' {
			n++
		}
		end := -1
		for j := i + n; j < len(s); {
			if s[j] != '`' {
				j++
				continue
			}
			m := 0
			for j+m < len(s) && s[j+m] == '`' {
				m++
			}
			if m == n {
				end = j
				break
			}
			j += m
		}
		if end < 0 {
			return b.String(), spans, false
		}
		content := s[i+n : end]
		if len(content) > 1 && content[0] == ' ' && content[len(content)-1] == ' ' && strings.Trim(content, " ") != "" {
			content = content[1 : len(content)-1]
		}
		spans = append(spans, content)
		i = end + n
	}
	return b.String(), spans, true
}

// A failure names its files in code spans, and the names are the PR's: a blank
// one, or one with backticks, must not leave the next name outside its span.
func TestFailureFilesAreSpans(t *testing.T) {
	names := []string{
		" ", "\u00a0", "", "<img src=x>`a", "@bob`x", "`", "``", "a``b", "```a", "a`", "`a", "x", "![x](u)", "a b", "`` ``", "``` ```",
	}
	check := func(files ...string) {
		s := Skill{Name: "s", Verdict: VerdictFailed, Batches: 2, Failures: []Failure{{Files: files, Message: "m"}}}
		out := Comment([]Skill{s}, Options{})
		i := strings.Index(out, "Files ")
		j := strings.Index(out, ": **Review failed:**")
		if i < 0 || j < i {
			t.Fatalf("files %q: no Files line:\n%s", files, out)
		}
		outside, spans, ok := parseSpans(out[i+len("Files ") : j])
		want := make([]string, len(files))
		for k, f := range files {
			if want[k] = oneLine(f); want[k] == "" {
				want[k] = " "
			}
		}
		if !ok || outside != strings.Repeat(", ", len(files)-1) || strings.Join(spans, "\x00") != strings.Join(want, "\x00") {
			t.Errorf("files %q: outside %q and spans %q (matched: %v), want spans %q", files, outside, spans, ok, want)
		}
	}
	check(" ", "<img src=x>`a")
	check(" ", "@bob`x")
	for _, a := range names {
		check(a)
		for _, b := range names {
			check(a, b)
			check(b, a, b)
		}
	}
}
