package finding

import (
	"fmt"
	"strings"
	"testing"
)

const oneFinding = `{"file":"a.go","line":1,"severity":"blocking","title":"t","explanation":"e"}`

func TestParseAnswer_Checks(t *testing.T) {
	a, err := ParseAnswer(jsonAnswer(`{"checks":[
		{"section":"Files","name":"Gateway file exists","status":"pass","detail":"a-gw.yaml"},
		{"section":"Files","name":"VirtualService file exists","status":"fail"},
		{"name":"ServiceEntry present","status":" NA "}
	],"findings":[` + oneFinding + `]}`))
	if err != nil {
		t.Fatalf("ParseAnswer() error: %v", err)
	}
	want := []Check{
		{Section: "Files", Name: "Gateway file exists", Status: CheckPass, Detail: "a-gw.yaml"},
		{Section: "Files", Name: "VirtualService file exists", Status: CheckFail},
		{Name: "ServiceEntry present", Status: CheckNA},
	}
	if len(a.Checks) != len(want) {
		t.Fatalf("Checks = %+v, want %+v", a.Checks, want)
	}
	for i := range want {
		if a.Checks[i] != want[i] {
			t.Errorf("Checks[%d] = %+v, want %+v", i, a.Checks[i], want[i])
		}
	}
	if len(a.Findings) != 1 {
		t.Errorf("len(Findings) = %d, want 1", len(a.Findings))
	}
}

func TestParseAnswer_ChecksMissingOrWrongType(t *testing.T) {
	for name, checks := range map[string]string{
		"missing": ``,
		"string":  `"checks":"x",`,
		"object":  `"checks":{},`,
	} {
		t.Run(name, func(t *testing.T) {
			a, err := ParseAnswer(jsonAnswer(`{` + checks + `"findings":[` + oneFinding + `]}`))
			if err != nil {
				t.Fatalf("ParseAnswer() error: %v", err)
			}
			if len(a.Checks) != 0 || len(a.Findings) != 1 {
				t.Errorf("Checks = %+v, Findings = %d; want no checks and 1 finding", a.Checks, len(a.Findings))
			}
		})
	}
}

func TestParseAnswer_InvalidChecksDropped(t *testing.T) {
	a, err := ParseAnswer(jsonAnswer(`{"checks":[
		{"name":"kept one","status":"pass"},
		{"status":"pass"},
		{"name":"bad status","status":"maybe"},
		"not an object",
		{"name":42,"status":"pass"},
		{"name":"kept two","status":"FAIL"}
	],"findings":[]}`))
	if err != nil {
		t.Fatalf("ParseAnswer() error: %v", err)
	}
	if len(a.Checks) != 2 || a.Checks[0].Name != "kept one" || a.Checks[1].Name != "kept two" || a.Checks[1].Status != CheckFail {
		t.Errorf("Checks = %+v, want the two valid ones", a.Checks)
	}
}

func TestParseAnswer_ChecksCappedAt60(t *testing.T) {
	var entries []string
	for i := range 200 {
		entries = append(entries, fmt.Sprintf(`{"name":"check %d","status":"pass"}`, i))
	}
	a, err := ParseAnswer(jsonAnswer(`{"checks":[` + strings.Join(entries, ",") + `],"findings":[]}`))
	if err != nil {
		t.Fatalf("ParseAnswer() error: %v", err)
	}
	if len(a.Checks) != 60 || a.ChecksDropped != 140 {
		t.Errorf("len(Checks) = %d, ChecksDropped = %d; want 60 and 140", len(a.Checks), a.ChecksDropped)
	}
	if a.Checks[59].Name != "check 59" {
		t.Errorf("Checks[59] = %q, want the first 60 kept in order", a.Checks[59].Name)
	}
}

func TestParseAnswer_CheckFieldsCleaned(t *testing.T) {
	t.Setenv("GITLAB_TOKEN", "glpat-secretvalue123456")
	a, err := ParseAnswer(jsonAnswer(`{"checks":[
		{"name":"` + strings.Repeat("n", 500) + `","status":"pass","detail":"line one\nline two"},
		{"name":"secret","status":"pass","detail":"` + strings.Repeat("x", 290) + ` glpat-secretvalue123456"}
	],"findings":[]}`))
	if err != nil {
		t.Fatalf("ParseAnswer() error: %v", err)
	}
	if r := []rune(a.Checks[0].Name); len(r) != 120 || !strings.HasSuffix(a.Checks[0].Name, "…") {
		t.Errorf("name = %d runes, want 120 ending with …", len(r))
	}
	if a.Checks[0].Detail != "line one line two" {
		t.Errorf("detail = %q, want one line", a.Checks[0].Detail)
	}
	if strings.Contains(a.Checks[1].Detail, "glpat") {
		t.Errorf("detail leaks part of the secret: %q", a.Checks[1].Detail)
	}
}

func TestParseAnswer_CheckStatusWordForms(t *testing.T) {
	a, err := ParseAnswer(jsonAnswer(`{"checks":[
		{"name":"a","status":"passed"},
		{"name":"b","status":"Failed"},
		{"name":"c","status":"not applicable"}
	],"findings":[]}`))
	if err != nil {
		t.Fatalf("ParseAnswer() error: %v", err)
	}
	want := []CheckStatus{CheckPass, CheckFail, CheckNA}
	if len(a.Checks) != 3 {
		t.Fatalf("Checks = %+v, want all 3 kept", a.Checks)
	}
	for i, w := range want {
		if a.Checks[i].Status != w {
			t.Errorf("Checks[%d].Status = %q, want %q", i, a.Checks[i].Status, w)
		}
	}
}
