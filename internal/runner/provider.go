package runner

import (
	"encoding/json"
	"regexp"
	"strings"
)

// ProviderClass sorts a provider error by what the caller can do about it.
// pergent.sh moves a review to another model for quota, billing and auth
// (Fallback); rate_limit is a short wait, so it is retried like any other
// transient error. The strings are a contract with pergent.sh.
type ProviderClass string

const (
	ClassNone      ProviderClass = ""
	ClassQuota     ProviderClass = "quota"
	ClassBilling   ProviderClass = "billing"
	ClassAuth      ProviderClass = "auth"
	ClassRateLimit ProviderClass = "rate_limit"
)

// Fallback reports whether the class is one waiting will not fix: the
// quota is used up, the account has no credit, or the key is refused.
func (c ProviderClass) Fallback() bool {
	return c == ClassQuota || c == ClassBilling || c == ClassAuth
}

// providerBody is the part of a provider's error body the rules read. It is
// decoded once and leniently: a body that is not JSON, or a field of another
// type, leaves the field empty.
type providerBody struct {
	code, typ, status, message string
	quotaIDs                   []string
}

type providerRule struct {
	name  string
	class ProviderClass
	match func(e EventError, b providerBody) bool
}

var creditTooLow = regexp.MustCompile(`(?i)credit balance is too low`)

// providerRules classifies a provider error; the first match wins. The list
// is strict on purpose: a wrong quota guess moves a review to a weaker or
// costlier model for an error a short wait would fix, so an unknown 429 is
// rate_limit, and a 5xx, a connection error or a context overflow is no
// class at all. Add a provider by adding a row and a testdata/provider
// fixture.
var providerRules = []providerRule{
	{"auth", ClassAuth, func(e EventError, _ providerBody) bool {
		return e.Name == "provider.auth" || e.StatusCode == 401 || e.StatusCode == 403
	}},
	{"payment required", ClassBilling, func(e EventError, _ providerBody) bool {
		return e.StatusCode == 402
	}},
	{"credit balance too low", ClassBilling, func(e EventError, b providerBody) bool {
		return e.StatusCode == 400 && (creditTooLow.MatchString(e.Message) || creditTooLow.MatchString(b.message))
	}},
	{"insufficient quota", ClassQuota, func(e EventError, b providerBody) bool {
		return e.StatusCode == 429 && (b.code == "insufficient_quota" || b.typ == "insufficient_quota")
	}},
	{"daily quota exhausted", ClassQuota, func(e EventError, b providerBody) bool {
		if e.StatusCode != 429 || b.status != "RESOURCE_EXHAUSTED" {
			return false
		}
		for _, id := range b.quotaIDs {
			if strings.Contains(id, "PerDay") {
				return true
			}
		}
		return false
	}},
	// An insufficient_quota stream error comes as a provider error with no
	// status code.
	{"insufficient quota in the stream", ClassQuota, func(e EventError, b providerBody) bool {
		return e.StatusCode == 0 && strings.HasPrefix(e.Name, "provider.") && b.code == "insufficient_quota"
	}},
	{"rate limit", ClassRateLimit, func(e EventError, _ providerBody) bool {
		return e.StatusCode == 429
	}},
}

// Classify sorts an opencode error event into a ProviderClass, or ClassNone
// when no rule matches. The body is read here and nowhere else.
func Classify(e EventError) ProviderClass {
	b := decodeProviderBody(e.ResponseBody)
	for _, r := range providerRules {
		if r.match(e, b) {
			return r.class
		}
	}
	return ClassNone
}

func decodeProviderBody(body string) providerBody {
	var b providerBody
	var top struct {
		Error map[string]json.RawMessage `json:"error"`
	}
	if body == "" || json.Unmarshal([]byte(body), &top) != nil {
		return b
	}
	b.code = rawString(top.Error["code"])
	b.typ = rawString(top.Error["type"])
	b.status = rawString(top.Error["status"])
	b.message = rawString(top.Error["message"])
	var details []map[string]json.RawMessage
	if json.Unmarshal(top.Error["details"], &details) != nil {
		return b
	}
	for _, d := range details {
		var violations []map[string]json.RawMessage
		if json.Unmarshal(d["violations"], &violations) != nil {
			continue
		}
		for _, v := range violations {
			if id := rawString(v["quotaId"]); id != "" {
				b.quotaIDs = append(b.quotaIDs, id)
			}
		}
	}
	return b
}

// rawString returns raw as a string, or "" when it is missing or not a
// JSON string.
func rawString(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) != nil {
		return ""
	}
	return s
}
