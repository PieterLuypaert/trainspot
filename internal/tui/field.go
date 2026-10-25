package tui

import (
	"strings"
	"unicode"

	"github.com/PieterLuypaert/trainspot/internal/stations"
)

// field is where the user types a station's name. Once a station is
// picked, the field shows its name; typing replaces it, the way a selected
// text is replaced.
type field struct {
	label   string
	text    string
	station *stations.Station
}

func (f *field) insert(text string) {
	text = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, text)
	if text == "" {
		return
	}
	if f.station != nil {
		f.text, f.station = "", nil
	}
	f.text += text
}

func (f *field) backspace() {
	runes := []rune(f.text)
	if len(runes) > 0 {
		f.text = string(runes[:len(runes)-1])
	}
	f.station = nil
}

func (f *field) deleteWord() {
	trimmed := strings.TrimRight(f.text, " ")
	f.text, f.station = trimmed[:strings.LastIndexAny(trimmed, " -")+1], nil
}

func (f *field) clear() {
	f.text, f.station = "", nil
}

func (f *field) pick(s stations.Station, lang string) {
	f.station, f.text = &s, s.Name(lang)
}
