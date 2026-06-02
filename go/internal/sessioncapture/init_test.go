package sessioncapture

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInit_FreshClaudeAndCodex(t *testing.T) {
	home := t.TempDir()
	rep, err := Init(home, HookCommand{Exe: "/usr/local/bin/amika"})
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	if !rep.ClaudeUpdated || !rep.CodexUpdated {
		t.Fatalf("expected both to be updated, got %+v", rep)
	}

	settings := readClaudeSettings(t, rep.ClaudeSettingsPath)
	wantCmd := "/usr/local/bin/amika sessions capture --source claude"
	if !claudeHasHook(settings, wantCmd) {
		t.Errorf("Claude settings missing Stop hook with command %q: %#v", wantCmd, settings)
	}

	codex, err := os.ReadFile(rep.CodexConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	want := `notify = ["/usr/local/bin/amika", "sessions", "capture", "--source", "codex"]`
	if !strings.Contains(string(codex), want) {
		t.Errorf("codex config missing %q, got:\n%s", want, codex)
	}
}

func TestInit_Idempotent(t *testing.T) {
	home := t.TempDir()
	cmd := HookCommand{Exe: "/usr/local/bin/amika"}
	if _, err := Init(home, cmd); err != nil {
		t.Fatal(err)
	}
	rep, err := Init(home, cmd)
	if err != nil {
		t.Fatal(err)
	}
	if rep.ClaudeUpdated || rep.CodexUpdated {
		t.Errorf("expected no changes on second run, got %+v", rep)
	}
}

func TestInit_PreservesExistingClaudeKeys(t *testing.T) {
	home := t.TempDir()
	settingsPath := filepath.Join(home, ".claude", "settings.json")
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o755); err != nil {
		t.Fatal(err)
	}
	existing := map[string]interface{}{
		"theme": "dark",
		"hooks": map[string]interface{}{
			"PreToolUse": []interface{}{
				map[string]interface{}{"hooks": []interface{}{map[string]interface{}{"type": "command", "command": "other-tool"}}},
			},
		},
	}
	raw, _ := json.Marshal(existing)
	if err := os.WriteFile(settingsPath, raw, 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := Init(home, HookCommand{Exe: "amika"}); err != nil {
		t.Fatal(err)
	}
	got := readClaudeSettings(t, settingsPath)
	if v, _ := got["theme"].(string); v != "dark" {
		t.Errorf("theme key lost: %#v", got)
	}
	hooks, _ := got["hooks"].(map[string]interface{})
	if _, ok := hooks["PreToolUse"]; !ok {
		t.Errorf("PreToolUse hooks lost: %#v", hooks)
	}
	if !claudeHasHook(got, "amika sessions capture --source claude") {
		t.Errorf("Stop hook not added: %#v", hooks)
	}
}

func TestInit_PreservesCodexConflict(t *testing.T) {
	home := t.TempDir()
	cfg := filepath.Join(home, ".codex", "config.toml")
	if err := os.MkdirAll(filepath.Dir(cfg), 0o755); err != nil {
		t.Fatal(err)
	}
	contents := `model = "gpt-5"
notify = ["my-tool", "--watch"]

[projects."/x"]
trust_level = "trusted"
`
	if err := os.WriteFile(cfg, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}

	rep, err := Init(home, HookCommand{Exe: "/usr/local/bin/amika"})
	if err != nil {
		t.Fatal(err)
	}
	if rep.CodexUpdated {
		t.Errorf("expected to leave conflicting notify alone, got updated=true")
	}
	if rep.CodexConflict == "" {
		t.Errorf("expected CodexConflict to be reported")
	}
	got, _ := os.ReadFile(cfg)
	if string(got) != contents {
		t.Errorf("config was modified despite conflict:\n%s", got)
	}
}

func TestInit_UpdatesExistingAmikaNotify(t *testing.T) {
	home := t.TempDir()
	cfg := filepath.Join(home, ".codex", "config.toml")
	if err := os.MkdirAll(filepath.Dir(cfg), 0o755); err != nil {
		t.Fatal(err)
	}
	contents := `model = "gpt-5"
notify = ["/old/path/amika", "sessions", "capture", "--source", "codex"]
`
	if err := os.WriteFile(cfg, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}

	rep, err := Init(home, HookCommand{Exe: "/new/path/amika"})
	if err != nil {
		t.Fatal(err)
	}
	if !rep.CodexUpdated {
		t.Errorf("expected update when path changed, got %+v", rep)
	}
	got, _ := os.ReadFile(cfg)
	if !strings.Contains(string(got), `"/new/path/amika"`) {
		t.Errorf("notify not updated: %s", got)
	}
	if strings.Contains(string(got), `"/old/path/amika"`) {
		t.Errorf("old notify path still present: %s", got)
	}
}

func TestInit_HonorsCODEX_HOME(t *testing.T) {
	home := t.TempDir()
	codexHome := t.TempDir()
	t.Setenv("CODEX_HOME", codexHome)

	rep, err := Init(home, HookCommand{Exe: "/usr/local/bin/amika"})
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	if rep.CodexConfigPath != filepath.Join(codexHome, "config.toml") {
		t.Errorf("CodexConfigPath = %q, want under CODEX_HOME (%q)", rep.CodexConfigPath, codexHome)
	}
	if _, err := os.Stat(filepath.Join(codexHome, "config.toml")); err != nil {
		t.Errorf("expected config.toml under CODEX_HOME: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".codex", "config.toml")); !os.IsNotExist(err) {
		t.Errorf("config.toml should not be written to ~/.codex when CODEX_HOME is set: %v", err)
	}
}

func TestInit_InsertsBeforeFirstSection(t *testing.T) {
	home := t.TempDir()
	cfg := filepath.Join(home, ".codex", "config.toml")
	if err := os.MkdirAll(filepath.Dir(cfg), 0o755); err != nil {
		t.Fatal(err)
	}
	contents := `model = "gpt-5"

[projects."/x"]
trust_level = "trusted"
`
	if err := os.WriteFile(cfg, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Init(home, HookCommand{Exe: "amika"}); err != nil {
		t.Fatal(err)
	}

	got, _ := os.ReadFile(cfg)
	notifyIdx := strings.Index(string(got), "notify =")
	sectionIdx := strings.Index(string(got), "[projects")
	if notifyIdx < 0 || sectionIdx < 0 {
		t.Fatalf("missing expected content: %s", got)
	}
	if notifyIdx >= sectionIdx {
		t.Errorf("notify not inserted before [projects] section:\n%s", got)
	}
}

func TestShellQuote(t *testing.T) {
	cases := map[string]string{
		"/usr/local/bin/amika":   "/usr/local/bin/amika",
		"/path with space/amika": "'/path with space/amika'",
		"weird'name":             `'weird'\''name'`,
		"":                       "''",
		"plain-thing_1.2":        "plain-thing_1.2",
	}
	for in, want := range cases {
		if got := shellQuote(in); got != want {
			t.Errorf("shellQuote(%q) = %q, want %q", in, got, want)
		}
	}
}

func readClaudeSettings(t *testing.T, path string) map[string]interface{} {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading claude settings: %v", err)
	}
	var out map[string]interface{}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decoding claude settings: %v", err)
	}
	return out
}

func claudeHasHook(settings map[string]interface{}, command string) bool {
	hooks, _ := settings["hooks"].(map[string]interface{})
	stop, _ := hooks["Stop"].([]interface{})
	return claudeHookAlreadyPresent(stop, command)
}
