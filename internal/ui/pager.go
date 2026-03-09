package ui

import (
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Pager displays content in a scrollable viewport on stderr.
// If the content fits within the terminal height, it prints directly instead.
func Pager(content string) error {
	lines := strings.Count(content, "\n")
	termHeight := GetTermHeight()
	if lines <= termHeight-2 {
		fmt.Fprint(os.Stderr, content)
		return nil
	}

	p := tea.NewProgram(
		newPagerModel(content),
		tea.WithOutput(os.Stderr),
		tea.WithAltScreen(),
	)
	_, err := p.Run()
	return err
}

type pagerModel struct {
	viewport viewport.Model
	quit     key.Binding
	ready    bool
	content  string
}

func newPagerModel(content string) pagerModel {
	return pagerModel{
		content: content,
		quit: key.NewBinding(
			key.WithKeys("q", "esc"),
			key.WithHelp("q/esc", "quit"),
		),
	}
}

func (m pagerModel) Init() tea.Cmd {
	return nil
}

func (m pagerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if key.Matches(msg, m.quit) {
			return m, tea.Quit
		}
	case tea.WindowSizeMsg:
		headerHeight := 0
		footerHeight := 1
		if !m.ready {
			m.viewport = viewport.New(msg.Width, msg.Height-headerHeight-footerHeight)
			m.viewport.SetContent(m.content)
			m.ready = true
		} else {
			m.viewport.Width = msg.Width
			m.viewport.Height = msg.Height - headerHeight - footerHeight
		}
	}

	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(msg)
	return m, cmd
}

func (m pagerModel) View() string {
	if !m.ready {
		return ""
	}
	return m.viewport.View() + "\n" + m.footerView()
}

var pagerFooterStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))

func (m pagerModel) footerView() string {
	pct := m.viewport.ScrollPercent() * 100
	info := fmt.Sprintf(" %3.0f%% · q to quit ", pct)
	return pagerFooterStyle.Render(info)
}
