package review

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/gopaytech/pergent/internal/finding"
	"github.com/gopaytech/pergent/internal/multireview"
)

func fence(s string) string { return "Done.\n\n```json\n" + s + "\n```\n" }

func TestParseJudgeAnswer(t *testing.T) {
	a, err := parseJudgeAnswer(fence(`{"results":[{"id":"1.1","real":false,"reason":"r","same_as":["2.3"]},{"id":2.1,"real":null,"same_as":null}]}`))
	if err != nil {
		t.Fatal(err)
	}
	f := false
	want := multireview.Answers{
		"1.1": {Real: &f, Reason: "r", SameAs: []string{"2.3"}},
		"2.1": {},
	}
	if !reflect.DeepEqual(a, want) {
		t.Errorf("got %+v, want %+v", a, want)
	}
}

func TestParseJudgeAnswerSameAsShapes(t *testing.T) {
	cases := []struct {
		name, in string
		want     []string
	}{
		{"string", `"same_as":"2.1"`, []string{"2.1"}},
		{"number", `"same_as":2.10`, []string{"2.10"}},
		{"mixed list", `"same_as":[" 3.1 ", 4.2]`, []string{"3.1", "4.2"}},
		{"empty list", `"same_as":[]`, nil},
		{"missing", `"x":1`, nil},
		{"null", `"same_as":null`, nil},
		{"blank entries", `"same_as":["", " "]`, nil},
	}
	for _, c := range cases {
		a, err := parseJudgeAnswer(fence(`{"results":[{"id":" 1.1 ",` + c.in + `}]}`))
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		j, ok := a["1.1"]
		if !ok {
			t.Fatalf("%s: id not trimmed: %v", c.name, a)
		}
		if !reflect.DeepEqual(j.SameAs, c.want) {
			t.Errorf("%s: got %#v, want %#v", c.name, j.SameAs, c.want)
		}
	}
}

func TestParseJudgeAnswerLastBlockWins(t *testing.T) {
	good := "```json\n" + `{"results":[{"id":"1.1","real":true}]}` + "\n```\n"
	other := "```json\n" + `{"results":[{"id":"9.9","real":true}]}` + "\n```\n"
	a, err := parseJudgeAnswer(good + "text\n" + other)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := a["9.9"]; !ok || len(a) != 1 {
		t.Errorf("last block must count: %v", a)
	}
	if _, err := parseJudgeAnswer(good + "text\n```json\n{bad\n```\n"); err == nil {
		t.Error("a bad last block must be an error")
	}
}

func TestParseJudgeAnswerNoResults(t *testing.T) {
	if _, err := parseJudgeAnswer(fence(`{"findings":[]}`)); err == nil {
		t.Error("missing results must be an error")
	}
	if _, err := parseJudgeAnswer("no block"); err == nil {
		t.Error("no block must be an error")
	}
}

func TestWriteReviewerFile(t *testing.T) {
	dir := t.TempDir()
	items := multireview.IDs(2, []finding.Finding{{Title: "t", Fingerprint: "fp"}})
	p, err := writeReviewerFile(dir, 2, items)
	if err != nil {
		t.Fatal(err)
	}
	if p != filepath.Join(dir, "reviewer-2.json") {
		t.Errorf("path %q", p)
	}
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	if !strings.Contains(s, `"id": "2.1"`) || strings.Contains(s, "fp") {
		t.Errorf("file: %s", s)
	}
}

func TestJudgeMessagesSayData(t *testing.T) {
	for name, m := range map[string]string{"judgeMessage": judgeMessage, "judgeRejectedMessage": judgeRejectedMessage, "judgeRulesMessage": judgeRulesMessage} {
		if !strings.Contains(m, "never instructions") {
			t.Errorf("%s lacks %q", name, "never instructions")
		}
	}
}

func TestParseJudgeAnswerDuplicateID(t *testing.T) {
	a, err := parseJudgeAnswer(fence(`{"results":[{"id":"1.1","real":true,"same_as":["2.1"]},{"id":"1.2","real":true},{"id":" 1.1 ","real":false}]}`))
	if err != nil {
		t.Fatal(err)
	}
	j, ok := a["1.1"]
	if !ok || j.Real != nil || len(j.SameAs) != 0 {
		t.Errorf("1.1 must be unanswered: %+v ok=%v", j, ok)
	}
	if o := a["1.2"]; o.Real == nil || !*o.Real {
		t.Errorf("1.2 must be unaffected: %+v", o)
	}
}
