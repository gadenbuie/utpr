package cmd

import (
	"fmt"

	"github.com/gadenbuie/utpr/internal/gh"
	"github.com/gadenbuie/utpr/internal/git"
	"github.com/gadenbuie/utpr/internal/remote"
	"github.com/gadenbuie/utpr/internal/ui"
)

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
func challengeUnpushedCommits() error {
	if !git.HasUnpushedCommits() {
		return nil
	}
	ui.Warn("You have unpushed commits.")
	return ui.MustConfirm("You have unpushed commits. Continue?", false)
}

// challengeBranchBehindRemote warns if the local branch is behind its remote.
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
		ui.Info("You may want to run 'utpr pull' to update before switching away.")
		return ui.MustConfirm("Proceed anyway?", false)
	}
	return nil
}

// challengeLocalBranchDelete warns if the branch has unpushed work.
func challengeLocalBranchDelete(branch string) error {
	tracking, _ := git.Run("rev-parse", "--abbrev-ref", "--symbolic-full-name", branch+"@{u}")
	if tracking == "" {
		ui.Warnf("Local branch '%s' has no associated remote branch.", branch)
		ui.Infof("If we delete '%s', any work that exists only on this branch may be hard for you to recover.", branch)
		return ui.MustConfirm("Proceed anyway?", false)
	}

	unpushed, _ := git.Run("log", tracking+".."+branch, "--oneline")
	if unpushed != "" {
		ui.Warnf("Local branch '%s' has 1 or more commits that have not been pushed to '%s'.", branch, tracking)
		ui.Infof("If we delete '%s', this work may be hard for you to recover.", branch)
		return ui.MustConfirm("Proceed anyway?", false)
	}
	return nil
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
		return err
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

// ghPRViewState gets the PR state for the current branch via the GitHub API.
func ghPRViewState() (string, error) {
	cfg := remote.Require()
	sourceURL, err := git.Run("remote", "get-url", cfg.SourceRemote)
	if err != nil {
		return "", err
	}
	ownerRepo, err := remote.ParseRepoSpec(sourceURL)
	if err != nil {
		return "", err
	}
	branch, err := git.GetCurrentBranch()
	if err != nil {
		return "", err
	}
	pr, err := gh.GetPRForBranch(ownerRepo, branch)
	if err != nil {
		return "", err
	}
	if pr == nil {
		return "", fmt.Errorf("no open PR found for branch '%s'", branch)
	}
	return pr.State, nil
}
