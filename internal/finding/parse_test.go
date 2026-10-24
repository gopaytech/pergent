package finding

import (
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
