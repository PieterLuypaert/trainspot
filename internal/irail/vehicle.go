package irail

import (
	"context"
	"fmt"
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

// RouteThrough returns the run of a train that stops at station at the
// scheduled time at, in Belgian time. It asks for the train on that day
// and, when that run does not stop there then, on the day before, as for a
// train that started before midnight.
func (c *Client) RouteThrough(ctx context.Context, vehicleID, station string, at time.Time, lang string) (Route, error) {
	sameDay, err := c.Route(ctx, vehicleID, at, lang)
	if err == nil && sameDay.stopsAt(station, at) {
		return sameDay, nil
	}
	dayBefore, errBefore := c.Route(ctx, vehicleID, at.AddDate(0, 0, -1), lang)
	if errBefore == nil && dayBefore.stopsAt(station, at) {
		return dayBefore, nil
	}
	if err != nil {
		return Route{}, err
	}
	return Route{}, fmt.Errorf("iRail has no run of %s that stops at %s at %s", sameDay.Train, station, at.Format("15:04"))
}

func (r Route) stopsAt(station string, at time.Time) bool {
	return slices.ContainsFunc(r.Stops, func(c Call) bool {
		return c.Station == station && (c.Departure.Equal(at) || c.Arrival.Equal(at))
	})
}

// Follow brings a leg up to date with its train's route: where it leaves,
// the calls on the way and where it arrives, as they are now. Stops are
// matched by station name, which both answers give in the same language.
// It reports false, with the leg as it was, when the route does not run
// between the leg's stations.
func (l Leg) Follow(r Route) (Leg, bool) {
	from := slices.IndexFunc(r.Stops, func(c Call) bool { return c.Station == l.Departure.Station })
	if from < 0 {
		return l, false
	}
	to := slices.IndexFunc(r.Stops[from+1:], func(c Call) bool { return c.Station == l.Arrival.Station })
	if to < 0 {
		return l, false
	}
	to += from + 1
	l.Departure, l.Calls, l.Arrival = departureStop(r.Stops[from]), slices.Clone(r.Stops[from+1:to]), arrivalStop(r.Stops[to])
	return l, true
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
		Cancelled: c.Cancelled || c.EndsHere, Passed: c.Passed, Arrived: c.Arrived,
	}
}

// arrivalStop is where a leg arrives, taken from the train's call there.
func arrivalStop(c Call) Stop {
	return Stop{
		Station: c.Station, Scheduled: c.Arrival, Delay: c.ArrivalDelay,
		Platform: c.Platform, PlatformChanged: c.PlatformChanged,
		Cancelled: c.Cancelled || c.StartsHere, Passed: c.Arrived,
	}
}
