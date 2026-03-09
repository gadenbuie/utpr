package git

import (
	"fmt"
	"path/filepath"
	"strings"
)

// Worktree represents a git worktree entry.
type Worktree struct {
	Path   string
	HEAD   string
	Branch string
}

// WorktreeList parses `git worktree list --porcelain` output.
func WorktreeList() ([]Worktree, error) {
	out, err := Run("worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}
	return ParseWorktreeListOutput(out), nil
}

// ParseWorktreeListOutput parses the porcelain output of `git worktree list`.
func ParseWorktreeListOutput(output string) []Worktree {
	var worktrees []Worktree
	var current Worktree
	for _, line := range strings.Split(output, "\n") {
		switch {
		case strings.HasPrefix(line, "worktree "):
			if current.Path != "" {
				worktrees = append(worktrees, current)
			}
			current = Worktree{Path: strings.TrimPrefix(line, "worktree ")}
		case strings.HasPrefix(line, "HEAD "):
			current.HEAD = strings.TrimPrefix(line, "HEAD ")
		case strings.HasPrefix(line, "branch "):
			current.Branch = strings.TrimPrefix(line, "branch ")
		case line == "":
			if current.Path != "" {
				worktrees = append(worktrees, current)
				current = Worktree{}
			}
		}
	}
	if current.Path != "" {
		worktrees = append(worktrees, current)
	}
	return worktrees
}

// GetBranchWorktreePath returns the worktree path for a branch, or empty string
// if the branch doesn't have a worktree (excluding the main worktree).
func GetBranchWorktreePath(branch string) string {
	worktrees, err := WorktreeList()
	if err != nil {
		return ""
	}

	return FindWorktreeForBranch(worktrees, branch)
}

// FindWorktreeForBranch searches worktrees (skipping index 0, the main worktree)
// for a branch and returns its path, or empty string if not found.
func FindWorktreeForBranch(worktrees []Worktree, branch string) string {
	ref := fmt.Sprintf("refs/heads/%s", branch)
	for i, wt := range worktrees {
		if i == 0 {
			continue
		}
		if wt.Branch == ref {
			return wt.Path
		}
	}
	return ""
}

// GetWorktreeDir computes the conventional worktree directory path for a branch.
func GetWorktreeDir(branch string) (string, error) {
	topLevel, err := GetTopLevel()
	if err != nil {
		return "", err
	}
	return ComputeWorktreeDir(topLevel, branch), nil
}

// ComputeWorktreeDir computes the conventional worktree directory path
// from a repo top-level path and branch name.
func ComputeWorktreeDir(topLevel, branch string) string {
	repoName := filepath.Base(topLevel)
	parentDir := filepath.Dir(topLevel)
	return filepath.Join(parentDir, repoName+".worktrees", branch)
}

// IsInWorktree returns true if the current directory is in a git worktree
// (as opposed to the main repo).
func IsInWorktree() bool {
	gitDir, err1 := Run("rev-parse", "--git-dir")
	commonDir, err2 := Run("rev-parse", "--git-common-dir")
	if err1 != nil || err2 != nil {
		return false
	}
	return gitDir != commonDir
}

// GetMainRepoRoot returns the root directory of the main repo when in a worktree.
func GetMainRepoRoot() (string, error) {
	commonDir, err := Run("rev-parse", "--git-common-dir")
	if err != nil {
		return "", err
	}
	// git-common-dir returns path to .git; parent is the repo root
	return filepath.Clean(filepath.Join(commonDir, "..")), nil
}

// WorktreeAdd creates a new worktree at the given path for the branch.
func WorktreeAdd(path, branch string) error {
	_, stderr, err := RunSilent("worktree", "add", path, branch)
	if err != nil {
		if stderr != "" {
			return fmt.Errorf("failed to create worktree: %s", stderr)
		}
		return fmt.Errorf("failed to create worktree at %s", path)
	}
	return nil
}

// WorktreeRemove removes a worktree at the given path.
func WorktreeRemove(path string, force bool) error {
	args := []string{"worktree", "remove"}
	if force {
		args = append(args, "--force")
	}
	args = append(args, path)
	_, stderr, err := RunSilent(args...)
	if err != nil {
		if stderr != "" {
			return fmt.Errorf("failed to remove worktree: %s", stderr)
		}
		return fmt.Errorf("failed to remove worktree at %s", path)
	}
	return nil
}
