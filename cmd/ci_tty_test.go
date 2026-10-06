package cmd

import (
	"strings"
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

func TestPickersRequireTTY(t *testing.T) {
	withCITTYFlags(t, true, false, false, false)

	if _, err := pickRunForBranch("o/r", "b", 5); err == nil {
		t.Error("pickRunForBranch() = nil error with non-TTY stdin, want error")
	}
	if _, _, err := pickCILogs(nil, nil, nil, 100); err == nil {
		t.Error("pickCILogs() = nil error with non-TTY stdin, want error")
	}
	if _, err := pickCIRerunJobs(nil, nil); err == nil {
		t.Error("pickCIRerunJobs() = nil error with non-TTY stdin, want error")
	}
}

func withCILogsFlags(t *testing.T, pick, all, failed bool, job string) {
	t.Helper()

	oldPick, oldAll, oldFailed, oldJob := flagCILogsPick, flagCILogsAll, flagCILogsFailed, flagCILogsJob
	flagCILogsPick, flagCILogsAll, flagCILogsFailed, flagCILogsJob = pick, all, failed, job

	t.Cleanup(func() {
		flagCILogsPick, flagCILogsAll, flagCILogsFailed, flagCILogsJob = oldPick, oldAll, oldFailed, oldJob
	})
}

func TestRequireCILogsTTY(t *testing.T) {
	t.Run("non-TTY no filter errors", func(t *testing.T) {
		withCITTYFlags(t, true, false, false, false)
		withCILogsFlags(t, false, false, false, "")
		if err := requireCILogsTTY(); err == nil {
			t.Error("requireCILogsTTY() = nil, want error")
		}
	})

	t.Run("non-TTY with filter passes", func(t *testing.T) {
		withCITTYFlags(t, true, false, false, false)
		withCILogsFlags(t, false, false, true, "")
		if err := requireCILogsTTY(); err != nil {
			t.Errorf("requireCILogsTTY() = %v, want nil", err)
		}
	})

	t.Run("pick guidance takes precedence", func(t *testing.T) {
		withCITTYFlags(t, true, false, false, false)
		withCILogsFlags(t, true, false, true, "")
		err := requireCILogsTTY()
		if err == nil || !strings.Contains(err.Error(), "--pick") {
			t.Errorf("requireCILogsTTY() = %v, want --pick guidance", err)
		}
	})

	t.Run("TTY passes", func(t *testing.T) {
		withCITTYFlags(t, true, true, false, false)
		withCILogsFlags(t, false, false, false, "")
		if err := requireCILogsTTY(); err != nil {
			t.Errorf("requireCILogsTTY() = %v, want nil", err)
		}
	})
}

func TestRunCILogsFailsFastWithoutTTY(t *testing.T) {
	withCITTYFlags(t, true, false, false, false)
	withCILogsFlags(t, false, false, false, "")

	err := runCILogs(nil, nil)
	if err == nil || !strings.Contains(err.Error(), "--failed") {
		t.Errorf("runCILogs() = %v, want TTY guidance error before any fetch", err)
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
