package finding

import (
	"fmt"
	"slices"
	"strings"
	"testing"
)

func TestParseAnswer(t *testing.T) {
	fence := "```"
	tests := []struct {
		name    string
		answer  string
		wantLen int
		wantErr string
		check   func(t *testing.T, fs []Finding)
	}{
		{
			name:    "prose then json block",
			answer:  "Here is my review.\n" + fence + "json\n" + `{"findings":[{"file":"a.go","line":3,"severity":"blocking","title":"t","explanation":"e"}]}` + "\n" + fence,
			wantLen: 1,
			check: func(t *testing.T, fs []Finding) {
				if fs[0].Severity != Blocking {
					t.Errorf("Severity = %q, want %q", fs[0].Severity, Blocking)
				}
			},
		},
		{
			name:    "empty findings",
			answer:  fence + "json\n" + `{"findings": []}` + "\n" + fence,
			wantLen: 0,
			check: func(t *testing.T, fs []Finding) {
				if fs == nil {
					t.Error("findings = nil, want empty non-nil slice")
				}
			},
		},
		{
			name: "last valid block wins",
			answer: "Example:\n" + fence + "json\n" + `{"findings":[{"file":"a.go","severity":"blocking","title":"t","explanation":"e"}]}` + "\n" + fence +
				"\nFinal:\n" + fence + "json\n" + `{"findings":[]}` + "\n" + fence,
			wantLen: 0,
		},
		{
			name: "bad finding in the last block does not fall back to an earlier block",
			answer: "Shape:\n" + fence + "json\n" + `{"findings":[]}` + "\n" + fence +
				"\nAnswer:\n" + fence + "json\n" + `{"findings":[{"file":"a.go","severity":"high","title":"t","explanation":"e"}]}` + "\n" + fence,
			wantErr: "severity",
		},
		{
			name: "cut-off last block does not fall back to an earlier block",
			answer: "Shape:\n" + fence + "json\n" + `{"findings":[]}` + "\n" + fence +
				"\nAnswer:\n" + fence + "json\n" + `{"findings":[{"file":"a.go","sever`,
			wantErr: "invalid json",
		},
		{
			name: "null findings in the last block does not fall back to an earlier block",
			answer: "Shape:\n" + fence + "json\n" + `{"findings":[]}` + "\n" + fence +
				"\nAnswer:\n" + fence + "json\n" + `{"findings":null}` + "\n" + fence,
			wantErr: "findings",
		},
		{
			name: "findings of the wrong type in the last block does not fall back to an earlier block",
			answer: "Shape:\n" + fence + "json\n" + `{"findings":[]}` + "\n" + fence +
				"\nAnswer:\n" + fence + "json\n" + `{"findings":"none"}` + "\n" + fence,
			wantErr: "invalid json",
		},
		{
			name: "block without a findings key after the answer is skipped",
			answer: fence + "json\n" + `{"findings":[{"file":"a.go","severity":"blocking","title":"t","explanation":"e"}]}` + "\n" + fence +
				"\nFor reference:\n" + fence + "json\n" + `{"other":1}` + "\n" + fence,
			wantLen: 1,
		},
		{
			name:    "no json block",
			answer:  "just prose, nothing fenced",
			wantErr: "no json code block",
		},
		{
			name:    "invalid severity",
			answer:  fence + "json\n" + `{"findings":[{"file":"a.go","severity":"critical","title":"t","explanation":"e"}]}` + "\n" + fence,
			wantErr: "severity",
		},
		{
			name:    "missing title",
			answer:  fence + "json\n" + `{"findings":[{"file":"a.go","severity":"blocking","explanation":"e"}]}` + "\n" + fence,
			wantErr: "title",
		},
		{
			name:    "missing findings key",
			answer:  fence + "json\n" + `{"other":1}` + "\n" + fence,
			wantErr: "findings",
		},
		{
			name: "fenced code inside a json string",
			answer: "Review.\n" + fence + "json\n" +
				`{"findings":[{"file":"a.go","severity":"blocking","title":"t","explanation":"e","suggested_fix":"Use this:\n` + fence + `go\nif err != nil {\n\treturn err\n}\n` + fence + `"}]}` +
				"\n" + fence + "\n",
			wantLen: 1,
			check: func(t *testing.T, fs []Finding) {
				want := "Use this:\n" + fence + "go\nif err != nil {\n\treturn err\n}\n" + fence
				if fs[0].SuggestedFix != want {
					t.Errorf("SuggestedFix = %q, want %q", fs[0].SuggestedFix, want)
				}
			},
		},
		{
			name:    "block without a closing fence at the end of the answer",
			answer:  "Review.\n" + fence + "json\n" + `{"findings":[]}` + "\n",
			wantLen: 0,
		},
		{
			name:    "text after the json value inside the block",
			answer:  fence + "json\n" + `{"findings":[]} and more` + "\n" + fence,
			wantErr: "invalid json",
		},
		{
			name: "fenced code in an earlier example block and a valid last block",
			answer: "Example:\n" + fence + "json\n" + `{"findings":[{"file":"a.go","severity":"blocking","title":"t","explanation":"` + fence + `x` + fence + `"}]}` + "\n" + fence +
				"\nFinal:\n" + fence + "json\n" + `{"findings":[]}` + "\n" + fence,
			wantLen: 0,
		},
		{
			name:    "fingerprint kept",
			answer:  fence + "json\n" + `{"findings":[{"file":"a.go","severity":"suggestion","title":"t","explanation":"e","fingerprint":"abc123"}]}` + "\n" + fence,
			wantLen: 1,
			check: func(t *testing.T, fs []Finding) {
				if fs[0].Fingerprint != "abc123" {
					t.Errorf("Fingerprint = %q, want %q", fs[0].Fingerprint, "abc123")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a, err := ParseAnswer(tt.answer)
			fs := a.Findings
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("ParseAnswer() error = nil, want containing %q", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("error = %q, want containing %q", err.Error(), tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseAnswer() error: %v", err)
			}
			if len(fs) != tt.wantLen {
				t.Fatalf("len(findings) = %d, want %d", len(fs), tt.wantLen)
			}
			if tt.check != nil {
				tt.check(t, fs)
			}
		})
	}
}

func jsonAnswer(body string) string {
	return "```json\n" + body + "\n```"
}

func TestParseAnswer_Summary(t *testing.T) {
	a, err := ParseAnswer(jsonAnswer(`{"summary":"Checked a.go.","findings":[]}`))
	if err != nil {
		t.Fatalf("ParseAnswer() error: %v", err)
	}
	if a.Summary != "Checked a.go." {
		t.Errorf("Summary = %q, want %q", a.Summary, "Checked a.go.")
	}
	if len(a.Findings) != 0 {
		t.Errorf("len(Findings) = %d, want 0", len(a.Findings))
	}
}

func TestParseAnswer_MissingSummaryIsFine(t *testing.T) {
	a, err := ParseAnswer(jsonAnswer(`{"findings":[]}`))
	if err != nil {
		t.Fatalf("ParseAnswer() error: %v", err)
	}
	if a.Summary != "" {
		t.Errorf("Summary = %q, want empty", a.Summary)
	}
}

func TestParseAnswer_SummaryCollapsedAndCapped(t *testing.T) {
	a, err := ParseAnswer(jsonAnswer(`{"summary":"Checked a.go\nand   b.go.\n","findings":[]}`))
	if err != nil {
		t.Fatalf("ParseAnswer() error: %v", err)
	}
	if a.Summary != "Checked a.go and b.go." {
		t.Errorf("Summary = %q, want one collapsed line", a.Summary)
	}

	long := strings.Repeat("x", 2000)
	a, err = ParseAnswer(jsonAnswer(`{"summary":"` + long + `","findings":[]}`))
	if err != nil {
		t.Fatalf("ParseAnswer() error: %v", err)
	}
	r := []rune(a.Summary)
	if len(r) != 600 {
		t.Errorf("len(Summary) = %d runes, want 600", len(r))
	}
	if !strings.HasSuffix(a.Summary, "…") {
		t.Errorf("Summary should end with …, got %q", string(r[len(r)-5:]))
	}
}

func TestParseAnswer_SummaryOfWrongTypeKeepsFindings(t *testing.T) {
	finding := `{"file":"a.go","line":1,"severity":"blocking","title":"t","explanation":"e"}`
	for name, summary := range map[string]string{
		"array":  `["Checked a.go.","Checked b.go."]`,
		"object": `{"text":"Checked a.go."}`,
		"number": `42`,
	} {
		t.Run(name, func(t *testing.T) {
			a, err := ParseAnswer(jsonAnswer(`{"summary":` + summary + `,"findings":[` + finding + `]}`))
			if err != nil {
				t.Fatalf("ParseAnswer() error: %v", err)
			}
			if len(a.Findings) != 1 {
				t.Fatalf("len(Findings) = %d, want 1", len(a.Findings))
			}
			if name == "array" && a.Summary != "Checked a.go. Checked b.go." {
				t.Errorf("Summary = %q, want the array joined", a.Summary)
			}
			if name != "array" && a.Summary != "" {
				t.Errorf("Summary = %q, want empty", a.Summary)
			}
		})
	}
}

func TestParseAnswer_SummaryRedactedBeforeCap(t *testing.T) {
	t.Setenv("GITLAB_TOKEN", "glpat-secretvalue123456")
	a, err := ParseAnswer(jsonAnswer(`{"summary":"` + strings.Repeat("x", 590) + ` glpat-secretvalue123456","findings":[]}`))
	if err != nil {
		t.Fatalf("ParseAnswer() error: %v", err)
	}
	if strings.Contains(a.Summary, "glpat") {
		t.Errorf("summary leaks part of the secret: %q", a.Summary[len(a.Summary)-20:])
	}
}

func TestParseAnswer_Confidence(t *testing.T) {
	for _, c := range []struct {
		raw        string
		want       int
		wantReason string
	}{
		{`4`, 4, "Looks fine."},
		{`4.0`, 4, "Looks fine."},
		{`1`, 1, "Looks fine."},
		{`5`, 5, "Looks fine."},
		{`0`, 0, ""},
		{`6`, 0, ""},
		{`4.5`, 0, ""},
		{`"4"`, 0, ""},
		{`null`, 0, ""},
	} {
		text := "```json\n{\"confidence\": " + c.raw + ", \"confidence_reason\": \"Looks  fine.\", \"findings\": []}\n```"
		a, err := ParseAnswer(text)
		if err != nil {
			t.Fatalf("%s: unexpected error %v", c.raw, err)
		}
		if a.Confidence != c.want || a.ConfidenceReason != c.wantReason {
			t.Errorf("%s: got (%d, %q), want (%d, %q)", c.raw, a.Confidence, a.ConfidenceReason, c.want, c.wantReason)
		}
	}
	a, err := ParseAnswer("```json\n{\"findings\": []}\n```")
	if err != nil || a.Confidence != 0 {
		t.Errorf("missing confidence: got (%d, %v), want (0, nil)", a.Confidence, err)
	}
}

func TestParseAnswer_ConfidenceReasonCappedAndRedacted(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "ghp_secretvalue123")
	long := strings.Repeat("x", 400)
	a, err := ParseAnswer("```json\n{\"confidence\": 3, \"confidence_reason\": \"ghp_secretvalue123 " + long + "\", \"findings\": []}\n```")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(a.ConfidenceReason, "ghp_secretvalue123") {
		t.Error("reason not redacted")
	}
	if n := len([]rune(a.ConfidenceReason)); n != 300 {
		t.Errorf("reason is %d runes, want 300", n)
	}
}

func TestParseAnswer_EndLineAndSuggestion(t *testing.T) {
	for _, c := range []struct {
		name, fields   string
		wantEnd        int
		wantSuggestion string
	}{
		{"both valid", `"line": 42, "end_line": 43, "suggestion": "a := 1\nb := 2\n"`, 43, "a := 1\nb := 2"},
		{"single line", `"line": 42, "suggestion": "x"`, 0, "x"},
		{"ten lines", `"line": 1, "end_line": 10, "suggestion": "x"`, 10, "x"},
		{"eleven lines", `"line": 1, "end_line": 11, "suggestion": "x"`, 11, ""},
		{"end before line", `"line": 42, "end_line": 41, "suggestion": "x"`, 0, "x"},
		{"no line", `"suggestion": "x"`, 0, ""},
		{"end_line string", `"line": 42, "end_line": "43", "suggestion": "x"`, 0, "x"},
		{"suggestion number", `"line": 42, "suggestion": 123`, 0, ""},
		{"empty suggestion", `"line": 42, "suggestion": ""`, 0, ""},
		{"crlf and indentation kept", `"line": 42, "end_line": 43, "suggestion": "\tif x {\r\n\t\ty()\r\n"`, 43, "\tif x {\n\t\ty()"},
		{"end_line fraction", `"line": 42, "end_line": 43.5, "suggestion": "x"`, 0, "x"},
	} {
		text := "```json\n{\"findings\": [{\"file\": \"a.go\", \"severity\": \"suggestion\", \"title\": \"T\", \"explanation\": \"E\", " + c.fields + "}]}\n```"
		a, err := ParseAnswer(text)
		if err != nil {
			t.Fatalf("%s: unexpected error %v", c.name, err)
		}
		f := a.Findings[0]
		if f.EndLine != c.wantEnd || f.Suggestion != c.wantSuggestion {
			t.Errorf("%s: got (%d, %q), want (%d, %q)", c.name, f.EndLine, f.Suggestion, c.wantEnd, c.wantSuggestion)
		}
	}
}

// leftOutAnswer wraps a left_out value in an otherwise empty answer.
func leftOutAnswer(leftOut string) string {
	return "```json\n{\"findings\": [], \"left_out\": " + leftOut + "}\n```"
}

func TestParseAnswer_LeftOut(t *testing.T) {
	a, err := ParseAnswer(leftOutAnswer(`[{"title": "A", "rejected": "B"}]`))
	if err != nil {
		t.Fatalf("unexpected error %v", err)
	}
	if want := []LeftOut{{"A", "B"}}; !slices.Equal(a.LeftOut, want) {
		t.Errorf("LeftOut = %+v, want %+v", a.LeftOut, want)
	}
}

func TestParseAnswer_LeftOutBadValues(t *testing.T) {
	for _, c := range []struct{ name, value string }{
		{"string", `"A"`},
		{"object", `{"title": "A", "rejected": "B"}`},
		{"number", `3`},
		{"no title", `[{"rejected": "B"}]`},
		{"title number", `[{"title": 1}]`},
		{"blank title", `[{"title": "  "}]`},
	} {
		a, err := ParseAnswer(leftOutAnswer(c.value))
		if err != nil {
			t.Errorf("%s: unexpected error %v", c.name, err)
		}
		if len(a.LeftOut) != 0 {
			t.Errorf("%s: LeftOut = %+v, want none", c.name, a.LeftOut)
		}
	}
}

func TestParseAnswer_LeftOutMissingRejected(t *testing.T) {
	a, err := ParseAnswer(leftOutAnswer(`[{"title": "A"}]`))
	if err != nil {
		t.Fatalf("unexpected error %v", err)
	}
	if want := []LeftOut{{"A", ""}}; !slices.Equal(a.LeftOut, want) {
		t.Errorf("LeftOut = %+v, want %+v", a.LeftOut, want)
	}
}

func TestParseAnswer_LeftOutLimit(t *testing.T) {
	var entries []string
	for i := 0; i < 120; i++ {
		entries = append(entries, fmt.Sprintf(`{"title": "Title\n%d %s", "rejected": "Rejected\n%d %s"}`, i, strings.Repeat("t", 300), i, strings.Repeat("r", 300)))
	}
	a, err := ParseAnswer(leftOutAnswer("[" + strings.Join(entries, ",") + "]"))
	if err != nil {
		t.Fatalf("unexpected error %v", err)
	}
	if len(a.LeftOut) != 100 {
		t.Fatalf("got %d entries, want 100", len(a.LeftOut))
	}
	for i, e := range a.LeftOut {
		for _, s := range []string{e.Title, e.Rejected} {
			if strings.Contains(s, "\n") || len([]rune(s)) != 200 {
				t.Errorf("entry %d field %q: want one line of 200 runes, got %d", i, s, len([]rune(s)))
			}
		}
	}
}
