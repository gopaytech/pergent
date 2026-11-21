package review

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/gopaytech/pergent/internal/config"
	"github.com/gopaytech/pergent/internal/runner"
	"github.com/gopaytech/pergent/internal/skill"
)

// newTestReviewer builds a reviewer the way Run does: a hardened sandbox, a
// one-minute budget and one slot.
func newTestReviewer(t *testing.T, run RunFunc) *reviewer {
	t.Helper()
	return &reviewer{
		req: Request{
			RepoPath: t.TempDir(),
			Budget:   time.Minute,
			MaxTurns: 20,
			Sandbox:  Sandbox{Hardened: true, ExcludeEnv: []string{"GITLAB_TOKEN"}},
			Run:      run,
		},
		slots:    make(chan struct{}, 1),
		start:    time.Now(),
		deadline: time.Now().Add(time.Minute),
	}
}

func configModel(t *testing.T, cfg []byte) string {
	t.Helper()
	var c struct {
		Model string `json:"model"`
	}
	if err := json.Unmarshal(cfg, &c); err != nil {
		t.Fatalf("config: %v\n%s", err, cfg)
	}
	return c.Model
}

func TestRunWithMemberUsesItsModelAndKey(t *testing.T) {
	t.Setenv("OPENCODE_PROVIDER", "anthropic")
	t.Setenv("OPENCODE_MODEL", "model-a")
	var got runner.Options
	var cfg []byte
	r := newTestReviewer(t, func(_ context.Context, o runner.Options) runner.RunResult {
		got = o
		cfg, _ = os.ReadFile(o.ConfigPath)
		return runner.RunResult{Status: runner.StatusCompleted, Output: "ok"}
	})
	m := &Member{Role: "b", Model: config.Model{Provider: "openai", Model: "model-b"}, Key: "key-b"}
	if _, err := r.runWith(context.Background(), skill.Skill{Name: "s", Body: "x"}, "judge b", "prompt", nil, "m", runMode{member: m}); err != nil {
		t.Fatal(err)
	}
	if g := configModel(t, cfg); g != "openai/model-b" {
		t.Errorf("config model = %q", g)
	}
	if !bytes.Contains(cfg, []byte("{env:OPENCODE_API_KEY}")) {
		t.Errorf("config does not name the key as {env:OPENCODE_API_KEY}: %s", cfg)
	}
	if bytes.Contains(cfg, []byte("key-b")) {
		t.Errorf("the key is in the config file: %s", cfg)
	}
	if !slices.Equal(got.Env, []string{"OPENCODE_API_KEY=key-b"}) {
		t.Errorf("Env = %v", got.Env)
	}
	for _, name := range []string{"OPENCODE_API_KEY", "PERGENT_PANEL_B_API_KEY", "GITLAB_TOKEN"} {
		if !slices.Contains(got.ExcludeEnv, name) {
			t.Errorf("ExcludeEnv %v lacks %s", got.ExcludeEnv, name)
		}
	}
}

func TestRunWithNoMemberIsUnchanged(t *testing.T) {
	t.Setenv("OPENCODE_PROVIDER", "anthropic")
	t.Setenv("OPENCODE_MODEL", "model-a")
	var got runner.Options
	var cfg []byte
	r := newTestReviewer(t, func(_ context.Context, o runner.Options) runner.RunResult {
		got = o
		cfg, _ = os.ReadFile(o.ConfigPath)
		return runner.RunResult{Status: runner.StatusCompleted, Output: "ok"}
	})
	if _, err := r.runWith(context.Background(), skill.Skill{Name: "s", Body: "x"}, "1/1", "prompt", nil, "m", runMode{}); err != nil {
		t.Fatal(err)
	}
	if got.Env != nil {
		t.Errorf("Env = %v, want nil", got.Env)
	}
	if g := configModel(t, cfg); g != "anthropic/model-a" {
		t.Errorf("config model = %q", g)
	}
	if slices.Contains(got.ExcludeEnv, "OPENCODE_API_KEY") {
		t.Errorf("ExcludeEnv = %v", got.ExcludeEnv)
	}
}

func TestRecordRunKeepsRole(t *testing.T) {
	t.Setenv("OPENCODE_PROVIDER", "anthropic")
	t.Setenv("OPENCODE_MODEL", "model-a")
	r := newTestReviewer(t, func(context.Context, runner.Options) runner.RunResult {
		return runner.RunResult{Status: runner.StatusCompleted, Output: "ok"}
	})
	ctx, l := withRunLog(context.Background())
	m := &Member{Role: "b", Model: config.Model{Provider: "openai", Model: "model-b"}, Key: "k"}
	if _, err := r.runWith(ctx, skill.Skill{Name: "s", Body: "x"}, "judge b", "p", nil, "m", runMode{member: m}); err != nil {
		t.Fatal(err)
	}
	if len(l.runs) != 1 || l.runs[0].Role != "b" {
		t.Errorf("runs = %+v", l.runs)
	}
}
