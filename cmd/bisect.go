package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/gadenbuie/utpr/internal/gh"
	"github.com/gadenbuie/utpr/internal/git"
	"github.com/gadenbuie/utpr/internal/remote"
	"github.com/gadenbuie/utpr/internal/ui"
	"github.com/spf13/cobra"
)

var bisectCmd = &cobra.Command{
	Use:   "bisect [bad-ref]",
	Short: "Find the commit that introduced a bug",
	Long:  "Find the commit that introduced a bug using git bisect.",
	RunE:  runBisect,
}

var (
	flagBisectRun      string
	flagBisectNoVerify bool
	flagBisectVerify   bool
)

func init() {
	bisectCmd.Flags().StringVar(&flagBisectRun, "run", "", "Run command at each bisect step")
	bisectCmd.Flags().BoolVar(&flagBisectNoVerify, "no-verify", false, "Skip pre-validation of test command")
	bisectCmd.Flags().BoolVar(&flagBisectVerify, "verify", false, "Force pre-validation of test command")
}

func runBisect(cmd *cobra.Command, args []string) error {
	badRef := "HEAD"

	// Handle -- separator for run command
	if cmd.ArgsLenAtDash() >= 0 {
		dashArgs := args[cmd.ArgsLenAtDash():]
		if len(dashArgs) > 0 {
			flagBisectRun = strings.Join(dashArgs, " ")
		}
		args = args[:cmd.ArgsLenAtDash()]
	}

	if len(args) > 0 {
		badRef = args[0]
	}

	// Check for in-progress bisect
	if bisectIsInProgress() {
		resumeResult, err := bisectOfferResume()
		if err != nil {
			return err
		}
		switch resumeResult {
		case "resume":
			return bisectContinue(flagBisectRun)
		case "abort":
			// Fall through to start fresh
		case "cancel":
			return nil
		}
	}

	ui.Info("Starting bisect. First, select a known-good commit.")
	goodRef, err := bisectPickGoodCommit()
	if err != nil {
		ui.Info("Cancelled.")
		return nil
	}

	// Validate refs
	if _, err := git.RevParse("--verify", badRef); err != nil {
		return ui.Dief("Bad ref '%s' does not exist.", badRef)
	}
	if _, err := git.RevParse("--verify", goodRef); err != nil {
		return ui.Dief("Good ref '%s' does not exist.", goodRef)
	}

	// Pre-validate test script
	if flagBisectRun != "" {
		badSHA, _ := git.RevParse("--verify", badRef)
		headSHA, _ := git.RevParse("HEAD")
		if badSHA == headSHA {
			if flagBisectVerify {
				if err := bisectVerifyScript(flagBisectRun); err != nil {
					return err
				}
			} else if !flagBisectNoVerify {
				confirmed, err := ui.Confirm("Run a quick check that the test fails on current commit first?", true)
				if err != nil {
					return err
				}
				if confirmed {
					if err := bisectVerifyScript(flagBisectRun); err != nil {
						return err
					}
				}
			}
		} else {
			ui.Warnf("Skipping pre-validation: bad ref '%s' is not the current checkout.", badRef)
		}
	}

	// Start bisect
	out, _, err := git.RunSilent("bisect", "start", badRef, goodRef)
	if err != nil {
		return ui.Die("git bisect start failed.")
	}
	if progress := extractBisectProgress(out); progress != "" {
		ui.Info(progress)
	}

	return bisectContinue(flagBisectRun)
}

func bisectContinue(runCmd string) error {
	var badSHA string
	var err error

	if runCmd != "" {
		badSHA, err = bisectRunAutomated(runCmd)
	} else {
		badSHA, err = bisectInteractiveLoop()
	}
	if err != nil {
		return err
	}

	prURL := bisectShowResult(badSHA)
	return bisectCleanupPrompt(badSHA, prURL)
}

func bisectIsInProgress() bool {
	gitDir, err := git.RevParse("--git-dir")
	if err != nil {
		return false
	}
	_, err = os.Stat(gitDir + "/BISECT_LOG")
	return err == nil
}

func bisectOfferResume() (string, error) {
	out, _ := git.Run("bisect", "log")
	nSteps := 0
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "# good") || strings.HasPrefix(line, "# bad") || strings.HasPrefix(line, "# skip") {
			nSteps++
		}
	}

	ui.Warnf("A bisect is already in progress (%d step(s) recorded).", nSteps)
	choice, err := ui.Choose("What would you like to do?", []string{
		"Resume the current bisect",
		"Abort and start fresh",
		"Cancel",
	})
	if err != nil {
		return "cancel", nil
	}

	switch {
	case strings.HasPrefix(choice, "Resume"):
		ui.Info("Resuming bisect...")
		return "resume", nil
	case strings.HasPrefix(choice, "Abort"):
		confirmed, err := ui.Confirm("This will reset the bisect state. Continue?", true)
		if err != nil {
			return "", err
		}
		if confirmed {
			_, _ = git.Run("bisect", "reset")
			ui.Success("Bisect aborted.")
			return "abort", nil
		}
		return "cancel", nil
	default:
		return "cancel", nil
	}
}

