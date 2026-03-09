package git

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Run executes a git command and returns stdout. If the command fails,
// the error includes stderr output.
func Run(args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		stderrStr := strings.TrimSpace(stderr.String())
		if stderrStr != "" {
			return "", fmt.Errorf("git %s: %w\n%s", args[0], err, stderrStr)
		}
		return "", fmt.Errorf("git %s: %w", args[0], err)
	}
	return strings.TrimSpace(stdout.String()), nil
}

// RunSilent executes a git command and returns stdout and stderr separately.
// Useful when you need to inspect stderr on failure.
func RunSilent(args ...string) (stdout, stderr string, err error) {
	cmd := exec.Command("git", args...)
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	err = cmd.Run()
	return strings.TrimSpace(outBuf.String()), strings.TrimSpace(errBuf.String()), err
}

// RunInteractive runs a git command with stdin/stdout/stderr connected
// to the terminal. Used for commands where the user needs to see output
// live (e.g., git merge with conflicts).
func RunInteractive(args ...string) error {
	cmd := exec.Command("git", args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// IsInstalled checks if git is available on PATH.
func IsInstalled() bool {
	_, err := exec.LookPath("git")
	return err == nil
}

// IsInsideWorkTree returns true if the current directory is inside a git repo.
func IsInsideWorkTree() bool {
	out, err := Run("rev-parse", "--is-inside-work-tree")
	return err == nil && out == "true"
}

// GetCurrentBranch returns the name of the currently checked-out branch.
func GetCurrentBranch() (string, error) {
	return Run("rev-parse", "--abbrev-ref", "HEAD")
}

// GetDefaultBranch tries to determine the default branch for the given remote.
// It checks symbolic-ref, then falls back to local/remote main/master.
func GetDefaultBranch(remote string) (string, error) {
	// Try symbolic-ref first
	out, err := Run("symbolic-ref", fmt.Sprintf("refs/remotes/%s/HEAD", remote))
	if err == nil && out != "" {
		branch := strings.TrimPrefix(out, fmt.Sprintf("refs/remotes/%s/", remote))
		if branch != "" {
			return branch, nil
		}
	}

	// Check local branches
	for _, name := range []string{"main", "master"} {
		_, err := Run("show-ref", "--verify", "--quiet", fmt.Sprintf("refs/heads/%s", name))
		if err == nil {
			return name, nil
		}
	}

	// Check remote branches
	for _, name := range []string{"main", "master"} {
		_, err := Run("show-ref", "--verify", "--quiet", fmt.Sprintf("refs/remotes/%s/%s", remote, name))
		if err == nil {
			return name, nil
		}
	}

	return "", fmt.Errorf("could not determine default branch")
}

// IsOnBranch returns true if HEAD is on the named branch.
func IsOnBranch(branch string) (bool, error) {
	current, err := GetCurrentBranch()
	if err != nil {
		return false, err
	}
	return current == branch, nil
}

// HasUncommittedChanges returns true if there are staged or unstaged changes
// (excluding untracked files).
func HasUncommittedChanges() bool {
	out, err := Run("status", "--porcelain", "--untracked-files=no")
	return err == nil && out != ""
}

// BranchExists checks if a local branch exists.
func BranchExists(branch string) bool {
	_, err := Run("show-ref", "--verify", "--quiet", fmt.Sprintf("refs/heads/%s", branch))
	return err == nil
}

// GetTrackingBranch returns the upstream tracking branch for HEAD, or empty string.
func GetTrackingBranch() string {
	out, err := Run("rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{u}")
	if err != nil {
		return ""
	}
	return out
}

// HasUnpushedCommits returns true if there are local commits not pushed to upstream.
func HasUnpushedCommits() bool {
	tracking := GetTrackingBranch()
	if tracking == "" {
		return true // no tracking = everything is unpushed
	}
	out, err := Run("log", "@{u}..HEAD", "--oneline")
	if err != nil {
		return false
	}
	return out != ""
}

// RevParse runs git rev-parse with the given args and returns the output.
func RevParse(args ...string) (string, error) {
	fullArgs := append([]string{"rev-parse"}, args...)
	return Run(fullArgs...)
}

// SwitchBranch switches to the named branch.
func SwitchBranch(branch string) error {
	_, stderr, err := RunSilent("switch", branch)
	if err != nil {
		if stderr != "" {
			return fmt.Errorf("failed to switch to branch '%s': %s", branch, stderr)
		}
		return fmt.Errorf("failed to switch to branch '%s'", branch)
	}
	return nil
}

// DeleteBranch force-deletes a local branch.
func DeleteBranch(branch string) error {
	_, stderr, err := RunSilent("branch", "-D", "--", branch)
	if err != nil {
		if stderr != "" {
			return fmt.Errorf("failed to delete branch '%s': %s", branch, stderr)
		}
		return fmt.Errorf("failed to delete branch '%s'", branch)
	}
	return nil
}

// ValidateBranchName checks if a branch name is valid.
func ValidateBranchName(branch string) error {
	if strings.HasPrefix(branch, "-") {
		return fmt.Errorf("invalid branch name (cannot start with '-'): %s", branch)
	}
	_, err := Run("check-ref-format", "--allow-onelevel", fmt.Sprintf("refs/heads/%s", branch))
	if err != nil {
		return fmt.Errorf("invalid branch name: %s", branch)
	}
	return nil
}

// RevListCount counts commits between two refs.
func RevListCount(rangeSpec string) (int, error) {
	out, err := Run("rev-list", "--count", rangeSpec)
	if err != nil {
		return 0, err
	}
	var n int
	fmt.Sscanf(out, "%d", &n)
	return n, nil
}

// Fetch fetches from a remote, optionally a specific branch.
func Fetch(remote string, refspecs ...string) error {
	args := append([]string{"fetch", remote}, refspecs...)
	_, stderr, err := RunSilent(args...)
	if err != nil {
		return fmt.Errorf("git fetch failed: %s", stderr)
	}
	return nil
}

// Pull pulls from the given remote and branch.
func Pull(remote, branch string) error {
	_, stderr, err := RunSilent("pull", remote, branch)
	if err != nil {
		return fmt.Errorf("git pull failed: %s", stderr)
	}
	return nil
}

// Push pushes the current branch. If setUpstream is true, uses -u flag.
func Push(remote string, setUpstream bool) error {
	args := []string{"push"}
	if setUpstream {
		args = append(args, "-u")
	}
	args = append(args, remote, "HEAD")
	_, stderr, err := RunSilent(args...)
	if err != nil {
		return fmt.Errorf("git push failed: %s", stderr)
	}
	return nil
}

// FindBranchByUpstream returns the name of a local branch whose upstream
// matches remote/ref, or empty string if none found.
func FindBranchByUpstream(remote, ref string) string {
	target := remote + "/" + ref
	out, err := ForEachRef("%(refname:short) %(upstream:short)", "-committerdate", "refs/heads/")
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(out, "\n") {
		parts := strings.SplitN(strings.TrimSpace(line), " ", 2)
		if len(parts) == 2 && parts[1] == target {
			return parts[0]
		}
	}
	return ""
}

// GetTopLevel returns the repository root directory.
func GetTopLevel() (string, error) {
	return Run("rev-parse", "--show-toplevel")
}

// ForEachRef lists refs matching the given pattern, sorted by the given key.
func ForEachRef(format, sort, pattern string) (string, error) {
	return Run("for-each-ref", fmt.Sprintf("--format=%s", format), fmt.Sprintf("--sort=%s", sort), pattern)
}
