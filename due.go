package main

import (
	"strings"
	"time"
)

// nextNever is written to the next file when no pin can become due; it is a
// plain integer so the zsh hook's arithmetic comparison never needs a special case.
const nextNever int64 = 9999999999

type pinView struct {
	name string
	eff  Effective
	text string
	last int64
}

func isBlank(s string) bool { return strings.TrimSpace(s) == "" }

// commandEligible must match exactly what `_hook command` could ever show,
// otherwise next would stay in the past and the hook would fork every prompt.
func commandEligible(p pinView) bool {
	return p.eff.Enabled && p.eff.OnCommand && len(p.eff.Schedule) > 0 && !isBlank(p.text)
}

func dueAt(p pinView) int64 {
	next := nextNever
	for _, e := range p.eff.Schedule {
		next = min(next, e.next(p.last, time.Local))
	}
	return next
}

func isDue(p pinView, now int64) bool {
	return commandEligible(p) && now >= dueAt(p)
}

func nextDue(pins []pinView) int64 {
	next := nextNever
	for _, p := range pins {
		if commandEligible(p) && dueAt(p) < next {
			next = dueAt(p)
		}
	}
	return next
}

func filterPins(pins []pinView, keep func(pinView) bool) []pinView {
	var out []pinView
	for _, p := range pins {
		if keep(p) && !isBlank(p.text) {
			out = append(out, p)
		}
	}
	return out
}
