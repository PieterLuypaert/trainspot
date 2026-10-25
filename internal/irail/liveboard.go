package irail

import (
	"context"
	"net/url"
	"time"
)

type Departure struct {
	Destination string
	Scheduled   time.Time
	Delay       time.Duration
	// Platform is empty when NMBS does not know it yet.
	Platform        string
	PlatformChanged bool
	Cancelled       bool
	Departed        bool
	Extra           bool
	Train           string
	// Occupancy is how busy NMBS expects the train to be: low, medium or
	// high, or empty when it has no estimate.
	Occupancy string
}

func (d Departure) Expected() time.Time {
	return d.Scheduled.Add(d.Delay)
}

type Liveboard struct {
	Station    string
	Updated    time.Time
	Departures []Departure
}

type liveboardJSON struct {
	Station    string `json:"station"`
	Timestamp  number `json:"timestamp"`
	Departures struct {
		Departure []departureJSON `json:"departure"`
	} `json:"departures"`
}

type departureJSON struct {
	Station      string `json:"station"`
	Time         number `json:"time"`
	Delay        number `json:"delay"`
	Canceled     flag   `json:"canceled"`
	Left         flag   `json:"left"`
	IsExtra      flag   `json:"isExtra"`
	Vehicle      string `json:"vehicle"`
	Platform     string `json:"platform"`
	PlatformInfo struct {
		Normal *flag `json:"normal"`
	} `json:"platforminfo"`
	VehicleInfo struct {
		ShortName string `json:"shortname"`
	} `json:"vehicleinfo"`
	Occupancy struct {
		Name string `json:"name"`
	} `json:"occupancy"`
}

// Liveboard returns the next departures from a station, named in lang.
func (c *Client) Liveboard(ctx context.Context, stationID, lang string) (Liveboard, error) {
	var raw liveboardJSON
	params := url.Values{"id": {stationID}, "arrdep": {"departure"}, "lang": {lang}}
	if err := c.get(ctx, "liveboard", params, &raw); err != nil {
		return Liveboard{}, err
	}
	board := Liveboard{Station: raw.Station, Updated: time.Unix(int64(raw.Timestamp), 0)}
	for _, d := range raw.Departures.Departure {
		board.Departures = append(board.Departures, d.departure())
	}
	return board, nil
}

func (d departureJSON) departure() Departure {
	platform, platformChanged := platform(d.Platform, d.PlatformInfo.Normal)
	occupancy := d.Occupancy.Name
	if occupancy == "unknown" {
		occupancy = ""
	}
	return Departure{
		Destination:     d.Station,
		Scheduled:       time.Unix(int64(d.Time), 0),
		Delay:           time.Duration(d.Delay) * time.Second,
		Platform:        platform,
		PlatformChanged: platformChanged,
		Cancelled:       bool(d.Canceled),
		Departed:        bool(d.Left),
		Extra:           bool(d.IsExtra),
		Train:           trainName(d.VehicleInfo.ShortName, d.Vehicle),
		Occupancy:       occupancy,
	}
}
