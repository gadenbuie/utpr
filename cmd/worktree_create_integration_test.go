//go:build integration

package cmd

// Integration tests in this file mutate the process working directory. Do NOT use t.Parallel().

import (
	"testing"

	"github.com/gadenbuie/utpr/internal/git"
	"github.com/gadenbuie/utpr/internal/testutil"
)

func setupWorktreeCreateTest(t *testing.T) {
	t.Helper()
	testutil.TempRepo(t)

	prevYes := flagInitYes
	t.Cleanup(func() { flagInitYes = prevYes })
	flagInitYes = true // avoid interactive editor prompt
}

// TestWorktreeCreateNewBranch verifies that 'worktree create' creates a new
// branch and its worktree (the 'init --worktree' alias behavior).
func TestWorktreeCreateNewBranch(t *testing.T) {
	setupWorktreeCreateTest(t)

	if err := worktreeCreateCmd.RunE(worktreeCreateCmd, []string{"feat/create-new"}); err != nil {
		t.Fatalf("worktree create failed: %v", err)
	}

	if !git.BranchExists("feat/create-new") {
		t.Fatal("expected branch 'feat/create-new' to be created")
	}
	current, err := git.GetCurrentBranch()
	if err != nil {
		t.Fatalf("failed to get current branch: %v", err)
	}
	if current == "feat/create-new" {
		t.Error("main repo should not have the new branch checked out")
	}
	if git.GetBranchWorktreePath("feat/create-new") == "" {
		t.Fatal("expected a worktree for 'feat/create-new'")
	}
}

// TestWorktreeCreateExistingBranch verifies that 'worktree create' on an
// existing branch creates a worktree for it.
func TestWorktreeCreateExistingBranch(t *testing.T) {
	setupWorktreeCreateTest(t)
	testutil.CreateBranch(t, ".", "existing-branch")

	if err := worktreeCreateCmd.RunE(worktreeCreateCmd, []string{"existing-branch"}); err != nil {
		t.Fatalf("worktree create failed for existing branch: %v", err)
	}

	if git.GetBranchWorktreePath("existing-branch") == "" {
		t.Fatal("expected a worktree for 'existing-branch'")
	}
}

// TestWorktreeCreateExistingWorktree verifies that 'worktree create' on a
// branch that already has a worktree offers navigation instead of failing.
func TestWorktreeCreateExistingWorktree(t *testing.T) {
	setupWorktreeCreateTest(t)

	if err := worktreeCreateCmd.RunE(worktreeCreateCmd, []string{"feat/first"}); err != nil {
		t.Fatalf("first worktree create failed: %v", err)
	}
	firstPath := git.GetBranchWorktreePath("feat/first")
	if firstPath == "" {
		t.Fatal("expected a worktree for 'feat/first'")
	}

	// Stub the navigation prompt to "Do nothing".
	restore := testutil.StubChoose("Do nothing")
	defer restore()

	if err := worktreeCreateCmd.RunE(worktreeCreateCmd, []string{"feat/first"}); err != nil {
		t.Fatalf("re-running worktree create failed: %v", err)
	}
	if got := git.GetBranchWorktreePath("feat/first"); got != firstPath {
		t.Errorf("worktree path changed: got %q, want %q", got, firstPath)
	}
}
