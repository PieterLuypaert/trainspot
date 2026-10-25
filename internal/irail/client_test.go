package irail

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func serve(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client := NewClient("trainspot-test/1.0")
	client.BaseURL = server.URL
	return client
}

func serveFile(t *testing.T, path string) (*Client, *http.Request) {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	seen := &http.Request{}
	client := serve(t, func(w http.ResponseWriter, r *http.Request) {
		*seen = *r
		w.Write(body)
	})
	return client, seen
}

func TestLiveboardAsksForTheStationByID(t *testing.T) {
	client, seen := serveFile(t, "testdata/liveboard-gent-sint-pieters.json")

	if _, err := client.Liveboard(context.Background(), "BE.NMBS.008892007", "fr"); err != nil {
		t.Fatal(err)
	}

	if seen.URL.Path != "/liveboard/" {
		t.Errorf("path = %q, want /liveboard/", seen.URL.Path)
	}
	query := seen.URL.Query()
	for key, want := range map[string]string{"id": "BE.NMBS.008892007", "arrdep": "departure", "lang": "fr", "format": "json"} {
		if got := query.Get(key); got != want {
			t.Errorf("%s = %q, want %q", key, got, want)
		}
	}
	if got := seen.Header.Get("User-Agent"); got != "trainspot-test/1.0" {
		t.Errorf("User-Agent = %q", got)
	}
}

func TestLiveboardReadsARecordedBoard(t *testing.T) {
	client, _ := serveFile(t, "testdata/liveboard-gent-sint-pieters.json")

	board, err := client.Liveboard(context.Background(), "BE.NMBS.008892007", "nl")
	if err != nil {
		t.Fatal(err)
	}

	if board.Station != "Gent-Sint-Pieters" || len(board.Departures) != 18 {
		t.Fatalf("got %q with %d departures", board.Station, len(board.Departures))
	}
	if !board.Updated.Equal(time.Unix(1792935916, 0)) {
		t.Errorf("Updated = %v", board.Updated)
	}
	first := board.Departures[0]
	if first.Destination != "Brugge" || first.Train != "L 13097" || first.Platform != "6" || !first.PlatformChanged {
		t.Errorf("first departure = %+v", first)
	}
	antwerp := board.Departures[3]
	if antwerp.Delay != 9*time.Minute || !antwerp.Expected().Equal(time.Unix(1792936380+540, 0)) || antwerp.PlatformChanged {
		t.Errorf("Antwerpen departure = %+v", antwerp)
	}
	if antwerp.Occupancy != "low" || board.Departures[8].Occupancy != "" {
		t.Errorf("occupancy = %q and %q, want low and empty for unknown", antwerp.Occupancy, board.Departures[8].Occupancy)
	}
}

func TestLiveboardReadsCancelledDepartedAndExtraTrains(t *testing.T) {
	client := serve(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"station":"Aarlen","timestamp":"1792935916","departures":{"departure":[
			{"station":"Namen","time":"1792936000","delay":"0","canceled":"1","left":"0","isExtra":"0","vehicle":"BE.NMBS.IC2110","platform":"?","platforminfo":{"name":"?","normal":"0"}},
			{"station":"Luxemburg","time":1792936100,"delay":60,"canceled":0,"left":1,"isExtra":1,"vehicle":"BE.NMBS.P8001","platform":"2"}
		]}}`))
	})

	board, err := client.Liveboard(context.Background(), "BE.NMBS.008866001", "nl")
	if err != nil {
		t.Fatal(err)
	}

	cancelled, departed := board.Departures[0], board.Departures[1]
	if !cancelled.Cancelled || cancelled.Departed || cancelled.Platform != "" || cancelled.PlatformChanged {
		t.Errorf("cancelled train = %+v", cancelled)
	}
	if cancelled.Train != "IC2110" {
		t.Errorf("train without short name = %q, want IC2110", cancelled.Train)
	}
	if !departed.Departed || !departed.Extra || departed.Cancelled || departed.PlatformChanged || departed.Delay != time.Minute {
		t.Errorf("departed extra train = %+v", departed)
	}
}

func TestStationsReadsNamesInTheAskedLanguage(t *testing.T) {
	client := serve(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/stations/" || r.URL.Query().Get("lang") != "fr" {
			t.Errorf("request = %s", r.URL)
		}
		w.Write([]byte(`{"station":[{"id":"BE.NMBS.008814001","name":"Bruxelles-Midi","standardname":"Brussel-Zuid/Bruxelles-Midi","locationX":"4.336531"}]}`))
	})

	stations, err := client.Stations(context.Background(), "fr")
	if err != nil {
		t.Fatal(err)
	}

	want := Station{ID: "BE.NMBS.008814001", Name: "Bruxelles-Midi", Standard: "Brussel-Zuid/Bruxelles-Midi"}
	if len(stations) != 1 || stations[0] != want {
		t.Errorf("stations = %+v", stations)
	}
}

func TestErrorsExplainWhatWentWrong(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		want   string
	}{
		{"iRail's own message", 400, `{"exception":"RequestedStopNotFoundException","message":"Requested stop not found: BE.NMBS.1"}`, "Requested stop not found: BE.NMBS.1"},
		{"rate limit", 429, ``, "too many requests"},
		{"server trouble", 503, `<html>down</html>`, "status.irail.be"},
		{"unreadable answer", 200, `<html>`, "cannot read"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			client := serve(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(c.status)
				w.Write([]byte(c.body))
			})

			_, err := client.Liveboard(context.Background(), "BE.NMBS.1", "nl")

			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Errorf("error = %v, want it to mention %q", err, c.want)
			}
		})
	}
}

func TestRateLimitIsRecognisable(t *testing.T) {
	client := serve(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	})

	_, err := client.Stations(context.Background(), "nl")

	if !errors.Is(err, ErrRateLimited) {
		t.Errorf("error = %v, want ErrRateLimited", err)
	}
}

func TestUnreachableServerSaysSo(t *testing.T) {
	client := NewClient("trainspot-test/1.0")
	client.BaseURL = "http://127.0.0.1:1"

	_, err := client.Stations(context.Background(), "nl")

	if err == nil || !strings.Contains(err.Error(), "could not reach iRail") {
		t.Errorf("error = %v", err)
	}
}
