package cmd

import "testing"

func TestCommandNeedsAuth(t *testing.T) {
	tests := []struct {
		name string
		path []string
		want bool
	}{
		{"init", []string{"init"}, false},
		{"resume", []string{"resume"}, false},
		{"worktree", []string{"worktree"}, false},
		{"worktree create", []string{"worktree", "create"}, false},
		{"worktree list", []string{"worktree", "list"}, false},
		{"push", []string{"push"}, true},
		{"finish", []string{"finish"}, true},
		{"fetch", []string{"fetch"}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd, _, err := rootCmd.Find(tt.path)
			if err != nil {
				t.Fatalf("rootCmd.Find(%v) failed: %v", tt.path, err)
			}
			if got := commandNeedsAuth(cmd); got != tt.want {
				t.Errorf("commandNeedsAuth(%v) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}
