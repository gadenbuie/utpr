package cmd

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/gadenbuie/utpr/internal/gh"
	"github.com/gadenbuie/utpr/internal/git"
	"github.com/gadenbuie/utpr/internal/remote"
	"github.com/gadenbuie/utpr/internal/ui"
	"github.com/spf13/cobra"
)

var finishCmd = &cobra.Command{
	Use:   "finish [pr-number]",
	Short: "Clean up after a merged PR",
	Long:  "Clean up a local branch after its PR has been merged. Automatically removes any associated worktree.",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runFinish,
}

func runFinish(cmd *cobra.Command, args []string) error {
	if git.IsInWorktree() {
		mainRoot, _ := git.GetMainRepoRoot()
		return ui.Dief("Navigate to the main repo first. Run: cd \"%s\"", mainRoot)
	}

	cfg, err := remote.Detect()
	if err != nil {
		return err
	}

	sourceURL, err := git.Run("remote", "get-url", cfg.SourceRemote)
	if err != nil {
		return err
	}
	sourceRepo, err := remote.ParseRepoSpec(sourceURL)
	if err != nil {
		return err
	}

	var prNumbers []int
	fromPicker := false

	if len(args) > 0 {
		n, err := strconv.Atoi(args[0])
		if err != nil {
			return ui.Dief("Invalid PR number: %s", args[0])
		}
		prNumbers = []int{n}
	} else {
		onDefault, _ := git.IsOnBranch(cfg.DefaultBranch)
		if !onDefault {
			// Infer from current branch — search all states (open, closed, merged)
			currentBranch, branchErr := git.GetCurrentBranch()
			if branchErr != nil {
				return ui.Die("Could not determine PR number for current branch.")
			}
			pr, prErr := gh.GetPRForBranch(sourceRepo, currentBranch, "all")
			if prErr != nil || pr == nil {
				return ui.Die("Could not determine PR number for current branch.")
			}
			prNumbers = []int{pr.Number}
		} else {
			fromPicker = true
			prNumbers, err = pickMergedPRs(cfg, sourceRepo)
			if err != nil {
				return err
			}
			if len(prNumbers) == 0 {
				return nil
			}
		}
	}

	// For non-picker path: confirm first
	if !fromPicker {
		pr, err := gh.GetPR(sourceRepo, prNumbers[0])
		if err != nil {
			return ui.Dief("Failed to fetch PR #%d details.", prNumbers[0])
		}
		if pr.State != "closed" {
			ui.Warnf("PR #%d is still open (state: %s).", prNumbers[0], pr.State)
			if err := ui.MustConfirm("Continue anyway?", false); err != nil {
				return nil
			}
		}
		if err := ui.MustConfirm(fmt.Sprintf("Finish PR #%d (branch: %s)?", prNumbers[0], pr.Head.Ref), true); err != nil {
			if err == ui.ErrCancelled {
				ui.Info("Cancelled.")
			}
			return nil
		}
	}

	// Switch to default branch and pull
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

	// Process each PR
	for _, prNumber := range prNumbers {
		if err := finishOnePR(cfg, sourceRepo, prNumber); err != nil {
			ui.Errorf("Error finishing PR #%d: %v", prNumber, err)
		}
	}
	return nil
}

