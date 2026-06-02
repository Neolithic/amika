// Package sessioncapture mirrors Claude Code and OpenAI Codex session
// transcripts into the amika state directory.
//
// Capture is driven by the agents themselves: `Init` writes hook entries into
// `~/.claude/settings.json` (Stop hook) and `~/.codex/config.toml` (notify
// program) that invoke `amika sessions capture --source ...` whenever the
// agent finishes a turn. Each invocation copies the relevant session JSONL
// into `<state>/sessions/<source>/`. No daemon, no background process.
package sessioncapture

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Source identifies the agent that produced the session being captured.
type Source string

const (
	// SourceClaude is the source identifier for Claude Code sessions.
	SourceClaude Source = "claude"
	// SourceCodex is the source identifier for OpenAI Codex sessions.
	SourceCodex Source = "codex"
)

// CaptureDir returns the directory under stateDir where mirrored sessions
// for the given source live.
func CaptureDir(stateDir string, src Source) string {
	return filepath.Join(stateDir, "sessions", string(src))
}

// claudeStopHookInput is the JSON shape Claude Code pipes on stdin when a
// `Stop` hook fires. Only the fields amika consumes are listed.
type claudeStopHookInput struct {
	SessionID      string `json:"session_id"`
	TranscriptPath string `json:"transcript_path"`
}

// CaptureClaude reads the Stop-hook JSON Claude pipes on stdin and copies
// the referenced transcript into the amika state directory.
func CaptureClaude(stdin io.Reader, stateDir string) error {
	data, err := io.ReadAll(stdin)
	if err != nil {
		return fmt.Errorf("reading claude hook stdin: %w", err)
	}
	var in claudeStopHookInput
	if err := json.Unmarshal(data, &in); err != nil {
		return fmt.Errorf("decoding claude hook stdin: %w", err)
	}
	if in.TranscriptPath == "" {
		return errors.New("claude hook input missing transcript_path")
	}
	name := filepath.Base(in.TranscriptPath)
	if in.SessionID != "" && !strings.HasSuffix(name, ".jsonl") {
		name = in.SessionID + ".jsonl"
	}
	dst := filepath.Join(CaptureDir(stateDir, SourceClaude), name)
	return copyFile(in.TranscriptPath, dst)
}

// CaptureCodex copies the most recently modified Codex session file under
// ~/.codex/sessions into the amika state directory. Codex's notify hook
// does not pass a session path, so we resolve the active session by mtime.
//
// homeDir is the directory containing the `.codex` config (i.e. the user's
// home). Returns nil with no error when no Codex session file exists yet.
func CaptureCodex(homeDir, stateDir string) error {
	sessionsDir := filepath.Join(homeDir, ".codex", "sessions")
	latest, err := newestSessionFile(sessionsDir)
	if err != nil {
		return err
	}
	if latest == "" {
		return nil
	}
	dst := filepath.Join(CaptureDir(stateDir, SourceCodex), filepath.Base(latest))
	return copyFile(latest, dst)
}

func newestSessionFile(dir string) (string, error) {
	var newest string
	var newestMod time.Time
	walkErr := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return filepath.SkipAll
			}
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".jsonl") {
			return nil
		}
		info, infoErr := d.Info()
		if infoErr != nil {
			return infoErr
		}
		if info.ModTime().After(newestMod) {
			newestMod = info.ModTime()
			newest = path
		}
		return nil
	})
	if walkErr != nil && !errors.Is(walkErr, os.ErrNotExist) {
		return "", walkErr
	}
	return newest, nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("opening session source %s: %w", src, err)
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return fmt.Errorf("creating capture dir: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(dst), ".capture-*")
	if err != nil {
		return fmt.Errorf("creating capture tempfile: %w", err)
	}
	tmpPath := tmp.Name()
	defer func() {
		if tmpPath != "" {
			_ = os.Remove(tmpPath)
		}
	}()
	if _, err := io.Copy(tmp, in); err != nil {
		tmp.Close()
		return fmt.Errorf("copying session contents: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("closing capture tempfile: %w", err)
	}
	if err := os.Rename(tmpPath, dst); err != nil {
		return fmt.Errorf("renaming capture file to %s: %w", dst, err)
	}
	tmpPath = ""
	return nil
}
