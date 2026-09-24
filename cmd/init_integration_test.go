//go:build integration

package cmd

// Integration tests in this file mutate the process working directory. Do NOT use t.Parallel().

import (
	"testing"

	"github.com/gadenbuie/utpr/internal/git"
	"github.com/gadenbuie/utpr/internal/testutil"
)

// TestInitWithoutRemote verifies that init works in a repo with no remotes:
// it should create the branch locally instead of failing.
func TestInitWithoutRemote(t *testing.T) {
	testutil.TempRepo(t) // no remotes configured

	if err := runInit(initCmd, []string{"feature-no-remote"}); err != nil {
		t.Fatalf("runInit failed without a remote: %v", err)
	}

	if !git.BranchExists("feature-no-remote") {
		t.Fatal("expected branch 'feature-no-remote' to be created")
	}
	current, err := git.GetCurrentBranch()
	if err != nil {
		t.Fatalf("failed to get current branch: %v", err)
	}
	if current != "feature-no-remote" {
		t.Errorf("current branch = %q, want %q", current, "feature-no-remote")
	}
}

// TestInitWorktreeWithoutRemote verifies that --worktree works without a remote.
func TestInitWorktreeWithoutRemote(t *testing.T) {
	testutil.TempRepo(t)

	prevWorktree, prevYes := flagInitWorktree, flagInitYes
	t.Cleanup(func() {
		flagInitWorktree = prevWorktree
		flagInitYes = prevYes
	})
	flagInitWorktree = true
	flagInitYes = true // avoid interactive editor prompt

	if err := runInit(initCmd, []string{"feature-wt"}); err != nil {
		t.Fatalf("runInit --worktree failed without a remote: %v", err)
	}

	wtPath := git.GetBranchWorktreePath("feature-wt")
	if wtPath == "" {
		t.Fatal("expected a worktree to be created for 'feature-wt'")
	}
}

// TestInitExistingBranchWithoutRemote verifies that re-running init on an
// existing branch (which delegates to resume) works without a remote.
func TestInitExistingBranchWithoutRemote(t *testing.T) {
	testutil.TempRepo(t)

	if err := runInit(initCmd, []string{"feature-again"}); err != nil {
		t.Fatalf("initial runInit failed: %v", err)
	}

	if err := runInit(initCmd, []string{"feature-again"}); err != nil {
		t.Fatalf("re-running init on an existing branch failed without a remote: %v", err)
	}
}
