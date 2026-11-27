package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"

	"github.com/gopaytech/pergent/internal/config"
)

// Stats is what `opencode stats` reports for one data directory: every
// session in it, sub-agents included. `opencode run --format json` prints the
// main session only, so this is where the sub-agents' tokens show.
type Stats struct {
	Sessions  int
	Subagents int
	Steps     int
	Tokens    Usage
	Models    []ModelStats
}

// ModelStats is one model's share of Stats.
type ModelStats struct {
	Provider string
	Model    string
	Steps    int
	Tokens   Usage
}

// maxStatsOutput caps the stats output pergent reads.
const maxStatsOutput = 4 << 20

type statsTokens struct {
	Input     int `json:"input"`
	Output    int `json:"output"`
	Reasoning int `json:"reasoning"`
	Cache     struct {
		Read  int `json:"read"`
		Write int `json:"write"`
	} `json:"cache"`
}

func (t statsTokens) usage() (Usage, error) {
	if t.Input < 0 || t.Output < 0 || t.Reasoning < 0 || t.Cache.Read < 0 || t.Cache.Write < 0 {
		return Usage{}, errors.New("a token count is negative")
	}
	return Usage{Input: t.Input, Output: t.Output, Reasoning: t.Reasoning, CacheRead: t.Cache.Read, CacheWrite: t.Cache.Write}, nil
}

// ParseStats reads the output of `opencode stats --json` (opencode 2: the
// SessionStatsInfo shape). Other keys are ignored. A negative count is an
// error, and so is an answer with no sessions or no tokens (`{}`, `null`, an
// error object): the numbers feed a usage limit and must not be able to
// lower it, so the caller falls back to the stream count instead.
func ParseStats(data []byte) (Stats, error) {
	var in struct {
		Sessions  *int         `json:"sessions"`
		Subagents int          `json:"subagents"`
		Steps     int          `json:"steps"`
		Tokens    *statsTokens `json:"tokens"`
		Models    []struct {
			Model struct {
				ID         string `json:"id"`
				ProviderID string `json:"providerID"`
			} `json:"model"`
			Steps  int         `json:"steps"`
			Tokens statsTokens `json:"tokens"`
		} `json:"models"`
	}
	if err := json.Unmarshal(data, &in); err != nil {
		return Stats{}, fmt.Errorf("reading opencode's stats: %w", err)
	}
	if in.Sessions == nil || in.Tokens == nil {
		return Stats{}, errors.New("opencode's stats have no sessions or no tokens")
	}
	if *in.Sessions < 0 || in.Subagents < 0 || in.Steps < 0 {
		return Stats{}, errors.New("opencode's stats hold a negative count")
	}
	out := Stats{Sessions: *in.Sessions, Subagents: in.Subagents, Steps: in.Steps}
	var err error
	if out.Tokens, err = in.Tokens.usage(); err != nil {
		return Stats{}, fmt.Errorf("opencode's stats: %w", err)
	}
	for _, m := range in.Models {
		if m.Steps < 0 {
			return Stats{}, errors.New("opencode's stats hold a negative count")
		}
		u, err := m.Tokens.usage()
		if err != nil {
			return Stats{}, fmt.Errorf("opencode's stats: %w", err)
		}
		out.Models = append(out.Models, ModelStats{Provider: m.Model.ProviderID, Model: m.Model.ID, Steps: m.Steps, Tokens: u})
	}
	return out, nil
}

// capWriter keeps up to max bytes and notes when more came.
type capWriter struct {
	buf  bytes.Buffer
	max  int
	over bool
}

func (w *capWriter) Write(p []byte) (int, error) {
	if room := w.max - w.buf.Len(); len(p) > room {
		w.over = true
		p = p[:max(room, 0)]
	}
	w.buf.Write(p)
	return len(p), nil
}

// ReadStats runs `opencode stats` on dataDir (a run's XDG_DATA_HOME, kept with
// Options.DataDir) and returns what it reports. It reads the database file
// itself (--standalone: no background service), from an empty directory, with
// no key and no token in its environment.
func ReadStats(ctx context.Context, dataDir string) (Stats, error) {
	work, err := os.MkdirTemp("", config.TempPrefix+"stats-")
	if err != nil {
		return Stats{}, fmt.Errorf("creating the stats directory: %w", err)
	}
	defer os.RemoveAll(work)
	state, err := newStateDir()
	if err != nil {
		return Stats{}, err
	}
	defer os.RemoveAll(state)

	cmd := exec.CommandContext(ctx, "opencode", "stats", "--standalone", "--all", "--models", "--json")
	cmd.Dir = work
	cmd.WaitDelay = waitDelay
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		if err != nil {
			return cmd.Process.Kill()
		}
		return nil
	}
	cmd.Env = append(allowedEnvOnly(os.Environ(), "PATH", "HOME", "TMPDIR"),
		"PWD="+work,
		"XDG_DATA_HOME="+dataDir,
		"XDG_STATE_HOME="+state,
		"OPENCODE_DISABLE_AUTOUPDATE=true",
		"OPENCODE_DISABLE_PROJECT_CONFIG=true",
		"OPENCODE_CONFIG_PROJECT_DISABLE=true",
		"OPENCODE_DISABLE_CLAUDE_CODE=true",
	)
	out := &capWriter{max: maxStatsOutput}
	cmd.Stdout = out
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return Stats{}, fmt.Errorf("opencode stats stopped: %w", ctx.Err())
		}
		return Stats{}, fmt.Errorf("opencode stats failed: %w", err)
	}
	if out.over {
		return Stats{}, errors.New("opencode stats printed more than 4 MiB")
	}
	return ParseStats(out.buf.Bytes())
}

// allowedEnvOnly returns the entries of env named in names.
func allowedEnvOnly(env []string, names ...string) []string {
	var out []string
	for _, kv := range env {
		for _, n := range names {
			if len(kv) > len(n) && kv[:len(n)] == n && kv[len(n)] == '=' {
				out = append(out, kv)
			}
		}
	}
	return out
}
