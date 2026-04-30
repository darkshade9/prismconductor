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
echo "→ killing any running prismconductor processes"
pkill -f "build/bin/prismconductor" 2>/dev/null || true
sleep 1

# ── 4. build ────────────────────────────────────────────────────────
echo "→ wails build${WAILS_FLAGS[*]:+ ${WAILS_FLAGS[*]}}"
if ! wails build "${WAILS_FLAGS[@]}"; then
  echo "✗ wails build failed" >&2
  exit 3
fi

if [[ ! -x "$BIN" ]]; then
  echo "✗ expected binary not found: $BIN" >&2
  exit 4
fi

# ── 5. relaunch ─────────────────────────────────────────────────────
echo "→ launching $APP"
open "$APP"

# Show the resulting PID for sanity.
sleep 1
PID="$(pgrep -f "build/bin/prismconductor.app" | head -1 || true)"
if [[ -n "$PID" ]]; then
  echo "✓ launched (PID $PID)"
else
  echo "⚠ open returned but no process visible — check Console"
fi
