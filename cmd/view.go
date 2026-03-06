package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/gadenbuie/utpr/internal/git"
	"github.com/gadenbuie/utpr/internal/remote"
	"github.com/gadenbuie/utpr/internal/ui"
	"github.com/spf13/cobra"
)

var viewCmd = &cobra.Command{
	Use:   "view [pr-or-issue-number]",
	Short: "View PR details and comments",
	Long:  "View a PR or issue. Shows full details with comments by default.",
	RunE:  runView,
}

var (
	flagViewWeb     bool
	flagViewSummary bool
	flagViewIssue   string
	flagViewState   string
)

func init() {
	viewCmd.Flags().BoolVarP(&flagViewWeb, "web", "w", false, "Open in the browser")
	viewCmd.Flags().BoolVar(&flagViewSummary, "summary", false, "Show brief summary (no comments)")
	viewCmd.Flags().StringVar(&flagViewIssue, "issue", "", "View an issue instead of a PR (optionally specify number)")
	viewCmd.Flags().StringVar(&flagViewState, "state", "open", "Filter picker by state: open, closed, merged, all")
}

func runView(cmd *cobra.Command, args []string) error {
	_, err := remote.Detect()
	if err != nil {
		return err
	}

	cfg := remote.Require()

	viewType := "pr"
	viewTypeExplicit := cmd.Flags().Changed("issue")
	if viewTypeExplicit {
		viewType = "issue"
	}

	var numberArg string
	if len(args) > 0 {
		numberArg = args[0]
	}
	// --issue=N case
	if flagViewIssue != "" {
		if _, err := strconv.Atoi(flagViewIssue); err == nil {
			numberArg = flagViewIssue
		}
	}

	// Auto-detect PR vs issue when given a number
	if numberArg != "" && !viewTypeExplicit {
		detected, err := detectPROrIssue(numberArg)
		if err != nil {
			return ui.Dief("Could not find issue or PR #%s.", numberArg)
		}
		viewType = detected
	}

	if viewType == "issue" {
		return viewIssue(numberArg, cfg)
	}
	return viewPR(numberArg, cfg)
}

