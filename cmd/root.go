package cmd

import (
	"fmt"
	"os"

	"github.com/gadenbuie/utpr/internal/gh"
	"github.com/gadenbuie/utpr/internal/git"
	"github.com/gadenbuie/utpr/internal/ui"
	"github.com/spf13/cobra"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

// SetVersionInfo sets the version information from build-time ldflags.
func SetVersionInfo(v, c, d string) {
	version = v
	commit = c
	date = d
	rootCmd.Version = version
	rootCmd.SetVersionTemplate(fmt.Sprintf("utpr %s (%s, %s)\n", version, commit, date))
}

var rootCmd *cobra.Command

func newRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "utpr",
		Short: "GitHub PR workflow CLI",
		Long:  "A CLI for GitHub PR workflows, inspired by the usethis R package.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	return cmd
}

func init() {
	rootCmd = newRootCmd()

	rootCmd.SetFlagErrorFunc(func(cmd *cobra.Command, err error) error {
		ui.Error(err.Error())
		cmd.SetOut(os.Stderr)
		_ = cmd.Help()
		return err
	})

	rootCmd.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		if cmd.Name() == "help" || cmd.CalledAs() == "help" {
			return nil
		}
		if cmd == rootCmd {
			return nil
		}
		return checkPrerequisites()
	}

	rootCmd.AddCommand(pullCmd)
	rootCmd.AddCommand(pauseCmd)
	rootCmd.AddCommand(resumeCmd)
	rootCmd.AddCommand(pushCmd)
	rootCmd.AddCommand(forgetCmd)
	rootCmd.AddCommand(mergeMainCmd)
	rootCmd.AddCommand(initCmd)
	rootCmd.AddCommand(fetchCmd)
	rootCmd.AddCommand(finishCmd)
	rootCmd.AddCommand(viewCmd)
	rootCmd.AddCommand(bisectCmd)

}

// Execute runs the root command.
func Execute() error {
	return rootCmd.Execute()
}

func checkPrerequisites() error {
	if !git.IsInstalled() {
		return ui.Die("Missing dependency: git. Install from https://git-scm.com")
	}
	if !git.IsInsideWorkTree() {
		return ui.Die("Not inside a git repository (or worktree). Run utpr from a repository checkout.")
	}
	if !gh.IsAuthenticated() {
		return ui.Die("GitHub authentication not found. Run 'gh auth login' or set GITHUB_TOKEN.")
	}
	return nil
}
