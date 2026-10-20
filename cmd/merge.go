package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"

	"github.com/gadenbuie/utpr/internal/gh"
	"github.com/gadenbuie/utpr/internal/git"
	"github.com/gadenbuie/utpr/internal/remote"
	"github.com/gadenbuie/utpr/internal/ui"
	"github.com/spf13/cobra"
)

var prMergeCmd = &cobra.Command{
	Use:   "merge [pr-number-or-branch]",
	Short: "Merge a PR and clean up",
	Long:  "Merge a pull request on GitHub, then switch to the default branch, pull, and clean up the local branch.\n\nAccepts a PR number or branch name. When called from the default branch with no argument, offers a picker of open PRs. When called from a PR branch, merges that branch's PR.\n\nDefaults to squash merge if no strategy flag is given.",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runPRMerge,
}

var (
	prMergeFlagMerge  bool
	prMergeFlagSquash bool
	prMergeFlagRebase bool
	prMergeFlagYes    bool
)

func init() {
	prMergeCmd.Flags().BoolVarP(&prMergeFlagMerge, "merge", "m", false, "Create a merge commit")
	prMergeCmd.Flags().BoolVarP(&prMergeFlagSquash, "squash", "s", false, "Squash commits into one commit and merge")
	prMergeCmd.Flags().BoolVarP(&prMergeFlagRebase, "rebase", "r", false, "Rebase commits onto the base branch and merge")
	prMergeCmd.Flags().BoolVar(&prMergeFlagYes, "yes", false, "Skip confirmation prompts")
	prMergeCmd.MarkFlagsMutuallyExclusive("merge", "squash", "rebase")
}

func runPRMerge(cmd *cobra.Command, args []string) error {
	if git.IsInWorktree() {
		mainRoot, _ := git.GetMainRepoRoot()
		ui.Warn("Navigate to the main repo first:")
		fmt.Fprintf(os.Stderr, "  cd \"%s\"\n", mainRoot)
		return fmt.Errorf("cannot run from a worktree")
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

	var prNumber int
	prRepo := sourceRepo

	if len(args) > 0 {
		prNumber, prRepo, err = resolveMergeArg(args[0], sourceRepo, cfg)
		if err != nil {
			return err
		}
	} else if onDefault {
		n, err := pickOpenPRForMerge(sourceRepo)
		if err != nil {
			return err
		}
		if n == 0 {
			return nil
		}
		prNumber = n
	} else {
		if err := challengeUncommittedChanges(); err != nil {
			return err
		}
		if err := challengeUnpushedCommits(); err != nil {
			return err
		}

		currentBranch, branchErr := git.GetCurrentBranch()
		if branchErr != nil {
			return ui.Die("Could not determine current branch.")
		}

		if n := prNumberFromStoredURL(git.GetBranchPRURL(currentBranch)); n != 0 {
			prNumber = n
			if repo := prRepoFromURL(git.GetBranchPRURL(currentBranch)); repo != "" {
				prRepo = repo
			}
		} else {
			tracking := git.GetTrackingBranch()
			found, repo := findPRForBranch(cfg, sourceRepo,
				remoteBranchName(tracking, currentBranch), "open",
				prHeadOwners(cfg, sourceRepo, tracking)...)
			if found == nil {
				return ui.Dief("No open PR found for branch '%s'.", currentBranch)
			}
			prNumber = found.Number
			prRepo = repo
		}
	}

	pr, err := gh.GetPR(prRepo, prNumber)
	if err != nil {
		return ui.Dief("Failed to fetch PR #%d details.", prNumber)
	}
	if pr.State != "open" {
		return ui.Dief("PR #%d is not open (state: %s). Use 'utpr finish' to clean up.", pr.Number, pr.State)
	}

	baseBranch := cfg.DefaultBranch
	if pr.Base.Ref != "" {
		baseBranch = pr.Base.Ref
	}

	// If the PR targets a non-default branch, we'll need to switch after
	// merge. Challenge uncommitted changes now, before the merge is
	// irreversible.
	if baseBranch != cfg.DefaultBranch {
		if err := challengeUncommittedChanges(); err != nil {
			return err
		}
	}

	strategyFlag := "--squash"
	strategyLabel := "squash commit"
	switch {
	case prMergeFlagMerge:
		strategyFlag = "--merge"
		strategyLabel = "merge commit"
	case prMergeFlagRebase:
		strategyFlag = "--rebase"
		strategyLabel = "rebase"
	}

	if err := confirmPRMerge(pr.Number, pr.Title, strategyLabel); err != nil {
		if err == ui.ErrCancelled {
			ui.Info("Cancelled.")
			return nil
		}
		return err
	}

	ghCmd := exec.Command("gh", "pr", "merge", "-R", prRepo, fmt.Sprintf("%d", pr.Number), strategyFlag)
	ghCmd.Stdin = os.Stdin
	ghCmd.Stdout = os.Stdout
	ghCmd.Stderr = os.Stderr
	if err := ghCmd.Run(); err != nil {
		return ui.Die("Failed to merge PR. See output above for details.")
	}

	if baseBranch != cfg.DefaultBranch {
		ui.Infof("PR targets '%s' (not '%s').", baseBranch, cfg.DefaultBranch)
	}

	if err := prepareBaseBranch(baseBranch, cfg.DefaultBranch, cfg.SourceRemote); err != nil {
		return err
	}

	return finishOnePR(cfg, prRepo, pr.Number)
}

func confirmPRMerge(prNumber int, title, strategyLabel string) error {
	if assumeYes() {
		return nil
	}
	if err := requireInteractiveTTY("pass --yes to confirm the merge"); err != nil {
		return err
	}
	return ui.MustConfirm(fmt.Sprintf("Merge PR #%d '%s' as %s?", prNumber, title, strategyLabel), true)
}

// resolveMergeArg interprets the argument as a PR number or branch name and
// returns the corresponding PR number.
func resolveMergeArg(arg, sourceRepo string, cfg *remote.Config) (int, string, error) {
	if n, err := strconv.Atoi(arg); err == nil {
		return n, sourceRepo, nil
	}
	pr, repo := findPRForBranch(cfg, sourceRepo, arg, "open", prHeadOwners(cfg, sourceRepo, "")...)
	if pr == nil {
		return 0, sourceRepo, ui.Dief("No open PR found for branch '%s'.", arg)
	}
	return pr.Number, repo, nil
}

// pickOpenPRForMerge shows a picker of open PRs and returns the selected number.
// Returns 0 if the user cancels.
func pickOpenPRForMerge(sourceRepo string) (int, error) {
	if err := requireInteractiveTTY("pass a PR number or branch name as an argument"); err != nil {
		return 0, err
	}

	prs, err := ui.SpinWithResult("Getting open PRs...", func() ([]gh.PRInfo, error) {
		return gh.ListPRs(sourceRepo, "open")
	})
	if err != nil {
		return 0, ui.Die("Failed to list open PRs.")
	}
	if len(prs) == 0 {
		return 0, ui.Die("No open PRs found.")
	}

	currentUser, _ := gh.GetLogin()
	var items []ui.PRPickerItem
	for _, pr := range prs {
		items = append(items, ui.PRPickerItem{
			Number:      pr.Number,
			Title:       pr.Title,
			Author:      pr.User.Login,
			IsHighlight: currentUser != "" && pr.User.Login == currentUser,
		})
	}

	opts := ui.FormatPRPickerOptions(items, ui.PickerDefault)
	selected, err := ui.ChooseWithOptions("Select a PR to merge:", opts)
	if err != nil {
		ui.Info("Cancelled.")
		return 0, nil
	}
	return selected, nil
}
