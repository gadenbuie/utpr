//go:build integration

package cmd

// Integration tests in this file mutate the process working directory. Do NOT use t.Parallel().

import (
	"os"
	"path/filepath"
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

// TestFinishNoRemotePRNumberResolves verifies that a PR-number argument in
// the local cleanup fallback resolves to its local branch via the stored
// PR URL instead of being treated as a branch name.
func TestFinishNoRemotePRNumberResolves(t *testing.T) {
	testutil.TempRepo(t)

	prevYes := flagFinishYes
	t.Cleanup(func() { flagFinishYes = prevYes })
	flagFinishYes = true

	testutil.CreateBranch(t, ".", "feature/pr-branch")
	if err := git.SetBranchPRURL("feature/pr-branch", "https://github.com/owner/repo/pull/99"); err != nil {
		t.Fatalf("failed to set stored PR URL: %v", err)
	}

	if err := runFinish(finishCmd, []string{"99"}); err != nil {
		t.Fatalf("runFinish failed with a PR number argument: %v", err)
	}

	if git.BranchExists("feature/pr-branch") {
		t.Error("expected branch 'feature/pr-branch' to be deleted via PR number resolution")
	}
}

// TestFinishNoRemoteUnresolvablePRNumber verifies that an unresolvable PR
// number errors clearly instead of deleting a branch named after it.
func TestFinishNoRemoteUnresolvablePRNumber(t *testing.T) {
	testutil.TempRepo(t)

	prevYes := flagFinishYes
	t.Cleanup(func() { flagFinishYes = prevYes })
	flagFinishYes = true

	testutil.CreateBranch(t, ".", "42")

	if err := runFinish(finishCmd, []string{"77"}); err == nil {
		t.Fatal("expected an error for an unresolvable PR number")
	}
	if !git.BranchExists("42") {
		t.Error("unrelated branch '42' must not be deleted")
	}
}

// TestForgetNoDefaultBranch verifies that forget refuses to run when the
// default branch cannot be determined (no remote, no local main/master).
func TestForgetNoDefaultBranch(t *testing.T) {
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get working directory: %v", err)
	}
	dir := t.TempDir()
	dir, err = filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatalf("failed to resolve symlinks: %v", err)
	}
	testutil.RunGit(t, dir, "init", "--initial-branch=trunk")
	testutil.RunGit(t, dir, "config", "user.name", "Test User")
	testutil.RunGit(t, dir, "config", "user.email", "test@example.com")
	testutil.AddCommit(t, dir, "initial commit")
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("failed to chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	prevYes := flagForgetYes
	t.Cleanup(func() { flagForgetYes = prevYes })
	flagForgetYes = true

	if err := runForget(forgetCmd, []string{"trunk"}); err == nil {
		t.Fatal("expected forget to refuse when the default branch cannot be determined")
	}
	if !git.BranchExists("trunk") {
		t.Error("branch 'trunk' must be preserved when forget refuses")
	}
}

// TestForgetAmbiguousDefaultRefused verifies that forget refuses to delete
// 'master' when both main and master exist locally and no remote can
// disambiguate the default branch.
func TestForgetAmbiguousDefaultRefused(t *testing.T) {
	testutil.TempRepo(t)

	prevYes := flagForgetYes
	t.Cleanup(func() { flagForgetYes = prevYes })
	flagForgetYes = true

	testutil.CreateBranch(t, ".", "master")
	testutil.CreateBranch(t, ".", "feature")

	if err := runForget(forgetCmd, []string{"master"}); err == nil {
		t.Fatal("expected forget to refuse deleting 'master' when the default is ambiguous")
	}
	if !git.BranchExists("master") {
		t.Error("branch 'master' must be preserved")
	}

	// A regular branch is still forgettable.
	if err := runForget(forgetCmd, []string{"feature"}); err != nil {
		t.Fatalf("runForget failed for a regular branch: %v", err)
	}
	if git.BranchExists("feature") {
		t.Error("expected branch 'feature' to be deleted")
	}
}

// TestFinishNoRemoteAmbiguousPRNumber verifies that a PR number referenced
// by multiple local branches errors instead of picking one arbitrarily.
func TestFinishNoRemoteAmbiguousPRNumber(t *testing.T) {
	testutil.TempRepo(t)

	prevYes := flagFinishYes
	t.Cleanup(func() { flagFinishYes = prevYes })
	flagFinishYes = true

	testutil.CreateBranch(t, ".", "feature/one")
	if err := git.SetBranchPRURL("feature/one", "https://github.com/owner/repo/pull/99"); err != nil {
		t.Fatalf("failed to set stored PR URL: %v", err)
	}
	testutil.CreateBranch(t, ".", "feature/two")
	if err := git.SetBranchPRURL("feature/two", "https://github.com/other/repo/pull/99"); err != nil {
		t.Fatalf("failed to set stored PR URL: %v", err)
	}

	if err := runFinish(finishCmd, []string{"99"}); err == nil {
		t.Fatal("expected an error for an ambiguous PR number")
	}
	if !git.BranchExists("feature/one") || !git.BranchExists("feature/two") {
		t.Error("no branch should be deleted when the PR number is ambiguous")
	}
}

// TestFreeUpSwitchesToLocalDefault verifies that moving the current branch
// into a worktree without a remote switches the main repo to the local
// default branch instead of leaving it detached.
func TestFreeUpSwitchesToLocalDefault(t *testing.T) {
	testutil.TempRepo(t)

	prevWorktree, prevYes := flagInitWorktree, flagInitYes
	t.Cleanup(func() {
		flagInitWorktree = prevWorktree
		flagInitYes = prevYes
	})
	flagInitWorktree = true
	flagInitYes = true

	// Create a branch checked out in the main repo, then move it into a worktree.
	testutil.CreateBranch(t, ".", "feature/move-me")
	if err := git.SwitchBranch("feature/move-me"); err != nil {
		t.Fatalf("failed to switch branch: %v", err)
	}

	if err := runInit(initCmd, []string{"feature/move-me"}); err != nil {
		t.Fatalf("runInit failed: %v", err)
	}

	if git.GetBranchWorktreePath("feature/move-me") == "" {
		t.Fatal("expected a worktree for 'feature/move-me'")
	}
	current, err := git.GetCurrentBranch()
	if err != nil {
		t.Fatalf("failed to get current branch: %v", err)
	}
	if current != "main" {
		t.Errorf("main repo should be on 'main' after moving the branch, got %q", current)
	}
}
