#!/bin/sh
set -eu

bin_dir="${BIN_DIR:-$HOME/.local/bin}"
cd "$(dirname "$0")"

mkdir -p "$bin_dir"
go build -o tpin .
install -m 755 tpin "$bin_dir/tpin"
echo "installed $bin_dir/tpin"

case ":$PATH:" in *":$bin_dir:"*) ;; *) echo "warning: $bin_dir is not on PATH" ;; esac

if ! grep -qs 'tpin init zsh' "${ZDOTDIR:-$HOME}/.zshrc"; then
  echo
  echo "add this to ${ZDOTDIR:-$HOME}/.zshrc:"
  echo '  eval "$(tpin init zsh)"'
fi
