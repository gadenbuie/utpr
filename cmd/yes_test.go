package cmd

import "testing"

func TestSetupCommandsHaveYesFlags(t *testing.T) {
	tests := []struct {
		name string
		has  func(string) bool
	}{
		{name: "init", has: func(name string) bool { return initCmd.Flags().Lookup(name) != nil }},
		{name: "fetch", has: func(name string) bool { return fetchCmd.Flags().Lookup(name) != nil }},
		{name: "resume", has: func(name string) bool { return resumeCmd.Flags().Lookup(name) != nil }},
		{name: "worktree create", has: func(name string) bool { return worktreeCreateCmd.Flags().Lookup(name) != nil }},
		{name: "finish", has: func(name string) bool { return finishCmd.Flags().Lookup(name) != nil }},
		{name: "forget", has: func(name string) bool { return forgetCmd.Flags().Lookup(name) != nil }},
	}

	for _, tt := range tests {
		if !tt.has("yes") {
			t.Errorf("%s command is missing the --yes flag", tt.name)
		}
	}
}

func TestAssumeYes(t *testing.T) {
	previous := [5]bool{flagInitYes, flagFetchYes, flagResumeYes, flagFinishYes, flagForgetYes}
	t.Cleanup(func() {
		flagInitYes = previous[0]
		flagFetchYes = previous[1]
		flagResumeYes = previous[2]
		flagFinishYes = previous[3]
		flagForgetYes = previous[4]
	})

	flagInitYes = false
	flagFetchYes = false
	flagResumeYes = false
	flagFinishYes = false
	flagForgetYes = false
	if assumeYes() {
		t.Fatal("assumeYes() = true with all flags disabled")
	}

	flagFetchYes = true
	if !assumeYes() {
		t.Fatal("assumeYes() = false with --fetch --yes enabled")
	}
}
