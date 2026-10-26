package main

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

const day = 24 * time.Hour

var errDuration = errors.New("not a duration")

// parseDuration accepts Go durations plus a leading "<n>d" day component
// (e.g. "2d", "1d12h") and "min"/"mins" for minutes.
func parseDuration(s string) (time.Duration, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.ReplaceAll(strings.ReplaceAll(s, "mins", "m"), "min", "m")
	if s == "" {
		return 0, errDuration
	}
	var total time.Duration
	rest := s
	if i := strings.IndexByte(s, 'd'); i >= 0 {
		n, err := strconv.ParseFloat(s[:i], 64)
		if err != nil || n < 0 {
			return 0, errDuration
		}
		total = time.Duration(n * float64(day))
		rest = s[i+1:]
	}
	if rest != "" {
		d, err := time.ParseDuration(rest)
		if err != nil || d < 0 {
			return 0, errDuration
		}
		total += d
	}
	if total < 0 {
		return 0, errDuration
	}
	return total, nil
}

func formatDuration(d time.Duration) string {
	if d%time.Second != 0 {
		return d.String()
	}
	var b strings.Builder
	for _, u := range []struct {
		unit time.Duration
		sfx  string
	}{{day, "d"}, {time.Hour, "h"}, {time.Minute, "m"}, {time.Second, "s"}} {
		if n := d / u.unit; n > 0 {
			fmt.Fprintf(&b, "%d%s", n, u.sfx)
			d -= n * u.unit
		}
	}
	return b.String()
}

func relTime(last, now int64) string {
	if last == 0 {
		return "never"
	}
	d := time.Duration(now-last) * time.Second
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", d/time.Minute)
	case d < day:
		return fmt.Sprintf("%dh ago", d/time.Hour)
	default:
		return fmt.Sprintf("%dd ago", d/day)
	}
}
