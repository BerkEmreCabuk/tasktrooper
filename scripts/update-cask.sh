#!/usr/bin/env bash
#
# Point the Homebrew cask at a release.
#
#   scripts/update-cask.sh 0.1.1 desktop/release/TaskTrooper-0.1.1-universal.dmg
#
# Rewrites `version` and `sha256` in packaging/homebrew/Casks/tasktrooper.rb.
# Copying the result into the tap (makifbaysal/homebrew-tasktrooper) is a
# separate, deliberate step — this script never pushes anything.
set -euo pipefail

version="${1:-}"
dmg="${2:-}"
cask="$(cd "$(dirname "$0")/.." && pwd)/packaging/homebrew/Casks/tasktrooper.rb"

if [ -z "$version" ] || [ -z "$dmg" ]; then
  echo "usage: $(basename "$0") <version> <dmg>" >&2
  exit 2
fi
[ -f "$dmg" ] || {
  echo "error: no such file: $dmg" >&2
  exit 1
}
[ -f "$cask" ] || {
  echo "error: no cask at $cask" >&2
  exit 1
}

# The URL is built from `version`, so a dmg whose name disagrees with it would
# produce a cask that downloads something that does not exist.
case "$(basename "$dmg")" in
  "TaskTrooper-$version-universal.dmg") ;;
  *)
    echo "error: $(basename "$dmg") is not TaskTrooper-$version-universal.dmg" >&2
    exit 1
    ;;
esac

sha="$(shasum -a 256 "$dmg" | awk '{print $1}')"

tmp="$(mktemp)"
trap 'rm -f "$tmp"' EXIT
sed -e "s/^  version \".*\"$/  version \"$version\"/" \
  -e "s/^  sha256 \".*\"$/  sha256 \"$sha\"/" "$cask" > "$tmp"
cp "$tmp" "$cask"

echo "tasktrooper.rb -> version $version, sha256 $sha"
grep -E '^  (version|sha256) ' "$cask"
