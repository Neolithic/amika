package eventlog

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCaptureClaude_WritesEvent(t *testing.T) {
	stateDir := t.TempDir()
	// cwd points at a non-repo dir so git context is deterministically null.
	cwd := t.TempDir()
	payload := `{"session_id":"sess-1","cwd":"` + cwd + `","hook_event_name":"PostToolUse","tool_name":"Bash"}`

	if err := CaptureClaude(strings.NewReader(payload), stateDir); err != nil {
		t.Fatalf("CaptureClaude: %v", err)
	}

	events := readEvents(t, stateDir, SourceClaude)
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}
	ev := events[0]
	if ev.Source != SourceClaude {
		t.Errorf("Source = %q, want claude", ev.Source)
	}
	if ev.HookEvent != "PostToolUse" {
		t.Errorf("HookEvent = %q, want PostToolUse", ev.HookEvent)
	}
	if ev.SessionID != "sess-1" {
		t.Errorf("SessionID = %q, want sess-1", ev.SessionID)
	}
	if ev.Seq != 0 {
		t.Errorf("Seq = %d, want 0", ev.Seq)
	}
	if ev.Git != nil {
		t.Errorf("Git = %+v, want nil for non-repo cwd", ev.Git)
	}
	var payloadBack map[string]any
	if err := json.Unmarshal(ev.Payload, &payloadBack); err != nil {
		t.Fatalf("payload not round-tripped: %v", err)
	}
	if payloadBack["tool_name"] != "Bash" {
		t.Errorf("payload tool_name = %v, want Bash", payloadBack["tool_name"])
	}
}

func TestCaptureClaude_SecondEventSameSessionIncrementsSeq(t *testing.T) {
	stateDir := t.TempDir()
	cwd := t.TempDir()
	mk := func(event string) string {
		return `{"session_id":"sess-1","cwd":"` + cwd + `","hook_event_name":"` + event + `"}`
	}
	if err := CaptureClaude(strings.NewReader(mk("UserPromptSubmit")), stateDir); err != nil {
		t.Fatal(err)
	}
	if err := CaptureClaude(strings.NewReader(mk("Stop")), stateDir); err != nil {
		t.Fatal(err)
	}

	root := EventsDir(stateDir, SourceClaude)
	sessionDirs, _ := os.ReadDir(root)
	if len(sessionDirs) != 1 {
		t.Fatalf("got %d session dirs, want 1 (events must share a session dir)", len(sessionDirs))
	}
	events := readEvents(t, stateDir, SourceClaude)
	if len(events) != 2 {
		t.Fatalf("got %d events, want 2", len(events))
	}
	seqs := map[int]bool{}
	for _, ev := range events {
		seqs[ev.Seq] = true
	}
	if !seqs[0] || !seqs[1] {
		t.Errorf("expected seq 0 and 1, got %v", seqs)
	}
}

func TestCaptureClaude_MalformedPayloadStillRecorded(t *testing.T) {
	stateDir := t.TempDir()
	if err := CaptureClaude(strings.NewReader("not json"), stateDir); err != nil {
		t.Fatalf("CaptureClaude: %v", err)
	}
	events := readEvents(t, stateDir, SourceClaude)
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}
	// SessionID falls back to "unknown"; payload preserved as a JSON string.
	var s string
	if err := json.Unmarshal(events[0].Payload, &s); err != nil || s != "not json" {
		t.Errorf("payload = %s (err %v), want JSON string \"not json\"", events[0].Payload, err)
	}
}

func TestCaptureCodex_DerivesSessionFromRollout(t *testing.T) {
	stateDir := t.TempDir()
	home := t.TempDir()
	t.Setenv("CODEX_HOME", "")
	rolloutDir := filepath.Join(home, ".codex", "sessions", "2026", "06", "02")
	if err := os.MkdirAll(rolloutDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(rolloutDir, "rollout-abc-uuid.jsonl"), "{}\n")

	payload := `{"type":"agent-turn-complete","turn-id":"t1"}`
	if err := CaptureCodex(payload, home, stateDir); err != nil {
		t.Fatalf("CaptureCodex: %v", err)
	}
	events := readEvents(t, stateDir, SourceCodex)
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}
	if events[0].HookEvent != "agent-turn-complete" {
		t.Errorf("HookEvent = %q, want agent-turn-complete", events[0].HookEvent)
	}
	if events[0].SessionID != "rollout-abc-uuid" {
		t.Errorf("SessionID = %q, want rollout-abc-uuid (derived from rollout)", events[0].SessionID)
	}
}

func TestCaptureCodex_PayloadSessionIDWins(t *testing.T) {
	stateDir := t.TempDir()
	home := t.TempDir()
	t.Setenv("CODEX_HOME", "")
	payload := `{"type":"agent-turn-complete","conversation_id":"conv-42"}`
	if err := CaptureCodex(payload, home, stateDir); err != nil {
		t.Fatalf("CaptureCodex: %v", err)
	}
	events := readEvents(t, stateDir, SourceCodex)
	if len(events) != 1 || events[0].SessionID != "conv-42" {
		t.Fatalf("SessionID = %v, want conv-42", events)
	}
}

// readEvents reads and decodes every event file for src under stateDir.
func readEvents(t *testing.T, stateDir string, src Source) []Event {
	t.Helper()
	root := EventsDir(stateDir, src)
	var events []Event
	sessionDirs, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("reading %s: %v", root, err)
	}
	for _, sd := range sessionDirs {
		if !sd.IsDir() {
			continue
		}
		files, err := os.ReadDir(filepath.Join(root, sd.Name()))
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range files {
			if !strings.HasPrefix(f.Name(), "event_") {
				continue
			}
			data, err := os.ReadFile(filepath.Join(root, sd.Name(), f.Name()))
			if err != nil {
				t.Fatal(err)
			}
			var ev Event
			if err := json.Unmarshal(data, &ev); err != nil {
				t.Fatalf("decoding %s: %v", f.Name(), err)
			}
			events = append(events, ev)
		}
	}
	return events
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
