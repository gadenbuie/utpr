package cmd

import (
	_ "embed"
	"fmt"
	"strings"

	"github.com/gadenbuie/utpr/internal/ui"
	"github.com/spf13/cobra"
)

//go:embed quickstart-human.md
var quickstartHumanDoc string

//go:embed quickstart-agent.md
var quickstartAgentDoc string

var quickstartCmd = &cobra.Command{
	Use:   "quickstart",
	Short: "Guided introduction to utpr for humans and AI agents",
	Long: `Print a guided introduction to utpr.

By default the guide is written for humans: a lifecycle-oriented tour of the
command set, rendered with markdown styling in a terminal. When stdout is not
a terminal — or with the global --agent flag — the guide for AI coding agents
is printed instead. --pretty forces the human guide.`,
	RunE: runQuickstart,
}

// runQuickstart prints the guide matching the active output mode: the agent
// guide whenever plain mode is active (piped, or --agent), and the human guide
// otherwise. quickstartGuide decides; the variant selection deliberately
// mirrors ui.AgentMode so piped output serves agents automatically.
func runQuickstart(cmd *cobra.Command, args []string) error {
	return printQuickstartGuide(quickstartGuide())
}

// quickstartGuide returns true when the agent guide should be printed.
// PersistentPreRunE has already applied --agent/--pretty and TTY detection
// to ui.PlainMode() by the time RunE runs.
func quickstartGuide() bool {
	return ui.PlainMode()
}

// printQuickstartGuide writes the guide to stdout: glamour-rendered for
// humans in a terminal, raw markdown for agent/plain consumption — the same
// split `utpr view` uses.
func printQuickstartGuide(agentGuide bool) error {
	doc := quickstartHumanDoc
	if agentGuide {
		doc = quickstartAgentDoc
	}

	out := doc
	if !agentGuide {
		rendered, err := ui.RenderMarkdown(doc)
		if err != nil {
			return ui.Dief("Failed to render the quickstart guide: %v", err)
		}
		out = rendered
	}

	if !strings.HasSuffix(out, "\n") {
		out += "\n"
	}
	fmt.Print(out)
	return nil
}
