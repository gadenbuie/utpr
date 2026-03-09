package testutil

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"testing"
)

// TempRepo creates a temporary git repository with an initial commit.
// It changes the working directory to the new repo and restores it on cleanup.
func TempRepo(t *testing.T) string {
	t.Helper()

	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get working directory: %v", err)
	}

	dir := t.TempDir()
	dir, err = filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatalf("failed to resolve symlinks: %v", err)
	}

	RunGit(t, dir, "init", "--initial-branch=main")
	RunGit(t, dir, "config", "user.name", "Test User")
	RunGit(t, dir, "config", "user.email", "test@example.com")

	AddCommit(t, dir, "initial commit")

	if err := os.Chdir(dir); err != nil {
		t.Fatalf("failed to chdir to temp repo: %v", err)
	}

	t.Cleanup(func() {
		_ = os.Chdir(origDir)
	})

	return dir
}

// TempRepoWithRemote creates a bare remote repo and a clone of it.
// Returns (clonePath, remotePath). Changes working directory to the clone.
func TempRepoWithRemote(t *testing.T) (clonePath, remotePath string) {
	t.Helper()

	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get working directory: %v", err)
	}

	parentDir := t.TempDir()
	parentDir, err = filepath.EvalSymlinks(parentDir)
	if err != nil {
		t.Fatalf("failed to resolve symlinks: %v", err)
	}

	remotePath = filepath.Join(parentDir, "remote.git")
	clonePath = filepath.Join(parentDir, "clone")

	// Create a non-bare repo first so we can make an initial commit
	seedPath := filepath.Join(parentDir, "seed")
	RunGit(t, parentDir, "init", "--initial-branch=main", seedPath)
	RunGit(t, seedPath, "config", "user.name", "Test User")
	RunGit(t, seedPath, "config", "user.email", "test@example.com")
	AddCommit(t, seedPath, "initial commit")

	// Clone seed to bare remote
	RunGit(t, parentDir, "clone", "--bare", seedPath, remotePath)

	// Clone from bare remote
	RunGit(t, parentDir, "clone", remotePath, clonePath)
	RunGit(t, clonePath, "config", "user.name", "Test User")
	RunGit(t, clonePath, "config", "user.email", "test@example.com")

	if err := os.Chdir(clonePath); err != nil {
		t.Fatalf("failed to chdir to clone: %v", err)
	}

	t.Cleanup(func() {
		_ = os.Chdir(origDir)
	})

	return clonePath, remotePath
}

var commitCounter atomic.Int64

// AddCommit creates a uniquely-named file and commits it in the given repo.
func AddCommit(t *testing.T, repoPath, message string) {
	t.Helper()

	n := commitCounter.Add(1)
	filename := fmt.Sprintf("file-%d.txt", n)
	filePath := filepath.Join(repoPath, filename)

	if err := os.WriteFile(filePath, []byte(message+"\n"), 0644); err != nil {
		t.Fatalf("failed to create file: %v", err)
	}

	RunGit(t, repoPath, "add", filename)
	RunGit(t, repoPath, "commit", "-m", message)
}

// CreateBranch creates and checks out a new branch in the given repo.
func CreateBranch(t *testing.T, repoPath, branch string) {
	t.Helper()
	RunGit(t, repoPath, "checkout", "-b", branch)
}

// RunGit runs a git command in the given repo directory, fails the test on error,
// and returns stdout.
func RunGit(t *testing.T, repoPath string, args ...string) string {
	t.Helper()

	fullArgs := append([]string{"-C", repoPath}, args...)
	cmd := exec.Command("git", fullArgs...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, string(out))
	}
	return string(out)
}
