//go:build integration

package cmd

// Integration tests in this file mutate the process working directory and
// os.Args. Do NOT use t.Parallel().

import (
	"os"
	"testing"

	"github.com/gadenbuie/utpr/internal/git"
	"github.com/gadenbuie/utpr/internal/testutil"
)

// withCLIArgs runs f with os.Args set to simulate a CLI invocation.
func withCLIArgs(t *testing.T, args ...string) {
	t.Helper()
	oldArgs := os.Args
	t.Cleanup(func() { os.Args = oldArgs })
	os.Args = append([]string{"utpr"}, args...)
}

// TestInitCLINoRemote verifies that 'utpr init' works end-to-end through
// the cobra command tree in a repo with no remotes.
func TestInitCLINoRemote(t *testing.T) {
	testutil.TempRepo(t)
	withCLIArgs(t, "init", "feature-cli")

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("utpr init failed: %v", err)
	}

	if !git.BranchExists("feature-cli") {
		t.Fatal("expected branch 'feature-cli' to be created")
	}
}

// TestWorktreeCreateCLINoRemote verifies that 'utpr worktree create' works
// end-to-end through the cobra command tree in a repo with no remotes.
func TestWorktreeCreateCLINoRemote(t *testing.T) {
	testutil.TempRepo(t)
	withCLIArgs(t, "worktree", "create", "feat/cli", "--yes")

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("utpr worktree create failed: %v", err)
	}

	if !git.BranchExists("feat/cli") {
		t.Fatal("expected branch 'feat/cli' to be created")
	}
	if git.GetBranchWorktreePath("feat/cli") == "" {
		t.Fatal("expected a worktree for 'feat/cli'")
	}
}
