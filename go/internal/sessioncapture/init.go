package sessioncapture

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

// HookCommand is the command Claude/Codex hooks invoke to mirror sessions.
// Exposed so callers (and tests) can swap the executable name.
type HookCommand struct {
	// Exe is the absolute path or bare name of the amika executable.
	Exe string
}

// DefaultHookCommand returns a HookCommand using the running amika binary.
func DefaultHookCommand() (HookCommand, error) {
	exe, err := os.Executable()
	if err != nil {
		return HookCommand{}, fmt.Errorf("resolving amika executable: %w", err)
	}
	abs, err := filepath.Abs(exe)
	if err != nil {
		return HookCommand{}, fmt.Errorf("resolving absolute path of amika executable: %w", err)
	}
	return HookCommand{Exe: abs}, nil
}

// ClaudeCommand returns the shell command Claude's Stop hook should run.
func (h HookCommand) ClaudeCommand() string {
	return fmt.Sprintf("%s sessions capture --source claude", shellQuote(h.Exe))
}

// CodexNotify returns the program-and-args list Codex's `notify` should run.
func (h HookCommand) CodexNotify() []string {
	return []string{h.Exe, "sessions", "capture", "--source", "codex"}
}

// InitReport summarizes what `Init` did so the CLI can print useful feedback.
type InitReport struct {
	ClaudeSettingsPath string
	ClaudeUpdated      bool // false when hook already present
	CodexConfigPath    string
	CodexUpdated       bool   // false when notify already pointed at amika
	CodexConflict      string // non-empty when notify pointed elsewhere; we left it alone
}

// Init wires the Stop hook into Claude Code's settings and a notify program
// into Codex's config under homeDir. It is idempotent: running it twice is a
// no-op.
func Init(homeDir string, cmd HookCommand) (InitReport, error) {
	rep := InitReport{
		ClaudeSettingsPath: filepath.Join(homeDir, ".claude", "settings.json"),
		CodexConfigPath:    filepath.Join(homeDir, ".codex", "config.toml"),
	}
	claudeUpdated, err := ensureClaudeStopHook(rep.ClaudeSettingsPath, cmd.ClaudeCommand())
	if err != nil {
		return rep, err
	}
	rep.ClaudeUpdated = claudeUpdated

	codexUpdated, conflict, err := ensureCodexNotify(rep.CodexConfigPath, cmd.CodexNotify())
	if err != nil {
		return rep, err
	}
	rep.CodexUpdated = codexUpdated
	rep.CodexConflict = conflict

	return rep, nil
}

// ensureClaudeStopHook reads `~/.claude/settings.json`, ensures a `Stop` hook
// matching command exists, and writes the file back. Returns whether the file
// was modified.
//
// The Claude hooks schema is documented at
// https://docs.claude.com/en/docs/claude-code/hooks. We model it as nested
// `map[string]interface{}` so unrelated keys round-trip unchanged.
func ensureClaudeStopHook(path, command string) (bool, error) {
	settings, err := readJSONObject(path)
	if err != nil {
		return false, err
	}

	hooks, _ := settings["hooks"].(map[string]interface{})
	if hooks == nil {
		hooks = map[string]interface{}{}
	}

	stopGroups, _ := hooks["Stop"].([]interface{})
	if claudeHookAlreadyPresent(stopGroups, command) {
		return false, nil
	}

	stopGroups = append(stopGroups, map[string]interface{}{
		"hooks": []interface{}{
			map[string]interface{}{
				"type":    "command",
				"command": command,
			},
		},
	})
	hooks["Stop"] = stopGroups
	settings["hooks"] = hooks

	return true, writeJSONObject(path, settings)
}

func claudeHookAlreadyPresent(groups []interface{}, command string) bool {
	for _, raw := range groups {
		group, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		entries, _ := group["hooks"].([]interface{})
		for _, e := range entries {
			entry, ok := e.(map[string]interface{})
			if !ok {
				continue
			}
			if s, _ := entry["command"].(string); s == command {
				return true
			}
		}
	}
	return false
}

func readJSONObject(path string) (map[string]interface{}, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return map[string]interface{}{}, nil
		}
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return map[string]interface{}{}, nil
	}
	var out map[string]interface{}
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	if out == nil {
		out = map[string]interface{}{}
	}
	return out, nil
}

