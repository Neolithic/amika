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
	// The legacy Codex `notify` program appends one trailing JSON payload to the
	// configured argv, e.g. '{"type":"agent-turn-complete",...}'. Accept 0 or 1
	// positional so that fallback path does not fail at argument validation.
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		source := eventlog.Source(must(cmd.Flags().GetString("source")))
		if source != eventlog.SourceClaude && source != eventlog.SourceCodex {
			return fmt.Errorf("unknown --source %q (want claude or codex)", source)
		}
		// A hook must never block or perturb the agent: lifecycle hooks gate
		// tool execution, so a non-zero exit can deny a tool call and stdout is
		// fed back to the model. Record best-effort, report failures on stderr,
		// and always exit 0 with no stdout.
		if err := capture(cmd, source, args); err != nil {
			fmt.Fprintf(os.Stderr, "amikalog: failed to record %s hook: %v\n", source, err)
		}
		return nil
	},
}

// capture records the current hook invocation as an event.
func capture(cmd *cobra.Command, source eventlog.Source, args []string) error {
	stateDir, err := config.StateDir()
	if err != nil {
		return fmt.Errorf("resolving state directory: %w", err)
	}
	if source == eventlog.SourceClaude {
		return eventlog.CaptureClaude(cmd.InOrStdin(), stateDir)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("resolving home directory: %w", err)
	}
	legacyArg := ""
	if len(args) == 1 {
		legacyArg = args[0]
	}
	return eventlog.CaptureCodex(cmd.InOrStdin(), legacyArg, home, stateDir)
}

func must(s string, _ error) string { return s }

func init() {
	rootCmd.AddCommand(hookCmd)
	hookCmd.Flags().String("source", "", "Source agent (claude|codex)")
	_ = hookCmd.MarkFlagRequired("source")
}
