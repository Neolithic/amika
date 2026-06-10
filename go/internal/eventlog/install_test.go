package eventlog

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testCommand() HookCommand {
	return HookCommand{Exe: "/opt/bin/amikalog"}
}

// readHooks returns the parsed "hooks" object from a hooks JSON file (Claude
// settings.json or Codex hooks.json).
func readHooks(t *testing.T, path string) map[string]interface{} {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	var obj map[string]interface{}
	if err := json.Unmarshal(data, &obj); err != nil {
		t.Fatalf("parsing %s: %v", path, err)
	}
	hooks, _ := obj["hooks"].(map[string]interface{})
	return hooks
}

// assertHookEntries verifies every event runs wantCmd exactly once, carrying a
// matcher exactly when matcher(event) says it should.
func assertHookEntries(t *testing.T, hooks map[string]interface{}, events []string, wantCmd string, matcher func(string) (string, bool)) {
	t.Helper()
	for _, event := range events {
		groups, ok := hooks[event].([]interface{})
		if !ok || len(groups) == 0 {
			t.Fatalf("event %s missing from hooks", event)
		}
		group := groups[len(groups)-1].(map[string]interface{})
		gotMatcher, hasMatcher := group["matcher"]
		wantVal, wantMatcher := matcher(event)
		switch {
		case wantMatcher && gotMatcher != wantVal:
			t.Errorf("event %s matcher = %v, want %q", event, gotMatcher, wantVal)
		case !wantMatcher && hasMatcher:
			t.Errorf("event %s should not carry a matcher, got %v", event, gotMatcher)
		}
		entry := group["hooks"].([]interface{})[0].(map[string]interface{})
		if entry["command"] != wantCmd {
			t.Errorf("event %s command = %v, want %q", event, entry["command"], wantCmd)
		}
	}
}

func TestInit_InstallsEveryClaudeEvent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CODEX_HOME", "")
	rep, err := Init(home, testCommand())
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	if !rep.ClaudeUpdated {
		t.Error("ClaudeUpdated = false, want true on first install")
	}

	hooks := readHooks(t, rep.ClaudeSettingsPath)
	assertHookEntries(t, hooks, claudeHookEvents, testCommand().ClaudeCommand(), claudeMatcher)
}

func TestInit_InstallsEveryCodexEvent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CODEX_HOME", "")
	rep, err := Init(home, testCommand())
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	if !rep.CodexUpdated {
		t.Error("CodexUpdated = false, want true on first install")
	}
	if rep.CodexHooksPath != filepath.Join(home, ".codex", "hooks.json") {
		t.Errorf("CodexHooksPath = %q, want ~/.codex/hooks.json", rep.CodexHooksPath)
	}
	hooks := readHooks(t, rep.CodexHooksPath)
	assertHookEntries(t, hooks, codexHookEvents, testCommand().CodexCommand(), codexMatcher)
}

func TestInit_Idempotent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CODEX_HOME", "")
	if _, err := Init(home, testCommand()); err != nil {
		t.Fatal(err)
	}
	settingsPath := filepath.Join(home, ".claude", "settings.json")
	hooksPath := CodexHooksFile(home)
	before := readFile(t, settingsPath)
	beforeHooks := readFile(t, hooksPath)

	rep, err := Init(home, testCommand())
	if err != nil {
		t.Fatal(err)
	}
	if rep.ClaudeUpdated {
		t.Error("ClaudeUpdated = true on re-run, want false")
	}
	if rep.CodexUpdated {
		t.Error("CodexUpdated = true on re-run, want false")
	}
	if after := readFile(t, settingsPath); after != before {
		t.Errorf("settings changed on re-run:\nbefore=%s\nafter=%s", before, after)
	}
	if after := readFile(t, hooksPath); after != beforeHooks {
		t.Errorf("codex hooks.json changed on re-run:\nbefore=%s\nafter=%s", beforeHooks, after)
	}
}

func TestInit_PreservesUnrelatedSettings(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CODEX_HOME", "")
	settingsPath := filepath.Join(home, ".claude", "settings.json")
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o755); err != nil {
		t.Fatal(err)
	}
	seed := `{
  "model": "opus",
  "hooks": {
    "Stop": [{"hooks": [{"type": "command", "command": "/usr/bin/other-tool"}]}]
  }
}`
	writeFile(t, settingsPath, seed)

	if _, err := Init(home, testCommand()); err != nil {
		t.Fatal(err)
	}

	data := readFile(t, settingsPath)
	if !strings.Contains(data, `"model": "opus"`) {
		t.Error("unrelated top-level key was dropped")
	}
	if !strings.Contains(data, "/usr/bin/other-tool") {
		t.Error("unrelated Stop hook was dropped")
	}
	if !strings.Contains(data, testCommand().ClaudeCommand()) {
		t.Error("amikalog hook was not added")
	}
}