func finishOnePR(cfg *remote.Config, sourceRepo string, prNumber int) error {
	pr, err := gh.GetPR(sourceRepo, prNumber)
	if err != nil {
		return ui.Dief("Failed to fetch PR #%d details.", prNumber)
	}

	// Find and delete local branch
	localBranch := findLocalBranchForPR(prNumber, pr.Head.Ref, pr.User.Login)

	if localBranch != "" {
		if err := removeWorktree(localBranch); err != nil {
			return err
		}
		if err := challengeLocalBranchDelete(localBranch); err != nil {
			return err
		}
		if err := git.DeleteBranch(localBranch); err != nil {
			return err
		}
		ui.Successf("Deleted local branch '%s'.", localBranch)
	} else {
		ui.Infof("No local branch found for PR #%d.", prNumber)
	}

	// Delete remote branch if merged and we own the repo
	pushURL, _ := git.Run("remote", "get-url", cfg.PushRemote)
	pushRepo, _ := remote.ParseRepoSpec(pushURL)
	if shouldDeleteRemoteBranch(pr.Merged, pr.Head.Repo.FullName, pushRepo) {
		err := gh.DeleteRemoteBranch(pr.Head.Repo.FullName, pr.Head.Ref)
		if err != nil {
			errStr := err.Error()
			if strings.Contains(errStr, "Reference does not exist") {
				ui.Infof("Remote branch '%s' already deleted.", pr.Head.Ref)
			} else {
				ui.Warnf("Could not delete remote branch '%s' (may require manual cleanup).", pr.Head.Ref)
			}
		} else {
			ui.Successf("Deleted remote branch '%s'.", pr.Head.Ref)
		}
	} else if pr.Merged {
		ui.Infof("Remote branch '%s' is on '%s' (not yours). Skipping deletion.", pr.Head.Ref, pr.Head.Repo.FullName)
	}

	if localBranch != "" {
		remote.CleanupUtprRemotes()
	}

	ui.Successf("Finished PR #%d.", prNumber)
	return nil
}

// shouldDeleteRemoteBranch returns true if the PR is merged and the
// head repo matches the push remote repo (we own the branch).
func shouldDeleteRemoteBranch(prMerged bool, prHeadRepoFullName, pushRepoFullName string) bool {
	return prMerged && prHeadRepoFullName == pushRepoFullName
}

func pickMergedPRs(cfg *remote.Config, sourceRepo string) ([]int, error) {
	mergedPRs, err := ui.SpinWithResult("Checking for merged PRs...", func() ([]gh.MergedPRInfo, error) {
		return gh.SearchMergedPRs(sourceRepo, 50)
	})
	if err != nil || len(mergedPRs) == 0 {
		// Fallback: check branches individually
		return pickMergedPRsFallback(cfg, sourceRepo)
	}

	currentUser, _ := gh.GetLogin()

	// Filter to only PRs that have a local branch
	var items []ui.PRPickerItem
	for _, pr := range mergedPRs {
		hasLocal := findLocalBranchForPR(pr.Number, pr.HeadRefName, pr.Author) != ""
		if hasLocal {
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

	opts := ui.FormatPRPickerOptions(items, ui.PickerDefault)
	selected, err := ui.ChooseMultiWithOptions("Select PR(s) to finish:", opts)
	if err != nil || len(selected) == 0 {
		ui.Info("Cancelled.")
		return nil, nil
	}

	if len(selected) == 1 {
		if err := ui.MustConfirm(fmt.Sprintf("Finish PR #%d?", selected[0]), true); err != nil {
			if err == ui.ErrCancelled {
				ui.Info("Cancelled.")
			}
			return nil, nil
		}
	} else {
		if err := ui.MustConfirm(fmt.Sprintf("Finish %d selected PRs?", len(selected)), true); err != nil {
			if err == ui.ErrCancelled {
				ui.Info("Cancelled.")
			}
			return nil, nil
		}
	}

	return selected, nil
}

func pickMergedPRsFallback(cfg *remote.Config, sourceRepo string) ([]int, error) {
	branchOutput, _ := git.ForEachRef("%(refname:short)", "-committerdate", "refs/heads/")
	branches := strings.Split(branchOutput, "\n")

	currentUser, _ := gh.GetLogin()
	var items []ui.PRPickerItem

	for _, branch := range branches {
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

	opts := ui.FormatPRPickerOptions(items, ui.PickerDefault)
	selected, err := ui.ChooseMultiWithOptions("Select PR(s) to finish:", opts)
	if err != nil || len(selected) == 0 {
		ui.Info("Cancelled.")
		return nil, nil
	}

	return selected, nil
}

func extractPRNumberFromURL(url string) int {
	re := regexp.MustCompile(`/(\d+)$`)
	m := re.FindStringSubmatch(url)
	if m == nil {
		return 0
	}
	n, _ := strconv.Atoi(m[1])
	return n
}
