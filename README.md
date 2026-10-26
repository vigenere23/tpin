# tpin

Pin plain-text messages to your terminal. Each pin pops up after a command
finishes once its schedule says it is due (see below); being shown in any
terminal resets it everywhere.
`tpin` shows them all at once; a pin with `on-open` also shows whenever a
terminal opens.

## Install

```sh
./install.sh                 # builds and copies to ~/.local/bin (override with BIN_DIR=...)
./uninstall.sh [--purge]     # removes the binary; --purge also deletes pins, config and state
```

Then add to `~/.zshrc`:

```sh
eval "$(tpin init zsh)"
```

The hook embeds the absolute path of the binary that generated it (falling back
to `tpin` on `$PATH` if that file disappears). With Powerlevel10k instant prompt,
put the line *before* the instant-prompt block, or the open-time output will
trigger its console-output warning.

## Usage

```
tpin                              show all enabled pins now
tpin show [name]                  show one pin
tpin edit [name]                  edit a pin in $VISUAL/$EDITOR (creates it)
tpin set [name] "text"            set a pin's text ("" clears it)
tpin rm <name>                    remove a pin (default cannot be removed)
tpin ls                           list pins
tpin config [name]                show a pin's effective config
tpin config [name] <key> <value>  set a pin override
tpin config [name] <key> --unset  revert to the inherited value
tpin config --defaults [...]      show or change the shared defaults
tpin init zsh                     print the zsh hook
```

Without a name, commands target the `default` pin. Keys:

| key          | values                                   | built-in |
|--------------|------------------------------------------|----------|
| `schedule`   | entries separated by `;`, each an interval (`45m`, `15min`, `2h`, `1d12h`, `@every 2h`) or a cron expression (`0 8 * * 1-5`, `@daily`); `off` never re-shows after commands | `1h; 0 8 * * *` |
| `on-open`    | `on`/`off`, `true`/`false`, `yes`/`no`   | `off`    |
| `on-command` | same                                     | `on`     |
| `enabled`    | same                                     | `on`     |
| `color`      | `yellow`, `green`, `blue`, `magenta`, `cyan`, `red`, `none` | random |

Each pin inherits from `[defaults]`, which inherits from the built-ins. Any
display (terminal open, after a command, or `tpin`/`tpin show`) counts as
"shown". Empty pins are never displayed. A newly created pin counts as just
shown: it first appears after a command once its schedule makes it due, or at
the next terminal open if `on-open` is on.

A pin is due after a command as soon as any one of its schedule entries is:

- An **interval** counts from when the pin was last shown, so `2h` means "two
  hours after the last popup". `@every 2h` is accepted and saved as `2h`.
- A **cron expression** (5 fields, or `@hourly`/`@daily`/`@weekly`/...) is
  read in the system's local time and fires at clock times: `0 8 * * 1-5` is
  due at 8:00 on weekdays, and `*/30 * * * *` at :00 and :30 rather than 30
  minutes after the last popup. A cron time missed while no terminal was in
  use (e.g. 8:00 with the laptop closed) shows once at the first prompt
  afterwards.

Any display resets every entry, so a pin that just popped up for its interval
will not pop up again for a cron time that already passed. Set it with one
quoted value, `tpin config work schedule "1h; 0 8 * * 1-5"`; in `config.toml`
it is a list, `schedule = ["1h", "0 8 * * 1-5"]`.

Pins are drawn as post-it notes side by side, wrapping onto new rows to fit the
terminal (each note at most 60 columns wide). Text wraps at spaces, list items
keep a hanging indent, `-`/`*`/`+` bullets render as `•`, `◦`, `▪` by nesting
depth, `**bold**` renders as bold, `~~text~~` is struck through and dimmed (the `~~` stay
when colors are off), and markdown headings
(`# ...`) render bold in the note's color (bold and underlined on a note with
no color). `color` tints the border, title and headings; a pin with no color at any layer gets
a random one saved as its override the first time it is seen (so `--unset`
rerolls it unless `[defaults]` sets one). Colors and bold are off under
`NO_COLOR` or when stdout is not a terminal.

## Files

| path | contents |
|------|----------|
| `${XDG_CONFIG_HOME:-~/.config}/tpin/config.toml` | `[defaults]` and `[pins.<name>]` overrides |
| `${XDG_CONFIG_HOME:-~/.config}/tpin/pins/<name>.txt` | pin text |
| `${XDG_STATE_HOME:-~/.local/state}/tpin/state.json` | last-shown time per pin |
| `${XDG_STATE_HOME:-~/.local/state}/tpin/next` | unix time the next pin becomes due (`9999999999` = never) |

The zsh `precmd` hook only reads `next` with a builtin and compares it to
`$EPOCHSECONDS`; it starts `tpin` only when something is actually due.
