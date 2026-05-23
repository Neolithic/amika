package main

import sandboxcmd "github.com/gofixpoint/amika/go/cmd/amika/sandbox"

func init() {
	rootCmd.AddCommand(sandboxcmd.New())
}
