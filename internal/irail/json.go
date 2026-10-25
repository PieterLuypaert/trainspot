package irail

import (
	"fmt"
	"strconv"
	"strings"
)

// platform turns iRail's platform into an empty one when it is unknown
// ("?"), and calls it changed when NMBS sent a realtime one.
func platform(name string, normal *flag) (string, bool) {
	if name == "?" {
		return "", false
	}
	return name, name != "" && normal != nil && !bool(*normal)
}

// trainName prefers the short name, such as "IC 514", over the vehicle's
// id.
func trainName(shortName, vehicle string) string {
	if shortName != "" {
		return shortName
	}
	return strings.TrimPrefix(vehicle, "BE.NMBS.")
}

// number is a whole number that iRail sends as a string, such as "60".
type number int64

func (n *number) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	if s == "" || s == "null" {
		*n = 0
		return nil
	}
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return fmt.Errorf("expected a number, got %s", b)
	}
	*n = number(v)
	return nil
}

// flag is a yes or no that iRail sends as "0" or "1".
type flag bool

func (f *flag) UnmarshalJSON(b []byte) error {
	var n number
	if err := n.UnmarshalJSON(b); err != nil {
		return err
	}
	*f = n != 0
	return nil
}
