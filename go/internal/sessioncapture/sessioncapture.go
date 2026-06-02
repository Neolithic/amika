// Package sessioncapture mirrors Claude Code and OpenAI Codex session
// transcripts into the amika state directory.
//
// Capture is driven by the agents themselves: `Init` writes hook entries into
// `~/.claude/settings.json` (Stop hook) and `<codex-home>/config.toml`
// (notify program) that invoke `amika sessions capture --source ...`
// whenever the agent finishes a turn. Each invocation copies the relevant
// session JSONL into `<state>/sessions/<source>/`. No daemon, no background
// process.
//
// `<codex-home>` is `$CODEX_HOME` when set, otherwise `~/.codex` (see
// CodexHome). Honoring the env var matters because Codex itself reads
// config and writes sessions there, not under `~/.codex`.
package sessioncapture

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
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

// CaptureCodex mirrors every Codex session file whose source mtime is newer
// than its mirror (or that has no mirror yet) under the Codex state root
// into the amika state directory.
//
// Codex's notify payload does not include a session path. Picking only the
// globally newest file across the tree would skip a turn whenever a second
// concurrent Codex session wrote something more recently — so we walk the
// whole tree and copy any file that's changed. Mirrors preserve Codex's
// `YYYY/MM/DD/<rollout>.jsonl` layout under `<state>/sessions/codex/` so
// nothing collides across days.
//
// The Codex state root is `$CODEX_HOME` when set, falling back to
// `<homeDir>/.codex`. Returns nil with no error when no Codex session
// directory exists yet.
func CaptureCodex(homeDir, stateDir string) error {
	sessionsDir := filepath.Join(CodexHome(homeDir), "sessions")
	dstRoot := CaptureDir(stateDir, SourceCodex)

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
		rel, relErr := filepath.Rel(sessionsDir, path)
		if relErr != nil {
			return relErr
		}
		dst := filepath.Join(dstRoot, rel)

		fresh, freshErr := mirrorIsFresh(path, dst)
		if freshErr != nil {
			return freshErr
		}
		if fresh {
			return nil
		}
		return copyFile(path, dst)
	})
	if walkErr != nil && !errors.Is(walkErr, os.ErrNotExist) {
		return walkErr
	}
	return nil
}

// CodexHome returns the directory Codex uses for config, sessions, auth and
// related state. Honors the `CODEX_HOME` environment variable when set,
// falling back to `<homeDir>/.codex`.
func CodexHome(homeDir string) string {
	if v := os.Getenv("CODEX_HOME"); v != "" {
		return v
	}
	return filepath.Join(homeDir, ".codex")
}

// mirrorIsFresh reports whether dst exists and its mtime is at least as new
// as src's. Used to skip rewrites of session files we've already mirrored.
func mirrorIsFresh(src, dst string) (bool, error) {
	srcInfo, err := os.Stat(src)
	if err != nil {
		return false, err
	}
	dstInfo, err := os.Stat(dst)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	return !srcInfo.ModTime().After(dstInfo.ModTime()), nil
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
