package cmd

import (
	"errors"
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
	Use:   "finish [pr-number-or-branch]",
	Short: "Clean up after a merged PR",
	Long:  "Clean up a local branch after its PR has been merged. Automatically removes any associated worktree.\n\nAccepts a PR number or branch name.",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runFinish,
}

var flagFinishYes bool

func init() {
	finishCmd.Flags().BoolVar(&flagFinishYes, "yes", false, "Assume yes for confirmation prompts")
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

	cfg, err := remote.Detect()
	if err != nil {
		if errors.Is(err, ui.ErrCancelled) {
			return err
		}
		ui.Warnf("Continuing without remote operations: %v", err)
		ok, err := offerLocalCleanupFallback("utpr finish needs a git remote to check PR status.")
		if err != nil || !ok {
			return err
		}
		return finishLocalCleanup(cmd, args, "")
	}

	sourceURL, err := git.Run("remote", "get-url", cfg.SourceRemote)
	if err != nil {
		return ui.Die(err.Error())
	}
	sourceRepo, err := remote.ParseRepoSpec(sourceURL)
	if err != nil {
		return ui.Die(err.Error())
	}

	if !gh.IsReachable() {
		ok, err := offerLocalCleanupFallback("utpr finish needs GitHub access to check PR status.")
		if err != nil || !ok {
			return err
		}
		return finishLocalCleanup(cmd, args, sourceRepo)
	}

	var prNumbers []int
	fromPicker := false

	if len(args) > 0 {
		n, err := resolveFinishArg(args[0], sourceRepo)
		if err != nil {
			return err
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
			if err := requireInteractiveTTY("pass a PR number or branch name as an argument"); err != nil {
				return err
			}
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

	// For non-picker path: confirm first and determine the PR's base branch
	var baseBranch string
	if !fromPicker {
		pr, err := gh.GetPR(sourceRepo, prNumbers[0])
		if err != nil {
			return ui.Dief("Failed to fetch PR #%d details.", prNumbers[0])
		}
		if pr.State != "closed" {
			ui.Warnf("PR #%d is still open (state: %s).", prNumbers[0], pr.State)
			if !assumeYes() {
				if err := ui.MustConfirm("Continue anyway?", false); err != nil {
					return nil
				}
			}
		}
		if !assumeYes() {
			if err := ui.MustConfirm(fmt.Sprintf("Finish PR #%d (branch: %s)?", prNumbers[0], pr.Head.Ref), true); err != nil {
				if err == ui.ErrCancelled {
					ui.Info("Cancelled.")
				}
				return nil
			}
		}
		if pr.Base.Ref != "" {
			baseBranch = pr.Base.Ref
		}
	} else {
		// Picker path: fetch base branches for selected stacked PRs so their
		// remote tracking refs are up to date when the user later switches.
		fetchedBases := map[string]bool{}
		for _, prNum := range prNumbers {
			pr, err := gh.GetPR(sourceRepo, prNum)
			if err != nil || pr == nil || pr.Base.Ref == "" || pr.Base.Ref == cfg.DefaultBranch {
				continue
			}
			if fetchedBases[pr.Base.Ref] {
				continue
			}
			fetchedBases[pr.Base.Ref] = true
			refspec := fmt.Sprintf("+refs/heads/%s:refs/remotes/%s/%s", pr.Base.Ref, cfg.SourceRemote, pr.Base.Ref)
			if err := git.Fetch(cfg.SourceRemote, refspec); err != nil {
				ui.Warnf("Could not fetch base branch '%s' for PR #%d.", pr.Base.Ref, prNum)
			}
		}
	}
	if baseBranch == "" {
		baseBranch = cfg.DefaultBranch
	}
	if baseBranch != cfg.DefaultBranch {
		ui.Infof("PR targets '%s' (not '%s').", baseBranch, cfg.DefaultBranch)
	}

	// Switch to base branch and pull (handles worktree case)
	if onBase, _ := git.IsOnBranch(baseBranch); !onBase {
		if err := challengeUncommittedChanges(); err != nil {
			return err
		}
	}
	if err := prepareBaseBranch(baseBranch, cfg.DefaultBranch, cfg.SourceRemote); err != nil {
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

	// Find and delete local branch (exclude the PR's base ref to avoid
	// matching a stacked PR's base branch that shares the head branch name)
	localBranch := findLocalBranchForPR(prNumber, pr.Head.Ref, pr.User.Login, cfg.DefaultBranch, pr.Base.Ref)

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

// finishLocalCleanup runs the local-only cleanup fallback, mapping
// PR-number arguments to their local branches first. sourceRepo may be
// empty when it can't be determined (no git remote).
func finishLocalCleanup(cmd *cobra.Command, args []string, sourceRepo string) error {
	forgetArgs, err := localCleanupArgs(args, sourceRepo)
	if err != nil {
		return err
	}
	return runForgetIn(cmd, forgetArgs, true)
}

// localCleanupArgs maps finish arguments to forget-style arguments for
// local-only cleanup. A PR number is resolved to its local branch via the
// stored PR URL (no GitHub access needed); branch names pass through.
func localCleanupArgs(args []string, sourceRepo string) ([]string, error) {
	if len(args) == 0 {
		return nil, nil
	}
	if _, err := strconv.Atoi(args[0]); err != nil {
		return args, nil
	}
	n, _ := strconv.Atoi(args[0])
	branch, err := findBranchWithPRNumber(n, sourceRepo)
	if err != nil {
		return nil, err
	}
	return []string{branch}, nil
}

// findBranchWithPRNumber returns the local branch whose stored PR URL is
// for the given PR number, searching all local branches. When sourceRepo
// is known, stored URLs for other repositories are ignored. Returns an
// error for zero or multiple matches.
func findBranchWithPRNumber(prNumber int, sourceRepo string) (string, error) {
	refs, err := git.ForEachRef("%(refname:short)", "-committerdate", "refs/heads/")
	if err != nil {
		return "", ui.Dief("Could not look up local branches to resolve PR #%d.", prNumber)
	}
	var matches []string
	for _, branch := range strings.Split(refs, "\n") {
		branch = strings.TrimSpace(branch)
		if branch == "" {
			continue
		}
		url := git.GetBranchPRURL(branch)
		if prNumberFromStoredURL(url) != prNumber {
			continue
		}
		if sourceRepo != "" {
			if prRepo := prRepoFromStoredURL(url); prRepo != "" && prRepo != sourceRepo {
				continue
			}
		}
		matches = append(matches, branch)
	}
	switch len(matches) {
	case 1:
		return matches[0], nil
	case 0:
		return "", ui.Dief("Could not find a local branch for PR #%d. Run 'utpr forget <branch>' to clean up locally.", prNumber)
	default:
		return "", ui.Dief("Multiple local branches reference PR #%d (%s). Run 'utpr forget <branch>' to choose one.", prNumber, strings.Join(matches, ", "))
	}
}

// prRepoFromStoredURL extracts "owner/repo" from a stored GitHub PR URL
// (the two path segments before "/pull/<number>"). Returns "" when it
// can't be determined.
func prRepoFromStoredURL(storedURL string) string {
	if storedURL == "" {
		return ""
	}
	re := regexp.MustCompile(`([A-Za-z0-9_.-]+)/([A-Za-z0-9_.-]+)/pull/\d+$`)
	m := re.FindStringSubmatch(storedURL)
	if m == nil {
		return ""
	}
	return m[1] + "/" + m[2]
}

// offerLocalCleanupFallback warns that PR checks are unavailable and asks
// whether to fall back to local cleanup (remove the worktree and delete the
// branch, like 'utpr forget'). Returns false when the user declines.
func offerLocalCleanupFallback(reason string) (bool, error) {
	ui.Warn(reason)
	if assumeYes() {
		return true, nil
	}
	confirmed, err := ui.Confirm("Fall back to local cleanup (like 'utpr forget')?", true)
	if err != nil {
		return false, err
	}
	return confirmed, nil
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

// resolveFinishArg resolves a finish argument to a PR number, accepting
// either a PR number or a branch name.
func resolveFinishArg(arg, sourceRepo string) (int, error) {
	if n, err := strconv.Atoi(arg); err == nil {
		return n, nil
	}
	if n := prNumberFromStoredURL(git.GetBranchPRURL(arg)); n != 0 {
		return n, nil
	}
	pr, err := gh.GetPRForBranch(sourceRepo, arg, "all")
	if err != nil || pr == nil {
		return 0, ui.Dief("Could not determine PR number for branch '%s'.", arg)
	}
	return pr.Number, nil
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