// bisectRefDate is a sentinel value that triggers date input mode.
const bisectRefDate = "__DATE_INPUT__"

func bisectPickGoodCommit() (string, error) {
	var opts []huh.Option[string]

	// Tags
	tagOut, _ := git.Run("tag", "--sort=-creatordate", "--format=%(refname:short)\t%(creatordate:relative)")
	if tagOut != "" {
		for i, line := range strings.Split(tagOut, "\n") {
			if i >= 20 || line == "" {
				break
			}
			parts := strings.SplitN(line, "\t", 2)
			date := ""
			if len(parts) > 1 {
				date = parts[1]
			}
			display := fmt.Sprintf("%s %s  %s",
				ui.StyleMuted.Render("[tag]"),
				ui.StyleBranch.Render(parts[0]),
				ui.StyleMuted.Render("("+date+")"))
			opts = append(opts, huh.NewOption(display, parts[0]))
		}
	}

	// Recent commits
	commitOut, _ := git.Run("log", "--format=%h\t%s\t%cr", "-20")
	if commitOut != "" {
		for _, line := range strings.Split(commitOut, "\n") {
			if line == "" {
				continue
			}
			parts := strings.SplitN(line, "\t", 3)
			if len(parts) < 3 {
				continue
			}
			display := fmt.Sprintf("%s %s %s  %s",
				ui.StyleMuted.Render("[commit]"),
				ui.StyleHash.Render(parts[0]),
				parts[1],
				ui.StyleMuted.Render("("+parts[2]+")"))
			opts = append(opts, huh.NewOption(display, parts[0]))
		}
	}

	dateDisplay := fmt.Sprintf("%s Enter a date or time...", ui.StyleMuted.Render("[date]"))
	opts = append(opts, huh.NewOption(dateDisplay, bisectRefDate))

	selected, err := ui.ChooseWithOptions("Select the last known good commit:", opts)
	if err != nil || selected == "" {
		return "", fmt.Errorf("cancelled")
	}

	if selected == bisectRefDate {
		for {
			dateInput, err := ui.Input("Enter a date (e.g. '2 weeks ago', '2024-01-15'):", "", "2 weeks ago")
			if err != nil || dateInput == "" {
				return "", fmt.Errorf("cancelled")
			}
			ref, err := git.Run("rev-list", "-1", "--before="+dateInput, "HEAD")
			if err == nil && ref != "" {
				return ref, nil
			}
			ui.Errorf("No commit found before '%s'. Try again.", dateInput)
		}
	}

	return selected, nil
}

func bisectVerifyScript(cmd string) error {
	ui.Info("Verifying test command fails on current HEAD...")
	shCmd := exec.Command("sh", "-c", cmd)
	shCmd.Stdout = nil
	shCmd.Stderr = nil
	err := shCmd.Run()
	if err == nil {
		return ui.Die("Test command exited 0 (success) on current HEAD. Expected failure — check your command.")
	}
	if exitErr, ok := err.(*exec.ExitError); ok {
		rc := exitErr.ExitCode()
		if rc == 125 {
			return ui.Die("Test command exited 125 (skip) on current HEAD. Exit code 125 tells git bisect to skip the commit, not mark it as bad. Check your command.")
		}
		if rc >= 128 {
			return ui.Dief("Test command exited %d on current HEAD. Exit codes >= 128 cause git bisect run to abort immediately. Check your command.", rc)
		}
		ui.Successf("Test command correctly fails on HEAD (exit code %d).", rc)
	}
	return nil
}

func bisectInteractiveLoop() (string, error) {
	for {
		fmt.Fprintln(os.Stderr)
		bisectShowCurrentCommit()

		mark, err := ui.Choose("Is this commit good or bad?", []string{"bad", "good", "skip"})
		if err != nil || mark == "" {
			ui.Warn("No selection made.")
			confirmed, err := ui.Confirm("Abort the bisect?", false)
			if confirmed || err != nil {
				_, _ = git.Run("bisect", "reset")
				ui.Info("Bisect aborted.")
				return "", fmt.Errorf("bisect aborted")
			}
			continue
		}

		out, _, bisectErr := git.RunSilent("bisect", mark)
		if bisectErr != nil && !strings.Contains(out, "is the first bad commit") {
			return "", ui.Dief("git bisect %s failed.", mark)
		}

		if strings.Contains(out, "is the first bad commit") {
			fields := strings.Fields(out)
			if len(fields) == 0 {
				return "", ui.Die("Could not parse bad commit SHA from bisect output.")
			}
			return fields[0], nil
		}

		if progress := extractBisectProgress(out); progress != "" {
			ui.Info(progress)
		}
	}
}

