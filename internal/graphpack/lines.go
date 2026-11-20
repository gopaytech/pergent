package graphpack

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// oldHunkHeader captures a hunk's old-file range: start and count. It reads
// the old side, unlike hunkHeader in internal/diff, which reads the new side.
var oldHunkHeader = regexp.MustCompile(`^@@ -(\d+)(?:,(\d+))? \+\d+(?:,(\d+))? @@`)

// OldLines returns the old-side line numbers a patch changes, sorted and
// unique: each removed line, plus, for each run of added lines, the old line
// just before it (or the hunk's first old line when the run starts the
// hunk). Lines below 1 are left out, so a new file has none.
func OldLines(patch string) []int {
	seen := map[int]bool{}
	var oldLeft, newLeft int
	var start, next, prev int // the hunk's first old line, the next one, the last one seen (0: none yet)
	inRun := false
	for _, line := range strings.Split(patch, "\n") {
		if oldLeft <= 0 && newLeft <= 0 {
			m := oldHunkHeader.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			start, _ = strconv.Atoi(m[1])
			oldLeft, newLeft = count(m[2]), count(m[3])
			next, prev, inRun = start, 0, false
			continue
		}
		if line == "" {
			continue
		}
		switch line[0] {
		case ' ', '-':
			if line[0] == '-' {
				seen[next] = true
			} else {
				newLeft--
			}
			prev, next = next, next+1
			oldLeft--
			inRun = false
		case '+':
			if !inRun {
				if prev > 0 {
					seen[prev] = true
				} else {
					seen[start] = true
				}
			}
			newLeft--
			inRun = true
		case '\\':
			// "\ No newline at end of file"
		default:
			oldLeft, newLeft = 0, 0
		}
	}
	out := make([]int, 0, len(seen))
	for n := range seen {
		if n >= 1 {
			out = append(out, n)
		}
	}
	if len(out) == 0 {
		return nil
	}
	sort.Ints(out)
	return out
}

// count reads a hunk header's line count; a missing count means 1.
func count(s string) int {
	if s == "" {
		return 1
	}
	n, _ := strconv.Atoi(s)
	return n
}
