package cmd

import (
	"testing"
)

func withCITTYFlags(t *testing.T, stdoutTTY, stdinTTY bool, agent, pretty bool) {
	t.Helper()

	oldStdout, oldStdin := ciStdoutIsTTY, ciStdinIsTTY
	oldAgent, oldLogsAgent, oldRerunAgent := flagCIAgent, flagCILogsAgent, flagCIRerunAgent
	oldPretty := flagCIPretty

	ciStdoutIsTTY = func() bool { return stdoutTTY }
	ciStdinIsTTY = func() bool { return stdinTTY }
	flagCIAgent, flagCILogsAgent, flagCIRerunAgent = agent, false, false
	flagCIPretty = pretty

	t.Cleanup(func() {
		ciStdoutIsTTY, ciStdinIsTTY = oldStdout, oldStdin
		flagCIAgent, flagCILogsAgent, flagCIRerunAgent = oldAgent, oldLogsAgent, oldRerunAgent
		flagCIPretty = oldPretty
	})
}

func TestCIAgentModeTTYMatrix(t *testing.T) {
	tests := []struct {
		name      string
		stdoutTTY bool
		agent     bool
		pretty    bool
		want      bool
	}{
		{"tty no flags", true, false, false, false},
		{"pipe no flags", false, false, false, true},
		{"pipe --agent", false, true, false, true},
		{"tty --agent", true, true, false, true},
		{"pipe --pretty", false, false, true, false},
		{"tty --pretty", true, false, true, false},
		{"pipe --agent --pretty", false, true, true, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			withCITTYFlags(t, tt.stdoutTTY, true, tt.agent, tt.pretty)
			if got := ciAgentMode(); got != tt.want {
				t.Errorf("ciAgentMode() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCIAgentModeSubcommandAgentFlags(t *testing.T) {
	for _, name := range []string{"flagCILogsAgent", "flagCIRerunAgent"} {
		withCITTYFlags(t, true, true, false, false)
		switch name {
		case "flagCILogsAgent":
			flagCILogsAgent = true
		case "flagCIRerunAgent":
			flagCIRerunAgent = true
		}
		if !ciAgentMode() {
			t.Errorf("ciAgentMode() = false with %s set, want true", name)
		}
	}
}

func TestCIPrettyFlagRegistered(t *testing.T) {
	for _, tc := range []struct {
		name string
		has  func(string) bool
	}{
		{name: "ci", has: func(n string) bool { return ciCmd.Flags().Lookup(n) != nil }},
		{name: "ci logs", has: func(n string) bool { return ciLogsCmd.Flags().Lookup(n) != nil }},
		{name: "ci rerun", has: func(n string) bool { return ciRerunCmd.Flags().Lookup(n) != nil }},
	} {
		if !tc.has("pretty") {
			t.Errorf("%s command is missing the --pretty flag", tc.name)
		}
		if !tc.has("agent") {
			t.Errorf("%s command is missing the --agent flag", tc.name)
		}
		if !tc.has("pick") {
			t.Errorf("%s command is missing the --pick flag", tc.name)
		}
	}
}

func TestRequireInteractiveTTY(t *testing.T) {
	withCITTYFlags(t, true, false, false, false)
	if err := requireInteractiveTTY("some guidance"); err == nil {
		t.Error("requireInteractiveTTY() = nil with non-TTY stdin, want error")
	}

	withCITTYFlags(t, true, true, false, false)
	if err := requireInteractiveTTY("some guidance"); err != nil {
		t.Errorf("requireInteractiveTTY() = %v with TTY stdin, want nil", err)
	}
}
