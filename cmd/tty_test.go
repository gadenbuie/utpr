package cmd

import (
	"testing"

	"github.com/gadenbuie/utpr/internal/ui"
	"github.com/spf13/cobra"
)

// allCommands returns every command in the tree, skipping the help and
// completion commands that cobra adds at execution time.
func allCommands() []*cobra.Command {
	var cmds []*cobra.Command
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		for _, sub := range c.Commands() {
			if sub.Name() == "help" || sub.Name() == "completion" {
				continue
			}
			cmds = append(cmds, sub)
			walk(sub)
		}
	}
	walk(rootCmd)
	return cmds
}

// resetPlainFlags returns agent and pretty flags to false so one test
// case cannot leak into the next. The flag instance may be a local
// --agent flag (push, view, ci) or the inherited root flag.
func resetPlainFlags(cmd *cobra.Command) {
	for _, name := range []string{"agent", "pretty"} {
		if f := cmd.Flags().Lookup(name); f != nil {
			_ = f.Value.Set("false")
		}
	}
}

func TestAllCommandsExposeAgentAndPrettyFlags(t *testing.T) {
	for _, cmd := range allCommands() {
		if err := cmd.ParseFlags(nil); err != nil {
			t.Fatalf("%s: ParseFlags() failed: %v", cmd.CommandPath(), err)
		}
		for _, name := range []string{"agent", "pretty"} {
			if cmd.Flags().Lookup(name) == nil {
				t.Errorf("%s is missing the --%s flag", cmd.CommandPath(), name)
			}
		}
		resetPlainFlags(cmd)
	}
}

func TestPlainModeIsConsistentAcrossCommands(t *testing.T) {
	restoreTTY := ui.SetTTYFuncs(func() bool { return true }, func() bool { return true })
	t.Cleanup(restoreTTY)
	t.Cleanup(func() { ui.SetPlainMode(false) })

	tests := []struct {
		name      string
		stdoutTTY bool
		args      []string
		want      bool
	}{
		{"tty no flags", true, nil, false},
		{"pipe no flags", false, nil, true},
		{"pipe --pretty", false, []string{"--pretty"}, false},
		{"tty --agent", true, []string{"--agent"}, true},
		{"pipe --agent", false, []string{"--agent"}, true},
		{"pipe --agent --pretty", false, []string{"--agent", "--pretty"}, false},
	}

	for _, tt := range tests {
		for _, cmd := range allCommands() {
			t.Run(tt.name+"/"+cmd.CommandPath(), func(t *testing.T) {
				restore := ui.SetTTYFuncs(func() bool { return tt.stdoutTTY }, func() bool { return true })
				t.Cleanup(restore)

				if err := cmd.ParseFlags(tt.args); err != nil {
					t.Fatalf("ParseFlags(%v) failed: %v", tt.args, err)
				}
				defer resetPlainFlags(cmd)

				initPlainMode(cmd)
				if got := ui.PlainMode(); got != tt.want {
					t.Errorf("plain mode after initPlainMode() = %v, want %v", got, tt.want)
				}
			})
		}
	}
}
