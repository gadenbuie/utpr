//go:build integration

package cmd

// Integration tests in this file mutate the process working directory. Do NOT use t.Parallel().

import (
	"strings"
	"testing"

	"github.com/gadenbuie/utpr/internal/testutil"
)

func TestMergeMainOnDefaultBranch(t *testing.T) {
	testutil.TempRepoWithRemote(t)
	defer testutil.StubSpin()()
	seedRemoteCache(t)

	err := runMergeMain(nil, nil)
	if err == nil {
		t.Fatal("expected error when on default branch, got nil")
	}
}

func TestMergeMainMerge(t *testing.T) {
	clonePath, _ := testutil.TempRepoWithRemote(t)
	defer testutil.StubSpin()()
	defer testutil.StubConfirm(true)()
	seedRemoteCache(t)
	oldRebase := mergeMainRebase
	t.Cleanup(func() { mergeMainRebase = oldRebase })
	mergeMainRebase = false

	testutil.CreateBranch(t, clonePath, "feature")
	testutil.AddCommit(t, clonePath, "feature work")
	testutil.RunGit(t, clonePath, "push", "-u", "origin", "feature")

	testutil.RunGit(t, clonePath, "checkout", "main")
	testutil.AddCommit(t, clonePath, "main upstream change")
	testutil.RunGit(t, clonePath, "push", "origin", "main")

	testutil.RunGit(t, clonePath, "checkout", "feature")

	err := runMergeMain(nil, nil)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	log := testutil.RunGit(t, clonePath, "log", "--oneline")
	if !strings.Contains(log, "main upstream change") {
		t.Fatalf("feature branch should contain main's commit, got log:\n%s", log)
	}
}

func TestMergeMainRebase(t *testing.T) {
	clonePath, _ := testutil.TempRepoWithRemote(t)
	defer testutil.StubSpin()()
	defer testutil.StubConfirm(true)()
	seedRemoteCache(t)
	oldRebase := mergeMainRebase
	t.Cleanup(func() { mergeMainRebase = oldRebase })
	mergeMainRebase = true

	testutil.CreateBranch(t, clonePath, "feature-rebase")
	testutil.AddCommit(t, clonePath, "feature rebase work")
	testutil.RunGit(t, clonePath, "push", "-u", "origin", "feature-rebase")

	testutil.RunGit(t, clonePath, "checkout", "main")
	testutil.AddCommit(t, clonePath, "main rebase change")
	testutil.RunGit(t, clonePath, "push", "origin", "main")

	testutil.RunGit(t, clonePath, "checkout", "feature-rebase")

	err := runMergeMain(nil, nil)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	log := testutil.RunGit(t, clonePath, "log", "--oneline")
	if !strings.Contains(log, "main rebase change") {
		t.Fatalf("feature branch should contain main's commit after rebase, got log:\n%s", log)
	}

	merges := testutil.RunGit(t, clonePath, "log", "--oneline", "--merges")
	if strings.TrimSpace(merges) != "" {
		t.Fatalf("expected no merge commits after rebase, got:\n%s", merges)
	}
}

func TestMergeMainNoChanges(t *testing.T) {
	clonePath, _ := testutil.TempRepoWithRemote(t)
	defer testutil.StubSpin()()
	seedRemoteCache(t)
	oldRebase := mergeMainRebase
	t.Cleanup(func() { mergeMainRebase = oldRebase })
	mergeMainRebase = false

	testutil.CreateBranch(t, clonePath, "feature-noop")
	testutil.AddCommit(t, clonePath, "feature noop work")
	testutil.RunGit(t, clonePath, "push", "-u", "origin", "feature-noop")

	err := runMergeMain(nil, nil)
	if err != nil {
		t.Fatalf("expected no error for no-op merge, got: %v", err)
	}
}
