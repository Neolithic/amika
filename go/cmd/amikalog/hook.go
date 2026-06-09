package main

import (
	"fmt"
	"os"

	"github.com/gofixpoint/amika/go/internal/config"
	"github.com/gofixpoint/amika/go/internal/eventlog"
	"github.com/spf13/cobra"
)

var hookCmd = &cobra.Command{
	Use:    "hook",
	Hidden: true, // invoked by the agents' hook systems, not by users
	Short:  "Record an agent hook call as an event",
	Long: `Record the current agent hook call as an append-only event in the amika
state directory, annotated with the git state of the working directory.

This is invoked by the agent's own hook system (see "amikalog start"), not by
hand. The --source flag selects the agent: the Claude variant reads the hook
payload from stdin, while Codex's notify hook appends the payload as a single
positional argument.`,
	// Codex's notify hook appends one trailing JSON payload to the configured
	// argv, e.g. '{"type":"agent-turn-complete",...}'. Accept 0 or 1 positional
	// so that path does not fail at argument validation.
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		source, _ := cmd.Flags().GetString("source")
		stateDir, err := config.StateDir()
		if err != nil {
			return fmt.Errorf("resolving state directory: %w", err)
		}
		switch eventlog.Source(source) {
		case eventlog.SourceClaude:
			return eventlog.CaptureClaude(cmd.InOrStdin(), stateDir)
		case eventlog.SourceCodex:
			home, err := os.UserHomeDir()
			if err != nil {
				return fmt.Errorf("resolving home directory: %w", err)
			}
			payload := ""
			if len(args) == 1 {
				payload = args[0]
			}
			return eventlog.CaptureCodex(payload, home, stateDir)
		default:
			return fmt.Errorf("unknown --source %q (want claude or codex)", source)
		}
	},
}

func init() {
	rootCmd.AddCommand(hookCmd)
	hookCmd.Flags().String("source", "", "Source agent (claude|codex)")
	_ = hookCmd.MarkFlagRequired("source")
}
