package ui

import (
	"github.com/charmbracelet/glamour"
)

// RenderMarkdown renders markdown text for terminal display using glamour.
// It auto-detects dark/light terminal background.
func RenderMarkdown(content string) (string, error) {
	r, err := glamour.NewTermRenderer(
		glamour.WithAutoStyle(),
		glamour.WithWordWrap(80),
	)
	if err != nil {
		return "", err
	}
	return r.Render(content)
}
