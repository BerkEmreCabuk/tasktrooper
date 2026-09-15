#!/usr/bin/env bash
#
# Install TaskTrooper on this Mac.
#
#   curl -fsSL https://raw.githubusercontent.com/makifbaysal/tasktrooper/main/scripts/install.sh | bash
#
# Downloads the latest release's universal .dmg, copies TaskTrooper.app into
# /Applications, and tells you what else the app needs. It never installs
# anything else for you — the commands are printed and you run them.
set -euo pipefail

REPO="makifbaysal/tasktrooper"
APP="TaskTrooper.app"
APPS="${TASKTROOPER_INSTALL_DIR:-/Applications}"
ASSUME_YES=false

usage() {
  cat <<'USAGE'
install.sh — install TaskTrooper into /Applications

  curl -fsSL https://raw.githubusercontent.com/makifbaysal/tasktrooper/main/scripts/install.sh | bash

  -y, --yes   replace an existing install and answer every prompt with yes
  -h, --help  this

  TASKTROOPER_INSTALL_DIR=~/Applications  install somewhere other than /Applications
USAGE
}

while [ $# -gt 0 ]; do
  case "$1" in
    -y | --yes) ASSUME_YES=true ;;
    -h | --help)
      usage
      exit 0
      ;;
    *)
      echo "install.sh: unknown option '$1'" >&2
      exit 2
      ;;
  esac
  shift
done

say() { printf '%s\n' "$*"; }
die() {
  printf 'error: %s\n' "$*" >&2
  exit 1
}

# Piped into bash, stdin is the script itself — a question has to be asked on the
# terminal or it silently eats the rest of this file. Returns 1 when there is no
# terminal to ask on, so every caller decides what an unanswerable question means.
ask() {
  $ASSUME_YES && return 0
  # /dev/tty can exist and be readable with no controlling terminal behind it
  # (cron, CI, a detached shell); only opening it tells the two apart.
  { : < /dev/tty; } 2> /dev/null || return 1
  printf '%s [y/N] ' "$1" > /dev/tty
  local answer
  read -r answer < /dev/tty
  case "$answer" in y | Y | yes | YES) return 0 ;; *) return 1 ;; esac
}

[ "$(uname -s)" = "Darwin" ] || die "TaskTrooper is a macOS app."

tmp="$(mktemp -d "${TMPDIR:-/tmp}/tasktrooper-install.XXXXXX")"
mnt=""
cleanup() {
  if [ -n "$mnt" ]; then hdiutil detach "$mnt" -quiet 2> /dev/null || true; fi
  rm -rf "$tmp"
}
trap cleanup EXIT

# --- fetch ------------------------------------------------------------------
# `gh` first, and not as a convenience: it is authenticated, so it can see the
# release while this repository is still private. The public API path is the one
# that works for everybody else, with nothing installed.
dmg=""
if command -v gh > /dev/null 2>&1 && gh auth status > /dev/null 2>&1; then
  tag="$(gh release view --repo "$REPO" --json tagName --jq .tagName)"
  say "Downloading TaskTrooper $tag..."
  gh release download --repo "$REPO" "$tag" --pattern "*-universal.dmg" --dir "$tmp"
  dmg="$(find "$tmp" -maxdepth 1 -name "*-universal.dmg" | head -1)"
else
  say "Looking up the latest release..."
  url="$(curl -fsSL "https://api.github.com/repos/$REPO/releases/latest" \
    | grep -o '"browser_download_url": *"[^"]*-universal\.dmg"' \
    | head -1 | sed 's/.*"\(https[^"]*\)"/\1/')"
  [ -n "$url" ] || die "No universal .dmg on the latest release of $REPO. While the repository is private, install the GitHub CLI and run 'gh auth login' first."
  dmg="$tmp/${url##*/}"
  say "Downloading ${url##*/}..."
  curl -fL --progress-bar -o "$dmg" "$url"
fi
[ -n "$dmg" ] && [ -f "$dmg" ] || die "The download produced no .dmg."

# --- install ----------------------------------------------------------------
mnt="$tmp/mnt"
mkdir -p "$mnt"
hdiutil attach "$dmg" -nobrowse -quiet -mountpoint "$mnt"
[ -d "$mnt/$APP" ] || die "$APP is not in that disk image."

sudo=""
if [ ! -w "$APPS" ]; then
  say "$APPS is not writable by you, so the copy runs under sudo."
  sudo="sudo"
fi

if [ -e "$APPS/$APP" ]; then
  ask "$APPS/$APP already exists. Replace it?" || die "Nothing was changed. Re-run with -y to replace it."
  if pgrep -f "$APPS/$APP/Contents/MacOS/TaskTrooper" > /dev/null 2>&1; then
    say "Quitting the running copy first..."
    osascript -e 'quit app "TaskTrooper"' > /dev/null 2>&1 || true
    sleep 3
  fi
  # Removed rather than copied over: ditto merges, so a file an older version
  # left behind would survive inside the new bundle and break its signature.
  # shellcheck disable=SC2086 # $sudo is empty or the literal word sudo
  $sudo rm -rf "$APPS/$APP"
fi

say "Copying $APP into $APPS..."
# shellcheck disable=SC2086
$sudo ditto "$mnt/$APP" "$APPS/$APP"
hdiutil detach "$mnt" -quiet
mnt=""

# Not notarized yet (release builds are ad-hoc signed): Gatekeeper refuses a
# quarantined copy on first launch, with a dialog that offers no way past it.
# These are the bytes this script just downloaded from the project's own
# release, so the attribute goes rather than the right-click → Open nobody guesses.
if ! spctl --assess --type exec "$APPS/$APP" > /dev/null 2>&1; then
  # shellcheck disable=SC2086
  $sudo xattr -dr com.apple.quarantine "$APPS/$APP" 2> /dev/null || true
  say "This build is not notarized yet, so the quarantine flag was removed - otherwise macOS refuses the first launch."
fi

# --- what the app needs -----------------------------------------------------
if ! command -v git > /dev/null 2>&1; then
  say ""
  say "git is missing. TaskTrooper needs it. Install it with:"
  say "    xcode-select --install"
fi
if ! command -v claude > /dev/null 2>&1; then
  say ""
  say "The claude CLI is missing. TaskTrooper needs it. Install it with:"
  say "    curl -fsSL https://claude.ai/install.sh | bash"
  say "then run 'claude' once to sign in to a plan that includes Claude Code."
fi

say ""
say "TaskTrooper is installed in $APPS."
if ask "Open it now?"; then
  open "$APPS/$APP"
else
  say "Start it whenever you like with:  open \"$APPS/$APP\""
fi
