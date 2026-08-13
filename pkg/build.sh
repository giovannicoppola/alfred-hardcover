#!/usr/bin/env bash
#
# Build the alfred-hardcover binary as a signed, notarizable universal Mach-O.
#
# CRITICAL: the -tags sqlite_fts5 flag is mandatory. The workflow creates and
# queries an FTS5 virtual table (createDatabase.go); mattn/go-sqlite3 only
# compiles FTS5 into SQLite when built with this tag. Without it the Script
# Filter fails at query time with "no such module: fts5".
#
# Usage:
#   ./build.sh              # build universal binary into ../source/
#   ./build.sh --sign       # also codesign + notarize (Developer ID)
#
# A downloaded .alfredworkflow is quarantined, so the shipped binary must be
# Developer ID signed AND notarized or Gatekeeper blocks it silently. Pass
# --sign for a release build. See ~/.claude/alfred-workflow-signing.md.

set -euo pipefail

PKG_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
OUT="$PKG_DIR/../source/alfred-hardcover"
TAGS="sqlite_fts5"

# Signing credentials (see ~/.claude/alfred-workflow-signing.md)
IDENTITY="C85516732AD930BB84E217AFF2C375E64268B111"
NOTARY_PROFILE="gio-notarytool"

echo "==> Building universal binary (tags: $TAGS)"
tmp_amd64="$(mktemp -t ah-amd64)"
tmp_arm64="$(mktemp -t ah-arm64)"
trap 'rm -f "$tmp_amd64" "$tmp_arm64"' EXIT

CGO_ENABLED=1 GOARCH=amd64 CC="clang -arch x86_64" \
  go build -C "$PKG_DIR" -tags "$TAGS" -o "$tmp_amd64" .
CGO_ENABLED=1 GOARCH=arm64 CC="clang -arch arm64" \
  go build -C "$PKG_DIR" -tags "$TAGS" -o "$tmp_arm64" .

lipo -create -output "$OUT" "$tmp_amd64" "$tmp_arm64"
echo "==> Wrote $OUT"
file "$OUT"

# Sanity check: FTS5 must be compiled in.
# (Capture first to avoid pipefail tripping on grep -q's early SIGPIPE.)
fts5_hits="$(strings "$OUT" | grep -c "fts5: syntax error" || true)"
if [[ "$fts5_hits" -eq 0 ]]; then
  echo "ERROR: FTS5 not found in binary — build tag missing?" >&2
  exit 1
fi
echo "==> FTS5 verified"

if [[ "${1:-}" == "--sign" ]]; then
  echo "==> Signing (Developer ID + hardened runtime)"
  codesign --force --options runtime --timestamp --sign "$IDENTITY" "$OUT"
  codesign -dvv "$OUT" 2>&1 | grep -i "authority=Developer ID" | head -1

  echo "==> Notarizing"
  zip="$(mktemp -t ah-notarize).zip"
  ditto -c -k --keepParent "$OUT" "$zip"
  xcrun notarytool submit "$zip" --keychain-profile "$NOTARY_PROFILE" --wait
  rm -f "$zip"

  echo "==> Verifying with spctl"
  spctl -a -vvv -t install "$OUT" 2>&1 | grep -i "source="
fi

echo "==> Done"
