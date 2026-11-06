package render

import (
	"html"
	"strings"
	"testing"
)

func lastLine(s string) string {
	s = strings.TrimRight(s, "\n")
	return s[strings.LastIndex(s, "\n")+1:]
}

func inlineFinding() Finding {
	return Finding{
		ID: "c4d9a1", Fingerprint: "fp1", File: "a.go", Line: 12, EndLine: 12,
		Severity: Blocking, Title: "Write to a nil map",
		Explanation:  "The map is never initialised.",
		SuggestedFix: "Initialise it with make.",
		Suggestion:   "\tseen := make(map[string]int)",
	}
}

func TestInlineCommentBlockingWithSuggestion(t *testing.T) {
	body := InlineComment(inlineFinding(), InlineOptions{
		SuggestionInfo: "suggestion", DismissCommand: "@pergent-review dismiss", Skill: "go",
	})
	if !strings.HasPrefix(body, "**❌ Blocking: Write to a nil map**\n") {
		t.Errorf("unexpected start:\n%s", body)
	}
	for _, want := range []string{
		"**Fix:** Initialise it with make.\n",
		"```suggestion\n\tseen := make(map[string]int)\n```\n",
		"Reply `@pergent-review dismiss <reason>` in this thread.",
		"<sub>`go` · id `c4d9a1` · Not a real problem?",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q in:\n%s", want, body)
		}
	}
	if got := lastLine(body); got != "<!-- pergent:finding fp1 -->" {
		t.Errorf("last line = %q", got)
	}
}

func TestInlineCommentSuggestionSeverity(t *testing.T) {
	f := inlineFinding()
	f.Severity, f.ID = Suggestion, ""
	body := InlineComment(f, InlineOptions{})
	if !strings.HasPrefix(body, "**💡 Suggestion: Write to a nil map**\n") {
		t.Errorf("unexpected start:\n%s", body)
	}
	if strings.Contains(body, "<sub>") {
		t.Errorf("sub line should be omitted when everything is empty:\n%s", body[strings.Index(body, "<sub>"):])
	}
	if !strings.HasSuffix(body, "\n<!-- pergent:finding fp1 -->") && !strings.HasSuffix(body, "\n<!-- pergent:finding fp1 -->\n") {
		t.Errorf("marker not last:\n%s", body)
	}
}

func TestInlineCommentGitLabSuggestionInfo(t *testing.T) {
	body := InlineComment(inlineFinding(), InlineOptions{SuggestionInfo: "suggestion:-0+2"})
	if !strings.Contains(body, "```suggestion:-0+2\n") {
		t.Errorf("missing GitLab fence:\n%s", body)
	}
}

func TestInlineCommentNoSuggestionBlockWithoutInfo(t *testing.T) {
	body := InlineComment(inlineFinding(), InlineOptions{})
	if strings.Contains(body, "```suggestion") || strings.Contains(body, "make(map") {
		t.Errorf("suggestion block present:\n%s", body)
	}
	if !strings.Contains(body, "**Fix:** Initialise it with make.") {
		t.Errorf("fix line missing:\n%s", body)
	}
}

func TestInlineCommentSuggestionKeptByteForByte(t *testing.T) {
	f := inlineFinding()
	f.Suggestion = "# <!-- x -->\n# @someone\n```go\nx\n```\n  /not at column 0"
	body := InlineComment(f, InlineOptions{SuggestionInfo: "suggestion"})
	want := "````suggestion\n" + f.Suggestion + "\n````\n"
	if !strings.Contains(body, want) {
		t.Errorf("suggestion changed:\n%s", body)
	}
	if got := lastLine(body); got != "<!-- pergent:finding fp1 -->" {
		t.Errorf("last line = %q", got)
	}
}

func TestInlineCommentSuggestionWithCarriageReturnDropped(t *testing.T) {
	f := inlineFinding()
	f.Suggestion = "a\r\nb"
	body := InlineComment(f, InlineOptions{SuggestionInfo: "suggestion"})
	if strings.Contains(body, "```suggestion") {
		t.Errorf("suggestion block present:\n%s", body)
	}
	if !strings.Contains(body, "**Fix:**") {
		t.Errorf("fix line missing:\n%s", body)
	}
}

