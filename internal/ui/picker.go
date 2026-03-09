package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/huh"
)

// PRPickerItem holds data for a single row in a PR/issue picker.
type PRPickerItem struct {
	Number      int
	Title       string
	Author      string
	State       string // "open", "closed", "merged"
	Branch      string // head branch (fetch mode)
	IsCrossRepo bool   // cross-repo fork indicator (fetch mode)
	Labels      string // comma-separated label names
	Assignees   string // comma-separated assignee logins
	IsHighlight bool   // bold title (current user's item)
}

// PRPickerMode controls which columns are shown.
type PRPickerMode int

const (
	PickerDefault   PRPickerMode = iota // #num  title  author
	PickerFetch                         // #num  title  author  branch [⎇]
	PickerWithState                     // #num  [state]  title  author
	PickerIssue                         // #num  title  → assignees  │ author  [labels]
)

// FormatPRPickerOptions builds styled, aligned huh.Option items for a picker.
func FormatPRPickerOptions(items []PRPickerItem, mode PRPickerMode) []huh.Option[int] {
	if len(items) == 0 {
		return nil
	}

	// Pass 1: measure column widths
	maxNumW := 0
	maxAuthorW := 0
	maxBranchW := 0
	maxAssigneeW := 0

	for _, item := range items {
		nw := len(fmt.Sprintf("#%d", item.Number))
		if nw > maxNumW {
			maxNumW = nw
		}
		if len(item.Author) > maxAuthorW {
			maxAuthorW = len(item.Author)
		}
		if mode == PickerFetch {
			bw := len(item.Branch)
			if item.IsCrossRepo {
				bw += 2 // " ⎇"
			}
			if bw > maxBranchW {
				maxBranchW = bw
			}
		}
		if mode == PickerIssue && item.Assignees != "" {
			aw := 2 + len(item.Assignees) // "→ assignees"
			if strings.Contains(item.Assignees, ",") {
				aw += strings.Count(item.Assignees, ",") // extra space for ", "
			}
			if aw > maxAssigneeW {
				maxAssigneeW = aw
			}
		}
	}

	stateColW := 0
	if mode == PickerWithState {
		stateColW = 10 // "[merged] " padded
	}

	// Compute title cap based on terminal width
	termW := GetTermWidth()
	overhead := maxNumW + 1 + maxAuthorW + 3 // num + space + author + margins
	switch mode {
	case PickerFetch:
		overhead += maxBranchW + 1
	case PickerWithState:
		overhead += stateColW
	case PickerIssue:
		if maxAssigneeW > 0 {
			overhead += maxAssigneeW + 1
		}
		overhead += 2 // "│ " before author
	}
	titleCap := termW - overhead
	if titleCap < 20 {
		titleCap = 20
	}

	// Measure max title length (after truncation)
	maxTitleW := 0
	for _, item := range items {
		tl := len([]rune(item.Title))
		if tl > titleCap {
			tl = titleCap
		}
		if tl > maxTitleW {
			maxTitleW = tl
		}
	}

	// Pass 2: build styled lines
	opts := make([]huh.Option[int], 0, len(items))
	for _, item := range items {
		displayTitle := TruncateWithEllipsis(item.Title, titleCap)

		// Number column: styled + padded
		numStr := StyleNumber.Render(fmt.Sprintf("#%d", item.Number))
		numStr = PadRight(numStr, maxNumW)

		// Title column: optionally bold + padded
		titleStr := displayTitle
		if item.IsHighlight {
			titleStr = StyleBold.Render(displayTitle)
		}
		titleStr = PadRight(titleStr, maxTitleW)

		// Author column: styled
		authorStr := StyleAuthor.Render(item.Author)

		var line string
		switch mode {
		case PickerFetch:
			authorStr = PadRight(authorStr, maxAuthorW)

			branchStr := StyleBranch.Render(item.Branch)
			if item.IsCrossRepo {
				branchStr += " " + StyleMuted.Render("⎇")
			}
			branchStr = PadRight(branchStr, maxBranchW)

			line = fmt.Sprintf("%s %s %s %s", numStr, titleStr, authorStr, branchStr)

		case PickerWithState:
			stateStr := PadRight(StyleStateTag(item.State), stateColW)
			line = fmt.Sprintf("%s %s %s %s", numStr, stateStr, titleStr, authorStr)

		case PickerIssue:
			// Assignees column
			assigneeStr := ""
			if item.Assignees != "" {
				display := "→ " + strings.ReplaceAll(item.Assignees, ",", ", ")
				assigneeStr = StyleAuthor.Render(display)
				assigneeStr = PadRight(assigneeStr, maxAssigneeW)
			} else if maxAssigneeW > 0 {
				assigneeStr = strings.Repeat(" ", maxAssigneeW)
			}

			// Author with separator
			authorWithSep := StyleMuted.Render("│ " + item.Author)

			line = fmt.Sprintf("%s %s %s %s", numStr, titleStr, assigneeStr, authorWithSep)

			// Labels
			if item.Labels != "" {
				line += " " + StyleLabel.Render("["+item.Labels+"]")
			}

		default:
			line = fmt.Sprintf("%s %s %s", numStr, titleStr, authorStr)
		}

		opts = append(opts, huh.NewOption(line, item.Number))
	}

	return opts
}

// BranchPickerItem holds data for a branch in the branch picker.
type BranchPickerItem struct {
	Name        string
	HasWorktree bool
}

// FormatBranchPickerOptions builds styled huh.Option items for a branch picker.
func FormatBranchPickerOptions(items []BranchPickerItem) []huh.Option[string] {
	opts := make([]huh.Option[string], 0, len(items))
	for _, item := range items {
		display := StyleBranchName(item.Name)
		if item.HasWorktree {
			display += "  " + StyleWorktree.Render("[worktree]")
		}
		opts = append(opts, huh.NewOption(display, item.Name))
	}
	return opts
}
