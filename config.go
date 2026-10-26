package main

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
)

const defaultPin = "default"

var configKeys = []string{"schedule", "on-open", "on-command", "enabled", "color"}

// pinColors are the standard 16-color ANSI hues, so they follow the terminal theme.
var pinColors = []string{"yellow", "green", "blue", "magenta", "cyan", "red"}

type Settings struct {
	Schedule  *[]Entry `toml:"schedule"`
	OnOpen    *bool    `toml:"on_open"`
	OnCommand *bool    `toml:"on_command"`
	Enabled   *bool    `toml:"enabled"`
	Color     *string  `toml:"color"`
}

type Config struct {
	Defaults Settings            `toml:"defaults"`
	Pins     map[string]Settings `toml:"pins,omitempty"`
}

func ptr[T any](v T) *T { return &v }

var builtin = Settings{
	Schedule:  ptr(mustSchedule("1h; 0 8 * * *")),
	OnOpen:    ptr(false),
	OnCommand: ptr(true),
	Enabled:   ptr(true),
}

type source int

const (
	srcBuiltin source = iota
	srcDefaults
	srcPin
)

type Effective struct {
	Schedule  []Entry
	OnOpen    bool
	OnCommand bool
	Enabled   bool
	Color     string
	src       map[string]source
}

// effective merges layers in order; layer i is attributed to source(i).
func effective(layers ...Settings) Effective {
	e := Effective{src: map[string]source{}}
	for i, l := range layers {
		s := source(i)
		if l.Schedule != nil {
			e.Schedule, e.src["schedule"] = *l.Schedule, s
		}
		if l.OnOpen != nil {
			e.OnOpen, e.src["on-open"] = *l.OnOpen, s
		}
		if l.OnCommand != nil {
			e.OnCommand, e.src["on-command"] = *l.OnCommand, s
		}
		if l.Enabled != nil {
			e.Enabled, e.src["enabled"] = *l.Enabled, s
		}
		if l.Color != nil {
			e.Color, e.src["color"] = *l.Color, s
		}
	}
	return e
}

func (c *Config) Effective(name string) Effective {
	return effective(builtin, c.Defaults, c.Pins[name])
}

func (c *Config) DefaultsEffective() Effective {
	return effective(builtin, c.Defaults)
}

func (e Effective) value(key string) string {
	switch key {
	case "schedule":
		return formatSchedule(e.Schedule)
	case "on-open":
		return onOff(e.OnOpen)
	case "on-command":
		return onOff(e.OnCommand)
	case "enabled":
		return onOff(e.Enabled)
	case "color":
		if e.Color == "" {
			return "none"
		}
		return e.Color
	}
	return ""
}

func onOff(b bool) string {
	if b {
		return "on"
	}
	return "off"
}

func normalizeKey(k string) (string, bool) {
	k = strings.ReplaceAll(strings.ToLower(k), "_", "-")
	for _, ck := range configKeys {
		if k == ck {
			return k, true
		}
	}
	return "", false
}

func parseBool(s string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "on", "true", "yes", "1":
		return true, nil
	case "off", "false", "no", "0":
		return false, nil
	}
	return false, fmt.Errorf("invalid boolean %q (use on/off, true/false, yes/no)", s)
}

func (s *Settings) set(key, val string) error {
	if key == "schedule" {
		sc, err := parseSchedule(val)
		if err != nil {
			return err
		}
		s.Schedule = &sc
		return nil
	}
	if key == "color" {
		v := strings.ToLower(strings.TrimSpace(val))
		if v != "none" && !slices.Contains(pinColors, v) {
			return fmt.Errorf("invalid color %q (use %s or none)", val, strings.Join(pinColors, ", "))
		}
		s.Color = &v
		return nil
	}
	b, err := parseBool(val)
	if err != nil {
		return err
	}
	switch key {
	case "on-open":
		s.OnOpen = &b
	case "on-command":
		s.OnCommand = &b
	case "enabled":
		s.Enabled = &b
	}
	return nil
}

func (s *Settings) unset(key string) {
	switch key {
	case "schedule":
		s.Schedule = nil
	case "on-open":
		s.OnOpen = nil
	case "on-command":
		s.OnCommand = nil
	case "enabled":
		s.Enabled = nil
	case "color":
		s.Color = nil
	}
}

var nameRe = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

func validateName(name string) error {
	if !nameRe.MatchString(name) {
		return fmt.Errorf("invalid pin name %q (allowed: letters, digits, _ and -)", name)
	}
	if _, isKey := normalizeKey(name); isKey {
		return fmt.Errorf("%q is a config key and cannot be used as a pin name", name)
	}
	return nil
}
