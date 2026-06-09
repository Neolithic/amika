package eventlog

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// GitInfo is the git state of the working directory a hook fired in. A nil
// *GitInfo (serialized as JSON null) means the directory was not a git
// repository or git was unavailable.
type GitInfo struct {
	// RepoRoot is the absolute path to the repository's top-level directory.
	RepoRoot string `json:"repo_root"`
	// Commit is the full SHA of HEAD, or "" in a repository with no commits.
	Commit string `json:"commit"`
	// Branch is the abbreviated ref name of HEAD, or "HEAD" when detached.
	Branch string `json:"branch"`
	// Dirty reports whether the working tree had uncommitted changes.
	Dirty bool `json:"dirty"`
}

// Event is one captured agent hook call. It is the on-disk record amikalog
// appends; the schema is intended to stay stable so a later push step can
// upload event files verbatim.
type Event struct {
	// Source is the agent that fired the hook (claude or codex).
	Source Source `json:"source"`
	// HookEvent is the hook's event name: Claude's hook_event_name (e.g.
	// "PostToolUse") or Codex's notify payload type (e.g. "agent-turn-complete").
	HookEvent string `json:"hook_event"`
	// SessionID identifies the agent session this event belongs to.
	SessionID string `json:"session_id"`
	// Timestamp is when the event was captured (RFC3339 with nanoseconds, UTC).
	Timestamp string `json:"timestamp"`
	// Seq is the event's position within its session directory, starting at 0.
	Seq int `json:"seq"`
	// CWD is the working directory the hook reported (or the process cwd).
	CWD string `json:"cwd"`
	// Git is the git state of CWD, or nil when CWD is not a repository.
	Git *GitInfo `json:"git"`
	// Payload is the raw hook payload exactly as the agent provided it.
	Payload json.RawMessage `json:"payload"`
}

// fileTimestamp renders t as a filesystem-safe, lexically sortable UTC stamp,
// e.g. "20060102T150405.000000000Z". t is assumed to already be in UTC.
func fileTimestamp(t time.Time) string {
	return t.Format("20060102T150405.000000000") + "Z"
}

// rawPayload returns data as a json.RawMessage when it is valid JSON, otherwise
// wrapping the bytes as a JSON string so the event always round-trips.
func rawPayload(data []byte) json.RawMessage {
	if len(data) > 0 && json.Valid(data) {
		return json.RawMessage(data)
	}
	encoded, err := json.Marshal(string(data))
	if err != nil {
		return json.RawMessage(`""`)
	}
	return json.RawMessage(encoded)
}

// writeEvent appends ev as a new JSON file under
// <stateDir>/events/<source>/sessions/<ts>_<session-id>/event_<seq>_<ts>.json.
//
// Writes are append-only: a new file is created for every call and existing
// files are never modified. The sequence number is derived from the count of
// events already in the session directory; on a name collision (concurrent
// hooks racing for the same seq) the seq is bumped and the create retried, so
// no event is ever clobbered.
func writeEvent(stateDir string, ev Event) error {
	now := time.Now().UTC()
	ev.Timestamp = now.Format(time.RFC3339Nano)

	root := EventsDir(stateDir, ev.Source)
	sessionDir, err := resolveSessionDir(root, ev.SessionID, now)
	if err != nil {
		return err
	}

	ts := fileTimestamp(now)
	seq := countEvents(sessionDir)
	for {
		path := filepath.Join(sessionDir, fmt.Sprintf("event_%d_%s.json", seq, ts))
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err != nil {
			if errors.Is(err, os.ErrExist) {
				seq++
				continue
			}
			return fmt.Errorf("creating event file %s: %w", path, err)
		}
		ev.Seq = seq
		enc := json.NewEncoder(f)
		enc.SetIndent("", "  ")
		if encErr := enc.Encode(ev); encErr != nil {
			f.Close()
			_ = os.Remove(path)
			return fmt.Errorf("writing event %s: %w", path, encErr)
		}
		if closeErr := f.Close(); closeErr != nil {
			return fmt.Errorf("closing event %s: %w", path, closeErr)
		}
		return nil
	}
}

// resolveSessionDir returns the directory holding sessionID's events, creating
// it (named "<ts>_<session-id>") when it does not yet exist. An existing
// directory is matched by its "_<session-id>" suffix so a session's events
// accumulate together regardless of when the directory was first created.
func resolveSessionDir(root, sessionID string, now time.Time) (string, error) {
	safe := sanitizeSessionID(sessionID)
	if entries, err := os.ReadDir(root); err == nil {
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			name := e.Name()
			if idx := strings.IndexByte(name, '_'); idx >= 0 && name[idx+1:] == safe {
				return filepath.Join(root, name), nil
			}
		}
	}
	dir := filepath.Join(root, fileTimestamp(now)+"_"+safe)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("creating session dir %s: %w", dir, err)
	}
	return dir, nil
}

// countEvents returns how many event files already live in sessionDir.
func countEvents(sessionDir string) int {
	entries, err := os.ReadDir(sessionDir)
	if err != nil {
		return 0
	}
	n := 0
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "event_") {
			n++
		}
	}
	return n
}

// sanitizeSessionID makes a session id safe to embed in a single path segment,
// replacing path separators and whitespace. Empty ids become "unknown".
func sanitizeSessionID(id string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return "unknown"
	}
	return strings.Map(func(r rune) rune {
		switch r {
		case '/', '\\', ' ', '\t', '\n', '\r':
			return '-'
		}
		return r
	}, id)
}
