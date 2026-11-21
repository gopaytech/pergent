package review

import (
	"testing"
)

func TestParseJudgeAnswer(t *testing.T) {
	text := "done\n```json\n" + `{"results":[{"id":"a1","real":false,"reason":"r","same_as":"b2"},{"id":2,"real":null,"same_as":null}]}` + "\n```\n"
	got, err := parseJudgeAnswer(text)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d answers: %v", len(got), got)
	}
	a1 := got["a1"]
	if a1.Real == nil || *a1.Real || a1.Reason != "r" || a1.SameAs != "b2" {
		t.Errorf("a1 = %+v", a1)
	}
	two, ok := got["2"]
	if !ok || two.Real != nil || two.Reason != "" || two.SameAs != "" {
		t.Errorf("2 = %+v, ok=%v", two, ok)
	}
}

func TestParseJudgeAnswerLastBlockWins(t *testing.T) {
	good := "```json\n" + `{"results":[{"id":"a1","real":true}]}` + "\n```\n"
	other := "```json\n" + `{"results":[{"id":"a1","real":false}]}` + "\n```\n"
	got, err := parseJudgeAnswer(good + "then\n" + other)
	if err != nil {
		t.Fatal(err)
	}
	if r := got["a1"].Real; r == nil || *r {
		t.Errorf("last block should win: %+v", got)
	}
	if _, err := parseJudgeAnswer(good + "then\n```json\n{bad\n```\n"); err == nil {
		t.Error("a bad last block must be an error, not the earlier block")
	}
}

func TestParseJudgeAnswerNoResults(t *testing.T) {
	if _, err := parseJudgeAnswer("```json\n{\"findings\":[]}\n```\n"); err == nil {
		t.Error("want error")
	}
	if _, err := parseJudgeAnswer("no block"); err == nil {
		t.Error("want error for no block")
	}
}
