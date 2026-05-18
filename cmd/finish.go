package cmd

import (
	"fmt"
	"os"
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
		branch, _ := git.GetCurrentBranch()
		ui.Warn("Navigate to the main repo first:")
		fmt.Fprintf(os.Stderr, "  cd \"%s\"\n", mainRoot)
		fmt.Fprintf(os.Stderr, "  utpr finish %s\n", branch)
		return fmt.Errorf("cannot run from a worktree")
	}

	if !gh.IsReachable() {
		ui.Warn("utpr finish requires a network connection to check PR status.")
		return ui.Die("To delete a local branch without checking GitHub, run: utpr forget")
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
			// Fast path: PR URL stored by `utpr fetch` — works even when the local
			// branch name differs from the remote branch name (e.g. fork PRs that
			// are checked out as pr/<number>-<author>-<branch>).
			if n := prNumberFromStoredURL(git.GetBranchPRURL(currentBranch)); n != 0 {
				prNumbers = []int{n}
			}
			// Slow path: ask GitHub using the remote tracking branch name, which
			// matches what GitHub knows — handles same-repo branches where the
			// local name was changed. Falls back to the local name if no upstream
			// is configured. (Fork PRs are covered by the fast path above.)
			if len(prNumbers) == 0 {
				pr, prErr := gh.GetPRForBranch(sourceRepo, remoteBranchName(git.GetTrackingBranch(), currentBranch), "all")
				if prErr != nil || pr == nil {
					return ui.Die("Could not determine PR number for current branch.")
				}
				prNumbers = []int{pr.Number}
			}
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
	localBranch := findLocalBranchForPR(prNumber, pr.Head.Ref, pr.User.Login, cfg.DefaultBranch)

	if localBranch != "" {
		if err := removeWorktree(localBranch); err != nil {
			return err
		}
		if _, err := challengeLocalBranchDelete(localBranch); err != nil {
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
	return runMergedPRPicker(cfg, sourceRepo, mergedPRPickerOpts{
		preselectAll:   false,
		requireConfirm: true,
		prompt:         "Select PR(s) to finish:",
		cancelMsg:      "Cancelled.",
	})
}

// prNumberFromStoredURL extracts the PR number from a stored GitHub PR URL.
// Returns 0 if the URL is empty or doesn't contain a numeric PR number.
func prNumberFromStoredURL(storedURL string) int {
	if storedURL == "" {
		return 0
	}
	return extractPRNumberFromURL(storedURL)
}

// remoteBranchName returns the branch name portion of a git tracking ref
// (e.g. "origin/feature" → "feature", "contributor/feat/thing" → "feat/thing").
// Falls back to localBranch when trackingRef is empty or contains no slash.
func remoteBranchName(trackingRef, localBranch string) string {
	if trackingRef != "" {
		if _, ref, ok := strings.Cut(trackingRef, "/"); ok {
			return ref
		}
	}
	return localBranch
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
