package main

import (
	"io"
	"strings"
)

func runRootCommand(args ...string) (string, error) {
	return runRootCommandStdin(strings.NewReader(""), args...)
}

func runRootCommandStdin(stdin io.Reader, args ...string) (string, error) {
	buf := &strings.Builder{}
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetIn(stdin)
	rootCmd.SetArgs(args)
	err := rootCmd.Execute()
	rootCmd.SetArgs(nil)
	rootCmd.SetIn(nil)
	return buf.String(), err
}
