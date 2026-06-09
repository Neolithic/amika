// Package eventlog records Claude Code and OpenAI Codex hook activity as raw,
// append-only events under the amika state directory, annotating every event
// with the git state of the directory the hook fired in.
//
// Capture is driven by the agents themselves: Init writes hook entries into
// ~/.claude/settings.json (one per Claude hook event) and
// <codex-home>/config.toml (the notify program) that invoke
// `amikalog hook --source ...` on every hook call. Each invocation appends one
// JSON file:
//
//	<state>/events/<source>/sessions/<ts>_<session-id>/event_<seq>_<ts>.json
//
// No daemon and no background process are involved. The state directory is the
// same one the rest of amika uses (see internal/config.StateDir), so
// AMIKA_STATE_DIRECTORY and the XDG variables apply.
//
// <codex-home> is $CODEX_HOME when set, otherwise ~/.codex (see CodexHome).
package eventlog

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Source identifies the agent that produced an event.
type Source string

const (
	// SourceClaude is the source identifier for Claude Code events.
	SourceClaude Source = "claude"
	// SourceCodex is the source identifier for OpenAI Codex events.
	SourceCodex Source = "codex"
)

// EventsDir returns the directory under stateDir that holds a source's session
// directories: <stateDir>/events/<source>/sessions.
func EventsDir(stateDir string, src Source) string {
	return filepath.Join(stateDir, "events", string(src), "sessions")
}

// claudeHookInput is the subset of Claude Code's hook stdin JSON that amikalog
// consumes to label the event. The full payload is preserved verbatim.
type claudeHookInput struct {
	SessionID     string `json:"session_id"`
	CWD           string `json:"cwd"`
	HookEventName string `json:"hook_event_name"`
}

// CaptureClaude reads the hook JSON Claude Code pipes on stdin and appends an
// event for it. The raw payload is stored unchanged; git context is gathered
// from the hook's reported cwd (falling back to the process cwd).
func CaptureClaude(stdin io.Reader, stateDir string) error {
	data, err := io.ReadAll(stdin)
	if err != nil {
		return fmt.Errorf("reading claude hook stdin: %w", err)
	}
	var in claudeHookInput
	// Tolerate malformed/empty stdin: still record what we received.
	_ = json.Unmarshal(data, &in)

	cwd := in.CWD
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	return writeEvent(stateDir, Event{
		Source:    SourceClaude,
		HookEvent: in.HookEventName,
		SessionID: in.SessionID,
		CWD:       cwd,
		Git:       GatherGit(cwd),
		Payload:   rawPayload(data),
	})
}

// codexNotifyInput is the subset of Codex's notify payload amikalog reads.
// Codex does not include a session id, so several plausible field names are
// accepted and CaptureCodex derives one when none is present.
type codexNotifyInput struct {
	Type           string `json:"type"`
	SessionID      string `json:"session_id"`
	SessionIDDash  string `json:"session-id"`
	ConversationID string `json:"conversation_id"`
	ConvIDDash     string `json:"conversation-id"`
}

// sessionID returns the first non-empty id field, if any.
func (in codexNotifyInput) sessionID() string {
	for _, v := range []string{in.SessionID, in.SessionIDDash, in.ConversationID, in.ConvIDDash} {
		if v != "" {
			return v
		}
	}
	return ""
}

// CaptureCodex appends an event for a Codex notify invocation. Codex passes its
// payload as a single positional argument (arg) and runs in the repository's
// working directory, so git context is gathered from the process cwd.
//
// Because Codex's notify payload carries no session id, the id is taken from
// the payload when present, otherwise derived from the most recently modified
// rollout file under <codex-home>/sessions (see deriveCodexSessionID).
func CaptureCodex(arg, homeDir, stateDir string) error {
	data := []byte(arg)
	var in codexNotifyInput
	_ = json.Unmarshal(data, &in)

	sessionID := in.sessionID()
	if sessionID == "" {
		sessionID = deriveCodexSessionID(homeDir)
	}

	cwd, _ := os.Getwd()
	return writeEvent(stateDir, Event{
		Source:    SourceCodex,
		HookEvent: in.Type,
		SessionID: sessionID,
		CWD:       cwd,
		Git:       GatherGit(cwd),
		Payload:   rawPayload(data),
	})
}

// CodexHome returns the directory Codex uses for config, sessions and related
// state. It honors $CODEX_HOME, falling back to <homeDir>/.codex.
func CodexHome(homeDir string) string {
	if v := os.Getenv("CODEX_HOME"); v != "" {
		return v
	}
	return filepath.Join(homeDir, ".codex")
}

// deriveCodexSessionID returns a stable session identifier for the Codex
// session that just completed a turn, taken from the basename (without the
// .jsonl suffix) of the most recently modified rollout file under
// <codex-home>/sessions. Returns "" when no rollout file can be found.
func deriveCodexSessionID(homeDir string) string {
	sessionsDir := filepath.Join(CodexHome(homeDir), "sessions")
	var newestPath string
	var newestMod int64 = -1
	walkErr := filepath.WalkDir(sessionsDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return filepath.SkipAll
			}
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".jsonl") {
			return nil
		}
		info, statErr := d.Info()
		if statErr != nil {
			return nil
		}
		if mod := info.ModTime().UnixNano(); mod > newestMod {
			newestMod = mod
			newestPath = path
		}
		return nil
	})
	if walkErr != nil || newestPath == "" {
		return ""
	}
	return strings.TrimSuffix(filepath.Base(newestPath), ".jsonl")
}
