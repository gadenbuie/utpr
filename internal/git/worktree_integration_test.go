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

func TestWorktreeListIntegration(t *testing.T) {
	repoPath := testutil.TempRepo(t)

	testutil.CreateBranch(t, repoPath, "wt-list-branch")
	testutil.RunGit(t, repoPath, "checkout", "-") // back to default branch

	wtPath := filepath.Join(filepath.Dir(repoPath), "wt-list")
	testutil.RunGit(t, repoPath, "worktree", "add", wtPath, "wt-list-branch")
	t.Cleanup(func() {
		_ = WorktreeRemove(wtPath, true)
	})

	worktrees, err := WorktreeList()
	if err != nil {
		t.Fatalf("WorktreeList failed: %v", err)
	}

	if len(worktrees) != 2 {
		t.Fatalf("expected 2 worktrees, got %d", len(worktrees))
	}

	if worktrees[0].Path != repoPath {
		t.Errorf("expected main worktree path %q, got %q", repoPath, worktrees[0].Path)
	}
	if worktrees[1].Path != wtPath {
		t.Errorf("expected second worktree path %q, got %q", wtPath, worktrees[1].Path)
	}
	if worktrees[1].Branch != "refs/heads/wt-list-branch" {
		t.Errorf("expected branch refs/heads/wt-list-branch, got %q", worktrees[1].Branch)
	}
}

func TestGetBranchWorktreePathIntegration(t *testing.T) {
	repoPath := testutil.TempRepo(t)

	testutil.CreateBranch(t, repoPath, "feat/test")
	testutil.RunGit(t, repoPath, "checkout", "-")

	wtPath := filepath.Join(filepath.Dir(repoPath), "wt-feat-test")
	testutil.RunGit(t, repoPath, "worktree", "add", wtPath, "feat/test")
	t.Cleanup(func() {
		_ = WorktreeRemove(wtPath, true)
	})

	got := GetBranchWorktreePath("feat/test")
	if got != wtPath {
		t.Fatalf("expected worktree path %q, got %q", wtPath, got)
	}

	got = GetBranchWorktreePath("nonexistent-branch")
	if got != "" {
		t.Fatalf("expected empty path for nonexistent branch, got %q", got)
	}
}

func TestIsInWorktreeIntegration(t *testing.T) {
	repoPath := testutil.TempRepo(t)

	if IsInWorktree() {
		t.Fatal("expected IsInWorktree to be false in main repo")
	}

	testutil.CreateBranch(t, repoPath, "wt-check-branch")
	testutil.RunGit(t, repoPath, "checkout", "-")

	wtPath := filepath.Join(filepath.Dir(repoPath), "wt-check")
	testutil.RunGit(t, repoPath, "worktree", "add", wtPath, "wt-check-branch")
	t.Cleanup(func() {
		_ = os.Chdir(repoPath)
		_ = WorktreeRemove(wtPath, true)
	})

	if err := os.Chdir(wtPath); err != nil {
		t.Fatalf("chdir to worktree failed: %v", err)
	}

	if !IsInWorktree() {
		t.Fatal("expected IsInWorktree to be true inside a worktree")
	}
}

func TestGetMainRepoRootIntegration(t *testing.T) {
	repoPath := testutil.TempRepo(t)

	testutil.CreateBranch(t, repoPath, "wt-root-branch")
	testutil.RunGit(t, repoPath, "checkout", "-")

	wtPath := filepath.Join(filepath.Dir(repoPath), "wt-root")
	testutil.RunGit(t, repoPath, "worktree", "add", wtPath, "wt-root-branch")
	t.Cleanup(func() {
		_ = os.Chdir(repoPath)
		_ = WorktreeRemove(wtPath, true)
	})

	if err := os.Chdir(wtPath); err != nil {
		t.Fatalf("chdir to worktree failed: %v", err)
	}

	mainRoot, err := GetMainRepoRoot()
	if err != nil {
		t.Fatalf("GetMainRepoRoot failed: %v", err)
	}

	if mainRoot != repoPath {
		t.Fatalf("expected main repo root %q, got %q", repoPath, mainRoot)
	}
}

func TestWorktreeAddAndRemove(t *testing.T) {
	repoPath := testutil.TempRepo(t)

	testutil.CreateBranch(t, repoPath, "wt-add-rm-branch")
	testutil.RunGit(t, repoPath, "checkout", "-")

	wtPath := filepath.Join(filepath.Dir(repoPath), "wt-add-rm")

	if err := WorktreeAdd(wtPath, "wt-add-rm-branch"); err != nil {
		t.Fatalf("WorktreeAdd failed: %v", err)
	}

	if _, err := os.Stat(wtPath); os.IsNotExist(err) {
		t.Fatal("expected worktree directory to exist after add")
	}

	if err := WorktreeRemove(wtPath, false); err != nil {
		t.Fatalf("WorktreeRemove failed: %v", err)
	}

	if _, err := os.Stat(wtPath); !os.IsNotExist(err) {
		t.Fatal("expected worktree directory to be gone after remove")
	}
}
