// Package main implements the amikalog CLI, which captures Claude Code and
// OpenAI Codex hook activity (with git context) as raw append-only events.
package main

import (
	"fmt"
	"os"

	"github.com/gofixpoint/amika/go/internal/buildmeta"
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "amikalog",
	Short: "Capture agent and git activity as append-only events",
	Long: `amikalog records Claude Code and Codex hook activity, together with the
git state of the directory each hook fired in, as raw append-only events under
the amika state directory.

Run "amikalog start" once to register the hooks. From then on each agent invokes
"amikalog hook" itself on every hook call; no daemon is involved.`,
	CompletionOptions: cobra.CompletionOptions{HiddenDefaultCmd: true},
	SilenceUsage:      true,
	SilenceErrors:     true,
}

func init() {
	rootCmd.Version = versionString()
	rootCmd.SetVersionTemplate("{{.Version}}\n")
	rootCmd.AddCommand(&cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, _ []string) {
			fmt.Fprintln(cmd.OutOrStdout(), versionString())
		},
	})
}

func versionString() string {
	return buildmeta.New("amikalog", buildmeta.AmikalogVersion).String()
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
