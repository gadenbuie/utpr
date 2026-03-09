//go:build integration

package git

// Integration tests in this file mutate the process working directory.
// Do NOT use t.Parallel().

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gadenbuie/utpr/internal/testutil"
)

func TestGetCurrentBranch(t *testing.T) {
	repoPath := testutil.TempRepo(t)

	branch, err := GetCurrentBranch()
	if err != nil {
		t.Fatalf("GetCurrentBranch failed: %v", err)
	}

	if branch != "main" {
		t.Fatalf("expected initial branch to be main, got %q", branch)
	}

	testutil.CreateBranch(t, repoPath, "feature-branch")

	branch, err = GetCurrentBranch()
	if err != nil {
		t.Fatalf("GetCurrentBranch after checkout failed: %v", err)
	}
	if branch != "feature-branch" {
		t.Fatalf("expected branch feature-branch, got %q", branch)
	}
}

func TestBranchExists(t *testing.T) {
	repoPath := testutil.TempRepo(t)

	testutil.CreateBranch(t, repoPath, "exists-branch")

	if !BranchExists("exists-branch") {
		t.Fatal("expected exists-branch to exist")
	}

	if BranchExists("nonexistent-branch") {
		t.Fatal("expected nonexistent-branch to not exist")
	}
}

func TestHasUncommittedChanges(t *testing.T) {
	repoPath := testutil.TempRepo(t)

	if HasUncommittedChanges() {
		t.Fatal("expected no uncommitted changes in fresh repo")
	}

	// Create a known tracked file and modify it to produce uncommitted changes.
	trackedFile := "dirty.txt"
	if err := os.WriteFile(filepath.Join(repoPath, trackedFile), []byte("initial\n"), 0644); err != nil {
		t.Fatalf("failed to create file: %v", err)
	}
	if _, err := Run("add", trackedFile); err != nil {
		t.Fatalf("git add failed: %v", err)
	}
	if _, err := Run("commit", "-m", "add dirty.txt"); err != nil {
		t.Fatalf("git commit failed: %v", err)
	}

	if err := os.WriteFile(filepath.Join(repoPath, trackedFile), []byte("modified\n"), 0644); err != nil {
		t.Fatalf("failed to modify file: %v", err)
	}

	if !HasUncommittedChanges() {
		t.Fatal("expected uncommitted changes after modifying tracked file")
	}

	// Stage the file
	if _, err := Run("add", trackedFile); err != nil {
		t.Fatalf("git add failed: %v", err)
	}

	if !HasUncommittedChanges() {
		t.Fatal("expected uncommitted changes after staging")
	}

	// Commit
	if _, err := Run("commit", "-m", "commit changes"); err != nil {
		t.Fatalf("git commit failed: %v", err)
	}

	if HasUncommittedChanges() {
		t.Fatal("expected no uncommitted changes after commit")
	}
}

func TestHasUnpushedCommits(t *testing.T) {
	clonePath, _ := testutil.TempRepoWithRemote(t)

	if HasUnpushedCommits() {
		t.Fatal("expected no unpushed commits initially")
	}

	testutil.AddCommit(t, clonePath, "local only commit")

	if !HasUnpushedCommits() {
		t.Fatal("expected unpushed commits after local commit")
	}

	if _, err := Run("push"); err != nil {
		t.Fatalf("git push failed: %v", err)
	}

	if HasUnpushedCommits() {
		t.Fatal("expected no unpushed commits after push")
	}
}

func TestRevListCount(t *testing.T) {
	clonePath, _ := testutil.TempRepoWithRemote(t)

	testutil.AddCommit(t, clonePath, "commit 1")
	testutil.AddCommit(t, clonePath, "commit 2")
	testutil.AddCommit(t, clonePath, "commit 3")

	tracking := GetTrackingBranch()
	if tracking == "" {
		t.Fatal("expected a tracking branch")
	}

	count, err := RevListCount(tracking + "..HEAD")
	if err != nil {
		t.Fatalf("RevListCount failed: %v", err)
	}
	if count != 3 {
		t.Fatalf("expected 3 unpushed commits, got %d", count)
	}
}

func TestGetDefaultBranch(t *testing.T) {
	testutil.TempRepoWithRemote(t)

	branch, err := GetDefaultBranch("origin")
	if err != nil {
		t.Fatalf("GetDefaultBranch failed: %v", err)
	}

	if branch != "main" {
		t.Fatalf("expected default branch to be main, got %q", branch)
	}
}

func TestIsInsideWorkTree(t *testing.T) {
	testutil.TempRepo(t)

	if !IsInsideWorkTree() {
		t.Fatal("expected to be inside work tree")
	}

	tmpDir := t.TempDir()
	tmpDir, _ = filepath.EvalSymlinks(tmpDir)
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("chdir failed: %v", err)
	}

	if IsInsideWorkTree() {
		t.Fatal("expected to not be inside work tree in a non-repo dir")
	}
}
