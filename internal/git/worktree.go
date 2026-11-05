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
// of it are deleted first -- but only when they are the only thing blocking
// the removal. Removing such a symlink never touches the files it points to,
// but git otherwise refuses to remove a worktree containing untracked files
// -- which trips over the shared-directory symlinks created by
// `utpr init --worktree` when the linked directory is not gitignored. If
// anything else would make git refuse the removal (other untracked files,
// changes to tracked files), the worktree -- and its symlinks -- are left
// untouched and git's refusal falls through to the force-removal prompt.
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
// that resolve to a location outside the worktree -- but only when those
// symlinks are the only thing standing in the way of a non-forced
// `git worktree remove`. If the worktree contains any other untracked or
// modified files, git refuses the removal anyway, so nothing is deleted and
// the worktree keeps its symlinks in case the user decides to keep it.
func removeOutsideSymlinks(path string) error {
	out, err := RunInDir(path, "status", "--porcelain=v1", "-z", "-uall")
	if err != nil {
		return fmt.Errorf("failed to check worktree status: %w", err)
	}
	if hasTrackedChanges(out) {
		// Changes to tracked files block the removal on their own; deleting
		// the symlinks would only damage a worktree the user may keep.
		return nil
	}

	var links []string
	for _, name := range untrackedPaths(out) {
		linkPath := filepath.Join(path, name)
		fi, err := os.Lstat(linkPath)
		if err != nil || fi.Mode()&os.ModeSymlink == 0 {
			// An untracked file git refuses the removal over regardless: the
			// symlinks are not the only blockers, so keep everything as-is.
			return nil
		}
		outside, err := symlinkPointsOutside(linkPath, path)
		if err != nil || !outside {
			return nil
		}
		links = append(links, linkPath)
	}

	for _, linkPath := range links {
		if err := os.Remove(linkPath); err != nil {
			return fmt.Errorf("failed to remove symlink %s: %w", linkPath, err)
		}
	}
	return nil
}

// hasTrackedChanges reports whether the `git status --porcelain=v1 -z` output
// contains any entry other than untracked files.
func hasTrackedChanges(z string) bool {
	for _, record := range strings.Split(z, "\x00") {
		if len(record) >= 4 && record[:2] != "??" {
			return true
		}
	}
	return false
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

// symlinkPointsOutside reports whether the symlink at linkPath points to a
// location outside root. A broken symlink is judged lexically from its raw
// target: one that would land inside the worktree (e.g. a link created before
// its target exists, like current -> build/out) counts as pointing inside,
// while one aimed outside the worktree counts as pointing outside.
func symlinkPointsOutside(linkPath, root string) (bool, error) {
	target, err := os.Readlink(linkPath)
	if err != nil {
		return false, err
	}
	absTarget := target
	if !filepath.IsAbs(target) {
		absTarget = filepath.Join(filepath.Dir(linkPath), target)
	}
	absTarget = filepath.Clean(absTarget)
	resolved, err := filepath.EvalSymlinks(absTarget)
	if err != nil {
		// Broken symlink: resolve its deepest existing ancestor (the missing
		// tail is appended lexically) so the comparison shares the same
		// real-path base as root, and deliberate inside-pointing links are
		// not destroyed.
		resolved = resolveBrokenPath(absTarget)
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

// resolveBrokenPath resolves a path whose tail does not exist to a real path:
// the deepest existing ancestor is resolved with EvalSymlinks and the missing
// elements are appended back lexically. If nothing along the way can be
// resolved, the lexical path is returned unchanged.
func resolveBrokenPath(absPath string) string {
	var missing []string
	dir := absPath
	for {
		if resolved, err := filepath.EvalSymlinks(dir); err == nil {
			result := resolved
			for i := len(missing) - 1; i >= 0; i-- {
				result = filepath.Join(result, missing[i])
			}
			return result
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return absPath
		}
		missing = append(missing, filepath.Base(dir))
		dir = parent
	}
}
