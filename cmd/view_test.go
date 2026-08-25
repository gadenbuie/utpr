package cmd

import (
	"testing"
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
