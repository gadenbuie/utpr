//go:build integration

package cmd

// Integration tests in this file mutate the process working directory. Do NOT use t.Parallel().

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/gadenbuie/utpr/internal/git"
	"github.com/gadenbuie/utpr/internal/testutil"
	"github.com/gadenbuie/utpr/internal/ui"
)

func TestPauseFromPRBranch(t *testing.T) {
	clonePath, _ := testutil.TempRepoWithRemote(t)
	defer testutil.StubSpin()()
	defer testutil.StubConfirm(true)()
	seedRemoteCache(t)

	testutil.CreateBranch(t, clonePath, "feature-branch")
	testutil.AddCommit(t, clonePath, "feature work")
	testutil.RunGit(t, clonePath, "push", "-u", "origin", "feature-branch")

	err := runPause(nil, nil)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	branch, err := git.GetCurrentBranch()
	if err != nil {
		t.Fatalf("failed to get current branch: %v", err)
	}
	if branch != "main" {
		t.Fatalf("expected to be on main, got %q", branch)
	}
}

func TestPauseAlreadyOnDefault(t *testing.T) {
	testutil.TempRepoWithRemote(t)
	defer testutil.StubSpin()()
	defer testutil.StubConfirm(true)()
	seedRemoteCache(t)

	err := runPause(nil, nil)
	if err == nil {
		t.Fatal("expected an error when pausing from default branch")
	}
}

func TestPauseWithUncommittedChanges(t *testing.T) {
	clonePath, _ := testutil.TempRepoWithRemote(t)
	defer testutil.StubSpin()()
	defer testutil.StubConfirm(false)()
	seedRemoteCache(t)

	testutil.CreateBranch(t, clonePath, "dirty-branch")
	testutil.AddCommit(t, clonePath, "initial feature work")
	testutil.RunGit(t, clonePath, "push", "-u", "origin", "dirty-branch")

	trackedFile := filepath.Join(clonePath, "file-to-modify.txt")
	if err := os.WriteFile(trackedFile, []byte("original\n"), 0644); err != nil {
		t.Fatalf("failed to create file: %v", err)
	}
	testutil.RunGit(t, clonePath, "add", "file-to-modify.txt")
	testutil.RunGit(t, clonePath, "commit", "-m", "add tracked file")

	if err := os.WriteFile(trackedFile, []byte("modified\n"), 0644); err != nil {
		t.Fatalf("failed to modify file: %v", err)
	}

	err := runPause(nil, nil)
	if !errors.Is(err, ui.ErrCancelled) {
		t.Fatalf("expected ErrCancelled, got %v", err)
	}
}

func TestPauseWithUncommittedChangesAccepted(t *testing.T) {
	clonePath, _ := testutil.TempRepoWithRemote(t)
	defer testutil.StubSpin()()
	defer testutil.StubConfirm(true)()
	seedRemoteCache(t)

	testutil.CreateBranch(t, clonePath, "dirty-accepted-branch")
	testutil.AddCommit(t, clonePath, "initial feature work")
	testutil.RunGit(t, clonePath, "push", "-u", "origin", "dirty-accepted-branch")

	// Modify a file that exists on both branches (from the initial commit)
	// so that git checkout won't block on the uncommitted change.
	entries, err := os.ReadDir(clonePath)
	if err != nil {
		t.Fatalf("failed to read dir: %v", err)
	}
	var trackedFile string
	for _, e := range entries {
		if !e.IsDir() && filepath.Ext(e.Name()) == ".txt" {
			trackedFile = filepath.Join(clonePath, e.Name())
			break
		}
	}
	if trackedFile == "" {
		t.Fatal("could not find a tracked .txt file from initial commit")
	}

	if err := os.WriteFile(trackedFile, []byte("modified content\n"), 0644); err != nil {
		t.Fatalf("failed to modify file: %v", err)
	}

	err = runPause(nil, nil)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	branch, err := git.GetCurrentBranch()
	if err != nil {
		t.Fatalf("failed to get current branch: %v", err)
	}
	if branch != "main" {
		t.Fatalf("expected to be on main, got %q", branch)
	}
}
