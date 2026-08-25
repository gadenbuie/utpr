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
	}

	for _, tt := range tests {
		if !tt.has("yes") {
			t.Errorf("%s command is missing the --yes flag", tt.name)
		}
	}
}

func TestAssumeYes(t *testing.T) {
	previous := [3]bool{flagInitYes, flagFetchYes, flagResumeYes}
	t.Cleanup(func() {
		flagInitYes = previous[0]
		flagFetchYes = previous[1]
		flagResumeYes = previous[2]
	})

	flagInitYes = false
	flagFetchYes = false
	flagResumeYes = false
	if assumeYes() {
		t.Fatal("assumeYes() = true with all flags disabled")
	}

	flagFetchYes = true
	if !assumeYes() {
		t.Fatal("assumeYes() = false with --fetch --yes enabled")
	}
}
