package main

import (
	"fmt"
	"os"

	"github.com/gofixpoint/amika/go/internal/config"
	"github.com/gofixpoint/amika/go/internal/eventlog"
	"github.com/spf13/cobra"
)

var startCmd = &cobra.Command{
	Use:   "start",
	Short: "Register amikalog hooks with Claude and Codex",
	Long: `Install hooks into the local Claude Code and Codex configurations so that
each agent invokes "amikalog hook" on every hook call, recording an append-only
event (with git context) into the amika state directory.

Writes to:
  ~/.claude/settings.json   (a hook entry for every Claude hook event)
  ~/.codex/config.toml      (the notify program; honors $CODEX_HOME)

Events land under:
  $AMIKA_STATE_DIRECTORY/events/{claude,codex}/sessions/
(default $AMIKA_STATE_DIRECTORY is ~/.local/state/amika).

The hooks are global (they fire in every repository); each event records the
git commit and working directory it ran in. This command is idempotent.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("resolving home directory: %w", err)
		}
		hookCmd, err := eventlog.DefaultHookCommand()
		if err != nil {
			return err
		}
		rep, err := eventlog.Init(home, hookCmd)
		if err != nil {
			return err
		}
		stateDir, err := config.StateDir()
		if err != nil {
			return fmt.Errorf("resolving state directory: %w", err)
		}
		out := cmd.OutOrStdout()
		if rep.ClaudeUpdated {
			fmt.Fprintf(out, "Installed hooks in %s\n", rep.ClaudeSettingsPath)
		} else {
			fmt.Fprintf(out, "Hooks already present in %s\n", rep.ClaudeSettingsPath)
		}
		switch {
		case rep.CodexConflict != "":
			fmt.Fprintf(os.Stderr,
				"Skipped %s: existing notify = %s does not look like amikalog; leaving it alone\n",
				rep.CodexConfigPath, rep.CodexConflict)
		case rep.CodexUpdated:
			fmt.Fprintf(out, "Set notify program in %s\n", rep.CodexConfigPath)
		default:
			fmt.Fprintf(out, "Notify program already set in %s\n", rep.CodexConfigPath)
		}
		fmt.Fprintln(out, "Events will be written to:")
		fmt.Fprintf(out, "  claude: %s\n", eventlog.EventsDir(stateDir, eventlog.SourceClaude))
		fmt.Fprintf(out, "  codex:  %s\n", eventlog.EventsDir(stateDir, eventlog.SourceCodex))
		return nil
	},
}

var stopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Remove amikalog hooks from Claude and Codex",
	Long: `Remove the hooks installed by "amikalog start" from the local Claude Code
and Codex configurations. Unrelated hooks and a notify program pointing
elsewhere are left untouched. Already-captured events are not deleted.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("resolving home directory: %w", err)
		}
		rep, err := eventlog.Uninstall(home)
		if err != nil {
			return err
		}
		out := cmd.OutOrStdout()
		if rep.ClaudeUpdated {
			fmt.Fprintf(out, "Removed hooks from %s\n", rep.ClaudeSettingsPath)
		} else {
			fmt.Fprintf(out, "No amikalog hooks found in %s\n", rep.ClaudeSettingsPath)
		}
		if rep.CodexUpdated {
			fmt.Fprintf(out, "Removed notify program from %s\n", rep.CodexConfigPath)
		} else {
			fmt.Fprintf(out, "No amikalog notify program found in %s\n", rep.CodexConfigPath)
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(startCmd)
	rootCmd.AddCommand(stopCmd)
}
