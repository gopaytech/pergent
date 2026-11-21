package review

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/gopaytech/pergent/internal/runner"
)

// Attachment is one file attached to a run: pergent's own file name and its size.
type Attachment struct {
	Name  string
	Bytes int
}

// RunUsage is what one opencode run of a skill used. Only a run that reached
// the runner is recorded: a run that was not started (errNotStarted, a config
// error, no sandbox config) is not. In own-config mode, review-instructions.md
// is the first attachment and its bytes are the prompt again.
type RunUsage struct {
	// Label is the run's label as runWith got it: "1/2", "1/2 repair",
	// "references", "cross", "verify", "check", "check repair".
	Label string
	// Role is the panel member's role ("a" or "b"); empty when the run was not
	// on a panel member.
	Role string
	// PromptBytes is the length of the agent prompt the run got (the untrusted
	// section included when hardened).
	PromptBytes int
	// Attachments are the run's files in order. Bytes is 0 when the file
	// cannot be read.
	Attachments []Attachment
	Usage       runner.Usage
	Steps       []runner.StepUsage
	ToolTotals  runner.ToolTotals
}

// runLog collects the runs of one skill.
type runLog struct {
	mu   sync.Mutex
	runs []RunUsage
}

type runLogKey struct{}

// withRunLog returns a context that carries a new log. Each skill (and the
// impact check) gets its own, so two skills with the same name never mix.
func withRunLog(ctx context.Context) (context.Context, *runLog) {
	l := &runLog{}
	return context.WithValue(ctx, runLogKey{}, l), l
}

func runLogFrom(ctx context.Context) *runLog {
	l, _ := ctx.Value(runLogKey{}).(*runLog)
	return l
}

// add records one run. It is safe for concurrent use.
func (l *runLog) add(u RunUsage) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.runs = append(l.runs, u)
}

// sorted returns the runs in runOrder, or nil when no run started.
func (l *runLog) sorted() []RunUsage {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.runs) == 0 {
		return nil
	}
	out := slices.Clone(l.runs)
	slices.SortStableFunc(out, func(a, b RunUsage) int { return runOrder(a.Label, b.Label) })
	return out
}

// runOrder puts labels that start with N/M first, by N as a number and then by
// label, and every other label after them, sorted as strings.
func runOrder(a, b string) int {
	an, aok := batchNumber(a)
	bn, bok := batchNumber(b)
	switch {
	case aok && bok:
		if an != bn {
			return an - bn
		}
		return strings.Compare(a, b)
	case aok:
		return -1
	case bok:
		return 1
	}
	return strings.Compare(a, b)
}

// batchNumber reads N from a label that starts with "N/M".
func batchNumber(label string) (int, bool) {
	head, _, _ := strings.Cut(label, " ")
	n, m, ok := strings.Cut(head, "/")
	if !ok {
		return 0, false
	}
	i, err := strconv.Atoi(n)
	if err != nil {
		return 0, false
	}
	if _, err := strconv.Atoi(m); err != nil {
		return 0, false
	}
	return i, true
}

// recordRun adds a finished run to the context's log, if it has one.
func recordRun(ctx context.Context, label, role string, promptBytes int, files []string, res runner.RunResult) {
	l := runLogFrom(ctx)
	if l == nil {
		return
	}
	u := RunUsage{
		Label:       label,
		Role:        role,
		PromptBytes: promptBytes,
		Usage:       res.Usage,
		Steps:       res.Steps,
		ToolTotals:  res.ToolTotals,
	}
	for _, p := range files {
		a := Attachment{Name: filepath.Base(p)}
		if st, err := os.Stat(p); err == nil {
			a.Bytes = int(st.Size())
		}
		u.Attachments = append(u.Attachments, a)
	}
	l.add(u)
}
