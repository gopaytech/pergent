package redact

import (
	"strings"
	"testing"
)

func TestString_EnvSecrets(t *testing.T) {
	t.Setenv("OPENCODE_API_KEY", "sk-live-0123456789abcdef")
	t.Setenv("GITHUB_TOKEN", "ghp_abcdefghijklmnop")
	t.Setenv("GITLAB_TOKEN", "glpat-xyzxyzxyzxyz")

	in := "provider said: invalid key sk-live-0123456789abcdef; github ghp_abcdefghijklmnop; gitlab glpat-xyzxyzxyzxyz"
	got := String(in)

	for _, secret := range []string{"sk-live-0123456789abcdef", "ghp_abcdefghijklmnop", "glpat-xyzxyzxyzxyz"} {
		if strings.Contains(got, secret) {
			t.Errorf("String() leaked %q: %q", secret, got)
		}
	}
	if strings.Count(got, "[REDACTED]") != 3 {
		t.Errorf("String() = %q, want 3 redactions", got)
	}
}

func TestString_ShortEnvValueIgnored(t *testing.T) {
	t.Setenv("OPENCODE_API_KEY", "x")
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("GITLAB_TOKEN", "")

	in := "the next step reads a file"
	if got := String(in); got != in {
		t.Errorf("String(%q) = %q, want unchanged", in, got)
	}
}

func TestString_Patterns(t *testing.T) {
	t.Setenv("OPENCODE_API_KEY", "")
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("GITLAB_TOKEN", "")

	tests := []struct {
		name   string
		in     string
		leaked string
	}{
		{"bearer header", "request failed: Authorization: Bearer eyJhbGciOiJIUzI1NiJ9.payload.sig", "eyJhbGciOiJIUzI1NiJ9"},
		{"bearer in text", `sent "bearer abc123def456ghi" to upstream`, "abc123def456ghi"},
		{"x-api-key header", `headers: {"x-api-key": "secret-key-value"}`, "secret-key-value"},
		{"private-token header", "PRIVATE-TOKEN: glpat-abc", "glpat-abc"},
		{"url key param", "GET https://example.com/v1/models?key=AIzaSyExample&alt=json failed", "AIzaSyExample"},
		{"url access_token param", "https://example.com/x?access_token=tok123", "tok123"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := String(tt.in)
			if strings.Contains(got, tt.leaked) {
				t.Errorf("String(%q) = %q, still contains %q", tt.in, got, tt.leaked)
			}
			if !strings.Contains(got, "[REDACTED]") {
				t.Errorf("String(%q) = %q, want a [REDACTED] marker", tt.in, got)
			}
		})
	}
}

func TestString_PlainTextUnchanged(t *testing.T) {
	t.Setenv("OPENCODE_API_KEY", "")
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("GITLAB_TOKEN", "")

	for _, in := range []string{
		"- **[main.go:42]** The token is never refreshed; see `auth.go`.",
		"The client sends a Bearer token but never checks the Authorization: header.",
	} {
		if got := String(in); got != in {
			t.Errorf("String(%q) = %q, want unchanged", in, got)
		}
	}
}
