// Command trainspot shows the live departures from any NMBS station in
// Belgium.
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime/debug"
	"slices"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata"

	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/term"

	"github.com/PieterLuypaert/trainspot/internal/board"
	"github.com/PieterLuypaert/trainspot/internal/irail"
	"github.com/PieterLuypaert/trainspot/internal/stations"
)

const usage = `trainspot shows live NMBS departures and journeys for any station in Belgium.

Usage:
  trainspot <station> [options]                  departures from a station
  trainspot <station> --to <station> [options]   journeys from one station to another

Examples:
  trainspot gent sint pieters
  trainspot gent sint pieters --to aarlen
  trainspot liège-guillemins --lang fr
  trainspot mons                 the same station as trainspot bergen

Options:
  --to <station>       show journeys to this station; every word after it is
                       part of its name
  --lang nl|fr|en|de   language of the station names (default nl)
  --no-color           print without colours
  --version            print the version
  -h, --help           show this help

Station names work in Dutch, French, English and German, with or without
accents. When a name fits several stations, trainspot asks which one.
`

// version is set at build time with -ldflags "-X main.version=v1.2.3".
var version = ""

var brussels, _ = time.LoadLocation("Europe/Brussels")

type options struct {
	query string
	// to is the destination of a journey, empty for departures.
	to      string
	lang    string
	noColor bool
	help    bool
	version bool
}

func main() {
	o, err := parseArgs(os.Args[1:])
	switch {
	case err != nil:
		fmt.Fprintf(os.Stderr, "trainspot: %v\nRun trainspot --help to see the options.\n", err)
		os.Exit(2)
	case o.help:
		fmt.Print(usage)
		return
	case o.version:
		fmt.Println("trainspot " + appVersion())
		return
	case o.query == "":
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}

	stdout := colorprofile.NewWriter(os.Stdout, os.Environ())
	stderr := colorprofile.NewWriter(os.Stderr, os.Environ())
	if o.noColor {
		// Only ever lower the profile: piped output stays free of any styling.
		stdout.Profile = min(stdout.Profile, colorprofile.ASCII)
		stderr.Profile = min(stderr.Profile, colorprofile.ASCII)
	}
	a := app{
		client:      irail.NewClient("trainspot/" + appVersion() + " (https://github.com/PieterLuypaert/trainspot)"),
		now:         time.Now,
		answers:     bufio.NewScanner(os.Stdin),
		out:         stdout,
		errOut:      stderr,
		interactive: term.IsTerminal(os.Stdin.Fd()),
		width:       terminalWidth(),
		cachePath:   cachePath(),
	}
	os.Exit(a.run(context.Background(), o))
}

// parseArgs allows options before, between and after the words of the
// station names. The words after --to name the destination, and everything
// after -- is part of the last name.
func parseArgs(args []string) (options, error) {
	o := options{}
	fs := flag.NewFlagSet("trainspot", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&o.lang, "lang", "nl", "")
	fs.BoolVar(&o.noColor, "no-color", false, "")
	fs.BoolVar(&o.help, "h", false, "")
	fs.BoolVar(&o.help, "help", false, "")
	fs.BoolVar(&o.version, "version", false, "")
	var rest []string
	if i := slices.Index(args, "--"); i >= 0 {
		args, rest = args[:i], args[i+1:]
	}
	fromArgs, toArgs, hasTo := splitAtTo(args)
	from, err := words(fs, fromArgs)
	if err != nil {
		return o, err
	}
	to, err := words(fs, toArgs)
	if err != nil {
		return o, err
	}
	if hasTo {
		to = append(to, rest...)
	} else {
		from = append(from, rest...)
	}
	o.query, o.to = strings.Join(from, " "), strings.Join(to, " ")
	if hasTo && o.to == "" {
		return o, errors.New("--to needs the name of a station")
	}
	if !slices.Contains(irail.Languages, o.lang) {
		return o, fmt.Errorf("--lang takes nl, fr, en or de, not %q", o.lang)
	}
	return o, nil
}

// words parses the options among args and returns the other words.
func words(fs *flag.FlagSet, args []string) ([]string, error) {
	var found []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		if fs.NArg() == 0 {
			return found, nil
		}
		found = append(found, fs.Arg(0))
		args = fs.Args()[1:]
	}
}