func bisectRunAutomated(runCmd string) (string, error) {
	ui.Info("Running automated bisect...")
	fmt.Fprintln(os.Stderr)

	shCmd := exec.Command("git", "bisect", "run", "sh", "-c", runCmd)
	combined, bisectErr := shCmd.CombinedOutput()

	// Show output to user on stderr
	fmt.Fprint(os.Stderr, string(combined))

	for _, line := range strings.Split(string(combined), "\n") {
		if strings.Contains(line, "is the first bad commit") {
			fields := strings.Fields(line)
			if len(fields) > 0 {
				return fields[0], nil
			}
		}
	}

	if bisectErr != nil {
		return "", ui.Dief("git bisect run failed. Check your test command.")
	}
	return "", ui.Die("Could not determine the bad commit from bisect output.")
}

func bisectShowCurrentCommit() {
	out, _ := git.Run("log", "-1", "--format=%h\t%s\t%an\t%cr", "HEAD")
	if out != "" {
		parts := strings.SplitN(out, "\t", 4)
		if len(parts) >= 4 {
			fmt.Fprintf(os.Stderr, "%s %s\n%s · %s\n",
				ui.StyleHash.Render(parts[0]),
				ui.StyleSubject.Render(parts[1]),
				ui.StyleCyan.Render(parts[2]),
				parts[3])
		} else {
			fmt.Fprintln(os.Stderr, out)
		}
	}
}

func bisectShowResult(sha string) string {
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, ui.StyleBold.Render(ui.StyleStateClosed.Render("Found the first bad commit:")))
	fmt.Fprintln(os.Stderr)

	out, _ := git.Run("log", "-1", "--format=%H\t%s\t%an <%ae>\t%ci", sha)
	if out != "" {
		parts := strings.SplitN(out, "\t", 4)
		if len(parts) >= 4 {
			fmt.Fprintf(os.Stderr, "  %s\n  %s\n  %s · %s\n",
				ui.StyleHash.Render(parts[0]),
				ui.StyleSubject.Render(parts[1]),
				ui.StyleCyan.Render(parts[2]),
				parts[3])
		} else {
			fmt.Fprintln(os.Stderr, "  "+out)
		}
	}

	return bisectFindPR(sha)
}

func bisectFindPR(sha string) string {
	cfg := remote.Require()
	sourceURL, err := git.Run("remote", "get-url", cfg.SourceRemote)
	if err != nil {
		ui.Info("No associated PR found for this commit.")
		return ""
	}
	ownerRepo, err := remote.ParseRepoSpec(sourceURL)
	if err != nil {
		ui.Info("No associated PR found for this commit.")
		return ""
	}

	prs, err := gh.SearchPRsByCommit(ownerRepo, sha)
	if err != nil || len(prs) == 0 {
		ui.Info("No associated PR found for this commit.")
		return ""
	}

	pr := prs[0]
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, ui.StyleBold.Render("Associated PR:"))
	fmt.Fprintf(os.Stderr, "  %s %s\n",
		ui.StyleNumber.Render(fmt.Sprintf("#%d", pr.Number)),
		pr.Title)
	fmt.Fprintf(os.Stderr, "  %s · %s\n",
		ui.StyleAuthor.Render(pr.User.Login),
		pr.HTMLURL)
	return pr.HTMLURL
}

func bisectCleanupPrompt(badSHA, prURL string) error {
	options := []string{"Reset to original branch (git bisect reset)", "Stay at the bad commit"}
	if prURL != "" {
		options = append(options, "Open PR in browser")
	}

	fmt.Fprintln(os.Stderr)
	choice, err := ui.Choose("What would you like to do?", options)
	if err != nil {
		return nil
	}

	switch {
	case strings.HasPrefix(choice, "Reset"):
		_, _ = git.Run("bisect", "reset")
		ui.Success("Bisect reset. Back on your original branch.")
	case strings.HasPrefix(choice, "Stay"):
		shortSHA := badSHA
		if len(shortSHA) > 10 {
			shortSHA = shortSHA[:10]
		}
		ui.Infof("Staying at commit %s. Run 'git bisect reset' when you're done.", shortSHA)
	case strings.HasPrefix(choice, "Open"):
		_ = openURL(prURL)
		ui.Success("Opened PR in browser.")
		confirmed, err := ui.Confirm("Reset the bisect now?", true)
		if err != nil {
			return err
		}
		if confirmed {
			_, _ = git.Run("bisect", "reset")
			ui.Success("Bisect reset.")
		} else {
			ui.Info("Run 'git bisect reset' when you're done.")
		}
	default:
		ui.Info("Run 'git bisect reset' when you're done.")
	}
	return nil
}

func extractBisectProgress(output string) string {
	for _, line := range strings.Split(output, "\n") {
		if strings.HasPrefix(line, "Bisecting:") {
			return line
		}
	}
	return ""
}
