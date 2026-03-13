package ui

import (
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"golang.org/x/term"
)

// Reusable styles for picker formatting and other colored output.
var (
	StyleNumber  = lipgloss.NewStyle().Foreground(lipgloss.Color("6")) // cyan
	StyleAuthor  = lipgloss.NewStyle().Foreground(lipgloss.Color("3")) // yellow
	StyleBranch  = lipgloss.NewStyle().Foreground(lipgloss.Color("5")) // magenta
	StyleMuted   = lipgloss.NewStyle().Foreground(lipgloss.Color("8")) // gray
	StyleLabel   = lipgloss.NewStyle().Foreground(lipgloss.Color("5")) // magenta
	StyleBold    = lipgloss.NewStyle().Bold(true)
	StyleHash    = lipgloss.NewStyle().Foreground(lipgloss.Color("3")) // yellow
	StyleSubject = lipgloss.NewStyle().Bold(true)
	StyleCyan    = lipgloss.NewStyle().Foreground(lipgloss.Color("6")) // cyan

	StyleStateOpen   = lipgloss.NewStyle().Foreground(lipgloss.Color("4")) // blue
	StyleStateClosed = lipgloss.NewStyle().Foreground(lipgloss.Color("1")) // red
	StyleStateMerged = lipgloss.NewStyle().Foreground(lipgloss.Color("5")) // magenta
)

// Worktree tag shown in branch pickers.
var StyleWorktree = lipgloss.NewStyle().Foreground(lipgloss.Color("6")) // cyan

// GetTermWidth returns the current terminal width, defaulting to 80.
func GetTermWidth() int {
	w, _, err := term.GetSize(int(os.Stderr.Fd()))
	if err != nil || w <= 0 {
		return 80
	}
	return w
}

// GetTermHeight returns the current terminal height, defaulting to 24.
func GetTermHeight() int {
	_, h, err := term.GetSize(int(os.Stderr.Fd()))
	if err != nil || h <= 0 {
		return 24
	}
	return h
}

// SelectHeight returns a sensible height for a Select/MultiSelect picker,
// leaving room for the title and filter input.
func SelectHeight(optionCount int) int {
	h := GetTermHeight() - 4
	if h < 5 {
		h = 5
	}
	if optionCount < h {
		return optionCount
	}
	return h
}

// TruncateWithEllipsis truncates s to maxLen characters, adding "…" if truncated.
func TruncateWithEllipsis(s string, maxLen int) string {
	if maxLen <= 0 {
		return s
	}
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen-1]) + "…"
}

// PadRight pads s with spaces so that its visible (non-ANSI) width is at least width.
func PadRight(s string, width int) string {
	visible := len([]rune(StripANSI(s)))
	if visible >= width {
		return s
	}
	return s + strings.Repeat(" ", width-visible)
}

// StyleStateTag returns a colored "[state]" tag.
func StyleStateTag(state string) string {
	tag := "[" + state + "]"
	switch state {
	case "merged":
		return StyleStateMerged.Render(tag)
	case "closed":
		return StyleStateClosed.Render(tag)
	default:
		return StyleStateOpen.Render(tag)
	}
}
