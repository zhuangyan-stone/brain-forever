package toolset

import (
	"fmt"
	"time"
)

// TryParseTimeString attempts to parse a date/time string using common layouts or layout
func TryParseTimeString(s, layout string) (*time.Time, error) {
	if s == "" {
		return nil, nil
	}

	if layout != "" {
		t, err := time.Parse(layout, s)
		if err != nil {
			return nil, err
		}

		return &t, nil
	}

	layouts := []string{
		time.RFC3339,
		"2006-01-02T15:04:05Z",
		"2006-01-02T15:04:05-07:00",
		"2006-01-02 15:04:05",
		"2006-01-02",
	}
	for _, l := range layouts {
		if l != layout {
			t, err := time.Parse(l, s)
			if err == nil {
				return &t, nil
			}
		}
	}

	return nil, fmt.Errorf("bad time format. %s", s)
}

// FormatTimeWithLocation formats a time.Time in "2006-01-02 15:04:05 (Location)" format.
// Uses the local timezone's IANA name (e.g., "Asia/Shanghai").
// Falls back to numeric UTC offset (e.g., "UTC+08:00") if the location name is "Local".
func FormatTimeWithLocation(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	t = t.In(time.Local)
	locName := t.Location().String()
	if locName == "Local" {
		_, offset := t.Zone()
		sign := "+"
		if offset < 0 {
			sign = "-"
			offset = -offset
		}
		locName = fmt.Sprintf("UTC%s%02d:%02d", sign, offset/3600, (offset%3600)/60)
	}
	return t.Format("2006-01-02 15:04:05") + " (" + locName + ")"
}
