#!/bin/sh
set -eu

bin_dir="${BIN_DIR:-$HOME/.local/bin}"

rm -f "$bin_dir/tpin"
echo "removed $bin_dir/tpin"

if [ "${1:-}" = "--purge" ]; then
  rm -rf "${XDG_CONFIG_HOME:-$HOME/.config}/tpin" "${XDG_STATE_HOME:-$HOME/.local/state}/tpin"
  echo "removed pins, config and state"
fi

echo 'remember to drop the `eval "$(tpin init zsh)"` line from ~/.zshrc'
