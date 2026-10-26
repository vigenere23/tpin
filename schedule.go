package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/robfig/cron/v3"
)

var cronParser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)

// Entry is one schedule item: an interval since the pin was last shown, or a cron expression.
type Entry struct {
	spec  string
	every time.Duration
	cron  cron.Schedule
}

func parseEntry(s string) (Entry, error) {
	f := strings.Fields(s)
	spec := strings.Join(f, " ")
	dur, interval := spec, len(f) == 1 && !strings.HasPrefix(spec, "@")
	if len(f) == 2 && strings.ToLower(f[0]) == "@every" {
		dur, interval = f[1], true
	}
	if interval {
		d, err := parseDuration(dur)
		if err != nil || d < time.Second {
			return Entry{}, fmt.Errorf("invalid schedule entry %q (intervals look like 45m, 15min, 2h, 1d12h, @every 2h; at least 1s)", spec)
		}
		return Entry{spec: formatDuration(d), every: d}, nil
	}
	c, err := cronParser.Parse(spec)
	if err != nil {
		return Entry{}, fmt.Errorf("invalid schedule entry %q: %v (cron looks like \"0 8 * * 1-5\" or @daily)", spec, err)
	}
	return Entry{spec: spec, cron: c}, nil
}

func (e Entry) MarshalText() ([]byte, error) { return []byte(e.spec), nil }

func (e *Entry) UnmarshalText(b []byte) error {
	p, err := parseEntry(string(b))
	if err != nil {
		return err
	}
	*e = p
	return nil
}

// next is when this entry makes a pin last shown at last due again; cron times are in loc.
func (e Entry) next(last int64, loc *time.Location) int64 {
	if e.cron == nil {
		return last + int64(e.every/time.Second)
	}
	t := e.cron.Next(time.Unix(last, 0).In(loc))
	if t.IsZero() {
		return nextNever
	}
	return t.Unix()
}

// parseSchedule splits on ";"; "off" or an empty value gives an empty, non-nil schedule.
func parseSchedule(s string) ([]Entry, error) {
	out := []Entry{}
	if v := strings.TrimSpace(s); v == "" || strings.EqualFold(v, "off") {
		return out, nil
	}
	for _, part := range strings.Split(s, ";") {
		if strings.TrimSpace(part) == "" {
			continue
		}
		e, err := parseEntry(part)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, nil
}

func formatSchedule(s []Entry) string {
	if len(s) == 0 {
		return "off"
	}
	specs := make([]string, len(s))
	for i, e := range s {
		specs[i] = e.spec
	}
	return strings.Join(specs, "; ")
}

func mustSchedule(s string) []Entry {
	e, err := parseSchedule(s)
	if err != nil {
		panic(err)
	}
	return e
}