func TestInlineCommentNeutralisesModelText(t *testing.T) {
	f := inlineFinding()
	f.Title = "Ping @team\nnow <!-- x -->"
	f.Explanation = "cc @team\n/approve\ntext <!-- pergent:finding evil -->"
	f.SuggestedFix = "ask @bob\n/merge"
	body := InlineComment(f, InlineOptions{})
	for _, bad := range []string{"@team", "@bob", "\n/approve", "\n/merge", "<!-- pergent:finding evil -->", "<!-- x -->"} {
		if strings.Contains(body, bad) {
			t.Errorf("%q survived:\n%s", bad, body)
		}
	}
	for _, want := range []string{"@‍team", "\n​/approve", "\n​/merge", "<\u200d!-- pergent:finding evil -->"} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q in:\n%s", want, body)
		}
	}
	if fp, ok := InlineFingerprint(body); !ok || fp != "fp1" {
		t.Errorf("InlineFingerprint = %q, %v", fp, ok)
	}
}

func TestInlineFingerprint(t *testing.T) {
	long := strings.Repeat("a", 129)
	tests := []struct {
		name, body, want string
		ok               bool
	}{
		{"valid", "text\n<!-- pergent:finding fp1 -->", "fp1", true},
		{"trailing blank lines", "text\n<!-- pergent:finding a:b_c-D9 -->\n\n  \n", "a:b_c-D9", true},
		{"only line", "<!-- pergent:finding x -->", "x", true},
		{"not last", "<!-- pergent:finding fp1 -->\nmore", "", false},
		{"bad characters", "x\n<!-- pergent:finding f p -->", "", false},
		{"bad characters 2", "x\n<!-- pergent:finding a/b -->", "", false},
		{"too long", "x\n<!-- pergent:finding " + long + " -->", "", false},
		{"empty fingerprint", "x\n<!-- pergent:finding  -->", "", false},
		{"prefix text", "x\nfoo <!-- pergent:finding fp1 -->", "", false},
		{"empty body", "", "", false},
	}
	for _, tt := range tests {
		got, ok := InlineFingerprint(tt.body)
		if got != tt.want || ok != tt.ok {
			t.Errorf("%s: got %q, %v; want %q, %v", tt.name, got, ok, tt.want, tt.ok)
		}
	}
}

func TestInlineMarkerRoundTrip(t *testing.T) {
	if got, ok := InlineFingerprint("x\n" + InlineMarker("ab:12")); got != "ab:12" || !ok {
		t.Errorf("round trip = %q, %v", got, ok)
	}
}

// liveSuggestionFences counts the fences in body a platform renders as a
// suggestion: a run of 3 or more backticks or tildes whose info string, after
// decoding entities and trimming ASCII white space, starts with "suggestion". It is
// written differently from the implementation on purpose.
func liveSuggestionFences(body string) int {
	n := 0
	for _, line := range strings.Split(body, "\n") {
		for i := 0; i < len(line); {
			if line[i] != '`' && line[i] != '~' {
				i++
				continue
			}
			j := i
			for j < len(line) && line[j] == line[i] {
				j++
			}
			if j-i >= 3 {
				// ASCII white space only, as CommonMark renderers trim it: a
				// non-ASCII space such as NBSP doesn't make the language "suggestion".
				info := strings.ToLower(strings.TrimLeft(html.UnescapeString(line[j:]), " \t\n\v\f\r"))
				if strings.HasPrefix(info, "suggestion") {
					n++
				}
			}
			i = j
		}
	}
	return n
}

