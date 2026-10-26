package main

import (
	"bytes"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/BurntSushi/toml"
)

// Fixed zone so cron tests and the built-in "0 8 * * *" don't depend on the machine.
func TestMain(m *testing.M) {
	time.Local = time.FixedZone("test", -5*3600)
	os.Exit(m.Run())
}

func TestParseDuration(t *testing.T) {
	ok := map[string]time.Duration{
		"45m":     45 * time.Minute,
		"15min":   15 * time.Minute,
		"2mins":   2 * time.Minute,
		"1h30min": 90 * time.Minute,
		"2h":      2 * time.Hour,
		"2d":      48 * time.Hour,
		"1d12h":   36 * time.Hour,
		"1.5d":    36 * time.Hour,
		" 3H ":    3 * time.Hour,
		"0s":      0,
	}
	for in, want := range ok {
		got, err := parseDuration(in)
		if err != nil || got != want {
			t.Errorf("parseDuration(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
	for _, in := range []string{"", "30", "abc", "-1h", "d", "xd", "1d-2h", "1w"} {
		if _, err := parseDuration(in); err == nil {
			t.Errorf("parseDuration(%q) should fail", in)
		}
	}
}

func TestFormatDurationRoundTrip(t *testing.T) {
	cases := map[time.Duration]string{
		time.Hour:        "1h",
		45 * time.Minute: "45m",
		36 * time.Hour:   "1d12h",
		48 * time.Hour:   "2d",
		90 * time.Second: "1m30s",
	}
	for d, want := range cases {
		if got := formatDuration(d); got != want {
			t.Errorf("formatDuration(%v) = %q, want %q", d, got, want)
		}
		if back, err := parseDuration(want); err != nil || back != d {
			t.Errorf("parseDuration(formatDuration(%v)) = %v, %v", d, back, err)
		}
	}
}

func TestParseEntry(t *testing.T) {
	ok := map[string]string{
		"15min":            "15m",
		"@every 2h":        "2h",
		" @every  1d12h ":  "1d12h",
		"90m":              "1h30m",
		"1D":               "1d",
		"0 8 * * *":        "0 8 * * *",
		"  0  8 * *  1-5 ": "0 8 * * 1-5",
		"*/15 9-17 * * *":  "*/15 9-17 * * *",
		"@daily":           "@daily",
		"@hourly":          "@hourly",
		"@weekly":          "@weekly",
	}
	for in, want := range ok {
		e, err := parseEntry(in)
		if err != nil || e.spec != want {
			t.Errorf("parseEntry(%q) = %q, %v; want %q", in, e.spec, err, want)
		}
	}
	for _, in := range []string{"soon", "0", "500ms", "-1h", "@every", "@every soon", "0 8 * *", "0 25 * * *", "@sometimes", "0 0 8 * * *", "off"} {
		_, err := parseEntry(in)
		if err == nil {
			t.Errorf("parseEntry(%q) should fail", in)
		} else if !strings.Contains(err.Error(), strings.Join(strings.Fields(in), " ")) {
			t.Errorf("parseEntry(%q) error does not name the entry: %v", in, err)
		}
	}
}

func TestParseSchedule(t *testing.T) {
	for _, off := range []string{"", "  ", "off", "OFF"} {
		s, err := parseSchedule(off)
		if err != nil || s == nil || len(s) != 0 || formatSchedule(s) != "off" {
			t.Errorf("parseSchedule(%q) = %v, %v", off, s, err)
		}
	}
	s, err := parseSchedule(" 15min ;@every 2h;  0 8 * * 1-5 ;")
	if err != nil || formatSchedule(s) != "15m; 2h; 0 8 * * 1-5" {
		t.Errorf("parseSchedule = %q, %v", formatSchedule(s), err)
	}
	if _, err := parseSchedule("1h; 0 8 * *; 2h"); err == nil || !strings.Contains(err.Error(), `"0 8 * *"`) {
		t.Errorf("bad entry error = %v", err)
	}
	if _, err := parseSchedule("1h; off"); err == nil {
		t.Error("off accepted as an entry")
	}
}

func TestInheritance(t *testing.T) {
	c := &Config{
		Defaults: Settings{Schedule: ptr(mustSchedule("30m")), OnOpen: ptr(false)},
		Pins: map[string]Settings{
			"work": {Schedule: ptr(mustSchedule("2h; @daily")), Enabled: ptr(false)},
		},
	}
	w := c.Effective("work")
	if formatSchedule(w.Schedule) != "2h; @daily" || w.OnOpen || !w.OnCommand || w.Enabled {
		t.Errorf("work effective = %+v", w)
	}
	wantSrc := map[string]source{"schedule": srcPin, "on-open": srcDefaults, "on-command": srcBuiltin, "enabled": srcPin}
	for k, s := range wantSrc {
		if w.src[k] != s {
			t.Errorf("work src[%s] = %v, want %v", k, w.src[k], s)
		}
	}

	d := c.Effective(defaultPin)
	if formatSchedule(d.Schedule) != "30m" || d.OnOpen || !d.OnCommand || !d.Enabled {
		t.Errorf("default effective = %+v", d)
	}

	empty := (&Config{}).Effective("x")
	if empty.value("schedule") != "1h; 0 8 * * *" || empty.OnOpen || !empty.OnCommand || !empty.Enabled {
		t.Errorf("built-ins = %+v", empty)
	}

	// An explicit off/false override must win over a non-empty/true parent.
	c.Pins["quiet"] = Settings{Schedule: ptr(mustSchedule("off")), OnCommand: ptr(false)}
	q := c.Effective("quiet")
	if len(q.Schedule) != 0 || q.OnCommand || q.src["schedule"] != srcPin || q.value("schedule") != "off" {
		t.Errorf("quiet effective = %+v", q)
	}
}

func TestSetUnset(t *testing.T) {
	var s Settings
	for k, v := range map[string]string{"schedule": "2d; @every 15min", "on-open": "no", "on-command": "yes", "enabled": "off", "color": "Cyan"} {
		if err := s.set(k, v); err != nil {
			t.Fatal(err)
		}
	}
	e := effective(builtin, s)
	if e.value("schedule") != "2d; 15m" || e.OnOpen || !e.OnCommand || e.Enabled || e.Color != "cyan" {
		t.Errorf("after set: %+v", e)
	}
	if err := s.set("enabled", "maybe"); err == nil {
		t.Error("invalid bool accepted")
	}
	if err := s.set("color", "purple"); err == nil {
		t.Error("invalid color accepted")
	}
	if err := s.set("schedule", "1h; nope"); err == nil || e.value("schedule") != "2d; 15m" {
		t.Errorf("invalid schedule accepted: %v", err)
	}
	for _, k := range configKeys {
		s.unset(k)
	}
	if s != (Settings{}) {
		t.Errorf("after unset: %+v", s)
	}
}

func TestConfigTOMLRoundTrip(t *testing.T) {
	c := &Config{
		Defaults: Settings{Schedule: ptr(mustSchedule("@every 45min; 0 8 * * 1-5"))},
		Pins:     map[string]Settings{"work": {OnOpen: ptr(false)}, "bare": {}, "quiet": {Schedule: ptr(mustSchedule("off"))}},
	}
	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(c); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, `schedule = ["45m", "0 8 * * 1-5"]`) || !strings.Contains(out, "schedule = []") ||
		strings.Contains(out, "@every") || strings.Contains(out, "enabled") {
		t.Errorf("unexpected encoding:\n%s", out)
	}
	var back Config
	if _, err := toml.Decode(out, &back); err != nil {
		t.Fatal(err)
	}
	if back.Effective("work").OnOpen || back.Effective("work").value("schedule") != "45m; 0 8 * * 1-5" {
		t.Errorf("round trip lost data: %+v", back)
	}
	if q := back.Effective("quiet"); q.src["schedule"] != srcPin || len(q.Schedule) != 0 {
		t.Errorf("empty schedule lost: %+v", q)
	}
	if _, ok := back.Pins["bare"]; !ok {
		t.Error("empty pin section dropped")
	}

	var hand Config
	if _, err := toml.Decode("[defaults]\nschedule = [\"@every 2h\", \"15min\"]\n", &hand); err != nil || hand.DefaultsEffective().value("schedule") != "2h; 15m" {
		t.Errorf("hand-written schedule: %v %+v", err, hand.DefaultsEffective())
	}
	if _, err := toml.Decode("[defaults]\nschedule = [\"1h\", \"soon\"]\n", &hand); err == nil || !strings.Contains(err.Error(), `"soon"`) {
		t.Errorf("bad schedule entry: %v", err)
	}
}

func TestLoadConfigRejectsBadSchedule(t *testing.T) {
	dir := t.TempDir()
	s := Store{cfgDir: dir + "/config", stateDir: dir + "/state"}
	if err := writeFileAtomic(s.configPath(), []byte("[pins.work]\nschedule = [\"0 8 * *\"]\n")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.loadConfig(); err == nil || !strings.Contains(err.Error(), `"0 8 * *"`) {
		t.Errorf("loadConfig error = %v", err)
	}
}

func pin(schedule string, last int64, opts ...func(*pinView)) pinView {
	p := pinView{
		name: "p",
		text: "hello\n",
		last: last,
		eff:  Effective{Schedule: mustSchedule(schedule), OnOpen: true, OnCommand: true, Enabled: true},
	}
	for _, o := range opts {
		o(&p)
	}
	return p
}

func TestDue(t *testing.T) {
	const now = 1_000_000
	disabled := func(p *pinView) { p.eff.Enabled = false }
	noCmd := func(p *pinView) { p.eff.OnCommand = false }
	blank := func(p *pinView) { p.text = "  \n" }

	cases := []struct {
		name string
		p    pinView
		due  bool
	}{
		{"elapsed", pin("1h", now-3600), true},
		{"not yet", pin("1h", now-3599), false},
		{"schedule off", pin("off", now-99999), false},
		{"disabled", pin("1h", now-99999, disabled), false},
		{"on_command off", pin("1h", now-99999, noCmd), false},
		{"blank text", pin("1h", now-99999, blank), false},
	}
	for _, c := range cases {
		if got := isDue(c.p, now); got != c.due {
			t.Errorf("%s: isDue = %v, want %v", c.name, got, c.due)
		}
	}
}

func TestCronDue(t *testing.T) {
	at := func(d, h, m int) int64 { return time.Date(2026, 3, d, h, m, 0, 0, time.Local).Unix() }
	cases := []struct {
		name      string
		last, now int64
		due       bool
	}{
		{"before 8:00", at(10, 7, 0), at(10, 7, 59), false},
		{"after 8:00", at(10, 7, 0), at(10, 8, 1), true},
		{"exactly 8:00", at(10, 7, 0), at(10, 8, 0), true},
		{"shown after 8:00", at(10, 8, 5), at(11, 7, 59), false},
		{"next day 8:00", at(10, 8, 5), at(11, 8, 0), true},
		{"missed while asleep", at(10, 18, 0), at(12, 14, 0), true},
	}
	for _, c := range cases {
		if got := isDue(pin("0 8 * * *", c.last), c.now); got != c.due {
			t.Errorf("%s: isDue = %v, want %v", c.name, got, c.due)
		}
	}
	if got, want := dueAt(pin("0 8 * * *", at(10, 8, 5))), at(11, 8, 0); got != want {
		t.Errorf("dueAt = %d, want %d", got, want)
	}
}

func TestMixedScheduleTakesMinimum(t *testing.T) {
	at := func(h, m int) int64 { return time.Date(2026, 3, 10, h, m, 0, 0, time.Local).Unix() }
	if got, want := dueAt(pin("1h; 0 8 * * *", at(7, 30))), at(8, 0); got != want {
		t.Errorf("cron first: dueAt = %d, want %d", got, want)
	}
	if got, want := dueAt(pin("0 8 * * *; 1h", at(9, 0))), at(10, 0); got != want {
		t.Errorf("interval first: dueAt = %d, want %d", got, want)
	}
	if got := dueAt(pin("off", at(9, 0))); got != nextNever {
		t.Errorf("empty schedule: dueAt = %d", got)
	}
}

func TestNextDue(t *testing.T) {
	const now = 1_000_000
	if got := nextDue(nil); got != nextNever {
		t.Errorf("no pins: %d", got)
	}
	ineligible := []pinView{
		pin("off", now),
		pin("1h", now, func(p *pinView) { p.eff.Enabled = false }),
		pin("1h", now, func(p *pinView) { p.text = "" }),
	}
	if got := nextDue(ineligible); got != nextNever {
		t.Errorf("ineligible pins: %d", got)
	}
	pins := append(ineligible, pin("1h", now), pin("10m", now-60), pin("2h", now-7000))
	if got, want := nextDue(pins), int64(now+200); got != want {
		t.Errorf("nextDue = %d, want %d", got, want)
	}
}

func TestParseConfigArgs(t *testing.T) {
	cases := []struct {
		args []string
		want configReq
	}{
		{nil, configReq{name: "default"}},
		{[]string{"work"}, configReq{name: "work"}},
		{[]string{"schedule", "30m"}, configReq{name: "default", key: "schedule", value: "30m", write: true}},
		{[]string{"work", "schedule", "30m"}, configReq{name: "work", key: "schedule", value: "30m", write: true}},
		{[]string{"work", "on_open", "off"}, configReq{name: "work", key: "on-open", value: "off", write: true}},
		{[]string{"enabled", "--unset"}, configReq{name: "default", key: "enabled", unset: true, write: true}},
		{[]string{"work", "schedule"}, configReq{name: "work", key: "schedule"}},
		{[]string{"--defaults"}, configReq{defaults: true}},
		{[]string{"--defaults", "on-command", "no"}, configReq{defaults: true, key: "on-command", value: "no", write: true}},
	}
	for _, c := range cases {
		got, err := parseConfigArgs(c.args)
		if err != nil || got != c.want {
			t.Errorf("parseConfigArgs(%q) = %+v, %v; want %+v", c.args, got, err, c.want)
		}
	}
	for _, bad := range [][]string{
		{"work", "colour", "red"},
		{"bad name!"},
		{"work", "schedule", "1h", "extra"},
		{"--defaults", "work", "schedule", "1h"},
	} {
		if _, err := parseConfigArgs(bad); err == nil {
			t.Errorf("parseConfigArgs(%q) should fail", bad)
		}
	}
}

func TestParseSetArgs(t *testing.T) {
	if n, txt, err := parseSetArgs([]string{"hello"}); err != nil || n != "default" || txt != "hello" {
		t.Errorf("1 arg: %q %q %v", n, txt, err)
	}
	if n, txt, err := parseSetArgs([]string{"work", "hi"}); err != nil || n != "work" || txt != "hi" {
		t.Errorf("2 args: %q %q %v", n, txt, err)
	}
	for _, bad := range [][]string{nil, {"a", "b", "c"}, {"no/slash", "x"}, {"schedule", "x"}} {
		if _, _, err := parseSetArgs(bad); err == nil {
			t.Errorf("parseSetArgs(%q) should fail", bad)
		}
	}
}

func TestValidateName(t *testing.T) {
	for _, ok := range []string{"default", "work", "a_b-C9"} {
		if err := validateName(ok); err != nil {
			t.Errorf("%q: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "a b", "../x", "x.txt", "schedule", "on_open", "Enabled", "color"} {
		if err := validateName(bad); err == nil {
			t.Errorf("%q should be invalid", bad)
		}
	}
}

func TestDisplayBoard(t *testing.T) {
	var buf bytes.Buffer
	a := &App{out: &buf, width: 80, now: func() time.Time { return time.Unix(42, 0) }}
	st := &State{LastShown: map[string]int64{}}

	a.display(st, []pinView{{name: "default", text: "one\n\n\n"}, {name: "work", text: "two\n"}})
	want := "\n" +
		"╭─────╮ ╭─ work ─╮\n" +
		"│ one │ │ two    │\n" +
		"╰─────╯ ╰────────╯\n" +
		"\n"
	if buf.String() != want {
		t.Errorf("board:\n%s\nwant:\n%s", buf.String(), want)
	}
	if st.LastShown["work"] != 42 || st.LastShown["default"] != 42 {
		t.Errorf("not marked shown: %v", st.LastShown)
	}
}

func TestRenderListHangingIndent(t *testing.T) {
	got := renderBoard([]pinView{{name: "default", text: "  - alpha beta gamma delta\n"}}, 16, false)
	want := "" +
		"╭───────────╮\n" +
		"│   ◦ alpha │\n" +
		"│     beta  │\n" +
		"│     gamma │\n" +
		"│     delta │\n" +
		"╰───────────╯\n"
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
	got = renderBoard([]pinView{{name: "default", text: "12) ab cd\nxxxxxxxxxxxxxxx"}}, 12, false)
	want = "" +
		"╭──────────╮\n" +
		"│ 12) ab   │\n" +
		"│     cd   │\n" +
		"│ xxxxxxxx │\n" +
		"│ xxxxxxx  │\n" +
		"╰──────────╯\n"
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestRenderBold(t *testing.T) {
	blue := Effective{Color: "blue"}
	got := renderBoard([]pinView{{name: "default", eff: blue, text: "# Title\nx **aa bb** y\n"}}, 10, true)
	b := "\x1b[34m│\x1b[0m"
	want := "" +
		"\x1b[34m╭───────╮\x1b[0m\n" +
		b + " \x1b[1;34mTitle\x1b[0m " + b + "\n" +
		b + " x \x1b[1maa\x1b[0m  " + b + "\n" +
		b + " \x1b[1mbb\x1b[0m y  " + b + "\n" +
		"\x1b[34m╰───────╯\x1b[0m\n"
	if got != want {
		t.Errorf("got:\n%q\nwant:\n%q", got, want)
	}
	uncolored := renderBoard([]pinView{{name: "default", eff: Effective{Color: "none"}, text: "# T\n"}}, 80, true)
	if !strings.Contains(uncolored, "\x1b[1;4mT\x1b[0m") {
		t.Errorf("uncolored heading: %q", uncolored)
	}
	plain := renderBoard([]pinView{{name: "work", eff: blue, text: "## **x** y\n"}}, 80, false)
	if strings.Contains(plain, "\x1b") || !strings.Contains(plain, "│ x y    │") || strings.Contains(plain, "#") {
		t.Errorf("plain:\n%s", plain)
	}
}

func TestRenderTitleAndFlow(t *testing.T) {
	pins := []pinView{
		{name: "default", text: "a\n"},
		{name: "work", text: "b\nc\n"},
		{name: "x", text: "wide 界\n"},
	}
	got := renderBoard(pins, 22, false)
	want := "" +
		"╭───╮ ╭─ work ─╮\n" +
		"│ a │ │ b      │\n" +
		"╰───╯ │ c      │\n" +
		"      ╰────────╯\n" +
		"╭─ x ─────╮\n" +
		"│ wide 界 │\n" +
		"╰─────────╯\n"
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestColorAssignedOnce(t *testing.T) {
	dir := t.TempDir()
	s := Store{cfgDir: dir + "/config", stateDir: dir + "/state"}
	err := s.transact(1, func(c *Config, _ *State) error {
		c.Pins["mine"] = Settings{Color: ptr("none")}
		if err := writeFileAtomic(s.pinPath("mine"), []byte("x\n")); err != nil {
			return err
		}
		return writeFileAtomic(s.pinPath("work"), []byte("w\n"))
	})
	if err != nil {
		t.Fatal(err)
	}
	c, _ := s.loadConfig()
	first := c.Effective("work").Color
	if !slices.Contains(pinColors, first) || !slices.Contains(pinColors, c.Effective(defaultPin).Color) {
		t.Errorf("no random color: %+v", c.Pins)
	}
	if c.Effective("mine").Color != "none" {
		t.Errorf("explicit color overridden: %+v", c.Pins["mine"])
	}
	for range 5 {
		if err := s.transact(2, func(*Config, *State) error { return nil }); err != nil {
			t.Fatal(err)
		}
	}
	c, _ = s.loadConfig()
	if c.Effective("work").Color != first {
		t.Errorf("color changed: %q -> %q", first, c.Effective("work").Color)
	}

	s2 := Store{cfgDir: dir + "/config2", stateDir: dir + "/state2"}
	if err := s2.saveConfig(&Config{Defaults: Settings{Color: ptr("cyan")}}); err != nil {
		t.Fatal(err)
	}
	if err := s2.transact(1, func(*Config, *State) error { return nil }); err != nil {
		t.Fatal(err)
	}
	c, _ = s2.loadConfig()
	if _, ok := c.Pins[defaultPin]; ok || c.Effective(defaultPin).Color != "cyan" {
		t.Errorf("defaults color not inherited: %+v", c.Pins)
	}
}

func TestNewPinsCountAsJustShown(t *testing.T) {
	dir := t.TempDir()
	s := Store{cfgDir: dir + "/config", stateDir: dir + "/state"}
	if err := writeFileAtomic(s.pinPath("old"), []byte("old\n")); err != nil {
		t.Fatal(err)
	}
	if err := s.saveState(&State{LastShown: map[string]int64{"old": 100}}); err != nil {
		t.Fatal(err)
	}
	const now = 5000
	err := s.transact(now, func(*Config, *State) error {
		return writeFileAtomic(s.pinPath("work"), []byte("work\n"))
	})
	if err != nil {
		t.Fatal(err)
	}
	st, err := s.loadState()
	if err != nil {
		t.Fatal(err)
	}
	if st.LastShown["work"] != now || st.LastShown["default"] != now || st.LastShown["old"] != 100 {
		t.Errorf("last shown = %v", st.LastShown)
	}
	b, err := os.ReadFile(s.nextPath())
	if err != nil {
		t.Fatal(err)
	}
	// old is overdue (100+3600 < now), so next stays at its due time; work waits a full hour.
	if got := strings.TrimSpace(string(b)); got != "3700" {
		t.Errorf("next = %s, want 3700", got)
	}
	c, _ := s.loadConfig()
	pins, _ := s.pins(c, st, now)
	for _, p := range pins {
		if p.name == "work" && isDue(p, now) {
			t.Error("new pin is due immediately")
		}
	}
}

func TestShowAllEmpty(t *testing.T) {
	dir := t.TempDir()
	var buf bytes.Buffer
	a := &App{
		store: Store{cfgDir: dir + "/config", stateDir: dir + "/state"},
		out:   &buf,
		now:   func() time.Time { return time.Unix(42, 0) },
	}
	if err := a.run(nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "No pins to show") {
		t.Errorf("empty output: %q", buf.String())
	}
	buf.Reset()
	if err := a.hook([]string{"open"}); err != nil {
		t.Fatal(err)
	}
	if buf.Len() != 0 {
		t.Errorf("hook printed %q", buf.String())
	}
}

func TestOpenHookShowsDuePins(t *testing.T) {
	dir := t.TempDir()
	var buf bytes.Buffer
	clock := time.Unix(1000, 0)
	a := &App{
		store: Store{cfgDir: dir + "/config", stateDir: dir + "/state"},
		out:   &buf,
		width: 80,
		now:   func() time.Time { return clock },
	}
	if err := a.set("work", "due soon"); err != nil {
		t.Fatal(err)
	}
	if err := a.hook([]string{"open"}); err != nil || buf.Len() != 0 {
		t.Fatalf("not yet due, got %q %v", buf.String(), err)
	}
	clock = clock.Add(2 * time.Hour)
	if err := a.hook([]string{"open"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "due soon") {
		t.Errorf("open hook skipped a due pin: %q", buf.String())
	}
}

func TestRenderBulletsAndStrike(t *testing.T) {
	got := renderBoard([]pinView{{name: "default", text: "- a\n  * b\n    + c\n1. n\n- ~~done~~ x\n"}}, 80, false)
	for _, want := range []string{"│ • a ", "│   ◦ b ", "│     ▪ c ", "│ 1. n ", "│ • ~~done~~ x "} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	colored := renderBoard([]pinView{{name: "default", text: "~~gone~~ **~~both~~** ~~open\n"}}, 80, true)
	if !strings.Contains(colored, "\x1b[2;9mgone\x1b[0m \x1b[1m\x1b[2;9mboth\x1b[0m ~~open") {
		t.Errorf("strike: %q", colored)
	}
}
