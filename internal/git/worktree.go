package git

import (
	"fmt"
	"os"
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

// IsBranchInMainWorktree returns true if the given branch is currently
// checked out in the main worktree.
func IsBranchInMainWorktree(branch string) bool {
	worktrees, err := WorktreeList()
	if err != nil {
		return false
	}
	return BranchInMainWorktree(worktrees, branch)
}

// BranchInMainWorktree returns true if the given branch is checked out in
// the main worktree (index 0 of `git worktree list`).
func BranchInMainWorktree(worktrees []Worktree, branch string) bool {
	if len(worktrees) == 0 {
		return false
	}
	return worktrees[0].Branch == fmt.Sprintf("refs/heads/%s", branch)
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
	// Both paths can be relative to CWD; resolve to absolute before comparing
	// so that subdirectory invocations don't produce false positives.
	absGitDir, err1 := filepath.Abs(gitDir)
	absCommonDir, err2 := filepath.Abs(commonDir)
	if err1 != nil || err2 != nil {
		return false
	}
	return absGitDir != absCommonDir
}

// GetMainRepoRoot returns the root directory of the main repo when in a worktree.
func GetMainRepoRoot() (string, error) {
	commonDir, err := Run("rev-parse", "--git-common-dir")
	if err != nil {
		return "", err
	}
	// Resolve to absolute path before taking the parent so the result is
	// always an absolute path regardless of CWD depth.
	absCommonDir, err := filepath.Abs(commonDir)
	if err != nil {
		return "", err
	}
	return filepath.Dir(absCommonDir), nil
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
//
// When force is false, untracked symlinks in the worktree that point outside
// of it are deleted first. Removing such a symlink never touches the files it
// points to, but git otherwise refuses to remove a worktree containing
// untracked files -- which trips over the shared-directory symlinks created by
// `utpr init --worktree` when the linked directory is not gitignored. All
// other untracked files and any changes to tracked files still make git
// refuse the removal, falling through to the force-removal prompt.
func WorktreeRemove(path string, force bool) error {
	if !force {
		if err := removeOutsideSymlinks(path); err != nil {
			return err
		}
	}
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

// removeOutsideSymlinks deletes untracked symlinks in the worktree at path
// that resolve to a location outside the worktree. Symlinks pointing inside
// the worktree are left alone, as are untracked files and any changes to
// tracked files: those still block a non-forced `git worktree remove`.
func removeOutsideSymlinks(path string) error {
	out, err := RunInDir(path, "status", "--porcelain=v1", "-z")
	if err != nil {
		return fmt.Errorf("failed to check worktree status: %w", err)
	}

	for _, name := range untrackedPaths(out) {
		linkPath := filepath.Join(path, name)
		fi, err := os.Lstat(linkPath)
		if err != nil || fi.Mode()&os.ModeSymlink == 0 {
			continue
		}
		outside, err := symlinkPointsOutside(linkPath, path)
		if err != nil || !outside {
			continue
		}
		if err := os.Remove(linkPath); err != nil {
			return fmt.Errorf("failed to remove symlink %s: %w", name, err)
		}
	}
	return nil
}

// untrackedPaths parses `git status --porcelain=v1 -z` output and returns the
// paths of untracked file entries. Untracked directories (which cannot be
// symlinks) are skipped.
func untrackedPaths(z string) []string {
	var paths []string
	for _, record := range strings.Split(z, "\x00") {
		if len(record) < 4 {
			continue
		}
		if record[:2] != "??" {
			continue
		}
		path := record[3:]
		if strings.HasSuffix(path, "/") {
			continue
		}
		paths = append(paths, path)
	}
	return paths
}

// symlinkPointsOutside reports whether the symlink at linkPath resolves to a
// location outside root. A broken symlink counts as pointing outside, since
// it does not resolve to anything inside the worktree.
func symlinkPointsOutside(linkPath, root string) (bool, error) {
	target, err := os.Readlink(linkPath)
	if err != nil {
		return false, err
	}
	absTarget := target
	if !filepath.IsAbs(target) {
		absTarget = filepath.Join(filepath.Dir(linkPath), target)
	}
	resolved, err := filepath.EvalSymlinks(absTarget)
	if err != nil {
		return true, nil
	}
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return false, err
	}
	rel, err := filepath.Rel(resolvedRoot, resolved)
	if err != nil {
		return true, nil
	}
	return rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)), nil
}
