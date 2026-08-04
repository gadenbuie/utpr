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
