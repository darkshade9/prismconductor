#!/usr/bin/env bash
# restart.sh — one-shot: switch to main, pull, kill, build, relaunch.
#
# Usage:
#   ./scripts/restart.sh             # release build
#   ./scripts/restart.sh --debug     # debug build (devtools enabled)
#   ./scripts/restart.sh --no-pull   # skip git pull (offline / mid-flight)
#   ./scripts/restart.sh --verbose   # stream every command's stderr
#
# Exit codes:
#   0  success — app launched
#   1  not in a git repo / repo path missing
#   2  git operation failed (switch, pull, dirty tree)
#   3  wails build failed (or generate module failed)
#   4  binary missing or unchanged after build (stale-bundle protection)

set -euo pipefail

REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
APP="$REPO/build/bin/prismconductor.app"
BIN="$APP/Contents/MacOS/prismconductor"

WAILS_FLAGS=()
DO_PULL=1
VERBOSE=0

for arg in "$@"; do
  case "$arg" in
    --debug)    WAILS_FLAGS+=("-debug") ;;
    --no-pull)  DO_PULL=0 ;;
    --verbose)  VERBOSE=1 ;;
    -h|--help)
      sed -n '2,11p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//'
      exit 0
      ;;
    *)
      echo "unknown flag: $arg" >&2
      exit 1
      ;;
  esac
done

cd "$REPO" || { echo "repo missing: $REPO" >&2; exit 1; }

# ── error-handling policy ────────────────────────────────────────────
# We do NOT swallow stderr. Every command's stderr is streamed to the
# terminal. The historical pattern of `2>/dev/null || true` hid real
# failures (e.g. `wails generate module` falling over because go.mod
# drifted). The few places we use `|| true` are explicitly for "no
# matches found" exits where the command's stderr is itself intentional
# noise — flagged with a comment at each site.

# Tee every step's stderr into a log file so a failed run leaves a
# breadcrumb trail.
LOG="$REPO/.prismconductor/restart.log"
mkdir -p "$(dirname "$LOG")"
: > "$LOG"  # truncate

run() {
  # Run a step; stream stdout to the terminal, stream stderr to BOTH
  # the terminal AND $LOG so a failure can be diagnosed post-facto.
  if [[ "$VERBOSE" -eq 1 ]]; then
    echo "+ $*" | tee -a "$LOG"
  fi
  "$@" 2> >(tee -a "$LOG" >&2)
}

# ── 1. switch to main ────────────────────────────────────────────────
CURRENT="$(git branch --show-current 2>/dev/null || echo)"
if [[ "$CURRENT" != "main" ]]; then
  echo "→ switching to main (was: ${CURRENT:-detached HEAD})"
  if ! run git switch main; then
    echo "✗ git switch main failed — resolve dirty tree first" >&2
    exit 2
  fi
else
  echo "→ already on main"
fi

# ── 2. pull latest ──────────────────────────────────────────────────
if [[ "$DO_PULL" -eq 1 ]]; then
  echo "→ pulling latest"
  if ! run git pull --ff-only origin main; then
    echo "✗ pull failed (non-fast-forward? local commits ahead?)" >&2
    exit 2
  fi
else
  echo "→ skipping pull (--no-pull)"
fi

# ── 3. kill existing processes ──────────────────────────────────────
# pkill exits 1 when no processes match — that's not an error here, it
# just means nothing was running. Allow that one specific case via `||
# true`; we DO NOT redirect stderr so a real permission/system error
# would still show up.
echo "→ killing any running prismconductor processes"
pkill -9 -f "build/bin/prismconductor" || true            # no-match=1, ok
pkill -9 -f "prismconductor.app/Contents/MacOS/prismconductor" || true
pkill -9 -x "prismconductor" || true
# Settle so subsequent rm doesn't trip on busy file handles.
sleep 1

# ── 3a. nuke the entire app bundle ──────────────────────────────────
# Per user request: delete the bundle whole-cloth so the next build is
# guaranteed to produce a fresh bundle. No incremental wails caching can
# wedge a stale Mach-O alongside fresh resources.
echo "→ deleting old app bundle: $APP"
if [[ -e "$APP" ]]; then
  if ! rm -rf "$APP"; then
    echo "✗ failed to delete $APP — check permissions / file locks" >&2
    exit 3
  fi
fi
# Verify the bundle is really gone before we proceed; if rm silently
# left files behind the build will then APPEAR fresh while still
# shipping stale assets.
if [[ -e "$APP" ]]; then
  echo "✗ $APP still exists after rm — aborting to avoid mixed bundle" >&2
  exit 3
fi