func TestInit_ReplacesStaleAmikalogExe(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CODEX_HOME", "")
	// Install with an old exe path, then re-install with a new one.
	if _, err := Init(home, HookCommand{Exe: "/old/path/amikalog"}); err != nil {
		t.Fatal(err)
	}
	if _, err := Init(home, HookCommand{Exe: "/new/path/amikalog"}); err != nil {
		t.Fatal(err)
	}
	settingsPath := filepath.Join(home, ".claude", "settings.json")
	data := readFile(t, settingsPath)
	if strings.Contains(data, "/old/path/amikalog") {
		t.Error("stale amikalog hook was not replaced")
	}
	if strings.Count(data, "/new/path/amikalog hook --source claude") != len(claudeHookEvents) {
		t.Errorf("expected exactly one new hook per event (%d), got %d", len(claudeHookEvents),
			strings.Count(data, "/new/path/amikalog hook --source claude"))
	}
}

func TestInit_LeavesThirdPartyCodexNotify(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CODEX_HOME", "")
	configPath := filepath.Join(CodexHome(home), "config.toml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, configPath, "notify = [\"/usr/bin/other\"]\n")

	rep, err := Init(home, testCommand())
	if err != nil {
		t.Fatal(err)
	}
	if rep.CodexNotifyRemoved {
		t.Error("CodexNotifyRemoved = true, want false: a third-party notify must be left alone")
	}
	if cfg := readFile(t, configPath); !strings.Contains(cfg, "/usr/bin/other") {
		t.Errorf("third-party notify was modified:\n%s", cfg)
	}
	// Lifecycle hooks are still installed regardless of the notify program.
	if hooks := readHooks(t, rep.CodexHooksPath); len(hooks) == 0 {
		t.Error("codex hooks.json was not written")
	}
}

func TestUninstall(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CODEX_HOME", "")
	if _, err := Init(home, testCommand()); err != nil {
		t.Fatal(err)
	}

	rep, err := Uninstall(home)
	if err != nil {
		t.Fatal(err)
	}
	if !rep.ClaudeUpdated {
		t.Error("ClaudeUpdated = false, want true (claude hooks should be removed)")
	}
	if !rep.CodexUpdated {
		t.Error("CodexUpdated = false, want true (codex hooks should be removed)")
	}

	if hooks := readHooks(t, rep.ClaudeSettingsPath); len(hooks) != 0 {
		t.Errorf("claude hooks remain after uninstall: %v", hooks)
	}
	if hooks := readHooks(t, rep.CodexHooksPath); len(hooks) != 0 {
		t.Errorf("codex hooks remain after uninstall: %v", hooks)
	}

	// Second uninstall is a no-op.
	rep2, err := Uninstall(home)
	if err != nil {
		t.Fatal(err)
	}
	if rep2.ClaudeUpdated || rep2.CodexUpdated {
		t.Errorf("second uninstall reported changes: %+v", rep2)
	}
}

func TestUninstall_LeavesUnrelatedHooks(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CODEX_HOME", "")
	settingsPath := filepath.Join(home, ".claude", "settings.json")
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, settingsPath, `{"hooks": {"Stop": [{"hooks": [{"type": "command", "command": "/usr/bin/other-tool"}]}]}}`)
	if _, err := Init(home, testCommand()); err != nil {
		t.Fatal(err)
	}
	if _, err := Uninstall(home); err != nil {
		t.Fatal(err)
	}
	data := readFile(t, settingsPath)
	if !strings.Contains(data, "/usr/bin/other-tool") {
		t.Errorf("unrelated hook removed by uninstall:\n%s", data)
	}
	if strings.Contains(data, "amikalog") {
		t.Errorf("amikalog hook survived uninstall:\n%s", data)
	}
}

func TestInit_MigratesLegacyClaudeCaptureHook(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CODEX_HOME", "")
	settingsPath := filepath.Join(home, ".claude", "settings.json")
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o755); err != nil {
		t.Fatal(err)
	}
	// Stale Stop hook left by the removed `amika sessions capture-init`.
	writeFile(t, settingsPath, `{"hooks": {"Stop": [{"hooks": [{"type": "command", "command": "/usr/local/bin/amika sessions capture --source claude"}]}]}}`)

	if _, err := Init(home, testCommand()); err != nil {
		t.Fatal(err)
	}
	data := readFile(t, settingsPath)
	if strings.Contains(data, "sessions capture --source claude") {
		t.Errorf("legacy amika capture hook was not migrated away:\n%s", data)
	}
	if strings.Count(data, testCommand().ClaudeCommand()) != len(claudeHookEvents) {
		t.Errorf("expected one amikalog hook per event after migration:\n%s", data)
	}
}

func TestInit_MigratesLegacyCodexNotify(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CODEX_HOME", "")
	configPath := filepath.Join(CodexHome(home), "config.toml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, configPath, `notify = ["/usr/local/bin/amika", "sessions", "capture", "--source", "codex"]`+"\n")

	rep, err := Init(home, testCommand())
	if err != nil {
		t.Fatal(err)
	}
	if !rep.CodexNotifyRemoved {
		t.Error("CodexNotifyRemoved = false, want true: the obsolete amika notify should be cleaned out")
	}
	if cfg := readFile(t, configPath); strings.Contains(cfg, "notify") {
		t.Errorf("obsolete amika notify was not removed from config.toml:\n%s", cfg)
	}
	// Lifecycle hooks take over from the removed notify program.
	if hooks := readHooks(t, rep.CodexHooksPath); len(hooks) == 0 {
		t.Error("codex hooks.json was not written")
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	return string(data)
}
