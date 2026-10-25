package stations

import (
	"cmp"
	"slices"
	"strings"
	"unicode"
)

// How well a query fits a name, best last.
const (
	noMatch = iota
	containsQuery
	wordPrefixes
	wholeWords
	exactName
)

type alias struct {
	text  string
	words []string
}

type Directory struct {
	stations []Station
	aliases  [][]alias
}

func NewDirectory(stations []Station) *Directory {
	d := &Directory{stations: stations, aliases: make([][]alias, len(stations))}
	for i, s := range stations {
		names := strings.Split(s.Standard, "/")
		for _, name := range s.Names {
			names = append(names, name)
		}
		for _, name := range names {
			text := normalize(name)
			if text != "" && !slices.ContainsFunc(d.aliases[i], func(a alias) bool { return a.text == text }) {
				d.aliases[i] = append(d.aliases[i], alias{text: text, words: strings.Fields(text)})
			}
		}
	}
	return d
}

func (d *Directory) Len() int {
	return len(d.stations)
}

// ByID finds a station by its iRail id, such as BE.NMBS.008892007.
func (d *Directory) ByID(id string) (Station, bool) {
	for _, s := range d.stations {
		if s.ID == id {
			return s, true
		}
	}
	return Station{}, false
}

type ranked struct {
	station Station
	score   int
	// sortName is the normalized name in the asked language, worked out
	// once rather than in every comparison.
	sortName string
}

func rank(s Station, score int, lang string) ranked {
	return ranked{station: s, score: score, sortName: normalize(s.Name(lang))}
}

// Lookup returns the stations a query can mean, best first. Sure says the
// first one fits better than all others, so there is no need to ask.
func (d *Directory) Lookup(query, lang string) (matches []Station, sure bool) {
	q := normalize(query)
	if q == "" {
		return nil, false
	}
	words := strings.Fields(q)
	var found []ranked
	for i, s := range d.stations {
		best := noMatch
		for _, a := range d.aliases[i] {
			best = max(best, fit(q, words, a))
		}
		if best != noMatch {
			found = append(found, rank(s, best, lang))
		}
	}
	sortRanked(found)
	for _, r := range found {
		matches = append(matches, r.station)
	}
	sure = len(found) == 1 || len(found) > 1 && found[0].score > found[1].score
	return matches, sure
}

func fit(query string, queryWords []string, a alias) int {
	switch {
	case a.text == query:
		return exactName
	case everyWord(queryWords, a.words, func(word, q string) bool { return word == q }):
		return wholeWords
	case everyWord(queryWords, a.words, strings.HasPrefix):
		return wordPrefixes
	case strings.Contains(a.text, query):
		return containsQuery
	}
	return noMatch
}

func everyWord(queryWords, words []string, fits func(word, q string) bool) bool {
	for _, q := range queryWords {
		if !slices.ContainsFunc(words, func(word string) bool { return fits(word, q) }) {
			return false
		}
	}
	return true
}

// Suggest returns up to n stations whose names are a few typos away from
// the query, for when Lookup finds nothing.
func (d *Directory) Suggest(query, lang string, n int) []Station {
	q := []rune(normalize(query))
	if len(q) == 0 {
		return nil
	}
	allowed := max(1, len(q)/3)
	var near []ranked
	for i, s := range d.stations {
		best := allowed + 1
		for _, a := range d.aliases[i] {
			best = min(best, distance(q, []rune(a.text)))
			for _, word := range a.words {
				best = min(best, distance(q, []rune(word)))
			}
		}
		if best <= allowed {
			near = append(near, rank(s, -best, lang))
		}
	}
	sortRanked(near)
	var suggestions []Station
	for _, r := range near[:min(n, len(near))] {
		suggestions = append(suggestions, r.station)
	}
	return suggestions
}

// sortRanked orders by score, highest first, then Belgian stations before
// foreign ones, then by name.
func sortRanked(list []ranked) {
	slices.SortStableFunc(list, func(a, b ranked) int {
		if c := cmp.Compare(b.score, a.score); c != 0 {
			return c
		}
		if a.station.Belgian() != b.station.Belgian() {
			if a.station.Belgian() {
				return -1
			}
			return 1
		}
		return cmp.Compare(a.sortName, b.sortName)
	})
}

// distance is the Levenshtein distance: how many letters to add, remove or
// change to turn a into b.
func distance(a, b []rune) int {
	prev := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur := make([]int, len(b)+1)
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			change := prev[j-1]
			if a[i-1] != b[j-1] {
				change++
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, change)
		}
		prev = cur
	}
	return prev[len(b)]
}

var accents = strings.NewReplacer(
	"à", "a", "á", "a", "â", "a", "ä", "a", "ã", "a", "å", "a",
	"ç", "c", "è", "e", "é", "e", "ê", "e", "ë", "e",
	"ì", "i", "í", "i", "î", "i", "ï", "i", "ñ", "n",
	"ò", "o", "ó", "o", "ô", "o", "ö", "o", "õ", "o",
	"ù", "u", "ú", "u", "û", "u", "ü", "u", "ý", "y", "ÿ", "y",
	"š", "s", "ž", "z", "œ", "oe", "æ", "ae", "ß", "ss",
)

// normalize lowercases, drops accents and turns everything that is not a
// letter or digit into single spaces, so "Liège-Guillemins" and
// "liege guillemins" compare equal. Accents typed as a separate combining
// mark, as text copied on macOS can have them, are dropped too.
func normalize(s string) string {
	s = accents.Replace(strings.ToLower(s))
	s = strings.Map(func(r rune) rune {
		if unicode.Is(unicode.Mn, r) {
			return -1
		}
		return r
	}, s)
	words := strings.FieldsFunc(s, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
	return strings.Join(words, " ")
}
