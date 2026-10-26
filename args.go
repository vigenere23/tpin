package main

import (
	"errors"
	"fmt"
	"strings"
)

func optionalName(args []string) (string, error) {
	switch len(args) {
	case 0:
		return defaultPin, nil
	case 1:
		return args[0], validateName(args[0])
	}
	return "", errors.New("too many arguments")
}

func parseSetArgs(args []string) (name, text string, err error) {
	switch len(args) {
	case 1:
		return defaultPin, args[0], nil
	case 2:
		return args[0], args[1], validateName(args[0])
	}
	return "", "", errors.New(`usage: tpin set [name] "text"`)
}

type configReq struct {
	defaults bool
	name     string
	key      string
	value    string
	unset    bool
	write    bool
}

// parseConfigArgs tells a pin name from a key by checking the fixed key set;
// validateName forbids pins named after keys so this is never ambiguous.
func parseConfigArgs(args []string) (configReq, error) {
	r := configReq{name: defaultPin}
	if len(args) > 0 && args[0] == "--defaults" {
		r.defaults, r.name = true, ""
		args = args[1:]
	} else if len(args) > 0 {
		if _, isKey := normalizeKey(args[0]); !isKey {
			if err := validateName(args[0]); err != nil {
				return r, err
			}
			r.name = args[0]
			args = args[1:]
		}
	}
	if len(args) == 0 {
		return r, nil
	}
	if len(args) > 2 {
		return r, errors.New("too many arguments")
	}
	key, ok := normalizeKey(args[0])
	if !ok {
		return r, fmt.Errorf("unknown key %q (keys: %s)", args[0], strings.Join(configKeys, ", "))
	}
	r.key = key
	if len(args) == 2 {
		r.write = true
		if args[1] == "--unset" {
			r.unset = true
		} else {
			r.value = args[1]
		}
	}
	return r, nil
}
