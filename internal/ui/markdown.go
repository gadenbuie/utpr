package ui

import (
	"os"
	"sync"

	"github.com/charmbracelet/glamour"
	"golang.org/x/term"
)

var (
	mdRenderer     *glamour.TermRenderer
	mdRendererOnce sync.Once
	mdRendererErr  error
)

func getMarkdownRenderer() (*glamour.TermRenderer, error) {
	mdRendererOnce.Do(func() {
		width := 80
		if w, _, err := term.GetSize(int(os.Stderr.Fd())); err == nil && w > 0 {
			width = w
		}
		if width < 40 {
			width = 40
		} else if width > 120 {
			width = 120
		}
		mdRenderer, mdRendererErr = glamour.NewTermRenderer(
			glamour.WithAutoStyle(),
			glamour.WithWordWrap(width),
		)
	})
	return mdRenderer, mdRendererErr
}

// IsStdoutTTY reports whether stdout is an interactive terminal.
func IsStdoutTTY() bool {
	return term.IsTerminal(int(os.Stdout.Fd()))
}

// RenderMarkdown renders markdown text for terminal display using glamour.
// It auto-detects dark/light terminal background and adapts to terminal width.
func RenderMarkdown(content string) (string, error) {
	r, err := getMarkdownRenderer()
	if err != nil {
		return "", err
	}
	return r.Render(content)
}
