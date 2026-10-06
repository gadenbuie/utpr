package cmd

import (
	"testing"

	"github.com/gadenbuie/utpr/internal/gh"
	"github.com/gadenbuie/utpr/internal/ui"
)

func TestRenderViewMarkdownAgent(t *testing.T) {
	previous := flagViewAgent
	t.Cleanup(func() {
		flagViewAgent = previous
	})

	flagViewAgent = true
	got, err := renderViewMarkdown("# Title\n\n**body**")
	if err != nil {
		t.Fatalf("renderViewMarkdown() returned an error: %v", err)
	}

	want := "# Title\n\n**body**\n"
	if got != want {
		t.Errorf("renderViewMarkdown() = %q, want %q", got, want)
	}
}

func TestRenderViewMarkdownAgentPreservesTrailingNewline(t *testing.T) {
	previous := flagViewAgent
	t.Cleanup(func() {
		flagViewAgent = previous
	})

	flagViewAgent = true
	input := "# Title\n"
	got, err := renderViewMarkdown(input)
	if err != nil {
		t.Fatalf("renderViewMarkdown() returned an error: %v", err)
	}

	if got != input {
		t.Errorf("renderViewMarkdown() = %q, want unchanged input %q", got, input)
	}
}

func TestViewHasAgentFlag(t *testing.T) {
	flag := viewCmd.Flags().Lookup("agent")
	if flag == nil {
		t.Fatal("view command is missing the --agent flag")
	}
	if flag.Usage != "Show raw Markdown output for agent consumption" {
		t.Errorf("unexpected --agent help: %q", flag.Usage)
	}
}

func TestCommentOnlyMode(t *testing.T) {
	previous := flagViewComments
	t.Cleanup(func() {
		flagViewComments = previous
	})

	tests := []struct {
		value string
		want  string
	}{
		{value: "only", want: "reviews"},
		{value: "only-reviews", want: "reviews"},
		{value: "only-regular", want: "regular"},
		{value: "reviews", want: ""},
		{value: "regular", want: ""},
		{value: "none", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.value, func(t *testing.T) {
			flagViewComments = tt.value
			if got := commentOnlyMode(); got != tt.want {
				t.Errorf("commentOnlyMode() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFormatViewAgentPRChoices(t *testing.T) {
	prs := []gh.PRInfo{
		{Number: 123, State: "open", Title: "Fix the flaky test"},
		{Number: 120, State: "closed", Merged: true, Title: "Ship the fix"},
	}
	prs[0].User.Login = "alice"
	prs[1].User.Login = "bob"

	got := formatViewAgentPRChoices(prs)
	want := "Multiple PRs found. Choose one by rerunning with its number:\n" +
		"#123\topen\tFix the flaky test\t@alice\n" +
		"#120\tmerged\tShip the fix\t@bob\n"
	if got != want {
		t.Errorf("formatViewAgentPRChoices() = %q, want %q", got, want)
	}
}

func TestViewAgentModeMatrix(t *testing.T) {
	previousAgent, previousRootAgent, previousRootPretty := flagViewAgent, flagRootAgent, flagRootPretty
	t.Cleanup(func() {
		flagViewAgent, flagRootAgent, flagRootPretty = previousAgent, previousRootAgent, previousRootPretty
	})

	restoreTTY := ui.SetTTYFuncs(func() bool { return true }, func() bool { return true })
	t.Cleanup(restoreTTY)

	tests := []struct {
		name      string
		stdoutTTY bool
		agent     bool
		pretty    bool
		wantAgent bool
	}{
		{"tty no flags", true, false, false, false},
		{"pipe no flags", false, false, false, true},
		{"tty --agent", true, true, false, true},
		{"pipe --pretty", false, false, true, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			restore := ui.SetTTYFuncs(func() bool { return tt.stdoutTTY }, func() bool { return true })
			t.Cleanup(restore)
			flagViewAgent, flagRootAgent, flagRootPretty = tt.agent, false, tt.pretty

			// showIDs follows the same decision, so comment IDs must
			// appear exactly when agent mode is active.
			if got := viewAgentMode(); got != tt.wantAgent {
				t.Errorf("viewAgentMode() = %v, want %v", got, tt.wantAgent)
			}
		})
	}
}
