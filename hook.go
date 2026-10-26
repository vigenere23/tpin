package main

import (
	"os"
	"strings"
)

func binaryPath() string {
	p, err := os.Executable()
	if err != nil {
		return "tpin"
	}
	return p
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

const zshHook = `# tpin zsh hook
if [[ -o interactive ]] && (( ! ${+_tpin_hooked} )); then
  typeset -g _tpin_hooked=1 _tpin_bin=__BIN__
  typeset -gi _tpin_skip=1
  zmodload zsh/datetime
  autoload -Uz add-zsh-hook

  _tpin_run() {
    [[ -x $_tpin_bin ]] || _tpin_bin=${commands[tpin]:-tpin}
    $_tpin_bin _hook $1 2>/dev/null
  }

  # Runs every prompt, so it must not fork: only builtins and a redirection.
  # next is re-read each time (not cached) to pick up config changes and
  # pins already shown from other terminals.
  _tpin_precmd() {
    if (( _tpin_skip )); then
      _tpin_skip=0
      return 0
    fi
    local dir=$XDG_STATE_HOME next
    [[ $dir == /* ]] || dir=$HOME/.local/state
    { read -r next < $dir/tpin/next } 2>/dev/null || return 0
    [[ $next == <-> ]] && (( EPOCHSECONDS >= next )) && _tpin_run command
    return 0
  }

  add-zsh-hook precmd _tpin_precmd
  _tpin_run open
fi
`

func zshInit(bin string) string {
	return strings.Replace(zshHook, "__BIN__", shellQuote(bin), 1)
}