func detectPROrIssue(number string) (string, error) {
	ghCmd := exec.Command("gh", "api",
		fmt.Sprintf("repos/{owner}/{repo}/issues/%s", number),
		"--jq", `if .pull_request then "pr" else "issue" end`)
	out, err := ghCmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func viewIssue(numberArg string, cfg *remote.Config) error {
	// Validate state
	switch flagViewState {
	case "open", "closed", "all":
	case "merged":
		return ui.Die("--state merged is not valid for issues (expected: open, closed, all)")
	default:
		return ui.Dief("Invalid --state value: '%s' (expected: open, closed, all)", flagViewState)
	}

	var issueNumber string
	if numberArg != "" {
		issueNumber = numberArg
	} else {
		n, err := pickIssueForView(cfg)
		if err != nil {
			return err
		}
		issueNumber = strconv.Itoa(n)
	}

	if flagViewWeb {
		webCmd := exec.Command("gh", "issue", "view", issueNumber, "--web")
		webCmd.Stderr = os.Stderr
		return webCmd.Run()
	}
	if flagViewSummary {
		return ghIssueViewSummary(issueNumber)
	}
	ghCmd := exec.Command("gh", "issue", "view", issueNumber, "--comments")
	ghCmd.Stdout = os.Stdout
	ghCmd.Stderr = os.Stderr
	return ghCmd.Run()
}

func viewPR(numberArg string, cfg *remote.Config) error {
	switch flagViewState {
	case "open", "closed", "merged", "all":
	default:
		return ui.Dief("Invalid --state value: '%s' (expected: open, closed, merged, all)", flagViewState)
	}

	var prNumber string
	if numberArg != "" {
		prNumber = numberArg
	} else {
		onDefault, _ := git.IsOnBranch(cfg.DefaultBranch)
		if !onDefault && flagViewState == "open" {
			// Try to get PR for current branch
			ghCmd := exec.Command("gh", "pr", "view", "--json", "number", "--jq", ".number")
			out, err := ghCmd.Output()
			if err == nil && strings.TrimSpace(string(out)) != "" {
				prNumber = strings.TrimSpace(string(out))
			}
		}
		if prNumber == "" {
			n, err := pickPRForView(cfg)
			if err != nil {
				return err
			}
			prNumber = strconv.Itoa(n)
		}
	}

	if flagViewWeb {
		webCmd := exec.Command("gh", "pr", "view", prNumber, "--web")
		webCmd.Stderr = os.Stderr
		return webCmd.Run()
	}
	if flagViewSummary {
		return ghPRViewSummary(prNumber)
	}
	ghCmd := exec.Command("gh", "pr", "view", prNumber, "--comments")
	ghCmd.Stdout = os.Stdout
	ghCmd.Stderr = os.Stderr
	return ghCmd.Run()
}

func pickIssueForView(cfg *remote.Config) (int, error) {
	ghCmd := exec.Command("gh", "issue", "list", "--state", flagViewState,
		"--json", "number,title,author,updatedAt,state",
		"--jq", `sort_by(.updatedAt) | reverse | .[] | "#\(.number)\t\(.title)\t\(.author.login)"`)

	out, err := ui.SpinWithResult(fmt.Sprintf("Getting %s issues...", flagViewState), func() (string, error) {
		output, err := ghCmd.Output()
		return string(output), err
	})
	if err != nil {
		return 0, ui.Die("Failed to list issues.")
	}
	if strings.TrimSpace(out) == "" {
		return 0, ui.Dief("No %s issues found.", flagViewState)
	}

	lines := strings.Split(strings.TrimSpace(out), "\n")
	var displayItems []string
	for _, line := range lines {
		parts := strings.SplitN(line, "\t", 3)
		if len(parts) >= 2 {
			displayItems = append(displayItems, fmt.Sprintf("%s  %s", parts[0], parts[1]))
		}
	}

	selected, err := ui.Choose("Select an issue to view:", displayItems)
	if err != nil || selected == "" {
		ui.Info("Cancelled.")
		return 0, fmt.Errorf("cancelled")
	}
	return parsePRNumber(selected)
}

func pickPRForView(cfg *remote.Config) (int, error) {
	ghCmd := exec.Command("gh", "pr", "list", "--state", flagViewState,
		"--json", "number,title,author,updatedAt,state",
		"--jq", `sort_by(.updatedAt) | reverse | .[] | "#\(.number)\t\(.title)\t\(.author.login)"`)

	out, err := ui.SpinWithResult(fmt.Sprintf("Getting %s PRs...", flagViewState), func() (string, error) {
		output, err := ghCmd.Output()
		return string(output), err
	})
	if err != nil {
		return 0, ui.Die("Failed to list PRs.")
	}
	if strings.TrimSpace(out) == "" {
		return 0, ui.Dief("No %s PRs found.", flagViewState)
	}

	lines := strings.Split(strings.TrimSpace(out), "\n")
	var displayItems []string
	for _, line := range lines {
		parts := strings.SplitN(line, "\t", 3)
		if len(parts) >= 2 {
			displayItems = append(displayItems, fmt.Sprintf("%s  %s", parts[0], parts[1]))
		}
	}

	selected, err := ui.Choose("Select a PR to view:", displayItems)
	if err != nil || selected == "" {
		ui.Info("Cancelled.")
		return 0, fmt.Errorf("cancelled")
	}
	return parsePRNumber(selected)
}

func ghIssueViewSummary(number string) error {
	ghCmd := exec.Command("gh", "issue", "view", number,
		"--json", "number,title,body,author,state,labels",
		"--jq", `"# #\(.number) \(.title)\n\n**Author:** \(.author.login) · **State:** \(.state)\(if (.labels | length) > 0 then (" · **Labels:** " + (.labels | map(.name) | join(", "))) else "" end)\n\n\(if (.body // "") != "" then .body else "*No description provided.*" end)"`)
	out, err := ghCmd.Output()
	if err != nil {
		return ui.Dief("Failed to fetch issue #%s.", number)
	}
	fmt.Print(string(out))
	return nil
}

func ghPRViewSummary(number string) error {
	ghCmd := exec.Command("gh", "pr", "view", number,
		"--json", "number,title,body,author,state,labels",
		"--jq", `"# #\(.number) \(.title)\n\n**Author:** \(.author.login) · **State:** \(.state)\(if (.labels | length) > 0 then (" · **Labels:** " + (.labels | map(.name) | join(", "))) else "" end)\n\n\(if (.body // "") != "" then .body else "*No description provided.*" end)"`)
	out, err := ghCmd.Output()
	if err != nil {
		return ui.Dief("Failed to fetch PR #%s.", number)
	}
	fmt.Print(string(out))
	return nil
}
