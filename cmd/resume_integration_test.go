//go:build integration

package cmd

// Integration tests in this file mutate the process working directory. Do NOT use t.Parallel().

import (
	"strings"
	"testing"

	"github.com/gadenbuie/utpr/internal/git"
	"github.com/gadenbuie/utpr/internal/testutil"
)

// TestResumeByPRNumber verifies that `utpr resume <number>` resolves the
// stored PR URL of a local branch and switches to that branch.
func TestResumeByPRNumber(t *testing.T) {
	testutil.TempRepo(t)

	testutil.CreateBranch(t, ".", "feature/resume-me")
	if err := git.SetBranchPRURL("feature/resume-me", "https://github.com/owner/repo/pull/123"); err != nil {
		t.Fatalf("failed to set stored PR URL: %v", err)
	}

	if err := runResume(resumeCmd, []string{"123"}); err != nil {
		t.Fatalf("runResume failed: %v", err)
	}

	if current, _ := git.GetCurrentBranch(); current != "feature/resume-me" {
		t.Errorf("current branch = %q, want %q", current, "feature/resume-me")
	}
}

// TestResumeByPRNumberNotFound verifies that resuming an unknown PR number
// fails without switching branches.
func TestResumeByPRNumberNotFound(t *testing.T) {
	testutil.TempRepo(t)

	testutil.CreateBranch(t, ".", "feature/unrelated")
	if err := git.SetBranchPRURL("feature/unrelated", "https://github.com/owner/repo/pull/123"); err != nil {
		t.Fatalf("failed to set stored PR URL: %v", err)
	}

	err := runResume(resumeCmd, []string{"456"})
	if err == nil {
		t.Fatal("expected an error for a PR number with no local branch")
	}
	if !strings.Contains(err.Error(), "#456") {
		t.Errorf("error should mention PR #456, got: %v", err)
	}

	if current, _ := git.GetCurrentBranch(); current != "feature/unrelated" {
		t.Errorf("current branch = %q, want %q (unchanged after failed resume)", current, "feature/unrelated")
	}
}

// TestResumeByPRNumberAmbiguous verifies that multiple local branches
// referencing the same PR number are refused.
func TestResumeByPRNumberAmbiguous(t *testing.T) {
	testutil.TempRepo(t)

	for _, branch := range []string{"feature/one", "feature/two"} {
		testutil.CreateBranch(t, ".", branch)
		if err := git.SetBranchPRURL(branch, "https://github.com/owner/repo/pull/123"); err != nil {
			t.Fatalf("failed to set stored PR URL for %q: %v", branch, err)
		}
	}

	err := runResume(resumeCmd, []string{"123"})
	if err == nil {
		t.Fatal("expected an error for an ambiguous PR number")
	}
	if !strings.Contains(err.Error(), "Multiple local branches") {
		t.Errorf("error should mention multiple matches, got: %v", err)
	}

	if current, _ := git.GetCurrentBranch(); current != "feature/two" {
		t.Errorf("current branch = %q, want %q (unchanged after failed resume)", current, "feature/two")
	}
}

// TestResumeByPRNumberOtherRepo verifies that, with a known source repo,
// stored PR URLs pointing at a different repository are ignored.
func TestResumeByPRNumberOtherRepo(t *testing.T) {
	clonePath, _ := testutil.TempRepoWithRemote(t)
	testutil.RunGit(t, clonePath, "remote", "set-url", "origin", "https://github.com/owner/repo.git")

	testutil.CreateBranch(t, clonePath, "feature/mine")
	if err := git.SetBranchPRURL("feature/mine", "https://github.com/someone/else/pull/123"); err != nil {
		t.Fatalf("failed to set stored PR URL: %v", err)
	}

	err := runResume(resumeCmd, []string{"123"})
	if err == nil {
		t.Fatal("expected an error when the stored PR URL belongs to another repo")
	}
	if !strings.Contains(err.Error(), "#123") {
		t.Errorf("error should mention PR #123, got: %v", err)
	}
}
