//go:build integration

package cmd

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/gadenbuie/utpr/internal/git"
	"github.com/gadenbuie/utpr/internal/testutil"
)

// TestPushRenamedBranchUsesRefspec verifies that pushing a local branch whose
// name differs from its upstream branch (e.g. a PR branch fetched as
// pr/<number>-<author>-<branch>) uses an explicit refspec instead of a bare
// `git push`, which push.default=simple rejects.
func TestPushRenamedBranchUsesRefspec(t *testing.T) {
	clonePath, remotePath := testutil.TempRepoWithRemote(t) // chdirs into the clone
	seedRemoteCache(t)

	// Create the "PR branch" and push it to the remote under its real name.
	testutil.RunGit(t, clonePath, "checkout", "-b", "fix/real-branch-name")
	testutil.AddCommit(t, clonePath, "pr work")
	testutil.RunGit(t, clonePath, "push", "origin", "fix/real-branch-name")

	// Rename the local branch, the way 'utpr fetch' does for PR branches.
	testutil.RunGit(t, clonePath, "branch", "-m", "pr/42-author-fix/real-branch-name")
	testutil.RunGit(t, clonePath, "branch", "-u", "origin/fix/real-branch-name")

	// New local commit to push (the merge commit after 'utpr merge-main').
	testutil.AddCommit(t, clonePath, "merge main")

	// Store the PR URL so push skips the GitHub PR lookup.
	if err := git.SetBranchPRURL("pr/42-author-fix/real-branch-name", "https://github.com/owner/repo/pull/42"); err != nil {
		t.Fatalf("failed to set stored PR URL: %v", err)
	}

	if err := runPush(nil, nil); err != nil {
		t.Fatalf("runPush failed: %v", err)
	}

	// The remote branch received the new commit...
	remoteHead := testutil.RunGit(t, remotePath, "rev-parse", "fix/real-branch-name")
	localHead := testutil.RunGit(t, clonePath, "rev-parse", "HEAD")
	if strings.TrimSpace(remoteHead) != strings.TrimSpace(localHead) {
		t.Errorf("remote branch is %q, want %q", remoteHead, localHead)
	}

	// ...and no branch was created under the local (renamed) name.
	if err := runGitErr(remotePath, "rev-parse", "--verify", "pr/42-author-fix/real-branch-name"); err == nil {
		t.Error("expected no branch named after the local branch on the remote")
	}
}

// runGitErr runs git without failing the test, reporting only the error.
func runGitErr(repoPath string, args ...string) error {
	fullArgs := append([]string{"-C", repoPath}, args...)
	_, err := exec.Command("git", fullArgs...).Output()
	return err
}
