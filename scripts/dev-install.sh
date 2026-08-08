#!/bin/sh
set -eu

action=${1:-status}
if [ -n "${TD_REPO_ROOT:-}" ]; then
  repo_root=$(CDPATH= cd -- "$TD_REPO_ROOT" && pwd -P)
else
  repo_root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd -P)
fi
state_root=${TD_DEV_STATE:-"$HOME/.local/state/td/dev-installs"}
go_command=${TD_GO:-go}
zsh_command=${TD_ZSH:-/bin/zsh}

die() {
  printf 'td dev install: %s\n' "$*" >&2
  exit 1
}

require_brew() {
  command -v brew >/dev/null 2>&1 ||
    die "Homebrew is required for managed installs; use 'make install' for an unmanaged Go install"
}

brew_prefix() {
  require_brew
  if [ -n "${TD_BREW_PREFIX:-}" ]; then
    printf '%s\n' "$TD_BREW_PREFIX"
  else
    brew --prefix
  fi
}

active_bin_dir() {
  printf '%s/bin\n' "$(brew_prefix)"
}

resolved_path() {
  realpath -q "$1" 2>/dev/null
}

path_is_below() {
  candidate=$1
  root=$2
  [ "$candidate" = "$root" ] || case "$candidate" in
    "$root"/*) return 0 ;;
    *) return 1 ;;
  esac
}

formula_root() {
  brew --prefix td 2>/dev/null | while IFS= read -r prefix; do
    resolved_path "$prefix" || printf '%s\n' "$prefix"
  done
}

link_kind() {
  path=$1
  if [ ! -e "$path" ] && [ ! -L "$path" ]; then
    printf 'missing\n'
    return
  fi
  if [ ! -L "$path" ]; then
    printf 'other\n'
    return
  fi
  target=$(resolved_path "$path" || true)
  [ -n "$target" ] || {
    printf 'other\n'
    return
  }
  resolved_state=$(resolved_path "$state_root" || true)
  if [ -n "$resolved_state" ] && path_is_below "$target" "$resolved_state"; then
    printf 'local\n'
    return
  fi
  resolved_formula=$(formula_root || true)
  if [ -n "$resolved_formula" ] && path_is_below "$target" "$resolved_formula"; then
    printf 'homebrew\n'
    return
  fi
  printf 'other\n'
}

describe_link() {
  path=$1
  kind=$(link_kind "$path")
  raw=$(readlink "$path" 2>/dev/null || printf 'regular file')
  resolved=$(resolved_path "$path" || printf 'unresolved')
  printf 'link state: %s\n' "$kind"
  printf 'activation path: %s\n' "$path"
  printf 'raw target: %s\n' "$raw"
  printf 'resolved target: %s\n' "$resolved"
  if [ "$kind" = local ]; then
    metadata=$(dirname "$resolved")/metadata
    if [ -r "$metadata" ]; then
      printf 'artifact metadata:\n'
      sed 's/^/  /' "$metadata"
    fi
  fi
  if [ -x "$path" ]; then
    printf 'activation version: '
    "$path" --version 2>&1 || true
  fi
}

shell_probe() {
  label=$1
  shift
  printf '%s resolves:\n' "$label"
  output=$({ "$@"; } 2>&1 || true)
  if [ -n "$output" ]; then
    printf '%s\n' "$output" | sed 's/^/  /'
  else
    printf '  not found\n'
  fi
}

login_shell_probe() {
  label=$1
  option=$2
  printf '%s resolves:\n' "$label"
  output=$("$zsh_command" "$option" \
    'command -v td || exit 0; td --version' 2>/dev/null || true)
  if [ -n "$output" ]; then
    printf '%s\n' "$output" | sed 's/^/  /'
  else
    printf '  not found\n'
  fi
}

status() {
  bin_dir=$(active_bin_dir)
  printf 'managed command directory: %s\n' "$bin_dir"
  describe_link "$bin_dir/td"
  shell_probe 'current shell' sh -c 'command -v td || exit 0; td --version'
  if [ -x "$zsh_command" ]; then
    login_shell_probe 'interactive login shell' -lic
    login_shell_probe 'non-interactive login shell' -lc
  else
    printf 'interactive login shell resolves:\n  unavailable: %s\n' "$zsh_command"
    printf 'non-interactive login shell resolves:\n  unavailable: %s\n' "$zsh_command"
  fi
}

restore_previous() {
  bin_dir=$1
  previous_kind=$2
  previous_target=$3
  path=$bin_dir/td
  rm -f "$path"
  case "$previous_kind" in
    local)
      ln -s "$previous_target" "$path" || return 1
      ;;
    homebrew)
      brew link td >/dev/null || return 1
      ;;
    missing) ;;
    *) return 1 ;;
  esac
}

rollback_on_signal() {
  trap - EXIT HUP INT TERM
  [ -z "${rollback_staged:-}" ] || rm -f "$rollback_staged"
  if ! restore_previous "$rollback_bin_dir" "$rollback_previous_kind" \
    "$rollback_previous_target"; then
    printf 'td dev install: interrupted; previous installation could not be restored\n' >&2
  fi
  exit 1
}

install_local() {
  mode=$1
  git -C "$repo_root" rev-parse --git-dir >/dev/null 2>&1 ||
    die "$repo_root is not a git checkout"
  branch=$(git -C "$repo_root" branch --show-current)
  [ -n "$branch" ] || branch=detached
  common_dir=$(git -C "$repo_root" rev-parse --path-format=absolute --git-common-dir)
  canonical=false
  canonical_git=$(resolved_path "$repo_root/.git" || true)
  resolved_common=$(resolved_path "$common_dir" || true)
  [ -n "$canonical_git" ] && [ "$resolved_common" = "$canonical_git" ] && canonical=true
  if [ "$mode" = main ] && { [ "$canonical" != true ] || [ "$branch" != main ]; }; then
    die "install-local requires the canonical main checkout; use 'make install-worktree' to activate this checkout deliberately"
  fi

  require_brew
  commit=$(git -C "$repo_root" rev-parse HEAD)
  short_commit=$(git -C "$repo_root" rev-parse --short HEAD)
  dirty=false
  [ -z "$(git -C "$repo_root" status --porcelain --untracked-files=normal)" ] || dirty=true
  safe_branch=$(printf '%s' "$branch" | tr -cs 'A-Za-z0-9._-' '-')
  checkout_id=$(printf '%s' "$repo_root" | shasum -a 256 | cut -c1-12)
  dirty_suffix=
  [ "$dirty" = false ] || dirty_suffix=+dirty
  version=devel+$safe_branch.$short_commit$dirty_suffix
  build_id=$(date -u '+%Y%m%dT%H%M%SZ')-$$
  destination=$state_root/$safe_branch-$checkout_id-$short_commit-$build_id
  temporary=$state_root/.build-$checkout_id-$$

  mkdir -p "$state_root"
  rm -rf "$temporary"
  mkdir "$temporary"
  trap 'rm -rf "$temporary"' EXIT HUP INT TERM
  (
    cd "$repo_root"
    GOWORK=off "$go_command" build -ldflags "-s -w -X main.Version=$version" \
      -o "$temporary/td" .
  )
  {
    printf 'source=%s\n' "$repo_root"
    printf 'revision=%s\n' "$commit"
    printf 'branch=%s\n' "$branch"
    printf 'dirty=%s\n' "$dirty"
    printf 'built_at=%s\n' "$(date -u '+%Y-%m-%dT%H:%M:%SZ')"
    printf 'version=%s\n' "$version"
  } >"$temporary/metadata"
  mv "$temporary" "$destination"
  trap - EXIT HUP INT TERM

  bin_dir=$(active_bin_dir)
  mkdir -p "$bin_dir"
  path=$bin_dir/td
  previous_kind=$(link_kind "$path")
  [ "$previous_kind" != other ] ||
    die "$path is not managed by this repository or Homebrew; refusing to replace it"
  previous_target=$(readlink "$path" 2>/dev/null || true)
  staged=$bin_dir/.td-dev-$$
  ln -s "$destination/td" "$staged"
  rollback_bin_dir=$bin_dir
  rollback_previous_kind=$previous_kind
  rollback_previous_target=$previous_target
  rollback_staged=$staged
  trap rollback_on_signal HUP INT TERM
  trap 'rm -f "$rollback_staged"' EXIT

  if [ "$previous_kind" = homebrew ]; then
    if ! brew unlink td >/dev/null; then
      rm -f "$staged"
      trap - EXIT HUP INT TERM
      die "Homebrew unlink failed; previous installation was left active"
    fi
  fi
  if ! mv "$staged" "$path" || [ ! -x "$path" ]; then
    rm -f "$staged"
    restore_previous "$bin_dir" "$previous_kind" "$previous_target" ||
      die "activation failed and the previous installation could not be restored"
    trap - EXIT HUP INT TERM
    die "activation failed; restored the previous installation"
  fi
  trap - EXIT HUP INT TERM
  printf 'activated local td build from %s\n' "$repo_root"
  status
}

use_homebrew() {
  require_brew
  brew list --versions td >/dev/null 2>&1 ||
    die "the td formula is not installed; run 'brew install marcus/tap/td'"
  bin_dir=$(active_bin_dir)
  mkdir -p "$bin_dir"
  path=$bin_dir/td
  previous_kind=$(link_kind "$path")
  case "$previous_kind" in
    homebrew)
      printf 'Homebrew td is already active\n'
      status
      return
      ;;
    local|missing) ;;
    other) die "$path is not managed by this repository or Homebrew; refusing to replace it" ;;
  esac
  previous_target=$(readlink "$path" 2>/dev/null || true)
  rollback_bin_dir=$bin_dir
  rollback_previous_kind=$previous_kind
  rollback_previous_target=$previous_target
  rollback_staged=
  trap rollback_on_signal HUP INT TERM
  [ "$previous_kind" != local ] || rm -f "$path"
  if ! brew link td >/dev/null || [ "$(link_kind "$path")" != homebrew ] || [ ! -x "$path" ]; then
    trap - HUP INT TERM
    brew unlink td >/dev/null 2>&1 || true
    restore_previous "$bin_dir" "$previous_kind" "$previous_target" ||
      die "Homebrew relinking failed and the previous installation could not be restored"
    die "Homebrew relinking failed; restored the previous installation"
  fi
  trap - HUP INT TERM
  printf 'activated Homebrew td\n'
  status
}

case "$action" in
  install-local) install_local main ;;
  install-worktree) install_local worktree ;;
  use-homebrew) use_homebrew ;;
  status) status ;;
  *) die "usage: $0 {install-local|install-worktree|use-homebrew|status}" ;;
esac
