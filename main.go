package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"text/tabwriter"
	"time"

	"golang.org/x/term"
)

const usage = `tpin - pin plain-text messages to your terminal

Usage:
  tpin                              show all enabled pins now
  tpin show [name]                  show one pin
  tpin edit [name]                  edit a pin in $VISUAL/$EDITOR (creates it)
  tpin set [name] "text"            set a pin's text ("" clears it)
  tpin rm <name>                    remove a pin (default cannot be removed)
  tpin ls                           list pins
  tpin config [name]                show a pin's effective config
  tpin config [name] <key> <value>  set a pin override
  tpin config [name] <key> --unset  revert a key to the inherited value
  tpin config --defaults [<key> <value>|<key> --unset]
                                    show or change the shared defaults
  tpin init zsh                     print the zsh hook: eval "$(tpin init zsh)"
  tpin help                         show this help

Keys: schedule ("1h; 0 8 * * 1-5": intervals and cron entries split by ";", or off),
      on-open, on-command, enabled (on/off),
      color (yellow, green, blue, magenta, cyan, red, none).
Without a name, commands target the "default" pin.
`

type App struct {
	store Store
	out   io.Writer
	color bool
	width int
	now   func() time.Time
}

func main() {
	a := &App{store: defaultStore(), out: os.Stdout, color: colorEnabled(), width: termWidth(), now: time.Now}
	args := os.Args[1:]
	if len(args) > 0 && args[0] == "_hook" {
		// Runs from precmd: never print errors or fail, or every prompt gets spammed.
		_ = a.hook(args[1:])
		return
	}
	if err := a.run(args); err != nil {
		fmt.Fprintln(os.Stderr, "tpin:", err)
		os.Exit(1)
	}
}

func colorEnabled() bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	fi, err := os.Stdout.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

func termWidth() int {
	if w, _, err := term.GetSize(int(os.Stdout.Fd())); err == nil && w > 0 {
		return w
	}
	return 80
}

func (a *App) run(args []string) error {
	if len(args) == 0 {
		return a.showAll()
	}
	cmd, rest := args[0], args[1:]
	switch cmd {
	case "show":
		name, err := optionalName(rest)
		if err != nil {
			return err
		}
		return a.showOne(name)
	case "edit":
		name, err := optionalName(rest)
		if err != nil {
			return err
		}
		return a.edit(name)
	case "set":
		name, text, err := parseSetArgs(rest)
		if err != nil {
			return err
		}
		return a.set(name, text)
	case "rm":
		if len(rest) != 1 {
			return errors.New("usage: tpin rm <name>")
		}
		return a.rm(rest[0])
	case "ls":
		return a.ls()
	case "config":
		req, err := parseConfigArgs(rest)
		if err != nil {
			return err
		}
		return a.config(req)
	case "init":
		if len(rest) != 1 || rest[0] != "zsh" {
			return errors.New("usage: tpin init zsh (only zsh is supported)")
		}
		fmt.Fprint(a.out, zshInit(binaryPath()))
		return nil
	case "help", "-h", "--help":
		fmt.Fprint(a.out, usage)
		return nil
	}
	return fmt.Errorf("unknown command %q (see tpin help)", cmd)
}

func (a *App) display(st *State, pins []pinView) {
	if len(pins) == 0 {
		return
	}
	fmt.Fprint(a.out, "\n"+renderBoard(pins, a.width, a.color)+"\n")
	now := a.now().Unix()
	for _, p := range pins {
		st.LastShown[p.name] = now
	}
}

func (a *App) showMatching(keep func(pinView) bool) (shown int, err error) {
	now := a.now().Unix()
	err = a.store.transact(now, func(c *Config, st *State) error {
		pins, err := a.store.pins(c, st, now)
		if err != nil {
			return err
		}
		pins = filterPins(pins, keep)
		a.display(st, pins)
		shown = len(pins)
		return nil
	})
	return shown, err
}

func (a *App) showAll() error {
	n, err := a.showMatching(func(p pinView) bool { return p.eff.Enabled })
	if err == nil && n == 0 {
		fmt.Fprintln(a.out, `No pins to show. Add one with: tpin set "text"`)
	}
	return err
}

func (a *App) hook(args []string) error {
	if len(args) != 1 {
		return errors.New("usage: tpin _hook open|command")
	}
	now := a.now().Unix()
	switch args[0] {
	case "open":
		// The zsh hook skips the first prompt check, so open must also cover due pins.
		_, err := a.showMatching(func(p pinView) bool { return p.eff.Enabled && p.eff.OnOpen || isDue(p, now) })
		return err
	case "command":
		_, err := a.showMatching(func(p pinView) bool { return isDue(p, now) })
		return err
	}
	return errors.New("unknown hook")
}

