//go:build integration

package cmd

// Integration tests in this file mutate the process working directory. Do NOT use t.Parallel().

import (
	"testing"

	"github.com/gadenbuie/utpr/internal/git"
	"github.com/gadenbuie/utpr/internal/testutil"
)

func TestResolveFinishArgBranchName(t *testing.T) {
	clonePath, _ := testutil.TempRepoWithRemote(t)
	testutil.CreateBranch(t, clonePath, "feature-branch")

	if err := git.SetBranchPRURL("feature-branch", "https://github.com/owner/repo/pull/99"); err != nil {
		t.Fatalf("failed to set stored PR URL: %v", err)
	}

	got, err := resolveFinishArg("feature-branch", "owner/repo")
	if err != nil {
		t.Fatalf("resolveFinishArg returned error: %v", err)
	}
	if got != 99 {
		t.Errorf("resolveFinishArg(%q) = %d, want %d", "feature-branch", got, 99)
	}
}

// TestFinishNoRemoteFallsBackToForget verifies that finish falls back to
// local cleanup (forget behavior) when there is no git remote, removing the
// worktree and deleting the branch.
func TestFinishNoRemoteFallsBackToForget(t *testing.T) {
	repoPath := testutil.TempRepo(t)

	prevYes := flagFinishYes
	t.Cleanup(func() { flagFinishYes = prevYes })
	flagFinishYes = true // run the fallback unattended

	// Create a branch with a worktree, like 'utpr init --worktree' would.
	prevWorktree, prevInitYes := flagInitWorktree, flagInitYes
	t.Cleanup(func() {
		flagInitWorktree = prevWorktree
		flagInitYes = prevInitYes
	})
	flagInitWorktree = true
	flagInitYes = true
	if err := runInit(initCmd, []string{"feature/finish-me"}); err != nil {
		t.Fatalf("failed to set up branch and worktree: %v", err)
	}
	wtPath := git.GetBranchWorktreePath("feature/finish-me")
	if wtPath == "" {
		t.Fatal("expected a worktree for 'feature/finish-me' before finish")
	}

	if err := runFinish(finishCmd, []string{"feature/finish-me"}); err != nil {
		t.Fatalf("runFinish failed without a remote: %v", err)
	}

	if git.BranchExists("feature/finish-me") {
		t.Error("expected branch 'feature/finish-me' to be deleted")
	}
	if git.GetBranchWorktreePath("feature/finish-me") != "" {
		t.Errorf("expected worktree to be removed: %s", wtPath)
	}
	if current, _ := git.GetCurrentBranch(); current != "main" {
		t.Errorf("current branch = %q, want %q", current, "main")
	}
	_ = repoPath
}

// TestFinishNoRemoteDeclined verifies that declining the fallback dialog
// leaves the branch and worktree untouched.
func TestFinishNoRemoteDeclined(t *testing.T) {
	testutil.TempRepo(t)

	restore := testutil.StubConfirm(false)
	defer restore()

	testutil.CreateBranch(t, ".", "feature/stay")

	if err := runFinish(finishCmd, []string{"feature/stay"}); err != nil {
		t.Fatalf("runFinish returned an error when declining fallback: %v", err)
	}

	if !git.BranchExists("feature/stay") {
		t.Error("branch should still exist after declining the fallback")
	}
}

// TestForgetNoRemote verifies that forget works without a git remote,
// switching to the local default branch and deleting the branch.
func TestForgetNoRemote(t *testing.T) {
	testutil.TempRepo(t)

	prevYes := flagForgetYes
	t.Cleanup(func() { flagForgetYes = prevYes })
	flagForgetYes = true

	testutil.CreateBranch(t, ".", "feature/forget-me")
	if err := git.SwitchBranch("feature/forget-me"); err != nil {
		t.Fatalf("failed to switch branch: %v", err)
	}

	if err := runForget(forgetCmd, []string{"feature/forget-me"}); err != nil {
		t.Fatalf("runForget failed without a remote: %v", err)
	}

	if git.BranchExists("feature/forget-me") {
		t.Error("expected branch 'feature/forget-me' to be deleted")
	}
	if current, _ := git.GetCurrentBranch(); current != "main" {
		t.Errorf("current branch = %q, want %q", current, "main")
	}
}
