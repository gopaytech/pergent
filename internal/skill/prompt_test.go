package skill

import (
	"strings"
	"testing"
)

const defaultBlocking = "Treat these as blocking: bugs, security vulnerabilities, and broken configuration."

func TestPrompt_AppendsDefaultBlockingOnlyWhenMissing(t *testing.T) {
	without := Skill{Name: "a", Body: "Review it.", HasBlocking: false}
	if !strings.Contains(without.Prompt(), defaultBlocking) {
		t.Error("prompt should include the default Blocking section when the skill has none")
	}

	with := Skill{Name: "b", Body: "Review it.\n\n## Blocking\n- crashes", HasBlocking: true}
	if strings.Contains(with.Prompt(), defaultBlocking) {
		t.Error("prompt should not include the default Blocking section when the skill defines one")
	}
}

func TestPrompt_AlwaysAppendsOutputFormat(t *testing.T) {
	for _, hasBlocking := range []bool{false, true} {
		s := Skill{Name: "a", Body: "Review it.", HasBlocking: hasBlocking}
		p := s.Prompt()
		if !strings.HasPrefix(p, "Review it.") {
			t.Errorf("HasBlocking=%v: prompt should start with the skill body", hasBlocking)
		}
		if !strings.Contains(p, "## Output format") {
			t.Errorf("HasBlocking=%v: prompt missing output format heading", hasBlocking)
		}
		if !strings.Contains(p, `{"findings": []}`) {
			t.Errorf("HasBlocking=%v: prompt missing empty findings example", hasBlocking)
		}
	}
}
