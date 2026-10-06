package cmd

import (
	"github.com/gadenbuie/utpr/internal/ui"
	"github.com/spf13/cobra"
)

var (
	flagRootAgent  bool
	flagRootPretty bool
)

// initPlainMode computes the plain-output mode for the command being run
// and applies it to the ui package. Command-level --agent flags (ci, ci
// logs, ci rerun, push, view) count the same as the global flag; a
// --pretty flag at either level forces styled output.
func initPlainMode(cmd *cobra.Command) {
	agent := flagRootAgent
	if v, ok := boolFlagSet(cmd, "agent"); ok {
		agent = agent || v
	}
	pretty := flagRootPretty
	if v, ok := boolFlagSet(cmd, "pretty"); ok {
		pretty = pretty || v
	}
	ui.SetPlainMode(ui.AgentMode(agent, pretty))
}

// boolFlagSet reads a boolean flag from the command's full flag set,
// which includes local flags shadowing inherited ones.
func boolFlagSet(cmd *cobra.Command, name string) (bool, bool) {
	if cmd.Flags().Lookup(name) == nil {
		return false, false
	}
	v, err := cmd.Flags().GetBool(name)
	if err != nil {
		return false, false
	}
	return v, true
}