func (a *App) showOne(name string) error {
	return a.store.transact(a.now().Unix(), func(c *Config, st *State) error {
		if !a.store.exists(c, name) {
			return fmt.Errorf("no pin named %q", name)
		}
		text, err := a.store.readText(name)
		if err != nil {
			return err
		}
		if isBlank(text) {
			fmt.Fprintf(os.Stderr, "tpin: pin %q is empty\n", name)
			return nil
		}
		a.display(st, []pinView{{name: name, eff: c.Effective(name), text: text}})
		return nil
	})
}

func (a *App) set(name, text string) error {
	if text != "" && !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	return a.store.transact(a.now().Unix(), func(*Config, *State) error {
		return writeFileAtomic(a.store.pinPath(name), []byte(text))
	})
}

func (a *App) edit(name string) error {
	path := a.store.pinPath(name)
	if !a.store.textExists(name) {
		if err := writeFileAtomic(path, nil); err != nil {
			return err
		}
	}
	editor := os.Getenv("VISUAL")
	if editor == "" {
		editor = os.Getenv("EDITOR")
	}
	if editor == "" {
		editor = "vi"
	}
	// Through sh so editors configured with flags (e.g. "code -w") work.
	cmd := exec.Command("sh", "-c", editor+` "$1"`, "sh", path)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("editor: %w", err)
	}
	return a.showOne(name)
}

func (a *App) rm(name string) error {
	if name == defaultPin {
		return errors.New(`the default pin cannot be removed; clear it with: tpin set ""`)
	}
	if err := validateName(name); err != nil {
		return err
	}
	return a.store.transact(a.now().Unix(), func(c *Config, st *State) error {
		if !a.store.exists(c, name) {
			return fmt.Errorf("no pin named %q", name)
		}
		if err := os.Remove(a.store.pinPath(name)); err != nil && !os.IsNotExist(err) {
			return err
		}
		delete(st.LastShown, name)
		if _, ok := c.Pins[name]; ok {
			delete(c.Pins, name)
			return a.store.saveConfig(c)
		}
		return nil
	})
}

func (a *App) ls() error {
	c, err := a.store.loadConfig()
	if err != nil {
		return err
	}
	st, err := a.store.loadState()
	if err != nil {
		return err
	}
	pins, err := a.store.pins(c, st, 0)
	if err != nil {
		return err
	}
	now := a.now().Unix()
	tw := tabwriter.NewWriter(a.out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "NAME\tSCHEDULE\tON-OPEN\tON-COMMAND\tENABLED\tCOLOR\tLAST SHOWN\tTEXT")
	for _, p := range pins {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n", p.name, formatSchedule(p.eff.Schedule),
			onOff(p.eff.OnOpen), onOff(p.eff.OnCommand), onOff(p.eff.Enabled), p.eff.value("color"),
			relTime(p.last, now), preview(p.text))
	}
	return tw.Flush()
}

func preview(text string) string {
	for _, line := range strings.Split(text, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			if r := []rune(line); len(r) > 40 {
				return string(r[:39]) + "…"
			}
			return line
		}
	}
	return "(empty)"
}

func (a *App) config(r configReq) error {
	if r.write {
		return a.store.transact(a.now().Unix(), func(c *Config, _ *State) error {
			if !r.defaults && !a.store.exists(c, r.name) && r.unset {
				return fmt.Errorf("no pin named %q", r.name)
			}
			s := &c.Defaults
			var ps Settings
			if !r.defaults {
				ps = c.Pins[r.name]
				s = &ps
			}
			if r.unset {
				s.unset(r.key)
			} else if err := s.set(r.key, r.value); err != nil {
				return err
			}
			if !r.defaults {
				c.Pins[r.name] = ps
			}
			return a.store.saveConfig(c)
		})
	}

	c, err := a.store.loadConfig()
	if err != nil {
		return err
	}
	eff, own, title := c.DefaultsEffective(), srcDefaults, "[defaults]"
	if !r.defaults {
		if !a.store.exists(c, r.name) {
			return fmt.Errorf("no pin named %q", r.name)
		}
		eff, own, title = c.Effective(r.name), srcPin, r.name
	}
	if r.key != "" {
		fmt.Fprintln(a.out, eff.value(r.key))
		return nil
	}
	var buf bytes.Buffer
	tw := tabwriter.NewWriter(&buf, 0, 0, 2, ' ', 0)
	for _, k := range configKeys {
		note := ""
		switch src := eff.src[k]; {
		case src == own:
		case src == srcBuiltin:
			note = "(built-in default)"
		default:
			note = "(inherited from [defaults])"
		}
		fmt.Fprintf(tw, "  %s\t%s\t%s\n", k, eff.value(k), note)
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	fmt.Fprintln(a.out, title)
	for _, line := range strings.Split(strings.TrimSuffix(buf.String(), "\n"), "\n") {
		fmt.Fprintln(a.out, strings.TrimRight(line, " "))
	}
	return nil
}
