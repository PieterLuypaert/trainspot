package tui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// The screens put what you choose from on the left and what goes with it
// on the right when the window is wide enough; in a narrower window the
// right pane goes under the left one instead.

// twoPanesWidth is the inner width from which the panes stand side by
// side.
const twoPanesWidth = 100

// divider stands between two panes.
var divider = faint.Render("│") + " "

// sideBySide puts the right pane beside the left one, which is padded to
// leftWidth, with a divider down the rows that hold either.
func sideBySide(left, right []string, leftWidth int) []string {
	rows := max(len(left), len(right))
	lines := make([]string, 0, rows)
	for i := range rows {
		l, r := "", ""
		if i < len(left) {
			l = ansi.Truncate(left[i], leftWidth, "…")
		}
		if i < len(right) {
			r = right[i]
		}
		lines = append(lines, strings.TrimRight(l+strings.Repeat(" ", max(0, leftWidth-lipgloss.Width(l)))+divider+r, " "))
	}
	return lines
}

// cut shortens lines to at most rows, ending in a line that says how much
// was left out and how to see it.
func cut(lines []string, rows int, howToSee string) []string {
	if len(lines) <= rows {
		return lines
	}
	keep := max(0, rows-1)
	note := indent + faint.Render(fmt.Sprintf("… %d more lines; %s", len(lines)-keep, howToSee))
	return append(lines[:keep:keep], note)
}
