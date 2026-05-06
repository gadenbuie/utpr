package cmd

import (
	"fmt"

	"github.com/gadenbuie/utpr/internal/gh"
	"github.com/gadenbuie/utpr/internal/git"
	"github.com/gadenbuie/utpr/internal/remote"
	"github.com/gadenbuie/utpr/internal/ui"
)

// findLocalBranchForPR returns the local branch name corresponding to a PR,
// checking the plain headRef, the current pr/{number}-{author}-{branch}
// scheme, and the legacy pr-{number}/{branch} scheme. Returns "" if no local
// branch is found. The defaultBranch is excluded from the bare headRef check
// to avoid matching the repo's own default branch when a fork's PR uses the
// same branch name.
func findLocalBranchForPR(prNumber int, headRef, author, defaultBranch string) string {
	return findLocalBranchForPRWith(prNumber, headRef, author, defaultBranch, git.BranchExists)
}

func findLocalBranchForPRWith(prNumber int, headRef, author, defaultBranch string, branchExists func(string) bool) string {
	if headRef != defaultBranch && branchExists(headRef) {
		return headRef
	}
	newName := fmt.Sprintf("pr/%d-%s-%s", prNumber, author, headRef)
	if branchExists(newName) {
		return newName
	}
	oldName := fmt.Sprintf("pr-%d/%s", prNumber, headRef)
	if branchExists(oldName) {
		return oldName
	}
	// usethis::pr_fetch() naming scheme: {author}-{branch}
	if author != "" {
		usethisName := author + "-" + headRef
		if branchExists(usethisName) {
			return usethisName
		}
	}
	return ""
}

// challengeUncommittedChanges warns the user about uncommitted changes and
// asks for confirmation to proceed.
func challengeUncommittedChanges() error {
	if !git.HasUncommittedChanges() {
		return nil
	}
	ui.Warn("There are uncommitted changes, which may cause problems or be lost when we push, pull, switch or compare branches.")
	return ui.MustConfirm("Do you want to proceed anyway?", false)
}

// challengeUnpushedCommits warns about unpushed commits and asks for confirmation.
// Skips silently if the branch has never been pushed (no tracking branch).
func challengeUnpushedCommits() error {
	if git.GetTrackingBranch() == "" {
		return nil
	}
	if !git.HasUnpushedCommits() {
		return nil
	}
	ui.Warn("You have unpushed commits.")
	return ui.MustConfirm("You have unpushed commits. Continue?", false)
}

// challengeBranchBehindRemote warns if the local branch is behind its remote
// and offers to pull before switching away.
func challengeBranchBehindRemote() error {
	tracking := git.GetTrackingBranch()
	if tracking == "" {
		return nil
	}
	behind, err := git.RevListCount("HEAD..@{u}")
	if err != nil {
		return nil // not fatal
	}
	if behind > 0 {
		ui.Warnf("Local branch is %d commit(s) behind '%s'.", behind, tracking)
		pullNow, err := ui.Confirm("Pull now before switching away?", true)
		if err != nil {
			return err
		}
		if pullNow {
			pullErr := ui.Spin(fmt.Sprintf("Pulling from %s...", tracking), func() error {
				_, runErr := git.Run("pull")
				return runErr
			})
			if pullErr != nil {
				return ui.Die("Pull failed. Fix conflicts or run 'utpr pull' manually.")
			}
			ui.Success("Pulled latest changes.")
		}
	}
	return nil
}

// challengeLocalBranchDelete warns if the branch has unpushed work.
// Returns (true, nil) if the user was prompted and confirmed, (false, nil) if
// no prompt was needed, or (false, err) if the user declined or an error occurred.
func challengeLocalBranchDelete(branch string) (bool, error) {
	tracking, _ := git.Run("rev-parse", "--abbrev-ref", "--symbolic-full-name", branch+"@{u}")
	if tracking == "" {
		ui.Warnf("Local branch '%s' has no associated remote branch.", branch)
		ui.Infof("If we delete '%s', any work that exists only on this branch may be hard for you to recover.", branch)
		return true, ui.MustConfirm("Proceed anyway?", false)
	}

	unpushed, _ := git.Run("log", tracking+".."+branch, "--oneline")
	if unpushed != "" {
		ui.Warnf("Local branch '%s' has 1 or more commits that have not been pushed to '%s'.", branch, tracking)
		ui.Infof("If we delete '%s', this work may be hard for you to recover.", branch)
		return true, ui.MustConfirm("Proceed anyway?", false)
	}
	return false, nil
}

// pullDefaultBranch fetches and pulls the default branch.
func pullDefaultBranch(cfg *remote.Config) error {
	before, _ := git.RevParse("HEAD")

	err := ui.Spin(
		fmt.Sprintf("Pulling %s...", cfg.DefaultBranch),
		func() error {
			return git.Pull(cfg.SourceRemote, cfg.DefaultBranch)
		},
	)
	if err != nil {
		return ui.Die(err.Error())
	}

	after, _ := git.RevParse("HEAD")
	if before != after {
		n, _ := git.RevListCount(before + ".." + after)
		if n > 0 {
			ui.Successf("Pulled %d new commit(s) on %s.", n, cfg.DefaultBranch)
		}
	}
	return nil
}

// ghGetPRForCurrentBranch finds the PR (any state) for the current branch.
// Returns nil if no PR is found or on error.
func ghGetPRForCurrentBranch() *gh.PRInfo {
	cfg := remote.Require()
	sourceURL, err := git.Run("remote", "get-url", cfg.SourceRemote)
	if err != nil {
		return nil
	}
	ownerRepo, err := remote.ParseRepoSpec(sourceURL)
	if err != nil {
		return nil
	}
	branch, err := git.GetCurrentBranch()
	if err != nil {
		return nil
	}
	pr, err := gh.GetPRForBranch(ownerRepo, branch, "all")
	if err != nil {
		return nil
	}
	return pr
}
