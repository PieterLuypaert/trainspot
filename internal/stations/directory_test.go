package stations

import (
	"encoding/json"
	"os"
	"slices"
	"testing"
)

// testdata/stations.json is the full list iRail served on 2026-10-25, in
// all four languages.
func recorded(t *testing.T) *Directory {
	t.Helper()
	data, err := os.ReadFile("testdata/stations.json")
	if err != nil {
		t.Fatal(err)
	}
	var list struct{ Stations []Station }
	if err := json.Unmarshal(data, &list); err != nil {
		t.Fatal(err)
	}
	return NewDirectory(list.Stations)
}

func names(list []Station) []string {
	var out []string
	for _, s := range list {
		out = append(out, s.Name("nl"))
	}
	return out
}

func TestLookupFindsStationsAllOverBelgium(t *testing.T) {
	d := recorded(t)
	cases := map[string]string{
		"de panne":               "De Panne",
		"la panne":               "De Panne",
		"knokke":                 "Knokke",
		"poperinge":              "Poperinge",
		"turnhout":               "Turnhout",
		"neerpelt":               "Neerpelt",
		"eupen":                  "Eupen",
		"gouvy":                  "Gouvy",
		"couvin":                 "Couvin",
		"virton":                 "Virton",
		"aarlen":                 "Aarlen",
		"arlon":                  "Aarlen",
		"bergen":                 "Bergen",
		"mons":                   "Bergen",
		"doornik":                "Doornik",
		"tournai":                "Doornik",
		"namur":                  "Namen",
		"liège-guillemins":       "Luik-Guillemins",
		"liege guillemins":       "Luik-Guillemins",
		"LUIK GUILLEMINS":        "Luik-Guillemins",
		"lie\u0300ge guillemins": "Luik-Guillemins",
		"bruxelles midi":         "Brussel-Zuid",
		"brussels-south":         "Brussel-Zuid",
		"brussel zuid":           "Brussel-Zuid",
		"gand saint pierre":      "Gent-Sint-Pieters",
		"gent sint pieters":      "Gent-Sint-Pieters",
		"antwerpen c":            "Antwerpen-Centraal",
		"anvers central":         "Antwerpen-Centraal",
	}
	for query, want := range cases {
		matches, sure := d.Lookup(query, "nl")
		if !sure || len(matches) == 0 || matches[0].Name("nl") != want {
			t.Errorf("Lookup(%q) = %v (sure %v), want %s first and sure", query, names(matches), sure, want)
		}
	}
}

func TestLookupAsksWhenSeveralStationsFitAsWell(t *testing.T) {
	d := recorded(t)
	cases := map[string][]string{
		"gent":         {"Gent-Dampoort", "Gent-Sint-Pieters", "Gentbrugge"},
		"sint pieters": {"Brugge-Sint-Pieters", "Gent-Sint-Pieters"},
		"charleroi":    {"Charleroi-Centraal", "Charleroi-West"},
	}
	for query, want := range cases {
		matches, sure := d.Lookup(query, "nl")
		if sure || !slices.Equal(names(matches), want) {
			t.Errorf("Lookup(%q) = %v (sure %v), want to ask between %v", query, names(matches), sure, want)
		}
	}
}

func TestLookupListsBelgianStationsBeforeForeignOnes(t *testing.T) {
	d := recorded(t)

	matches, _ := d.Lookup("zuid", "nl")

	foreign := slices.IndexFunc(matches, func(s Station) bool { return !s.Belgian() })
	if foreign == -1 || slices.ContainsFunc(matches[foreign:], Station.Belgian) {
		t.Errorf("Lookup(zuid) = %v, want Amsterdam Zuid after every Belgian station", names(matches))
	}
}

func TestLookupFindsNothingForNonsense(t *testing.T) {
	d := recorded(t)
	for _, query := range []string{"xyzzy", "", " - "} {
		if matches, sure := d.Lookup(query, "nl"); len(matches) != 0 || sure {
			t.Errorf("Lookup(%q) = %v", query, names(matches))
		}
	}
}

func TestSuggestCorrectsTypos(t *testing.T) {
	d := recorded(t)
	cases := map[string]string{
		"brugee":      "Brugge",
		"oostend":     "Oostende",
		"leuvn":       "Leuven",
		"guillemains": "Luik-Guillemins",
	}
	for query, want := range cases {
		got := d.Suggest(query, "nl", 3)
		if len(got) == 0 || got[0].Name("nl") != want {
			t.Errorf("Suggest(%q) = %v, want %s first", query, names(got), want)
		}
	}
	if got := d.Suggest("xyzzy", "nl", 3); len(got) != 0 {
		t.Errorf("Suggest(xyzzy) = %v, want nothing", names(got))
	}
}

func TestLocalNameShowsTheOfficialNameWhenItDiffers(t *testing.T) {
	d := recorded(t)
	cases := []struct{ query, lang, want string }{
		{"bergen", "nl", "Mons"},
		{"mons", "fr", ""},
		{"brussel zuid", "nl", "Bruxelles-Midi"},
		{"brussel zuid", "de", ""},
		{"gent sint pieters", "nl", ""},
		{"gent sint pieters", "fr", "Gent-Sint-Pieters"},
	}
	for _, c := range cases {
		matches, _ := d.Lookup(c.query, c.lang)
		if got := matches[0].LocalName(c.lang); got != c.want {
			t.Errorf("LocalName of %s in %s = %q, want %q", c.query, c.lang, got, c.want)
		}
	}
}
