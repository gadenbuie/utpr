package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/gadenbuie/utpr/internal/gh"
	"github.com/gadenbuie/utpr/internal/git"
	"github.com/gadenbuie/utpr/internal/remote"
	"github.com/gadenbuie/utpr/internal/ui"
	"github.com/spf13/cobra"
)

var cleanCmd = &cobra.Command{
	Use:   "clean",
	Short: "Finish merged PRs, prune remotes, and remove stale local branches",
	Long:  "Run finish for all merged PRs, fetch and prune all remotes, delete stale local branches, and remove unused utpr-created remotes.",
	RunE:  runClean,
}

func runClean(cmd *cobra.Command, args []string) error {
	if git.IsInWorktree() {
		mainRoot, _ := git.GetMainRepoRoot()
		ui.Warn("Navigate to the main repo first:")
		fmt.Fprintf(os.Stderr, "  cd \"%s\"\n", mainRoot)
		fmt.Fprintf(os.Stderr, "  utpr clean\n")
		return fmt.Errorf("cannot run from a worktree")
	}

	if !gh.IsReachable() {
		ui.Warn("utpr clean requires a network connection to check PR status.")
		return ui.Die("Connect to GitHub and try again.")
	}

	cfg, err := remote.Detect()
	if err != nil {
		return ui.Die(err.Error())
	}

	sourceURL, err := git.Run("remote", "get-url", cfg.SourceRemote)
	if err != nil {
		return ui.Die(err.Error())
	}
	sourceRepo, err := remote.ParseRepoSpec(sourceURL)
	if err != nil {
		return ui.Die(err.Error())
	}

	onDefault, _ := git.IsOnBranch(cfg.DefaultBranch)
	if !onDefault {
		if err := challengeUncommittedChanges(); err != nil {
			return err
		}
		if err := git.SwitchBranch(cfg.DefaultBranch); err != nil {
			return err
		}
	}
	if err := pullDefaultBranch(cfg); err != nil {
		return err
	}

	// Collect decisions interactively before any destructive operations.
	prNumbers, err := pickMergedPRsForClean(cfg, sourceRepo)
	if err != nil {
		return err
	}

	deadRemotes, fetchErr := ui.SpinWithResult("Fetching and pruning remotes...", git.FetchAllPrune)
	if fetchErr != nil {
		ui.Warnf("Fetch error: %v", fetchErr)
	}
	for _, r := range deadRemotes {
		ui.Infof("Remote '%s' is unreachable (fork deleted).", r)
	}

	stale, err := collectStaleBranches(cfg, sourceRepo, deadRemotes)
	if err != nil {
		return err
	}

	var toDelete []string
	if len(stale) == 0 {
		ui.Info("No stale local branches to clean up.")
	} else {
		selected, err := pickStaleBranchesToDelete(stale)
		if err != nil && err != ui.ErrCancelled {
			return err
		}
		toDelete = selected
	}

	// Final confirmation before any destructive operation.
	nPRs, nBranches, nRemotes := len(prNumbers), len(toDelete), len(deadRemotes)
	if nPRs+nBranches+nRemotes == 0 {
		ui.Info("Nothing to clean up.")
		return nil
	}
	if nPRs > 0 {
		ui.Infof("Will finish %d PR(s).", nPRs)
	}
	if nBranches > 0 {
		ui.Infof("Will delete %d local branch(es): %s.", nBranches, strings.Join(toDelete, ", "))
	}
	if nRemotes > 0 {
		ui.Infof("Will remove %d unreachable remote(s): %s.", nRemotes, strings.Join(deadRemotes, ", "))
	}
	if err := ui.MustConfirm("Proceed?", true); err != nil {
		if err == ui.ErrCancelled {
			ui.Info("Cancelled.")
		}
		return nil
	}

	// Execute destructive operations.
	for _, prNumber := range prNumbers {
		if err := finishOnePR(cfg, sourceRepo, prNumber); err != nil {
			ui.Errorf("Error finishing PR #%d: %v", prNumber, err)
		}
	}

	for _, branch := range toDelete {
		if err := removeWorktree(branch); err != nil {
			ui.Errorf("Error removing worktree for '%s': %v", branch, err)
			continue
		}
		if err := git.DeleteBranch(branch); err != nil {
			ui.Errorf("Error deleting branch '%s': %v", branch, err)
		} else {
			ui.Successf("Deleted local branch '%s'.", branch)
		}
	}

	for _, r := range deadRemotes {
		if err := git.RemoveRemote(r); err != nil {
			ui.Warnf("Could not remove dead remote '%s': %v", r, err)
		} else {
			ui.Successf("Removed unreachable remote '%s'.", r)
		}
	}
	remote.CleanupUtprRemotes()

	return nil
}

