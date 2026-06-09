package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// countEventFiles walks dir and counts files named like event_*.json.
func countEventFiles(t *testing.T, dir string) int {
	t.Helper()
	n := 0
	_ = filepath.WalkDir(dir, func(_ string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() && strings.HasPrefix(d.Name(), "event_") {
			n++
		}
		return nil
	})
	return n
}

func TestHook_Claude_WritesEvent(t *testing.T) {
	stateDir := t.TempDir()
	t.Setenv("AMIKA_STATE_DIRECTORY", stateDir)

	payload := `{"session_id":"abc","cwd":"` + t.TempDir() + `","hook_event_name":"PostToolUse"}`
	if _, err := runRootCommandStdin(strings.NewReader(payload), "hook", "--source", "claude"); err != nil {
		t.Fatalf("hook --source claude: %v", err)
	}
	if got := countEventFiles(t, filepath.Join(stateDir, "events", "claude")); got != 1 {
		t.Fatalf("got %d claude event files, want 1", got)
	}
}

// TestHook_Codex_AcceptsNotifyPayload guards against re-introducing
// cobra.NoArgs on the hook command: Codex's notify hook appends one trailing
// JSON payload to the configured argv, so the command must accept it.
func TestHook_Codex_AcceptsNotifyPayload(t *testing.T) {
	stateDir := t.TempDir()
	t.Setenv("AMIKA_STATE_DIRECTORY", stateDir)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CODEX_HOME", "")

	payload := `{"type":"agent-turn-complete","turn-id":"t1"}`
	if _, err := runRootCommand("hook", "--source", "codex", payload); err != nil {
		t.Fatalf("hook --source codex with payload: %v", err)
	}
	if got := countEventFiles(t, filepath.Join(stateDir, "events", "codex")); got != 1 {
		t.Fatalf("got %d codex event files, want 1", got)
	}
}

func TestHook_UnknownSource(t *testing.T) {
	t.Setenv("AMIKA_STATE_DIRECTORY", t.TempDir())
	if _, err := runRootCommand("hook", "--source", "bogus"); err == nil {
		t.Fatal("expected error for unknown --source, got nil")
	}
}
