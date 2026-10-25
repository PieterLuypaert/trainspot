package irail

import (
	"context"
	"net/url"
)

type Station struct {
	ID   string
	Name string
	// Standard is the official name, in both languages for stations such
	// as Brussel-Zuid/Bruxelles-Midi.
	Standard string
}

// Stations returns every station iRail knows, named in lang.
func (c *Client) Stations(ctx context.Context, lang string) ([]Station, error) {
	var raw struct {
		Station []struct {
			ID           string `json:"id"`
			Name         string `json:"name"`
			StandardName string `json:"standardname"`
		} `json:"station"`
	}
	if err := c.get(ctx, "stations", url.Values{"lang": {lang}}, &raw); err != nil {
		return nil, err
	}
	stations := make([]Station, 0, len(raw.Station))
	for _, s := range raw.Station {
		stations = append(stations, Station{ID: s.ID, Name: s.Name, Standard: s.StandardName})
	}
	return stations, nil
}
