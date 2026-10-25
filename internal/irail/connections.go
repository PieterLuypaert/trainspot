package irail

import (
	"context"
	"net/url"
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

// Trip holds the journeys iRail found between two stations.
type Trip struct {
	Updated  time.Time
	Journeys []Journey
}

// Journeys returns the next ways to travel between two stations, named in
// lang.
func (c *Client) Journeys(ctx context.Context, fromID, toID, lang string) (Trip, error) {
	var raw struct {
		Timestamp  number           `json:"timestamp"`
		Connection []connectionJSON `json:"connection"`
	}
	params := url.Values{"from": {fromID}, "to": {toID}, "lang": {lang}, "timesel": {"departure"}, "results": {"6"}}
	if err := c.get(ctx, "connections", params, &raw); err != nil {
		return Trip{}, err
	}
	trip := Trip{Updated: time.Unix(int64(raw.Timestamp), 0)}
	for _, conn := range raw.Connection {
		trip.Journeys = append(trip.Journeys, conn.journey())
	}
	return trip, nil
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
			leg.Train = trainName(dep.VehicleInfo.ShortName, dep.Vehicle)
			leg.Direction = dep.Direction.Name
		}
		j.Legs = append(j.Legs, leg)
	}
	return j
}

func (e endpointJSON) stop() Stop {
	platform, platformChanged := platform(e.Platform, e.PlatformInfo.Normal)
	return Stop{
		Station:         e.Station,
		Scheduled:       time.Unix(int64(e.Time), 0),
		Delay:           time.Duration(e.Delay) * time.Second,
		Platform:        platform,
		PlatformChanged: platformChanged,
		Cancelled:       bool(e.Canceled),
	}
}

// TightChange is how short delays may squeeze a change before it counts
// as tight.
const TightChange = 3 * time.Minute

// Change is a change from one train to the next.
type Change struct {
	// Planned is the time to change by the timetable, Real the time left
	// once the delays are counted.
	Planned time.Duration
	Real    time.Duration
}

func (c Change) Missed() bool {
	return c.Real < 0
}

// Tight tells whether the delays squeezed the change below TightChange;
// a change the timetable plans that short is not tight.
func (c Change) Tight() bool {
	return c.Real >= 0 && c.Real < TightChange && c.Real < c.Planned
}

// Changes lists the changes from one train to the next, one for every
// leg after the first that is not a walk. A walk between two stations
// belongs to the change after it, since it starts when the train before
// it arrives.
func (j Journey) Changes() []Change {
	var changes []Change
	for i := 1; i < len(j.Legs); i++ {
		if j.Legs[i].Walking {
			continue
		}
		planned, expected := j.reach(i)
		departure := j.Legs[i].Departure
		changes = append(changes, Change{
			Planned: departure.Scheduled.Sub(planned),
			Real:    departure.Expected().Sub(expected),
		})
	}
	return changes
}

// reach is when you get to the start of leg i, by the timetable and with
// the delays. A walk takes its planned time from when you can start it.
func (j Journey) reach(i int) (planned, expected time.Time) {
	before := j.Legs[i-1]
	if !before.Walking {
		return before.Arrival.Scheduled, before.Arrival.Expected()
	}
	start := before.Departure.Scheduled
	if i >= 2 {
		if _, arrives := j.reach(i - 1); arrives.After(start) {
			start = arrives
		}
	}
	return before.Arrival.Scheduled, start.Add(before.Arrival.Scheduled.Sub(before.Departure.Scheduled))
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
