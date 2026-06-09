package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStartInstallsHooks(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", "")
	t.Setenv("AMIKA_STATE_DIRECTORY", t.TempDir())

	out, err := runRootCommand("start")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if !strings.Contains(out, "Events will be written to:") {
		t.Errorf("start output missing events location:\n%s", out)
	}

	settings, err := os.ReadFile(filepath.Join(home, ".claude", "settings.json"))
	if err != nil {
		t.Fatalf("reading settings: %v", err)
	}
	if !strings.Contains(string(settings), "hook --source claude") {
		t.Errorf("settings missing amikalog hook:\n%s", settings)
	}
}
