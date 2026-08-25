package cmd

import (
	"fmt"
	"strings"

	"github.com/gadenbuie/utpr/internal/gh"
	"github.com/gadenbuie/utpr/internal/git"
	"github.com/gadenbuie/utpr/internal/remote"
	"github.com/gadenbuie/utpr/internal/ui"
)

func assumeYes() bool {
	return flagInitYes || flagFetchYes || flagResumeYes
}

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
	if assumeYes() {
		return nil
	}
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
		} else {
			return ui.MustConfirm("Proceed anyway?", false)
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

// pullBranch fetches and pulls the given branch from the remote.
func pullBranch(remote, branch string) error {
	before, _ := git.RevParse("HEAD")

	err := ui.Spin(
		fmt.Sprintf("Pulling %s...", branch),
		func() error {
			return git.Pull(remote, branch)
		},
	)
	if err != nil {
		return ui.Die(err.Error())
	}

	after, _ := git.RevParse("HEAD")
	if before != after {
		n, _ := git.RevListCount(before + ".." + after)
		if n > 0 {
			ui.Successf("Pulled %d new commit(s) on %s.", n, branch)
		}
	}
	return nil
}

// pullDefaultBranch fetches and pulls the default branch.
func pullDefaultBranch(cfg *remote.Config) error {
	return pullBranch(cfg.SourceRemote, cfg.DefaultBranch)
}

// switchToBranch switches to the given branch. If the branch doesn't exist
// locally, it is fetched from the remote and created as a tracking branch.
func switchToBranch(branch, remote string) error {
	if git.BranchExists(branch) {
		return git.SwitchBranch(branch)
	}
	// Branch doesn't exist locally — fetch and create a tracking branch.
	ui.Infof("Branch '%s' not found locally, fetching from %s...", branch, remote)
	refspec := fmt.Sprintf("+refs/heads/%s:refs/remotes/%s/%s", branch, remote, branch)
	if err := git.Fetch(remote, refspec); err != nil {
		return ui.Dief("Could not fetch branch '%s' from '%s'.", branch, remote)
	}
	if _, err := git.Run("switch", "-c", branch, fmt.Sprintf("%s/%s", remote, branch)); err != nil {
		return ui.Dief("Could not switch to branch '%s'.", branch)
	}
	return nil
}

// pullBranchInDir pulls a branch from the remote within the given directory.
func pullBranchInDir(remote, branch, dir string) error {
	err := ui.Spin(
		fmt.Sprintf("Pulling %s...", branch),
		func() error {
			_, e := git.Run("-C", dir, "pull", remote, branch)
			return e
		},
	)
	if err != nil {
		return ui.Die(err.Error())
	}
	return nil
}

// prepareBaseBranch switches to the base branch and pulls it. If the base
// branch is checked out in a linked worktree, pulls within the worktree and
// switches to the default branch instead (git refuses to switch to a branch
// that is already checked out in another worktree).
//
// The caller should call challengeUncommittedChanges before this function
// when switching away from the current branch.
func prepareBaseBranch(baseBranch, defaultBranch, remote string) error {
	// Case 1: base branch is checked out in a worktree — can't switch to it.
	if wtPath := git.GetBranchWorktreePath(baseBranch); wtPath != "" {
		ui.Infof("Branch '%s' is checked out in a worktree (%s). Pulling there.", baseBranch, wtPath)
		if err := pullBranchInDir(remote, baseBranch, wtPath); err != nil {
			return err
		}
		// We need to get off the current branch so it can be deleted. Try
		// the default branch; if that's also in a worktree (or is the same
		// as the base), use detached HEAD instead.
		if baseBranch != defaultBranch && git.GetBranchWorktreePath(defaultBranch) == "" {
			onDefault, _ := git.IsOnBranch(defaultBranch)
			if !onDefault {
				if err := git.SwitchBranch(defaultBranch); err != nil {
					return err
				}
			}
			return pullBranch(remote, defaultBranch)
		}
		ui.Info("Switching to detached HEAD (target branch is in a worktree).")
		if _, err := git.Run("switch", "--detach"); err != nil {
			return err
		}
		return nil
	}

	// Case 2: normal — switch to base branch and pull.
	onBase, _ := git.IsOnBranch(baseBranch)
	if !onBase {
		if err := switchToBranch(baseBranch, remote); err != nil {
			return err
		}
	}
	return pullBranch(remote, baseBranch)
}

type mergedPRPickerOpts struct {
	preselectAll   bool
	requireConfirm bool
	prompt         string
	cancelMsg      string
}

// runMergedPRPicker is the shared implementation for both `finish` and `clean`.
// It searches for recently merged PRs, filters to those with a local branch,
// shows a multi-select picker, and optionally pre-selects all items and/or
// requires a final confirmation.  Falls back to per-branch GitHub queries when
// the search API returns nothing.
func runMergedPRPicker(cfg *remote.Config, sourceRepo string, opts mergedPRPickerOpts) ([]int, error) {
	mergedPRs, err := ui.SpinWithResult("Checking for merged PRs...", func() ([]gh.MergedPRInfo, error) {
		return gh.SearchMergedPRs(sourceRepo, 50)
	})
	if err != nil || len(mergedPRs) == 0 {
		return runMergedPRPickerFallback(cfg, sourceRepo, opts)
	}

	currentUser, _ := gh.GetLogin()
	var items []ui.PRPickerItem
	for _, pr := range mergedPRs {
		if findLocalBranchForPR(pr.Number, pr.HeadRefName, pr.Author, cfg.DefaultBranch) != "" {
			items = append(items, ui.PRPickerItem{
				Number:      pr.Number,
				Title:       pr.Title,
				Author:      pr.Author,
				IsHighlight: currentUser != "" && pr.Author == currentUser,
			})
		}
	}

	if len(items) == 0 {
		ui.Info("No merged PRs with a local branch to clean up.")
		return nil, nil
	}

	pickerOpts := ui.FormatPRPickerOptions(items, ui.PickerDefault)
	if opts.preselectAll {
		for i := range pickerOpts {
			pickerOpts[i] = pickerOpts[i].Selected(true)
		}
	}

	selected, err := ui.ChooseMultiWithOptions(opts.prompt, pickerOpts)
	if err != nil || len(selected) == 0 {
		ui.Info(opts.cancelMsg)
		return nil, nil
	}

	if opts.requireConfirm {
		var msg string
		if len(selected) == 1 {
			msg = fmt.Sprintf("Finish PR #%d?", selected[0])
		} else {
			msg = fmt.Sprintf("Finish %d selected PRs?", len(selected))
		}
		if err := ui.MustConfirm(msg, true); err != nil {
			if err == ui.ErrCancelled {
				ui.Info(opts.cancelMsg)
			}
			return nil, nil
		}
	}

	return selected, nil
}

// runMergedPRPickerFallback queries GitHub per branch when the search API
// returns no results.  It does not run a confirmation step regardless of opts.
func runMergedPRPickerFallback(cfg *remote.Config, sourceRepo string, opts mergedPRPickerOpts) ([]int, error) {
	branchOutput, _ := git.ForEachRef("%(refname:short)", "-committerdate", "refs/heads/")
	currentUser, _ := gh.GetLogin()
	var items []ui.PRPickerItem

	for _, branch := range strings.Split(branchOutput, "\n") {
		branch = strings.TrimSpace(branch)
		if branch == "" || branch == cfg.DefaultBranch {
			continue
		}
		pr, err := gh.GetMergedPRForBranch(sourceRepo, branch)
		if err != nil || pr == nil {
			continue
		}
		items = append(items, ui.PRPickerItem{
			Number:      pr.Number,
			Title:       pr.Title,
			Author:      pr.User.Login,
			IsHighlight: currentUser != "" && pr.User.Login == currentUser,
		})
	}

	if len(items) == 0 {
		ui.Info("No merged PRs with a local branch to clean up.")
		return nil, nil
	}

	pickerOpts := ui.FormatPRPickerOptions(items, ui.PickerDefault)
	if opts.preselectAll {
		for i := range pickerOpts {
			pickerOpts[i] = pickerOpts[i].Selected(true)
		}
	}

	selected, err := ui.ChooseMultiWithOptions(opts.prompt, pickerOpts)
	if err != nil || len(selected) == 0 {
		ui.Info(opts.cancelMsg)
		return nil, nil
	}

	return selected, nil
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
