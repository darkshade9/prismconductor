#!/usr/bin/env bash
# restart.sh — one-shot: switch to main, pull, kill, build, relaunch.
#
# Usage:
#   ./scripts/restart.sh             # release build
#   ./scripts/restart.sh --debug     # debug build (devtools enabled)
#   ./scripts/restart.sh --no-pull   # skip git pull (offline / mid-flight)
#
# Exit codes:
#   0  success — app launched
#   1  not in a git repo / repo path missing
#   2  git operation failed (switch, pull, dirty tree)
#   3  wails build failed
#   4  binary missing after build

set -euo pipefail

REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BIN="$REPO/build/bin/prismconductor.app/Contents/MacOS/prismconductor"
APP="$REPO/build/bin/prismconductor.app"

WAILS_FLAGS=()
DO_PULL=1

for arg in "$@"; do
  case "$arg" in
    --debug)    WAILS_FLAGS+=("-debug") ;;
    --no-pull)  DO_PULL=0 ;;
    -h|--help)
      sed -n '2,10p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//'
      exit 0
      ;;
    *)
      echo "unknown flag: $arg" >&2
      exit 1
      ;;
  esac
done

cd "$REPO" || { echo "repo missing: $REPO" >&2; exit 1; }

# ── 1. switch to main ────────────────────────────────────────────────
CURRENT="$(git branch --show-current 2>/dev/null || echo)"
if [[ "$CURRENT" != "main" ]]; then
  echo "→ switching to main (was: ${CURRENT:-detached HEAD})"
  if ! git switch main; then
    echo "✗ git switch main failed — resolve dirty tree first" >&2
    exit 2
  fi
else
  echo "→ already on main"
fi

# ── 2. pull latest ──────────────────────────────────────────────────
if [[ "$DO_PULL" -eq 1 ]]; then
  echo "→ pulling latest"
  git pull --ff-only origin main || {
    echo "✗ pull failed (non-fast-forward? local commits ahead?)" >&2
    exit 2
  }
else
  echo "→ skipping pull (--no-pull)"
fi

# ── 3. kill existing processes ──────────────────────────────────────
# Match BOTH the build/bin path AND the bundle name, so a launched-from-
# Dock / ~/Applications / spotlight copy doesn't survive. Done in two
# pkill passes because -f can't OR alternatives portably.
echo "→ killing any running prismconductor processes"
pkill -9 -f "build/bin/prismconductor" 2>/dev/null || true
pkill -9 -f "prismconductor.app/Contents/MacOS/prismconductor" 2>/dev/null || true
pkill -9 -x "prismconductor" 2>/dev/null || true
sleep 1

# ── 3b. regenerate Wails bindings ───────────────────────────────────
# `wails generate module` rewrites frontend/wailsjs/go/* from the current
# Go-side bound-method signatures. If we skip this, freshly-pulled
# `App.NewMethod` calls stay invisible to the frontend bundle even after
# build, because Vite still imports the OLD App.js / App.d.ts on disk.
echo "→ regenerating Wails bindings"
if ! wails generate module >/dev/null 2>&1; then
  echo "✗ wails generate module failed" >&2
  exit 3
fi

# ── 3c. wipe stale build artifacts ──────────────────────────────────
# wails build is incremental and will happily ship a Frankenstein bundle
# (new Go + old JS) if any of these caches disagree with the source.
# Nuke them to force every layer to rebuild from current sources.
echo "→ wiping build/bin + frontend/dist + vite cache"
rm -rf "$REPO/build/bin"
rm -rf "$REPO/frontend/dist"
rm -rf "$REPO/frontend/node_modules/.vite"

# ── 4. build ────────────────────────────────────────────────────────
# bash 3.2 (macOS default) trips on `${WAILS_FLAGS[@]}` under `set -u`
# when the array is empty — it treats an empty array as unbound. The
# `${arr[@]+"${arr[@]}"}` idiom expands to nothing when unset, and to
# the array's elements otherwise, dodging the error on every bash from
# 3.2 through 5.x.
echo "→ wails build${WAILS_FLAGS[*]:+ ${WAILS_FLAGS[*]}}"
if ! wails build ${WAILS_FLAGS[@]+"${WAILS_FLAGS[@]}"}; then
  echo "✗ wails build failed" >&2
  exit 3
fi

if [[ ! -x "$BIN" ]]; then
  echo "✗ expected binary not found: $BIN" >&2
  exit 4
fi

# Sanity-check the binary is genuinely fresh: its mtime must be within
# the last minute. If it's older we've shipped a stale bundle (build
# silently no-op'd despite the wipe) and the user would never see new
# code — fail loudly instead.
BIN_AGE=$(( $(date +%s) - $(stat -f %m "$BIN") ))
if [[ "$BIN_AGE" -gt 60 ]]; then
  echo "✗ binary is ${BIN_AGE}s old — wails build appears to have skipped" >&2
  exit 4
fi
echo "→ binary mtime check ok (${BIN_AGE}s old)"

# ── 5. relaunch ─────────────────────────────────────────────────────
# `open -n` forces a NEW instance instead of letting LaunchServices
# activate a cached one. Without -n macOS Sequoia/Sonoma may bring an
# already-running copy to the front (and on bundle-ID match even resurrect
# a recently-quit one) — and the user sees old code.
echo "→ launching $APP"
open -n "$APP"

# Show the resulting PID for sanity.
sleep 1
PID="$(pgrep -f "build/bin/prismconductor.app" | head -1 || true)"
if [[ -n "$PID" ]]; then
  echo "✓ launched (PID $PID)"
else
  echo "⚠ open returned but no process visible — check Console"
fi
