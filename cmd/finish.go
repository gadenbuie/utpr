package cmd

import (
	"fmt"
	"os/exec"
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
			// Infer from current branch
			prURL, _ := ghGetPRURL()
			if prURL == "" {
				return ui.Die("Could not determine PR number for current branch.")
			}
			n := extractPRNumberFromURL(prURL)
			if n == 0 {
				// Try gh pr view
				ghCmd := exec.Command("gh", "pr", "view", "--json", "number", "--jq", ".number")
				out, err := ghCmd.Output()
				if err != nil {
					return ui.Die("Could not determine PR number for current branch.")
				}
				n, err = strconv.Atoi(strings.TrimSpace(string(out)))
				if err != nil {
					return ui.Die("Could not determine PR number for current branch.")
				}
			}
			prNumbers = []int{n}
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
	localBranch := ""
	if git.BranchExists(pr.Head.Ref) {
		localBranch = pr.Head.Ref
	} else if git.BranchExists(fmt.Sprintf("pr-%d/%s", prNumber, pr.Head.Ref)) {
		localBranch = fmt.Sprintf("pr-%d/%s", prNumber, pr.Head.Ref)
	}

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
	if pr.Merged {
		pushURL, _ := git.Run("remote", "get-url", cfg.PushRemote)
		pushRepo, _ := remote.ParseRepoSpec(pushURL)
		if pr.Head.Repo.FullName == pushRepo {
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
		} else {
			ui.Infof("Remote branch '%s' is on '%s' (not yours). Skipping deletion.", pr.Head.Ref, pr.Head.Repo.FullName)
		}
	}

	if localBranch != "" {
		remote.CleanupUtprRemotes()
	}

	ui.Successf("Finished PR #%d.", prNumber)
	return nil
}

func pickMergedPRs(cfg *remote.Config, sourceRepo string) ([]int, error) {
	// Find merged PRs that have local branches
	ghCmd := exec.Command("gh", "api", "graphql",
		"-f", fmt.Sprintf("query=query { search(query: \"repo:%s is:pr is:merged\", type: ISSUE, first: 50) { nodes { ... on PullRequest { number title author { login } headRefName } } } }", sourceRepo),
		"--jq", `.data.search.nodes[] | "#\(.number)\t\(.title)\t\(.author.login)\t\(.headRefName)"`)

	out, err := ui.SpinWithResult("Checking for merged PRs...", func() (string, error) {
		output, err := ghCmd.Output()
		return string(output), err
	})
	if err != nil || strings.TrimSpace(out) == "" {
		// Fallback: check branches individually
		return pickMergedPRsFallback(cfg)
	}

	// Filter to only PRs that have a local branch
	var displayItems []string
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		parts := strings.SplitN(line, "\t", 4)
		if len(parts) < 4 {
			continue
		}
		headRef := parts[3]
		hasLocal := git.BranchExists(headRef)
		if !hasLocal {
			// Check pr-N/branch pattern
			re := regexp.MustCompile(`^#(\d+)`)
			m := re.FindStringSubmatch(parts[0])
			if m != nil {
				hasLocal = git.BranchExists(fmt.Sprintf("pr-%s/%s", m[1], headRef))
			}
		}
		if hasLocal {
			displayItems = append(displayItems, fmt.Sprintf("%s  %s", parts[0], parts[1]))
		}
	}

	if len(displayItems) == 0 {
		ui.Info("No merged PRs with a local branch to clean up.")
		return nil, nil
	}

	// For simplicity, use single-select (the bash version uses multi-select)
	selected, err := ui.Choose("Select a PR to finish:", displayItems)
	if err != nil || selected == "" {
		ui.Info("Cancelled.")
		return nil, nil
	}

	n, err := parsePRNumber(selected)
	if err != nil {
		return nil, err
	}

	if err := ui.MustConfirm(fmt.Sprintf("Finish PR #%d?", n), true); err != nil {
		if err == ui.ErrCancelled {
			ui.Info("Cancelled.")
		}
		return nil, nil
	}

	return []int{n}, nil
}

func pickMergedPRsFallback(cfg *remote.Config) ([]int, error) {
	branchOutput, _ := git.ForEachRef("%(refname:short)", "-committerdate", "refs/heads/")
	branches := strings.Split(branchOutput, "\n")

	var displayItems []string

	for _, branch := range branches {
		branch = strings.TrimSpace(branch)
		if branch == "" || branch == cfg.DefaultBranch {
			continue
		}
		// Check if there's a merged PR for this branch
		ghCmd := exec.Command("gh", "pr", "list", "--head", branch, "--state", "merged",
			"--json", "number,title", "--jq", `first | select(. != null) | "#\(.number)\t\(.title)"`)
		out, err := ghCmd.Output()
		if err != nil || strings.TrimSpace(string(out)) == "" {
			continue
		}
		line := strings.TrimSpace(string(out))
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) >= 2 {
			displayItems = append(displayItems, fmt.Sprintf("%s  %s", parts[0], parts[1]))
		}
	}

	if len(displayItems) == 0 {
		ui.Info("No merged PRs with a local branch to clean up.")
		return nil, nil
	}

	selected, err := ui.Choose("Select a PR to finish:", displayItems)
	if err != nil || selected == "" {
		ui.Info("Cancelled.")
		return nil, nil
	}

	n, err := parsePRNumber(selected)
	if err != nil {
		return nil, err
	}
	return []int{n}, nil
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