# ── 3a. force-sync frontend dependencies from lockfile ─────────────
# `wails build`'s default `npm install` step will sometimes silently
# no-op against a stale node_modules tree when a pulled commit added a
# new dep — `package.json` + `package-lock.json` are updated, but the
# files in node_modules remain from the prior install. tsc then fails
# with "Cannot find module 'X' or its corresponding type declarations"
# even though the lockfile knows about X. `npm ci` wipes node_modules
# and reinstalls strictly from the lockfile so the tree matches what
# the just-pulled commit expects.
echo "→ npm ci (frontend deps from lockfile)"
if ! ( cd "$REPO/frontend" && run npm ci ); then
  echo "✗ npm ci failed in frontend/ — see $LOG" >&2
  exit 3
fi

# ── 3b. regenerate Wails bindings BEFORE wiping artifacts ──────────
# Critical ordering. `wails generate module` runs `go build` under the
# hood to introspect the bound-method signatures. main.go has a
# //go:embed all:frontend/dist directive — if frontend/dist is missing
# OR empty, the embed pattern fails with "no matching files found" and
# the whole script aborts. Run generate FIRST (prior build's dist
# probably still has files); for fresh-checkout / post-wipe scenarios
# seed dist with a placeholder so embed has something.
if [[ ! -d "$REPO/frontend/dist" ]] || [[ -z "$(ls -A "$REPO/frontend/dist" 2>/dev/null)" ]]; then
  echo "→ frontend/dist missing or empty; seeding placeholder for embed"
  mkdir -p "$REPO/frontend/dist"
  : > "$REPO/frontend/dist/.placeholder"
fi
echo "→ regenerating Wails bindings"
if ! run wails generate module; then
  echo "✗ wails generate module failed — see $LOG" >&2
  exit 3
fi

# ── 3c. wipe stale build artifacts beyond the bundle ────────────────
echo "→ wiping frontend/dist + vite cache + go build cache scope"
rm -rf "$REPO/build/bin"
rm -rf "$REPO/frontend/dist"
rm -rf "$REPO/frontend/node_modules/.vite"
# Drop Go's per-package cache for this module so a hidden type-check
# regression can't be glossed over by a cached object file.
go clean -cache 2>>"$LOG" || true  # best-effort; never fail the script

# ── 4. build ────────────────────────────────────────────────────────
# bash 3.2 (macOS default) trips on `${WAILS_FLAGS[@]}` under `set -u`
# when the array is empty — the `${arr[@]+"${arr[@]}"}` idiom expands
# to nothing when unset, dodging the error.
echo "→ wails build${WAILS_FLAGS[*]:+ ${WAILS_FLAGS[*]}}"
if ! run wails build ${WAILS_FLAGS[@]+"${WAILS_FLAGS[@]}"}; then
  echo "✗ wails build failed — see $LOG" >&2
  exit 3
fi

# ── 4a. stale-bundle protection ─────────────────────────────────────
# Two checks to make absolutely sure the build produced a NEW binary:
#   1. The .app bundle and the inner Mach-O must exist.
#   2. The binary's mtime must be within the last 30 seconds. Anything
#      older means wails build silently no-op'd — a stale build cache
#      somewhere we didn't wipe.
if [[ ! -x "$BIN" ]]; then
  echo "✗ expected binary not found at $BIN — build did not produce a bundle" >&2
  exit 4
fi
BIN_AGE=$(( $(date +%s) - $(stat -f %m "$BIN") ))
if [[ "$BIN_AGE" -gt 30 ]]; then
  echo "✗ binary is ${BIN_AGE}s old — wails build appears to have skipped." >&2
  echo "  This usually means a build cache wasn't cleared. See $LOG." >&2
  exit 4
fi
BIN_SHA="$(shasum -a 256 "$BIN" | awk '{print $1}')"
echo "→ fresh binary: ${BIN_AGE}s old, sha256=${BIN_SHA:0:12}…"

# ── 5. relaunch ─────────────────────────────────────────────────────
# `open -n` forces a NEW instance — without it, macOS LaunchServices
# may activate a cached/zombie process on bundle-ID match and the user
# sees old code.
echo "→ launching $APP"
if ! open -n "$APP"; then
  echo "✗ open failed — bundle may not be properly signed" >&2
  exit 4
fi

# Sanity-check the launched process matches the binary we just built.
sleep 1
PID="$(pgrep -f "build/bin/prismconductor.app" | head -1 || true)"
if [[ -z "$PID" ]]; then
  echo "⚠ open returned but no process visible — check Console.app" >&2
  exit 4
fi
echo "✓ launched (PID $PID, sha256=${BIN_SHA:0:12}…)"
echo "  log: $LOG"
