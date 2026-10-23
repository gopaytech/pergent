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
			fs, err := ParseAnswer(tt.answer)
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
