package main

import (
	"fmt"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/PieterLuypaert/trainspot/internal/stations"
)

// maxChoices is how many stations the picker lists at most.
const maxChoices = 12

var (
	errorStyle  = lipgloss.NewStyle().Foreground(lipgloss.Red).Bold(true)
	numberStyle = lipgloss.NewStyle().Bold(true)
	noteStyle   = lipgloss.NewStyle().Faint(true)
)

// chooseStation turns what the user typed into one station. It asks only
// when several stations fit equally well and someone is there to answer.
func (a app) chooseStation(dir *stations.Directory, query, lang string) (stations.Station, error) {
	matches, sure := dir.Lookup(query, lang)
	switch {
	case sure:
		return matches[0], nil
	case len(matches) == 0:
		return stations.Station{}, noMatch(dir, query, lang)
	case !a.interactive:
		return stations.Station{}, ambiguous(query, matches, lang)
	}
	return a.ask(query, matches, lang)
}

// checkNames reports a name that fits no station at all before the picker
// asks about another one.
func checkNames(dir *stations.Directory, lang string, queries ...string) error {
	for _, query := range queries {
		if query == "" {
			continue
		}
		if matches, _ := dir.Lookup(query, lang); len(matches) == 0 {
			return noMatch(dir, query, lang)
		}
	}
	return nil
}

func noMatch(dir *stations.Directory, query, lang string) error {
	message := fmt.Sprintf("no station is called %q", query)
	if suggestions := dir.Suggest(query, lang, 3); len(suggestions) > 0 {
		var names []string
		for _, s := range suggestions {
			names = append(names, s.Name(lang))
		}
		message += "; did you mean " + orList(names) + "?"
	}
	return usageError{message}
}

func ambiguous(query string, matches []stations.Station, lang string) error {
	var b strings.Builder
	fmt.Fprintf(&b, "%q fits %d stations; type more of the name, such as:", query, len(matches))
	for _, s := range matches[:min(len(matches), maxChoices)] {
		b.WriteString("\n  " + s.Name(lang))
	}
	return usageError{b.String()}
}

func (a app) ask(query string, matches []stations.Station, lang string) (stations.Station, error) {
	shown := matches[:min(len(matches), maxChoices)]
	nameWidth := 0
	for _, s := range shown {
		nameWidth = max(nameWidth, lipgloss.Width(s.Name(lang)))
	}
	fmt.Fprintf(a.errOut, "  %q fits %d stations:\n\n", query, len(matches))
	for i, s := range shown {
		number := numberStyle.Render(fmt.Sprintf("%2d", i+1))
		name := s.Name(lang) + strings.Repeat(" ", nameWidth-lipgloss.Width(s.Name(lang)))
		fmt.Fprintln(a.errOut, strings.TrimRight("  "+number+"  "+name+describe(s, lang), " "))
	}
	if hidden := len(matches) - len(shown); hidden > 0 {
		fmt.Fprintln(a.errOut, noteStyle.Render(fmt.Sprintf("      and %d more; type more of the name to see them", hidden)))
	}
	fmt.Fprintln(a.errOut)

	for {
		fmt.Fprintf(a.errOut, "  Which one? Type a number from 1 to %d: ", len(shown))
		if !a.answers.Scan() {
			fmt.Fprintln(a.errOut)
			return stations.Station{}, usageError{"no station chosen"}
		}
		n, err := strconv.Atoi(strings.TrimSpace(a.answers.Text()))
		if err == nil && n >= 1 && n <= len(shown) {
			fmt.Fprintln(a.errOut)
			return shown[n-1], nil
		}
	}
}

// describe adds what tells look-alike stations apart: the local name and
// whether it lies abroad.
func describe(s stations.Station, lang string) string {
	var notes []string
	if local := s.LocalName(lang); local != "" {
		notes = append(notes, local)
	}
	if !s.Belgian() {
		notes = append(notes, "abroad")
	}
	if len(notes) == 0 {
		return ""
	}
	return "  " + noteStyle.Render(strings.Join(notes, " · "))
}

func orList(items []string) string {
	if len(items) == 1 {
		return items[0]
	}
	return strings.Join(items[:len(items)-1], ", ") + " or " + items[len(items)-1]
}
