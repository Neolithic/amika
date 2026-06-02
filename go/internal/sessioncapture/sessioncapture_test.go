package sessioncapture

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCaptureClaude_MirrorsTranscript(t *testing.T) {
	tmp := t.TempDir()
	transcript := filepath.Join(tmp, "src", "abc.jsonl")
	if err := os.MkdirAll(filepath.Dir(transcript), 0o755); err != nil {
		t.Fatal(err)
	}
	const body = `{"role":"user","content":"hi"}` + "\n" + `{"role":"assistant","content":"hello"}` + "\n"
	if err := os.WriteFile(transcript, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	stateDir := filepath.Join(tmp, "state")
	payload := map[string]string{
		"session_id":      "abc",
		"transcript_path": transcript,
		"hook_event_name": "Stop",
	}
	stdin, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}

	if err := CaptureClaude(strings.NewReader(string(stdin)), stateDir); err != nil {
		t.Fatalf("CaptureClaude: %v", err)
	}
	mirrored, err := os.ReadFile(filepath.Join(stateDir, "sessions", "claude", "abc.jsonl"))
	if err != nil {
		t.Fatalf("reading mirror: %v", err)
	}
	if string(mirrored) != body {
		t.Errorf("mirrored body = %q, want %q", mirrored, body)
	}
}

func TestCaptureClaude_MissingTranscriptPath(t *testing.T) {
	stdin := strings.NewReader(`{"session_id":"abc"}`)
	err := CaptureClaude(stdin, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "transcript_path") {
		t.Fatalf("expected transcript_path error, got %v", err)
	}
}

func TestCaptureCodex_MirrorsNewestSession(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".codex", "sessions", "2026", "06", "01")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	older := filepath.Join(dir, "older.jsonl")
	newer := filepath.Join(dir, "newer.jsonl")
	if err := os.WriteFile(older, []byte(`{"k":"older"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(newer, []byte(`{"k":"newer"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Force older to be older than newer regardless of FS resolution.
	past := time.Now().Add(-1 * time.Hour)
	if err := os.Chtimes(older, past, past); err != nil {
		t.Fatal(err)
	}

	stateDir := filepath.Join(home, "state")
	if err := CaptureCodex(home, stateDir); err != nil {
		t.Fatalf("CaptureCodex: %v", err)
	}
	mirrored, err := os.ReadFile(filepath.Join(stateDir, "sessions", "codex", "newer.jsonl"))
	if err != nil {
		t.Fatalf("reading mirror: %v", err)
	}
	if !strings.Contains(string(mirrored), `"newer"`) {
		t.Errorf("mirrored unexpected content: %s", mirrored)
	}
	if _, err := os.Stat(filepath.Join(stateDir, "sessions", "codex", "older.jsonl")); !os.IsNotExist(err) {
		t.Errorf("did not expect older session to be mirrored: %v", err)
	}
}

func TestCaptureCodex_NoSessionsIsNoOp(t *testing.T) {
	home := t.TempDir()
	stateDir := filepath.Join(home, "state")
	if err := CaptureCodex(home, stateDir); err != nil {
		t.Fatalf("CaptureCodex with no sessions: %v", err)
	}
	if _, err := os.Stat(filepath.Join(stateDir, "sessions", "codex")); !os.IsNotExist(err) {
		t.Errorf("expected no capture dir to be created, got %v", err)
	}
}