func TestInlineCommentModelSuggestionFencesDefused(t *testing.T) {
	fences := []string{
		"```suggestion", "```suggestion:-0+3", "```suggestion:-20+20", "~~~suggestion", "~~~suggestion:-0+3",
		"```Suggestion", "``` suggestion", "   ```suggestion:-0+3",
		"> ```suggestion:-20+20", "- ```suggestion:-20+20", "1. x\n\n    ```suggestion:-20+20",
		"> > ~~~suggestion", "text ```suggestion:-0+3",
		"```&#115;uggestion:-20+20", "```&#x73;uggestion", "```sugg&#x65;stion", "~~~&#83;uggestion",
		"```&#83;UGGESTION", "```\vsuggestion", "```\fsuggestion", "```\tsuggestion", "````  suggestion",
		"> ```&#115;uggestion:-20+20", "> ```\vsuggestion", "- ~~~&#x73;uggestion", "```&#32;suggestion",
	}
	harmless := []string{"```\u00a0suggestion", "```&amp;suggestion", "```&amp;#115;uggestion", "```sugg estion", "```text"}
	for _, info := range []string{"", "suggestion", "suggestion:-0+2"} {
		want := 0
		if info != "" {
			want = 1 // the one built from Finding.Suggestion
		}
		for _, fence := range append(append([]string{}, fences...), harmless...) {
			f := inlineFinding()
			f.Explanation = "see\n" + fence + "\nrm -rf everything\n```"
			f.SuggestedFix = "or\n" + fence + "\nrm -rf everything\n```"
			body := InlineComment(f, InlineOptions{SuggestionInfo: info})
			if got := liveSuggestionFences(body); got != want {
				t.Errorf("info %q fence %q: %d live suggestion fences, want %d:\n%s", info, fence, got, want, body)
			}
			if info != "" && !strings.Contains(body, "```"+info+"\n"+f.Suggestion+"\n```") {
				t.Errorf("info %q fence %q: real suggestion block missing:\n%s", info, fence, body)
			}
		}
	}
}

func TestInlineCommentNBSPFenceInfoDropped(t *testing.T) {
	f := inlineFinding()
	f.Explanation = "```\u00a0suggestion\nx\n```"
	body := InlineComment(f, InlineOptions{})
	if !strings.Contains(body, "\n\n```\nx\n```\n\n") || strings.Contains(body, "suggestion") {
		t.Errorf("info string of the fence survived:\n%s", body)
	}
	if got := liveSuggestionFences(body); got != 0 {
		t.Errorf("%d live suggestion fences, want 0", got)
	}
}

func TestLiveSuggestionFencesCounter(t *testing.T) {
	for body, want := range map[string]int{
		"```suggestion": 1, "> ~~~&#83;UGGESTION": 1, "```\vsuggestion": 1, "```text-suggestion": 0,
		"``suggestion": 0, "```\u00a0suggestion": 0, "```&amp;suggestion": 0, "``` ```suggestion": 1,
	} {
		if got := liveSuggestionFences(body); got != want {
			t.Errorf("%q: got %d, want %d", body, got, want)
		}
	}
}

func TestInlineCommentRealSuggestionIsOnlyLiveFence(t *testing.T) {
	f := inlineFinding()
	f.Explanation = "```suggestion:-0+3\nx\n```"
	f.SuggestedFix = "~~~suggestion\ny\n~~~"
	body := InlineComment(f, InlineOptions{SuggestionInfo: "suggestion"})
	n := 0
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(strings.ToLower(strings.TrimLeft(line, " ~`")), "suggestion") {
			n++
		}
	}
	if n != 1 {
		t.Errorf("want one suggestion fence, got %d:\n%s", n, body)
	}
	for _, want := range []string{"\n\n```\nx\n```\n\n", "\n\n```\ny\n```\n\n"} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q:\n%s", want, body)
		}
	}
}

func TestInlineCommentSuggestionWithColumnZeroSlashDropped(t *testing.T) {
	for _, sug := range []string{
		"/approve",
		"a\n/b",
		"a\n```\n/approve",
		"</p>\n/approve",
		">>>\n/approve\n>>>",
		"x\n```go\ny\n```\n/merge",
		"// comment",
	} {
		f := inlineFinding()
		f.Suggestion = sug
		body := InlineComment(f, InlineOptions{SuggestionInfo: "suggestion"})
		if strings.Contains(body, "```suggestion") || strings.Contains(body, "\n/") {
			t.Errorf("suggestion %q not dropped:\n%s", sug, body)
		}
		if !strings.Contains(body, "**Fix:** Initialise it with make.") {
			t.Errorf("fix line missing:\n%s", body)
		}
	}
}

func TestInlineCommentIndentedSlashSuggestionKept(t *testing.T) {
	for _, sug := range []string{"a\n /b", "\t/b\n  /c", "x\n```\n  /y", " /only"} {
		f := inlineFinding()
		f.Suggestion = sug
		fence := strings.Repeat("`", max(3, longestBacktickRun(sug)+1))
		want := fence + "suggestion\n" + sug + "\n" + fence + "\n"
		if body := InlineComment(f, InlineOptions{SuggestionInfo: "suggestion"}); !strings.Contains(body, want) {
			t.Errorf("suggestion %q not kept byte for byte:\n%s", sug, body)
		}
	}
}