// splitAtTo cuts the arguments at --to, or at --to=name.
func splitAtTo(args []string) (from, to []string, found bool) {
	for i, arg := range args {
		switch {
		case arg == "--to" || arg == "-to":
			return args[:i], args[i+1:], true
		case strings.HasPrefix(arg, "--to=") || strings.HasPrefix(arg, "-to="):
			_, name, _ := strings.Cut(arg, "=")
			return args[:i], append([]string{name}, args[i+1:]...), true
		}
	}
	return args, nil, false
}

func appVersion() string {
	if version != "" {
		return version
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return "dev"
}

// cachePath follows the XDG spec, which ignores a relative XDG_CACHE_HOME.
// Without a home folder, as under some service managers, the list goes to
// the temporary folder rather than stopping trainspot.
func cachePath() string {
	dir := os.Getenv("XDG_CACHE_HOME")
	if !filepath.IsAbs(dir) {
		dir = os.TempDir()
		if home, err := os.UserHomeDir(); err == nil {
			dir = filepath.Join(home, ".cache")
		}
	}
	return filepath.Join(dir, "trainspot", "stations.json")
}

func terminalWidth() int {
	if width, _, err := term.GetSize(os.Stdout.Fd()); err == nil && width > 0 {
		return width
	}
	if width, err := strconv.Atoi(os.Getenv("COLUMNS")); err == nil && width > 0 {
		return width
	}
	return 80
}

type app struct {
	client    *irail.Client
	cachePath string
	now       func() time.Time
	// answers reads what the user types into the picker; one scanner for
	// the whole run, since a second one could miss lines the first buffered.
	answers     *bufio.Scanner
	out         io.Writer
	errOut      io.Writer
	interactive bool
	width       int
}

func (a app) run(ctx context.Context, o options) int {
	dir, err := stations.Load(ctx, a.client, a.cachePath, a.now(), a.errOut)
	if err != nil {
		return a.fail(err)
	}
	from, err := a.chooseStation(dir, o.query, o.lang)
	if err != nil {
		return a.fail(err)
	}
	if o.to == "" {
		err = a.showDepartures(ctx, from, o.lang)
	} else {
		err = a.showJourneys(ctx, dir, from, o.to, o.lang)
	}
	if err != nil {
		return a.fail(err)
	}
	return 0
}

func (a app) showDepartures(ctx context.Context, station stations.Station, lang string) error {
	departures, err := a.client.Liveboard(ctx, station.ID, lang)
	if err != nil {
		return fmt.Errorf("could not get the departures from %s: %w", station.Name(lang), err)
	}
	fmt.Fprint(a.out, board.Render(departures, board.Options{
		Station:   station.Name(lang),
		LocalName: station.LocalName(lang),
		Width:     a.width,
		Now:       a.now(),
		Location:  brussels,
	}))
	return nil
}

func (a app) showJourneys(ctx context.Context, dir *stations.Directory, from stations.Station, toQuery, lang string) error {
	to, err := a.chooseStation(dir, toQuery, lang)
	if err != nil {
		return err
	}
	if to.ID == from.ID {
		return usageError{fmt.Sprintf("%s is both where the journey starts and ends; pick two different stations", from.Name(lang))}
	}
	journeys, err := a.client.Journeys(ctx, from.ID, to.ID, lang)
	if err != nil {
		return fmt.Errorf("could not get the journeys from %s to %s: %w", from.Name(lang), to.Name(lang), err)
	}
	fmt.Fprint(a.out, board.Journeys(journeys, board.JourneyOptions{
		From:     from.Name(lang),
		To:       to.Name(lang),
		Width:    a.width,
		Now:      a.now(),
		Location: brussels,
	}))
	return nil
}

// usageError is a mistake in what the user typed, which exits with 2.
type usageError struct{ message string }

func (e usageError) Error() string { return e.message }

func (a app) fail(err error) int {
	fmt.Fprintln(a.errOut, errorStyle.Render("trainspot:")+" "+err.Error())
	if errors.As(err, new(usageError)) {
		return 2
	}
	return 1
}
