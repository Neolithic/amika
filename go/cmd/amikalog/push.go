package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

var pushCmd = &cobra.Command{
	Use:   "push",
	Short: "Push captured events to remote storage (not yet implemented)",
	Long: `Push locally captured events to remote storage.

This is a placeholder: remote push is not implemented yet. A future version will
upload the append-only events written by "amikalog hook", either automatically
at the end of a session or on demand.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		fmt.Fprintln(cmd.OutOrStdout(), "amikalog push is not implemented yet; events are captured locally only.")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(pushCmd)
}