func pickMergedPRsForClean(cfg *remote.Config, sourceRepo string) ([]int, error) {
	return runMergedPRPicker(cfg, sourceRepo, mergedPRPickerOpts{
		preselectAll:   true,
		requireConfirm: false,
		prompt:         "Select merged PR(s) to finish:",
		cancelMsg:      "Skipping merged PR cleanup.",
	})
}

type staleBranch struct {
	name   string
	reason string
}

func collectStaleBranches(cfg *remote.Config, sourceRepo string, deadRemotes []string) ([]staleBranch, error) {
	seen := make(map[string]bool)
	currentBranch, _ := git.GetCurrentBranch()

	exclude := func(branch string) bool {
		return branch == cfg.DefaultBranch || branch == currentBranch || branch == ""
	}

	var result []staleBranch

	add := func(name, reason string) {
		if !exclude(name) && !seen[name] {
			seen[name] = true
			result = append(result, staleBranch{name: name, reason: reason})
		}
	}

	for _, r := range deadRemotes {
		for _, b := range git.ListBranchesTrackingRemote(r) {
			add(b, "[fork deleted]")
		}
	}

	gone, goneErr := git.ListBranchesWithGoneUpstream()
	if goneErr != nil {
		ui.Warnf("Could not list branches with gone upstream: %v", goneErr)
	}
	for _, b := range gone {
		add(b, "[gone]")
	}

	merged, mergedErr := git.ListMergedBranches(cfg.DefaultBranch)
	if mergedErr != nil {
		ui.Warnf("Could not list merged branches: %v", mergedErr)
	}
	for _, b := range merged {
		add(b, "[merged]")
	}

	utprBranches := listUtprCreatedBranches()
	if len(utprBranches) > 0 {
		statuses, _ := ui.SpinWithResult("Checking utpr branch statuses...", func() ([]staleBranch, error) {
			var results []staleBranch
			for _, branch := range utprBranches {
				if seen[branch] {
					continue
				}
				prURL := git.GetBranchPRURL(branch)
				prNum := prNumberFromStoredURL(prURL)
				if prNum == 0 {
					continue
				}
				pr, err := gh.GetPR(sourceRepo, prNum)
				if err != nil || pr == nil || pr.State != "closed" {
					continue
				}
				if pr.Merged {
					results = append(results, staleBranch{name: branch, reason: "[PR merged]"})
				} else {
					results = append(results, staleBranch{name: branch, reason: "[PR closed]"})
				}
			}
			return results, nil
		})
		for _, s := range statuses {
			add(s.name, s.reason)
		}
	}

	return result, nil
}

func listUtprCreatedBranches() []string {
	out, _ := git.Run("config", "--get-regexp", `branch\..*\.created-by`)
	if out == "" {
		return nil
	}
	var branches []string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) < 2 || parts[1] != git.CreatedByValue {
			continue
		}
		branch := strings.TrimPrefix(parts[0], "branch.")
		branch = strings.TrimSuffix(branch, ".created-by")
		if git.BranchExists(branch) {
			branches = append(branches, branch)
		}
	}
	return branches
}

func pickStaleBranchesToDelete(stale []staleBranch) ([]string, error) {
	opts := make([]huh.Option[string], 0, len(stale))
	for _, b := range stale {
		display := ui.StyleBranchName(b.name) + "  " + ui.StyleMuted.Render(b.reason)
		opts = append(opts, huh.NewOption(display, b.name).Selected(true))
	}

	selected, err := ui.ChooseMultiWithOptions("Select stale branches to delete (all pre-selected):", opts)
	if err != nil || len(selected) == 0 {
		return nil, err
	}
	return selected, nil
}
