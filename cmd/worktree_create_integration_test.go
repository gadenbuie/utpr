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

func setupWorktreeCreateTest(t *testing.T) {
	t.Helper()
	testutil.TempRepo(t)

	prevYes := flagInitYes
	t.Cleanup(func() { flagInitYes = prevYes })
	flagInitYes = true // avoid interactive editor prompt
}

// TestWorktreeCreateNewBranch verifies that 'worktree create' creates a new
// branch and its worktree (the 'init --worktree' alias behavior).
func TestWorktreeCreateNewBranch(t *testing.T) {
	setupWorktreeCreateTest(t)

	if err := worktreeCreateCmd.RunE(worktreeCreateCmd, []string{"feat/create-new"}); err != nil {
		t.Fatalf("worktree create failed: %v", err)
	}

	if !git.BranchExists("feat/create-new") {
		t.Fatal("expected branch 'feat/create-new' to be created")
	}
	current, err := git.GetCurrentBranch()
	if err != nil {
		t.Fatalf("failed to get current branch: %v", err)
	}
	if current == "feat/create-new" {
		t.Error("main repo should not have the new branch checked out")
	}
	if git.GetBranchWorktreePath("feat/create-new") == "" {
		t.Fatal("expected a worktree for 'feat/create-new'")
	}
}

// TestWorktreeCreateExistingBranch verifies that 'worktree create' on an
// existing branch creates a worktree for it.
func TestWorktreeCreateExistingBranch(t *testing.T) {
	setupWorktreeCreateTest(t)
	testutil.CreateBranch(t, ".", "existing-branch")

	if err := worktreeCreateCmd.RunE(worktreeCreateCmd, []string{"existing-branch"}); err != nil {
		t.Fatalf("worktree create failed for existing branch: %v", err)
	}

	if git.GetBranchWorktreePath("existing-branch") == "" {
		t.Fatal("expected a worktree for 'existing-branch'")
	}
}

// TestWorktreeCreateSymlinkMemory verifies that the symlink prompt remembers
// the last confirmed selection per repo: the first worktree records what was
// linked, and later worktrees start from that same suggestion instead of the
// default -- and --yes links exactly the remembered items.
func TestWorktreeCreateSymlinkMemory(t *testing.T) {
	setupWorktreeCreateTest(t)
	repo := testutil.TempRepo(t)

	// Untracked items at the repo root become symlink candidates.
	if err := os.WriteFile(filepath.Join(repo, ".env"), []byte("SECRET=1\n"), 0o644); err != nil {
		t.Fatalf("failed to create .env: %v", err)
	}
	if err := os.Mkdir(filepath.Join(repo, ".vscode"), 0o755); err != nil {
		t.Fatalf("failed to create .vscode: %v", err)
	}

	// First worktree: nothing remembered yet, so --yes links every
	// non-managed candidate and records the selection.
	if err := worktreeCreateCmd.RunE(worktreeCreateCmd, []string{"feat/first"}); err != nil {
		t.Fatalf("first worktree create failed: %v", err)
	}
	if got, want := git.ConfigGet(symlinkLastConfigKey), ".env,.vscode"; got != want {
		t.Fatalf("after first create, %s = %q, want %q", symlinkLastConfigKey, got, want)
	}

	// Narrow the memory, as if .vscode had been deselected last time.
	if err := git.ConfigSet(symlinkLastConfigKey, ".env"); err != nil {
		t.Fatalf("failed to set %s: %v", symlinkLastConfigKey, err)
	}

	// Second worktree: --yes replays the remembered selection exactly.
	if err := worktreeCreateCmd.RunE(worktreeCreateCmd, []string{"feat/second"}); err != nil {
		t.Fatalf("second worktree create failed: %v", err)
	}
	second := git.GetBranchWorktreePath("feat/second")
	if second == "" {
		t.Fatal("expected a worktree for 'feat/second'")
	}
	info, err := os.Lstat(filepath.Join(second, ".env"))
	if err != nil {
		t.Fatalf("expected .env symlinked into the second worktree: %v", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Error("worktree .env is not a symlink")
	}
	if _, err := os.Lstat(filepath.Join(second, ".vscode")); !os.IsNotExist(err) {
		t.Errorf("expected .vscode to be left out of the second worktree, got err=%v", err)
	}
	if got, want := git.ConfigGet(symlinkLastConfigKey), ".env"; got != want {
		t.Errorf("after second create, %s = %q, want %q", symlinkLastConfigKey, got, want)
	}
}

// TestWorktreeCreateExistingWorktree verifies that 'worktree create' on a
// branch that already has a worktree offers navigation instead of failing.
func TestWorktreeCreateExistingWorktree(t *testing.T) {
	setupWorktreeCreateTest(t)

	if err := worktreeCreateCmd.RunE(worktreeCreateCmd, []string{"feat/first"}); err != nil {
		t.Fatalf("first worktree create failed: %v", err)
	}
	firstPath := git.GetBranchWorktreePath("feat/first")
	if firstPath == "" {
		t.Fatal("expected a worktree for 'feat/first'")
	}

	// Stub the navigation prompt to "Do nothing".
	restore := testutil.StubChoose("Do nothing")
	defer restore()

	if err := worktreeCreateCmd.RunE(worktreeCreateCmd, []string{"feat/first"}); err != nil {
		t.Fatalf("re-running worktree create failed: %v", err)
	}
	if got := git.GetBranchWorktreePath("feat/first"); got != firstPath {
		t.Errorf("worktree path changed: got %q, want %q", got, firstPath)
	}
}
