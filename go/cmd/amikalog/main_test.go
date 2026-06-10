package main

import (
	"strings"
	"testing"

	"github.com/gofixpoint/amika/go/internal/buildmeta"
)

func TestRootVersionFlag(t *testing.T) {
	originalVersion := buildmeta.AmikalogVersion
	t.Cleanup(func() {
		buildmeta.AmikalogVersion = originalVersion
		rootCmd.Version = versionString()
	})

	buildmeta.AmikalogVersion = buildmeta.MustParseSemVer("v2.0.0-beta.1")
	rootCmd.Version = versionString()

	output, err := runRootCommand("--version")
	if err != nil {
		t.Fatalf("runRootCommand() error = %v", err)
	}
	if !strings.Contains(output, "version: v2.0.0-beta.1") {
		t.Fatalf("runRootCommand() output = %q, want version line", output)
	}
	if !strings.Contains(output, "commit: ") {
		t.Fatalf("runRootCommand() output = %q, want commit line", output)
	}
}

func TestVersionSubcommand(t *testing.T) {
	originalVersion := buildmeta.AmikalogVersion
	t.Cleanup(func() {
		buildmeta.AmikalogVersion = originalVersion
		rootCmd.Version = versionString()
	})

	buildmeta.AmikalogVersion = buildmeta.MustParseSemVer("v2.0.0-beta.1")
	rootCmd.Version = versionString()

	output, err := runRootCommand("version")
	if err != nil {
		t.Fatalf("runRootCommand() error = %v", err)
	}
	if !strings.Contains(output, "version: v2.0.0-beta.1") {
		t.Fatalf("runRootCommand() output = %q, want version line", output)
	}
	if !strings.Contains(output, "date: ") {
		t.Fatalf("runRootCommand() output = %q, want date line", output)
	}
}
