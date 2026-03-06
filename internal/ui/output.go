package ui

import (
	"fmt"
	"os"
	"regexp"

	"github.com/charmbracelet/lipgloss"
)

var (
	infoPrefix    = lipgloss.NewStyle().Foreground(lipgloss.Color("4"))
	warnPrefix    = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
	errorPrefix   = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
	successPrefix = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
)

func Info(msg string) {
	fmt.Fprintln(os.Stderr, infoPrefix.Render("ℹ "+msg))
}

func Warn(msg string) {
	fmt.Fprintln(os.Stderr, warnPrefix.Render("⚠ "+msg))
}

func Error(msg string) {
	fmt.Fprintln(os.Stderr, errorPrefix.Render("✖ "+msg))
}

func Success(msg string) {
	fmt.Fprintln(os.Stderr, successPrefix.Render("✔ "+msg))
}

func Infof(format string, args ...any) {
	Info(fmt.Sprintf(format, args...))
}

func Warnf(format string, args ...any) {
	Warn(fmt.Sprintf(format, args...))
}

func Errorf(format string, args ...any) {
	Error(fmt.Sprintf(format, args...))
}

func Successf(format string, args ...any) {
	Success(fmt.Sprintf(format, args...))
}

var ansiRe = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func StripANSI(s string) string {
	return ansiRe.ReplaceAllString(s, "")
}

// StyleBranchName colors the conventional-commit prefix of a branch name.
func StyleBranchName(branch string) string {
	re := regexp.MustCompile(`^([^/]+)(/.+)$`)
	matches := re.FindStringSubmatch(branch)
	if matches == nil {
		return branch
	}

	prefix := matches[1]
	rest := matches[2]

	var color string
	switch prefix {
	case "feat", "feature":
		color = "2" // green
	case "fix", "hotfix", "bugfix", "patch":
		color = "1" // red
	case "chore":
		color = "3" // yellow
	case "docs", "doc":
		color = "4" // blue
	case "test", "tests", "testing":
		color = "6" // cyan
	case "refactor", "refac":
		color = "5" // magenta
	case "perf":
		color = "11" // bright yellow
	case "ci", "build":
		color = "12" // bright blue
	case "style":
		color = "13" // bright magenta
	default:
		color = "8" // muted/gray
	}

	styled := lipgloss.NewStyle().Foreground(lipgloss.Color(color)).Render(prefix + "/")
	return styled + rest[1:] // rest starts with /, skip the duplicate /
}
