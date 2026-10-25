// Command trainspot shows the live departures from any NMBS station in
// Belgium.
package main

import (
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

const usage = `trainspot shows the live departures from any NMBS station in Belgium.

Usage:
  trainspot <station> [options]

Examples:
  trainspot gent sint pieters
  trainspot liège-guillemins --lang fr
  trainspot mons                 the same station as trainspot bergen

Options:
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
	query   string
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
		in:          os.Stdin,
		out:         stdout,
		errOut:      stderr,
		interactive: term.IsTerminal(os.Stdin.Fd()),
		width:       terminalWidth(),
	}
	a.cachePath, err = cachePath()
	if err != nil {
		os.Exit(a.fail(err))
	}
	os.Exit(a.run(context.Background(), o))
}

// parseArgs allows options before, between and after the words of the
// station name.
func parseArgs(args []string) (options, error) {
	o := options{}
	fs := flag.NewFlagSet("trainspot", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&o.lang, "lang", "nl", "")
	fs.BoolVar(&o.noColor, "no-color", false, "")
	fs.BoolVar(&o.help, "h", false, "")
	fs.BoolVar(&o.help, "help", false, "")
	fs.BoolVar(&o.version, "version", false, "")
	var words []string
	for {
		if err := fs.Parse(args); err != nil {
			return o, err
		}
		if fs.NArg() == 0 {
			break
		}
		words = append(words, fs.Arg(0))
		args = fs.Args()[1:]
	}
	o.query = strings.Join(words, " ")
	if !slices.Contains(irail.Languages, o.lang) {
		return o, fmt.Errorf("--lang takes nl, fr, en or de, not %q", o.lang)
	}
	return o, nil
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

func cachePath() (string, error) {
	dir := os.Getenv("XDG_CACHE_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("could not find your home folder to keep the station list in: %w", err)
		}
		dir = filepath.Join(home, ".cache")
	}
	return filepath.Join(dir, "trainspot", "stations.json"), nil
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
	client      *irail.Client
	cachePath   string
	now         func() time.Time
	in          io.Reader
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
	station, err := a.chooseStation(dir, o.query, o.lang)
	if err != nil {
		return a.fail(err)
	}
	departures, err := a.client.Liveboard(ctx, station.ID, o.lang)
	if err != nil {
		return a.fail(fmt.Errorf("could not get the departures from %s: %w", station.Name(o.lang), err))
	}
	fmt.Fprint(a.out, board.Render(departures, board.Options{
		Station:   station.Name(o.lang),
		LocalName: station.LocalName(o.lang),
		Width:     a.width,
		Now:       a.now(),
		Location:  brussels,
	}))
	return 0
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
