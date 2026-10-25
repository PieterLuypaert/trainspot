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
	dep, arr := r.Stops[from], r.Stops[to]
	l.Departure = Stop{
		Station: dep.Station, Scheduled: dep.Departure, Delay: dep.DepartureDelay,
		Platform: dep.Platform, PlatformChanged: dep.PlatformChanged,
		Cancelled: dep.Cancelled || dep.EndsHere, Passed: dep.Passed,
	}
	l.Calls = slices.Clone(r.Stops[from+1 : to])
	l.Arrival = Stop{
		Station: arr.Station, Scheduled: arr.Arrival, Delay: arr.ArrivalDelay,
		Platform: arr.Platform, PlatformChanged: arr.PlatformChanged,
		Cancelled: arr.Cancelled, Passed: arr.Arrived,
	}
	return l
}
