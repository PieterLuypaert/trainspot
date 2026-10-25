// Package stations finds NMBS stations by any of their names, in Dutch,
// French, English or German, with or without accents.
package stations

import "strings"

type Station struct {
	ID string `json:"id"`
	// Names holds the station's name per language: nl, fr, en and de.
	Names    map[string]string `json:"names"`
	Standard string            `json:"standard"`
}

func (s Station) Name(lang string) string {
	if name := s.Names[lang]; name != "" {
		return name
	}
	if name := s.Names["nl"]; name != "" {
		return name
	}
	return s.Standard
}

// LocalName is the station's official name where it lies, when the name in
// lang is a different one: Mons for Bergen, Bruxelles-Midi for Brussel-Zuid.
func (s Station) LocalName(lang string) string {
	shown := map[string]bool{}
	for _, name := range strings.Split(s.Name(lang), "/") {
		shown[normalize(name)] = true
	}
	var local []string
	for _, name := range strings.Split(s.Standard, "/") {
		if !shown[normalize(name)] {
			local = append(local, name)
		}
	}
	return strings.Join(local, " / ")
}

// Belgian tells stations in Belgium (UIC country code 88) from the foreign
// ones iRail also knows, such as Lille or Aachen.
func (s Station) Belgian() bool {
	return strings.HasPrefix(s.ID, "BE.NMBS.0088")
}
