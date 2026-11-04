package render

import (
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
	f.Suggestion = "// <!-- x -->\n// @someone\n/ divide\n```go\nx\n```"
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
	for _, want := range []string{"@‍team", "\n​/approve", "\n​/merge", "&lt;!-- pergent:finding evil -->"} {
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
