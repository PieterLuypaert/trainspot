// Package irail talks to the iRail API (api.irail.be), which serves the live
// NMBS timetable for every station in Belgium.
package irail

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

const DefaultBaseURL = "https://api.irail.be/v1"

// Languages are the languages iRail can name stations in.
var Languages = []string{"nl", "fr", "en", "de"}

var ErrRateLimited = errors.New("iRail is getting too many requests from this network; wait a few seconds and try again")

// Error is an error iRail reported itself.
type Error struct {
	Status  int
	Message string
}

func (e *Error) Error() string {
	if e.Status >= 500 {
		return fmt.Sprintf("iRail has a problem (HTTP %d: %s); try again in a moment or check status.irail.be", e.Status, e.Message)
	}
	return fmt.Sprintf("iRail refused the request (HTTP %d): %s", e.Status, e.Message)
}

type Client struct {
	BaseURL   string
	UserAgent string
	HTTP      *http.Client
}

func NewClient(userAgent string) *Client {
	return &Client{
		BaseURL:   DefaultBaseURL,
		UserAgent: userAgent,
		HTTP:      &http.Client{Timeout: 15 * time.Second},
	}
}

func (c *Client) get(ctx context.Context, endpoint string, params url.Values, out any) error {
	params.Set("format", "json")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/"+endpoint+"/?"+params.Encode(), nil)
	if err != nil {
		return fmt.Errorf("could not build the iRail request: %w", err)
	}
	req.Header.Set("User-Agent", c.UserAgent)
	req.Header.Set("Accept", "application/json")

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("could not reach iRail, check your internet connection: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return fmt.Errorf("could not read the answer from iRail: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return statusError(resp.StatusCode, body)
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("iRail sent an answer trainspot cannot read: %w", err)
	}
	return nil
}

func statusError(status int, body []byte) error {
	if status == http.StatusTooManyRequests {
		return ErrRateLimited
	}
	var reported struct {
		Message string `json:"message"`
	}
	if json.Unmarshal(body, &reported) == nil && reported.Message != "" {
		return &Error{Status: status, Message: reported.Message}
	}
	return &Error{Status: status, Message: http.StatusText(status)}
}
