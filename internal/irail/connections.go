package irail

import (
	"context"
	"net/url"
	"strings"
	"time"
)

// Stop is where a leg of a journey leaves or arrives.
type Stop struct {
	Station   string
	Scheduled time.Time
	Delay     time.Duration
	// Platform is empty when NMBS does not know it yet.
	Platform        string
	PlatformChanged bool
	Cancelled       bool
}

func (s Stop) Expected() time.Time {
	return s.Scheduled.Add(s.Delay)
}

// Leg is one train ride, or a walk between two stations, within a journey.
type Leg struct {
	// Train is empty for a walk.
	Train     string
	Direction string
	Walking   bool
	Departure Stop
	Arrival   Stop
}

// Journey is a way to travel from one station to another, in one or more
// legs.
type Journey struct {
	Legs []Leg
}

func (j Journey) Departure() Stop {
	return j.Legs[0].Departure
}

func (j Journey) Arrival() Stop {
	return j.Legs[len(j.Legs)-1].Arrival
}

type connectionJSON struct {
	Departure endpointJSON `json:"departure"`
	Arrival   endpointJSON `json:"arrival"`
	Vias      struct {
		Via []struct {
			Arrival   endpointJSON `json:"arrival"`
			Departure endpointJSON `json:"departure"`
		} `json:"via"`
	} `json:"vias"`
}

type endpointJSON struct {
	Station      string `json:"station"`
	Time         number `json:"time"`
	Delay        number `json:"delay"`
	Canceled     flag   `json:"canceled"`
	Walking      flag   `json:"walking"`
	Vehicle      string `json:"vehicle"`
	Platform     string `json:"platform"`
	PlatformInfo struct {
		Normal *flag `json:"normal"`
	} `json:"platforminfo"`
	VehicleInfo struct {
		ShortName string `json:"shortname"`
	} `json:"vehicleinfo"`
	Direction struct {
		Name string `json:"name"`
	} `json:"direction"`
}

// Journeys returns the next ways to travel between two stations, named in
// lang.
func (c *Client) Journeys(ctx context.Context, fromID, toID, lang string) ([]Journey, error) {
	var raw struct {
		Connection []connectionJSON `json:"connection"`
	}
	params := url.Values{"from": {fromID}, "to": {toID}, "lang": {lang}, "timesel": {"departure"}, "results": {"6"}}
	if err := c.get(ctx, "connections", params, &raw); err != nil {
		return nil, err
	}
	journeys := make([]Journey, 0, len(raw.Connection))
	for _, conn := range raw.Connection {
		journeys = append(journeys, conn.journey())
	}
	return journeys, nil
}

// journey pairs each departure with the next arrival: iRail lists the
// first departure, then per change the arrival there and the departure
// after it, then the last arrival.
func (c connectionJSON) journey() Journey {
	departures := []endpointJSON{c.Departure}
	var arrivals []endpointJSON
	for _, via := range c.Vias.Via {
		arrivals = append(arrivals, via.Arrival)
		departures = append(departures, via.Departure)
	}
	arrivals = append(arrivals, c.Arrival)

	var j Journey
	for i, dep := range departures {
		leg := Leg{Walking: bool(dep.Walking), Departure: dep.stop(), Arrival: arrivals[i].stop()}
		if !leg.Walking {
			leg.Train = dep.VehicleInfo.ShortName
			if leg.Train == "" {
				leg.Train = strings.TrimPrefix(dep.Vehicle, "BE.NMBS.")
			}
			leg.Direction = dep.Direction.Name
		}
		j.Legs = append(j.Legs, leg)
	}
	return j
}

func (e endpointJSON) stop() Stop {
	platform := e.Platform
	if platform == "?" {
		platform = ""
	}
	return Stop{
		Station:         e.Station,
		Scheduled:       time.Unix(int64(e.Time), 0),
		Delay:           time.Duration(e.Delay) * time.Second,
		Platform:        platform,
		PlatformChanged: platform != "" && e.PlatformInfo.Normal != nil && !bool(*e.PlatformInfo.Normal),
		Cancelled:       bool(e.Canceled),
	}
}

// TightChange is how short a change can be before it counts as tight.
const TightChange = 3 * time.Minute

// Change is a change from one leg of a journey to the next.
type Change struct {
	Station string
	// Planned is the time to change by the timetable, Real the time left
	// once the delays are counted.
	Planned time.Duration
	Real    time.Duration
}

func (c Change) Missed() bool {
	return c.Real < 0
}

func (c Change) Tight() bool {
	return c.Real >= 0 && c.Real < TightChange
}

func (j Journey) Changes() []Change {
	var changes []Change
	for i := 1; i < len(j.Legs); i++ {
		arrival, departure := j.Legs[i-1].Arrival, j.Legs[i].Departure
		changes = append(changes, Change{
			Station: arrival.Station,
			Planned: departure.Scheduled.Sub(arrival.Scheduled),
			Real:    departure.Expected().Sub(arrival.Expected()),
		})
	}
	return changes
}

// Duration is how long the journey takes with the delays counted.
func (j Journey) Duration() time.Duration {
	return j.Arrival().Expected().Sub(j.Departure().Expected())
}

// Cancelled tells whether any train of the journey is cancelled at one of
// its stops.
func (j Journey) Cancelled() bool {
	for _, leg := range j.Legs {
		if leg.Departure.Cancelled || leg.Arrival.Cancelled {
			return true
		}
	}
	return false
}

// Missed tells whether the delays leave too little time for a change.
func (j Journey) Missed() bool {
	for _, c := range j.Changes() {
		if c.Missed() {
			return true
		}
	}
	return false
}
