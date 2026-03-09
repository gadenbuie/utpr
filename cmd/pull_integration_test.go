//go:build integration

package cmd

// Integration tests in this file mutate the process working directory. Do NOT use t.Parallel().

import (
	"testing"

	"github.com/gadenbuie/utpr/internal/remote"
	"github.com/gadenbuie/utpr/internal/testutil"
)

func seedPullRemoteCache(t *testing.T) {
	t.Helper()
	remote.SetCacheForTest(&remote.Config{
		Layout:        "ours",
		SourceRemote:  "origin",
		PushRemote:    "origin",
		DefaultBranch: "main",
	})
	t.Cleanup(func() { remote.ResetCacheForTest() })
}

func TestPullOnDefaultBranch(t *testing.T) {
	testutil.TempRepoWithRemote(t)
	defer testutil.StubSpin()()
	seedPullRemoteCache(t)

	err := runPull(nil, nil)
	if err != nil {
		t.Fatalf("runPull on default branch failed: %v", err)
	}
}

func TestPullOnPRBranchUpToDate(t *testing.T) {
	clonePath, _ := testutil.TempRepoWithRemote(t)
	defer testutil.StubSpin()()
	seedPullRemoteCache(t)

	testutil.CreateBranch(t, clonePath, "feature-up-to-date")
	testutil.AddCommit(t, clonePath, "feature work")
	testutil.RunGit(t, clonePath, "push", "-u", "origin", "feature-up-to-date")

	err := runPull(nil, nil)
	if err != nil {
		t.Fatalf("runPull on up-to-date PR branch failed: %v", err)
	}
}

func TestPullOnPRBranchBehind(t *testing.T) {
	clonePath, _ := testutil.TempRepoWithRemote(t)
	defer testutil.StubSpin()()
	seedPullRemoteCache(t)

	testutil.CreateBranch(t, clonePath, "feature-behind")
	testutil.AddCommit(t, clonePath, "commit 1")
	testutil.AddCommit(t, clonePath, "commit 2")
	testutil.RunGit(t, clonePath, "push", "-u", "origin", "feature-behind")

	testutil.RunGit(t, clonePath, "reset", "--hard", "HEAD~1")

	err := runPull(nil, nil)
	if err != nil {
		t.Fatalf("runPull on behind PR branch failed: %v", err)
	}
}

func TestPullOnPRBranchNoTracking(t *testing.T) {
	clonePath, _ := testutil.TempRepoWithRemote(t)
	defer testutil.StubSpin()()
	seedPullRemoteCache(t)

	testutil.CreateBranch(t, clonePath, "feature-no-tracking")
	testutil.AddCommit(t, clonePath, "local only work")

	err := runPull(nil, nil)
	if err != nil {
		t.Fatalf("runPull on branch with no tracking failed: %v", err)
	}
}
