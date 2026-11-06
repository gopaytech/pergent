package review

import (
	"strings"
	"testing"
)

// A key across the cut must not survive in part: redaction can only match a
// whole key, so the text is redacted first and cut after.
func TestExcerpt_NoPartOfAKeySurvivesTheCut(t *testing.T) {
	const key = "sk-test-0123456789abcdefghij"
	t.Setenv("OPENCODE_API_KEY", key)

	for _, pad := range []int{0, 150, excerptLen - 20, excerptLen - 10, excerptLen - 1, excerptLen} {
		answer := strings.Repeat("a", pad) + key + " and more text after the key"
		got := excerpt(answer)
		for n := 4; n <= len(key); n++ {
			if strings.Contains(got, key[:n]) {
				t.Errorf("pad %d: excerpt keeps the first %d characters of the key: %q", pad, n, got)
				break
			}
		}
	}
}

func TestExcerpt_CutsLongTextAndKeepsShortText(t *testing.T) {
	if got := excerpt("short"); got != "short" {
		t.Errorf("excerpt(short) = %q, want it unchanged", got)
	}
	long := strings.Repeat("é", excerptLen+50)
	if got := excerpt(long); got != strings.Repeat("é", excerptLen) {
		t.Errorf("excerpt(long) has %d runes, want %d", len([]rune(got)), excerptLen)
	}
}
