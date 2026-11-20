package runner

import (
	"os"
	"path/filepath"
	"testing"
)

// loadProviderError reads one testdata/provider fixture: the "error" value
// of an opencode 2 error event, with each provider's public error body. The
// type of a 4xx or 5xx is a stand-in where opencode's own choice is not
// pinned: only "provider.auth" and a missing status change a class.
func loadProviderError(t *testing.T, name string) EventError {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "provider", name))
	if err != nil {
		t.Fatal(err)
	}
	e := readError(data)
	if e.Name == unreadableError {
		t.Fatalf("%s: not an opencode error", name)
	}
	return e
}

func TestClassify(t *testing.T) {
	want := map[string]ProviderClass{
		"openai_quota_429.json":         ClassQuota,
		"openai_quota_stream.json":      ClassQuota,
		"openai_rate_limit_429.json":    ClassRateLimit,
		"openai_invalid_key_401.json":   ClassAuth,
		"anthropic_credit_400.json":     ClassBilling,
		"anthropic_rate_limit_429.json": ClassRateLimit,
		"anthropic_auth_401.json":       ClassAuth,
		"gemini_quota_day_429.json":     ClassQuota,
		"gemini_quota_minute_429.json":  ClassRateLimit,
		"gemini_denied_403.json":        ClassAuth,
		"openrouter_credits_402.json":   ClassBilling,
		"deepseek_balance_402.json":     ClassBilling,
		"provider_auth_error.json":      ClassAuth,
		"server_503.json":               ClassNone,
		"unknown_429_html.json":         ClassRateLimit,
		"quota_body_500.json":           ClassNone,
		"context_overflow.json":         ClassNone,
	}
	entries, err := os.ReadDir(filepath.Join("testdata", "provider"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != len(want) {
		t.Errorf("%d fixtures, %d expectations: give every fixture a class", len(entries), len(want))
	}
	for _, e := range entries {
		t.Run(e.Name(), func(t *testing.T) {
			class, ok := want[e.Name()]
			if !ok {
				t.Fatalf("no expected class for %s", e.Name())
			}
			if got := Classify(loadProviderError(t, e.Name())); got != class {
				t.Errorf("Classify = %q, want %q", got, class)
			}
		})
	}
}

// An auth error wins over a quota body: the key is the problem, and a
// fallback for auth marks the key until someone edits it.
func TestClassify_RuleOrder(t *testing.T) {
	e := loadProviderError(t, "openai_quota_429.json")
	e.Name = "provider.auth"
	if got := Classify(e); got != ClassAuth {
		t.Errorf("provider.auth with a 429 quota body: Classify = %q, want auth", got)
	}
	e = loadProviderError(t, "openai_quota_429.json")
	e.StatusCode = 401
	if got := Classify(e); got != ClassAuth {
		t.Errorf("401 with a quota body: Classify = %q, want auth", got)
	}
}

// A body that is not the JSON shape a rule reads leaves the class to the
// status alone, and never panics.
func TestClassify_BodyNotJSON(t *testing.T) {
	for _, body := range []string{
		"",
		"Too Many Requests",
		"[1,2,3]",
		`{"error":"insufficient_quota"}`,
		`{"error":{"code":["insufficient_quota"],"details":"none"}}`,
		`{"error":{"details":[{"violations":"PerDay"}],"status":"RESOURCE_EXHAUSTED"}}`,
		`{"error":`,
	} {
		e := EventError{Name: "APIError", StatusCode: 429, ResponseBody: body}
		if got := Classify(e); got != ClassRateLimit {
			t.Errorf("429 with body %q: Classify = %q, want rate_limit", body, got)
		}
		e.StatusCode = 0
		if got := Classify(e); got != ClassNone {
			t.Errorf("no status with body %q: Classify = %q, want none", body, got)
		}
	}
}

func TestProviderClass_Fallback(t *testing.T) {
	for c, want := range map[ProviderClass]bool{
		ClassQuota: true, ClassBilling: true, ClassAuth: true,
		ClassRateLimit: false, ClassNone: false,
	} {
		if got := c.Fallback(); got != want {
			t.Errorf("%q.Fallback() = %v, want %v", c, got, want)
		}
	}
}
