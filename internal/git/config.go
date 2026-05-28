package git

import (
	"fmt"
	"strings"
)

const CreatedByValue = "utpr"

// ConfigGet reads a git config value. Returns empty string if the key doesn't exist.
func ConfigGet(key string) string {
	out, err := Run("config", "--get", key)
	if err != nil {
		return ""
	}
	return out
}

// ConfigSet writes a git config value.
func ConfigSet(key, value string) error {
	_, err := Run("config", key, value)
	return err
}

// MarkBranchCreatedByUtpr sets the created-by marker on a branch.
func MarkBranchCreatedByUtpr(branch string) error {
	return ConfigSet(fmt.Sprintf("branch.%s.created-by", branch), CreatedByValue)
}

// SetBranchPRURL stores a PR URL in git config for the branch.
func SetBranchPRURL(branch, url string) error {
	return ConfigSet(fmt.Sprintf("branch.%s.pr-url", branch), url)
}

// GetBranchPRURL retrieves the stored PR URL for a branch.
func GetBranchPRURL(branch string) string {
	return ConfigGet(fmt.Sprintf("branch.%s.pr-url", branch))
}

// MarkRemoteCreatedByUtpr sets the created-by marker on a remote.
func MarkRemoteCreatedByUtpr(remote string) error {
	return ConfigSet(fmt.Sprintf("remote.%s.created-by", remote), CreatedByValue)
}

// IsRemoteCreatedByUtpr checks if a remote was created by utpr.
func IsRemoteCreatedByUtpr(remote string) bool {
	return ConfigGet(fmt.Sprintf("remote.%s.created-by", remote)) == CreatedByValue
}

// IsRemoteCreatedByPRTool checks if a remote was created by utpr or usethis.
func IsRemoteCreatedByPRTool(remote string) bool {
	val := ConfigGet(fmt.Sprintf("remote.%s.created-by", remote))
	return val == CreatedByValue || strings.HasPrefix(val, "usethis::")
}

// SetBranchUpstream sets the upstream tracking branch.
func SetBranchUpstream(branch, upstream string) error {
	_, _, err := RunSilent("branch", "--set-upstream-to="+upstream, branch)
	return err
}

// GetBranchMergeRef returns the upstream branch name (without refs/heads/) for a branch,
// as stored in branch.<name>.merge. Returns empty string if not set.
func GetBranchMergeRef(branch string) string {
	ref := ConfigGet(fmt.Sprintf("branch.%s.merge", branch))
	return strings.TrimPrefix(ref, "refs/heads/")
}
