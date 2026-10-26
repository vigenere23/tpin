package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"math/rand/v2"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"

	"github.com/BurntSushi/toml"
)

type State struct {
	LastShown map[string]int64 `json:"last_shown"`
}

type Store struct {
	cfgDir   string
	stateDir string
}

func xdgDir(env, fallback string) string {
	if d := os.Getenv(env); filepath.IsAbs(d) {
		return d
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, fallback)
}

func defaultStore() Store {
	return Store{
		cfgDir:   filepath.Join(xdgDir("XDG_CONFIG_HOME", ".config"), "tpin"),
		stateDir: filepath.Join(xdgDir("XDG_STATE_HOME", filepath.Join(".local", "state")), "tpin"),
	}
}

func (s Store) configPath() string { return filepath.Join(s.cfgDir, "config.toml") }
func (s Store) pinsDir() string    { return filepath.Join(s.cfgDir, "pins") }
func (s Store) pinPath(n string) string {
	return filepath.Join(s.pinsDir(), n+".txt")
}
func (s Store) statePath() string { return filepath.Join(s.stateDir, "state.json") }
func (s Store) nextPath() string  { return filepath.Join(s.stateDir, "next") }

func writeFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Chmod(0o644); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

func (s Store) loadConfig() (*Config, error) {
	c := &Config{}
	b, err := os.ReadFile(s.configPath())
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	if err == nil {
		if _, err := toml.Decode(string(b), c); err != nil {
			return nil, fmt.Errorf("%s: %w", s.configPath(), err)
		}
	}
	if c.Pins == nil {
		c.Pins = map[string]Settings{}
	}
	return c, nil
}

func (s Store) saveConfig(c *Config) error {
	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(c); err != nil {
		return err
	}
	return writeFileAtomic(s.configPath(), buf.Bytes())
}

func (s Store) loadState() (*State, error) {
	st := &State{}
	b, err := os.ReadFile(s.statePath())
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	if err == nil && len(bytes.TrimSpace(b)) > 0 {
		if err := json.Unmarshal(b, st); err != nil {
			return nil, fmt.Errorf("%s: %w", s.statePath(), err)
		}
	}
	if st.LastShown == nil {
		st.LastShown = map[string]int64{}
	}
	return st, nil
}

func (s Store) saveState(st *State) error {
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(s.statePath(), append(b, '\n'))
}

func (s Store) writeNext(next int64) error {
	return writeFileAtomic(s.nextPath(), []byte(strconv.FormatInt(next, 10)+"\n"))
}

func (s Store) readText(name string) (string, error) {
	b, err := os.ReadFile(s.pinPath(name))
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	return string(b), err
}

func (s Store) textExists(name string) bool {
	_, err := os.Stat(s.pinPath(name))
	return err == nil
}

func (s Store) exists(c *Config, name string) bool {
	_, inCfg := c.Pins[name]
	return name == defaultPin || inCfg || s.textExists(name)
}

// names returns every known pin: default first, then alphabetical.
func (s Store) names(c *Config) []string {
	set := map[string]bool{}
	for n := range c.Pins {
		set[n] = true
	}
	files, _ := filepath.Glob(filepath.Join(s.pinsDir(), "*.txt"))
	for _, f := range files {
		n := strings.TrimSuffix(filepath.Base(f), ".txt")
		if validateName(n) == nil {
			set[n] = true
		}
	}
	delete(set, defaultPin)
	out := make([]string, 0, len(set)+1)
	for n := range set {
		out = append(out, n)
	}
	sort.Strings(out)
	return append([]string{defaultPin}, out...)
}

// pins records a pin with no last-shown entry as shown at now (unless now is 0),
// so newly created pins wait for their schedule instead of popping on the next prompt.
func (s Store) pins(c *Config, st *State, now int64) ([]pinView, error) {
	var out []pinView
	for _, n := range s.names(c) {
		if _, ok := st.LastShown[n]; !ok && now != 0 {
			st.LastShown[n] = now
		}
		text, err := s.readText(n)
		if err != nil {
			return nil, err
		}
		out = append(out, pinView{name: n, eff: c.Effective(n), text: text, last: st.LastShown[n]})
	}
	return out, nil
}

// assignColors gives every pin with no color at any layer a random one, persisted
// as its override, so a pin keeps the same color across runs.
func (s Store) assignColors(c *Config) bool {
	changed := false
	for _, n := range s.names(c) {
		if _, set := c.Effective(n).src["color"]; set {
			continue
		}
		ps := c.Pins[n]
		ps.Color = ptr(pinColors[rand.IntN(len(pinColors))])
		c.Pins[n] = ps
		changed = true
	}
	return changed
}

// transact serialises read-modify-write across terminals so two tabs hitting
// the same due pin don't both display it, and always rewrites next afterwards.
func (s Store) transact(now int64, fn func(*Config, *State) error) error {
	if err := os.MkdirAll(s.stateDir, 0o755); err != nil {
		return err
	}
	lock, err := os.OpenFile(filepath.Join(s.stateDir, "lock"), os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	c, err := s.loadConfig()
	if err != nil {
		return err
	}
	st, err := s.loadState()
	if err != nil {
		return err
	}
	colored := s.assignColors(c)
	if err := fn(c, st); err != nil {
		return err
	}
	if s.assignColors(c) || colored {
		if err := s.saveConfig(c); err != nil {
			return err
		}
	}
	pins, err := s.pins(c, st, now)
	if err != nil {
		return err
	}
	if err := s.saveState(st); err != nil {
		return err
	}
	return s.writeNext(nextDue(pins))
}