func writeJSONObject(path string, obj map[string]interface{}) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("creating dir for %s: %w", path, err)
	}
	encoded, err := json.MarshalIndent(obj, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding %s: %w", path, err)
	}
	encoded = append(encoded, '\n')
	tmp, err := os.CreateTemp(filepath.Dir(path), ".settings-*")
	if err != nil {
		return fmt.Errorf("creating tempfile for %s: %w", path, err)
	}
	tmpPath := tmp.Name()
	defer func() {
		if tmpPath != "" {
			_ = os.Remove(tmpPath)
		}
	}()
	if _, err := tmp.Write(encoded); err != nil {
		tmp.Close()
		return fmt.Errorf("writing %s: %w", path, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("closing tempfile for %s: %w", path, err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("renaming tempfile to %s: %w", path, err)
	}
	tmpPath = ""
	return nil
}

// ensureCodexNotify ensures Codex's top-level `notify` array invokes the given
// argv. It edits the TOML file line-wise so user comments and formatting are
// preserved. Returns:
//   - updated: true when we modified the file
//   - conflict: a description of an existing notify value that points at
//     something other than amika; in that case we make no changes
func ensureCodexNotify(path string, argv []string) (updated bool, conflict string, err error) {
	want := formatCodexNotify(argv)

	data, readErr := os.ReadFile(path)
	if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
		return false, "", fmt.Errorf("reading %s: %w", path, readErr)
	}

	lines := splitLines(string(data))
	idx, existing := findTopLevelNotify(lines)

	if idx >= 0 {
		if existing == want {
			return false, "", nil
		}
		if !codexNotifyIsAmika(existing) {
			return false, existing, nil
		}
		lines[idx] = "notify = " + want
	} else {
		lines = insertCodexNotify(lines, "notify = "+want)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, "", fmt.Errorf("creating dir for %s: %w", path, err)
	}
	out := strings.Join(lines, "\n")
	if !strings.HasSuffix(out, "\n") {
		out += "\n"
	}
	if err := writeFileAtomic(path, []byte(out)); err != nil {
		return false, "", err
	}
	return true, "", nil
}

func writeFileAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".config-*")
	if err != nil {
		return fmt.Errorf("creating tempfile for %s: %w", path, err)
	}
	tmpPath := tmp.Name()
	defer func() {
		if tmpPath != "" {
			_ = os.Remove(tmpPath)
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("writing %s: %w", path, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("closing tempfile for %s: %w", path, err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("renaming tempfile to %s: %w", path, err)
	}
	tmpPath = ""
	return nil
}

func formatCodexNotify(argv []string) string {
	parts := make([]string, len(argv))
	for i, a := range argv {
		b, _ := json.Marshal(a) // JSON strings happen to be valid TOML basic strings
		parts[i] = string(b)
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

// splitLines splits on '\n' while preserving an empty trailing element so a
// final newline survives round-tripping.
func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	out := strings.Split(s, "\n")
	if len(out) > 0 && out[len(out)-1] == "" {
		out = out[:len(out)-1]
	}
	return out
}

// findTopLevelNotify locates a top-level `notify = ...` assignment, ignoring
// any assignments that appear after the first `[section]` header. Returns the
// line index and the trimmed value (the right-hand side), or (-1, "").
func findTopLevelNotify(lines []string) (int, string) {
	for i, raw := range lines {
		trimmed := strings.TrimSpace(raw)
		if strings.HasPrefix(trimmed, "[") {
			return -1, ""
		}
		if strings.HasPrefix(trimmed, "notify") {
			rest := strings.TrimPrefix(trimmed, "notify")
			rest = strings.TrimLeft(rest, " \t")
			if !strings.HasPrefix(rest, "=") {
				continue
			}
			rest = strings.TrimSpace(strings.TrimPrefix(rest, "="))
			return i, rest
		}
	}
	return -1, ""
}

// codexNotifyIsAmika reports whether the value of `notify` invokes the amika
// binary. We only treat array-form values with an "amika" argv[0] as ours, so
// users running other notify programs aren't silently overwritten.
func codexNotifyIsAmika(value string) bool {
	v := strings.TrimSpace(value)
	if !strings.HasPrefix(v, "[") {
		return false
	}
	var argv []string
	if err := tomlArrayDecode(v, &argv); err != nil {
		return false
	}
	if len(argv) == 0 {
		return false
	}
	return strings.HasSuffix(argv[0], "amika") || strings.HasSuffix(argv[0], "/amika")
}

// tomlArrayDecode decodes a TOML inline array of strings (e.g.
// `["a", "b"]`) into argv. We wrap it as `x = <value>` and let the toml
// library do the parsing rather than write our own.
func tomlArrayDecode(value string, dst *[]string) error {
	doc := "x = " + value + "\n"
	var holder struct {
		X []string `toml:"x"`
	}
	if _, err := toml.Decode(doc, &holder); err != nil {
		return err
	}
	*dst = holder.X
	return nil
}

func insertCodexNotify(lines []string, assignment string) []string {
	// Insert before the first [section] header so the assignment is top-level.
	for i, raw := range lines {
		if strings.HasPrefix(strings.TrimSpace(raw), "[") {
			head := append([]string{}, lines[:i]...)
			if len(head) > 0 && head[len(head)-1] != "" {
				head = append(head, "")
			}
			head = append(head, assignment, "")
			return append(head, lines[i:]...)
		}
	}
	// No section headers — append. Add a blank line before only if the file
	// already has content that doesn't end on a blank line.
	if len(lines) > 0 && lines[len(lines)-1] != "" {
		lines = append(lines, "")
	}
	return append(lines, assignment)
}

// shellQuote wraps s in single quotes if it contains characters that would
// otherwise be interpreted by /bin/sh. Hook commands are passed through a
// shell, so paths with spaces need quoting.
func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			continue
		}
		switch r {
		case '_', '-', '/', '.', ':', ',', '=', '+':
			continue
		}
		return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
	}
	return s
}
