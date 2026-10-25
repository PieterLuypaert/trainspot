package irail

import (
	"context"
	"net/url"
	"slices"
	"time"
)

// Route is one train's run: every station it stops at, in order.
type Route struct {
	Train   string
	Updated time.Time
	Stops   []Call
}

// Route returns the run of a train by its vehicle id, such as
// BE.NMBS.IC2117, on day in Belgian time: a train that runs past midnight
// is asked for by the day it started.
func (c *Client) Route(ctx context.Context, vehicleID string, day time.Time, lang string) (Route, error) {
	var raw struct {
		Timestamp   number `json:"timestamp"`
		Vehicle     string `json:"vehicle"`
		VehicleInfo struct {
			ShortName string `json:"shortname"`
		} `json:"vehicleinfo"`
		Stops struct {
			Stop []callJSON `json:"stop"`
		} `json:"stops"`
	}
	params := url.Values{"id": {vehicleID}, "date": {day.Format("020106")}, "lang": {lang}}
	if err := c.get(ctx, "vehicle", params, &raw); err != nil {
		return Route{}, err
	}
	route := Route{Train: trainName(raw.VehicleInfo.ShortName, raw.Vehicle), Updated: time.Unix(int64(raw.Timestamp), 0)}
	for _, s := range raw.Stops.Stop {
		route.Stops = append(route.Stops, s.call())
	}
	return route, nil
}

// Follow brings a leg up to date with its train's route: where it leaves,
// the calls on the way and where it arrives, as they are now. Stops are
// matched by station name, which both answers give in the same language;
// a leg whose stations are not on the route comes back as it was.
func (l Leg) Follow(r Route) Leg {
	from := slices.IndexFunc(r.Stops, func(c Call) bool { return c.Station == l.Departure.Station })
	if from < 0 {
		return l
	}
	to := slices.IndexFunc(r.Stops[from+1:], func(c Call) bool { return c.Station == l.Arrival.Station })
	if to < 0 {
		return l
	}
	to += from + 1
	l.Departure, l.Calls, l.Arrival = departureStop(r.Stops[from]), slices.Clone(r.Stops[from+1:to]), arrivalStop(r.Stops[to])
	return l
}

// Leg is the whole run as one leg: from the first station to the last, with
// every stop in between as its calls.
func (r Route) Leg() Leg {
	if len(r.Stops) < 2 {
		return Leg{Train: r.Train}
	}
	first, last := r.Stops[0], r.Stops[len(r.Stops)-1]
	return Leg{
		Train: r.Train, Direction: last.Station,
		Departure: departureStop(first), Calls: slices.Clone(r.Stops[1 : len(r.Stops)-1]), Arrival: arrivalStop(last),
	}
}

// departureStop is where a leg leaves, taken from the train's call there.
func departureStop(c Call) Stop {
	return Stop{
		Station: c.Station, Scheduled: c.Departure, Delay: c.DepartureDelay,
		Platform: c.Platform, PlatformChanged: c.PlatformChanged,
		Cancelled: c.Cancelled || c.EndsHere, Passed: c.Passed,
	}
}

// arrivalStop is where a leg arrives, taken from the train's call there.
func arrivalStop(c Call) Stop {
	return Stop{
		Station: c.Station, Scheduled: c.Arrival, Delay: c.ArrivalDelay,
		Platform: c.Platform, PlatformChanged: c.PlatformChanged,
		Cancelled: c.Cancelled, Passed: c.Arrived,
	}
}
