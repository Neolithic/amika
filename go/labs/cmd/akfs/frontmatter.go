package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/gofixpoint/amika/go/labs/akfs/frontmatter"
	"github.com/spf13/cobra"
)

func init() {
	cmd := &cobra.Command{
		Use:     "frontmatter [file...]",
		Aliases: []string{"fm"},
		Short:   "Parse YAML frontmatter from files or stdin",
		Long: `Parse the YAML frontmatter block from one or more documents and emit
it as JSON.

Each input may be a file path; when no path is given (or the path is "-"),
stdin is read. The document must begin with a "---" delimiter line and close
the frontmatter with a matching "---" line. The block between is parsed as YAML
and written to stdout as a single line of compact JSON, wrapped under a "data"
key. With multiple files, one JSON line is emitted per file, in order.`,
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			enc := json.NewEncoder(out)

			if len(args) == 0 {
				return emit(enc, os.Stdin, "<stdin>")
			}
			for _, arg := range args {
				if err := emitPath(enc, arg); err != nil {
					return err
				}
			}
			return nil
		},
	}

	rootCmd.AddCommand(cmd)
}

// emitPath parses the frontmatter of a single path ("-" meaning stdin) and
// writes it as a JSON line.
func emitPath(enc *json.Encoder, path string) error {
	if path == "-" {
		return emit(enc, os.Stdin, "<stdin>")
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return emit(enc, f, path)
}

// emit parses frontmatter from r and encodes it as a single compact JSON line,
// wrapping the parsed frontmatter under a "data" key.
func emit(enc *json.Encoder, r io.Reader, name string) error {
	data, err := frontmatter.Parse(r)
	if err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	return enc.Encode(map[string]any{"data": data})
}
