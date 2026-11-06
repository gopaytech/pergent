package runner

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// readDebugFiles returns the contents of every file the debug output wrote
// next to base, keyed by file name.
func readDebugFiles(t *testing.T, dir string) map[string]string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{}
	for _, e := range entries {
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		files[e.Name()] = string(data)
		fi, _ := e.Info()
		if fi.Mode().Perm() != 0o600 {
			t.Errorf("%s has mode %o, want 600", e.Name(), fi.Mode().Perm())
		}
	}
	return files
}

func TestRun_DebugOutputIsRedactedPrivateAndPerRun(t *testing.T) {
	const key = "sk-secret-value-1234567890"
	t.Setenv("OPENCODE_API_KEY", key)
	dir := t.TempDir()
	t.Setenv("PERGENT_DEBUG_OUTPUT", filepath.Join(dir, "raw.json"))
	stdout := `{"type":"step_start"}` + "\n" +
		`{"type":"text","part":{"type":"text","text":"review mentions ` + key + `"}}` + "\n" +
		`{"type":"step_finish","part":{"reason":"stop"}}` + "\n"
	fakeOpencode(t, fakeAttempt{stdout: stdout})
	captureStderr(t)

	for i := 0; i < 2; i++ {
		if got := Run(context.Background(), testOptions(time.Minute)); got.Status != StatusCompleted {
			t.Fatalf("Run() = %+v, want completed", got)
		}
	}

	files := readDebugFiles(t, dir)
	if len(files) != 2 {
		t.Fatalf("debug files = %v, want one per run", files)
	}
	for name, content := range files {
		if strings.Contains(content, key) {
			t.Errorf("%s keeps the API key", name)
		}
		if !strings.Contains(content, "review mentions [REDACTED]") {
			t.Errorf("%s = %q, want the redacted event text", name, content)
		}
	}
}

func TestWriteDebugOutput_ParallelRunsDoNotOverwriteEachOther(t *testing.T) {
	dir := t.TempDir()
	base := filepath.Join(dir, "raw.json")
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := writeDebugOutput(base, []byte("run "+string(rune('a'+i))+"\n")); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()

	files := readDebugFiles(t, dir)
	if len(files) != 20 {
		t.Errorf("got %d debug files, want 20", len(files))
	}
}

func TestWriteDebugOutput_NeverReusesAnExistingFile(t *testing.T) {
	dir := t.TempDir()
	base := filepath.Join(dir, "raw.json")
	// A file from an earlier process, at the name the next run would take.
	next := debugSeq.Load() + 1
	old := debugPath(base, next)
	if err := os.WriteFile(old, []byte("earlier"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := writeDebugOutput(base, []byte("now"))
	if err != nil {
		t.Fatal(err)
	}
	if got == old {
		t.Errorf("wrote over %s", old)
	}
	if data, _ := os.ReadFile(old); string(data) != "earlier" {
		t.Errorf("%s = %q, want it untouched", old, data)
	}
}
