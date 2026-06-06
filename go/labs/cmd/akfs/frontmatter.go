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
and written to stdout as a single line of compact JSON: the parsed frontmatter
under a "data" key, alongside a "filename" key naming the source file (omitted
for stdin). With multiple files, one JSON line is emitted per file, in order.`,
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			enc := json.NewEncoder(out)

			if len(args) == 0 {
				return emit(enc, os.Stdin, "")
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

// record is the JSON envelope emitted per input: the source filename (omitted
// for stdin) alongside the parsed frontmatter.
type record struct {
	Filename string         `json:"filename,omitempty"`
	Data     map[string]any `json:"data"`
}

// emitPath parses the frontmatter of a single path ("-" meaning stdin) and
// writes it as a JSON line.
func emitPath(enc *json.Encoder, path string) error {
	if path == "-" {
		return emit(enc, os.Stdin, "")
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return emit(enc, f, path)
}

// emit parses frontmatter from r and encodes it as a single compact JSON line.
// filename names the source for the output's "filename" field and error
// messages; an empty filename denotes stdin.
func emit(enc *json.Encoder, r io.Reader, filename string) error {
	label := filename
	if label == "" {
		label = "<stdin>"
	}
	data, err := frontmatter.Parse(r)
	if err != nil {
		return fmt.Errorf("%s: %w", label, err)
	}
	return enc.Encode(record{Filename: filename, Data: data})
}
