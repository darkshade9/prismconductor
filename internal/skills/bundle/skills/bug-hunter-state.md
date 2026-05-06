---
name: bug-hunter-state
description: Scans the conductor's persistent state (SQLite DB, transcripts dir, workspaces.json, live process table) and produces a structured JSON + Markdown report of detected bugs, drift, and zombies. Read-only. Never mutates state.
---

# bug-hunter-state

Bundled by PrismConductor (PRISMCONDUCTOR_PLAN.md §15.7).

Scans conductor persistent state for 12 known broken-state patterns and writes a timestamped report under `.prismconductor/bug-hunter/`. Read-only — never mutates the DB, worktrees, or `workspaces.json`. Safe to run at any time including while the conductor is running.

## Inputs

- `--data-dir <path>` (optional) — override the data directory. Resolution order: `--data-dir` flag → `$PRISMCONDUCTOR_DATA_DIR` → `~/Library/Application Support/PrismConductor` (darwin default).
- `--repo <path>` (optional) — path to the prismconductor source repo (needed for rule 2 migration-drift check). Defaults to the directory containing this skill's checkout.

## Invocation

```
/bug-hunter-state
/bug-hunter-state --data-dir /tmp/test-data-dir
/loop 24h /bug-hunter-state
```

## Behavior

Run each rule below in order. Accumulate all findings into a JSON array. After all rules run, write the report files and print the report path.

```bash
set -euo pipefail

# ── 0. Resolve data dir ──────────────────────────────────────────────────────
DATA_DIR=""
REPO_DIR=""
args=("$@")
i=0
while [ $i -lt ${#args[@]} ]; do
  case "${args[$i]}" in
    --data-dir)  i=$((i+1)); DATA_DIR="${args[$i]}" ;;
    --repo)      i=$((i+1)); REPO_DIR="${args[$i]}" ;;
  esac
  i=$((i+1))
done

if [ -z "$DATA_DIR" ]; then
  DATA_DIR="${PRISMCONDUCTOR_DATA_DIR:-}"
fi
if [ -z "$DATA_DIR" ]; then
  DATA_DIR="$HOME/Library/Application Support/PrismConductor"
fi

DB="$DATA_DIR/conductor.db"
WORKTREES_DIR="$DATA_DIR/worktrees"
WORKSPACES_JSON="$DATA_DIR/workspaces.json"
TRANSCRIPTS_DIR="$DATA_DIR/transcripts"

if [ ! -f "$DB" ]; then
  echo "BLOCKED: conductor.db not found at $DB — is DATA_DIR correct?" >&2
  exit 1
fi

SCANNED_AT="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
REPORT_DIR="$(dirname "$DB")/../.prismconductor/bug-hunter"
# report dir lives alongside the repo's .prismconductor/, not inside DATA_DIR
# If caller is in a repo worktree, find the repo root via git
REPO_ROOT=""
if command -v git &>/dev/null; then
  REPO_ROOT="$(git -C "${REPO_DIR:-.}" rev-parse --show-toplevel 2>/dev/null || true)"
fi
if [ -n "$REPO_ROOT" ]; then
  REPORT_DIR="$REPO_ROOT/.prismconductor/bug-hunter"
else
  # Fallback: put reports alongside conductor.db
  REPORT_DIR="$DATA_DIR/bug-hunter"
fi

mkdir -p "$REPORT_DIR"

TIMESTAMP="$(date -u +%Y%m%dT%H%M%SZ)"
REPORT_JSON="$REPORT_DIR/${TIMESTAMP}.json"
REPORT_MD="$REPORT_DIR/${TIMESTAMP}.md"

# Accumulator: newline-delimited JSON objects, assembled into array at the end
FINDINGS_FILE="$(mktemp /tmp/bh-findings-XXXXXX.ndjson)"
trap 'rm -f "$FINDINGS_FILE"' EXIT

add_finding() {
  local rule="$1" severity="$2" evidence="$3" suggested_fix="$4"
  printf '%s\n' "$(jq -cn \
    --arg rule "$rule" \
    --arg severity "$severity" \
    --arg evidence "$evidence" \
    --arg fix "$suggested_fix" \
    '{rule:$rule,severity:$severity,evidence:$evidence,suggested_fix:$fix}')" >> "$FINDINGS_FILE"
}

q() {
  # Read-only sqlite3 query; returns rows as newline-delimited text
  sqlite3 -readonly -separator '|' "$DB" "$1"
}
```

### Rule 1 — orphan_subprocess

Orphan: a claude/worker process whose cwd is inside the conductor's worktrees dir AND whose runtime exceeds 60 seconds AND that has no matching `sessions` row with `state='running'` and the same PID.

Severity: **high** (orphan PIDs >1h), **medium** (orphan PIDs 1–60 min).

```bash
# macOS: ps -eo pid,etime,command
# Filter: command contains 'claude' or 'worker.sh'
# lsof -p PID -a -d cwd -Fn | grep '^n' to get cwd
if ps -eo pid,etime,command &>/dev/null; then
  while IFS='|' read -r pid etime_raw cmd; do
    pid="${pid// /}"
    # Only care about processes with claude or worker.sh in command line
    if ! (echo "$cmd" | grep -qE 'claude|worker\.sh'); then continue; fi
    # Parse etime: [[DD-]HH:]MM:SS
    etime="${etime_raw// /}"
    total_secs=0
    if [[ "$etime" =~ ^([0-9]+)-([0-9]+):([0-9]+):([0-9]+)$ ]]; then
      total_secs=$(( ${BASH_REMATCH[1]}*86400 + ${BASH_REMATCH[2]}*3600 + ${BASH_REMATCH[3]}*60 + ${BASH_REMATCH[4]} ))
    elif [[ "$etime" =~ ^([0-9]+):([0-9]+):([0-9]+)$ ]]; then
      total_secs=$(( ${BASH_REMATCH[1]}*3600 + ${BASH_REMATCH[2]}*60 + ${BASH_REMATCH[3]} ))
    elif [[ "$etime" =~ ^([0-9]+):([0-9]+)$ ]]; then
      total_secs=$(( ${BASH_REMATCH[1]}*60 + ${BASH_REMATCH[2]} ))
    fi
    # Skip processes younger than 60s (may not yet be recorded)
    if [ "$total_secs" -lt 60 ]; then continue; fi
    # Get cwd via lsof
    proc_cwd="$(lsof -p "$pid" -a -d cwd -Fn 2>/dev/null | grep '^n' | sed 's/^n//' | head -1 || true)"
    if [ -z "$proc_cwd" ]; then continue; fi
    # Must be under worktrees dir
    if [[ "$proc_cwd" != "$WORKTREES_DIR"* ]]; then continue; fi
    # Check DB: any running session with this PID?
    db_match="$(q "SELECT COUNT(*) FROM sessions WHERE pid=$pid AND state='running';" 2>/dev/null || echo 0)"
    if [ "${db_match:-0}" -gt 0 ]; then continue; fi
    # Orphan confirmed
    mins=$(( total_secs / 60 ))
    if [ "$total_secs" -ge 3600 ]; then
      sev="high"
    else
      sev="medium"
    fi
    add_finding "orphan_subprocess" "$sev" \
      "PID $pid ($mins min elapsed): $cmd" \
      "kill $pid   # then verify the linked worktree is cleaned up"
  done < <(ps -eo pid,etime,command 2>/dev/null | tail -n +2 | awk '{pid=$1; etime=$2; $1=$2=""; cmd=substr($0,3); print pid "|" etime "|" cmd}')
fi
```

### Rule 2 — migration_drift

Rows in `schema_migrations` whose IDs are not in `internal/store/migrations/registry.go`. Indicates a DB that was migrated by a newer binary than the current checkout (downgrade), or a hand-edited DB.

Severity: **high**.

```bash
# Extract known migration IDs from registry.go
REGISTRY=""
if [ -n "$REPO_ROOT" ]; then
  REG_FILE="$REPO_ROOT/internal/store/migrations/registry.go"
  if [ -f "$REG_FILE" ]; then
    # Pattern: quoted strings like "20250504_00_initial_migration_framework"
    REGISTRY="$(grep -oE '"[0-9]{8}_[0-9]{2}_[a-z0-9_]+"' "$REG_FILE" | tr -d '"' | sort)"
  fi
fi

# DB migration IDs
DB_IDS="$(q "SELECT id FROM schema_migrations ORDER BY id;" 2>/dev/null || true)"

if [ -n "$DB_IDS" ] && [ -n "$REGISTRY" ]; then
  # Find DB IDs not in registry
  while IFS= read -r db_id; do
    [ -z "$db_id" ] && continue
    if ! echo "$REGISTRY" | grep -qxF "$db_id"; then
      add_finding "migration_drift" "high" \
        "DB has migration '$db_id' not present in registry.go" \
        "Ensure the running binary matches the current checkout; if intentional, update registry.go"
    fi
  done <<< "$DB_IDS"
elif [ -n "$DB_IDS" ] && [ -z "$REGISTRY" ]; then
  add_finding "migration_drift" "medium" \
    "registry.go not found — could not validate DB migrations against source" \
    "Pass --repo <path> pointing to the prismconductor source checkout"
fi
```

### Rule 3 — terminal_session_null_failure_reason

Sessions in a terminal state (`failed` or `blocked`) whose JSON blob has no `failure_reason`. These sessions surface as stuck red cards with no diagnosis text in the UI.

Severity: **medium**.

```bash
while IFS='|' read -r id ws_id issue_num; do
  [ -z "$id" ] && continue
  add_finding "terminal_session_null_failure_reason" "medium" \
    "session $id (workspace=$ws_id issue=#$issue_num) is in failed/blocked state with null failure_reason" \
    "Inspect the transcript at the session's transcript_path; set failure_reason via store.SetSessionFailureReason"
done < <(q "SELECT id, workspace_id, issue_number FROM sessions WHERE state IN ('failed','blocked') AND json_extract(json,'$.failure_reason') IS NULL;" 2>/dev/null || true)
```

### Rule 4 — review_card_no_pr

Issues in the `review` column with neither a `pr_number` nor `needs_pr_info` in their JSON. These cards show in REVIEW but have no linked PR — usually a sign the worker exited before opening a PR.

Severity: **medium**.

```bash
while IFS='|' read -r ws_id num; do
  [ -z "$ws_id" ] && continue
  add_finding "review_card_no_pr" "medium" \
    "issue #$num (workspace=$ws_id) is in REVIEW column but has no pr_number or needs_pr_info" \
    "Check the session transcript; the worker may have exited before gh pr create. Manually open a PR or move card back to IN_PROGRESS."
done < <(q "SELECT workspace_id, number FROM issues WHERE column_name='review' AND json_extract(json,'$.pr_number') IS NULL AND json_extract(json,'$.needs_pr_info') IS NULL AND archived_at IS NULL;" 2>/dev/null || true)
```

### Rule 5 — in_progress_no_session

Issues in the `in_progress` column with no `running` session and no `failure_reason` in the issue JSON. Indicates the worker silently exited without transitioning the card.

Severity: **medium**.

```bash
while IFS='|' read -r ws_id num; do
  [ -z "$ws_id" ] && continue
  running_count="$(q "SELECT COUNT(*) FROM sessions WHERE workspace_id='$ws_id' AND issue_number=$num AND state='running';" 2>/dev/null || echo 0)"
  if [ "${running_count:-0}" -eq 0 ]; then
    add_finding "in_progress_no_session" "medium" \
      "issue #$num (workspace=$ws_id) is in IN_PROGRESS column but has no running session and no failure_reason" \
      "Re-trigger execution or move the card back to TODO so the conductor re-queues it."
  fi
done < <(q "SELECT workspace_id, number FROM issues WHERE column_name='in_progress' AND json_extract(json,'$.failure_reason') IS NULL AND archived_at IS NULL;" 2>/dev/null || true)
```

### Rule 6 — provisioning_stuck

Workspace entries in `workspaces.json` where `provisioning=true` and `provisioning_at` is older than 600 seconds. These workspaces are stuck mid-deploy.

Severity: **high**.

```bash
if [ -f "$WORKSPACES_JSON" ]; then
  NOW_EPOCH="$(date +%s)"
  jq -r '.[] | select(.provisioning == true) | [.id, .display_name, (.provisioning_at // "")] | @tsv' \
    "$WORKSPACES_JSON" 2>/dev/null | while IFS=$'\t' read -r ws_id ws_name prov_at; do
    [ -z "$ws_id" ] && continue
    if [ -z "$prov_at" ]; then
      add_finding "provisioning_stuck" "high" \
        "workspace $ws_id ($ws_name) has provisioning=true but no provisioning_at timestamp" \
        "Manually set provisioning=false in workspaces.json or re-deploy via Settings."
      continue
    fi
    # provisioning_at is RFC3339; convert to epoch
    prov_epoch="$(date -j -f '%Y-%m-%dT%H:%M:%SZ' "$prov_at" +%s 2>/dev/null || date -d "$prov_at" +%s 2>/dev/null || echo 0)"
    elapsed=$(( NOW_EPOCH - prov_epoch ))
    if [ "$elapsed" -gt 600 ]; then
      mins=$(( elapsed / 60 ))
      add_finding "provisioning_stuck" "high" \
        "workspace $ws_id ($ws_name) has been provisioning for ${mins}m (since $prov_at)" \
        "Check CloudFlare worker deploy logs; set provisioning=false in workspaces.json or re-deploy."
    fi
  done
fi
```

### Rule 7 — stale_pending_pool_for

Rows in `pending_pool_for` whose paired issue is in `review`, `done`, or archived. The paired issue already completed — these queue entries are stale orphans.

Severity: **low**.

```bash
while IFS='|' read -r ppf_id ws_id issue_num role action; do
  [ -z "$ppf_id" ] && continue
  add_finding "stale_pending_pool_for" "low" \
    "pending_pool_for id=$ppf_id (workspace=$ws_id issue=#$issue_num role=$role action=$action) points to a completed/archived issue" \
    "DELETE FROM pending_pool_for WHERE id=$ppf_id;  -- run against conductor.db after stopping the conductor"
done < <(q "
SELECT ppf.id, ppf.workspace_id, ppf.issue_number, ppf.role, ppf.action
FROM pending_pool_for ppf
JOIN issues i ON i.workspace_id = ppf.workspace_id AND i.number = ppf.issue_number
WHERE i.column_name IN ('review','done') OR i.archived_at IS NOT NULL;" 2>/dev/null || true)
```

### Rule 8 — waiting_for_pool_no_queue

Issues with `waiting_for_pool=true` in their JSON but no corresponding row in `pending_pool_for`. The UI shows these with a spinner that will never resolve.

Severity: **low**.

```bash
while IFS='|' read -r ws_id num; do
  [ -z "$ws_id" ] && continue
  ppf_count="$(q "SELECT COUNT(*) FROM pending_pool_for WHERE workspace_id='$ws_id' AND issue_number=$num;" 2>/dev/null || echo 0)"
  if [ "${ppf_count:-0}" -eq 0 ]; then
    add_finding "waiting_for_pool_no_queue" "low" \
      "issue #$num (workspace=$ws_id) has waiting_for_pool=true but no pending_pool_for row" \
      "UPDATE issues SET json=json_set(json,'\$.waiting_for_pool',json('false')) WHERE workspace_id='$ws_id' AND number=$num;  -- run against conductor.db"
  fi
done < <(q "SELECT workspace_id, number FROM issues WHERE json_extract(json,'\$.waiting_for_pool')=1 AND archived_at IS NULL;" 2>/dev/null || true)
```

### Rule 9 — pool_active_count_mismatch

DB-only: for each pool, compare the count of `sessions` rows in `state='running'` against `pool_usage.used` for the `active` window. A mismatch means `pool_usage` was not flushed when a session terminated.

Note: v1 is DB-only. Runtime in-memory counter check is deferred to v2 (requires conductor IPC endpoint).

Severity: **low**.

```bash
while IFS='|' read -r pool_id pool_name db_running pu_used; do
  [ -z "$pool_id" ] && continue
  db_running="${db_running:-0}"
  pu_used="${pu_used:-0}"
  if [ "$db_running" -ne "$pu_used" ]; then
    add_finding "pool_active_count_mismatch" "low" \
      "pool '$pool_name' ($pool_id): DB shows $db_running running session(s) but pool_usage.used=$pu_used for window='active'" \
      "pool_usage will self-correct on the next conductor-initiated rate-limit update; restart the conductor if the mismatch persists >10 min"
  fi
done < <(q "
SELECT p.id, p.name,
       COALESCE((SELECT COUNT(*) FROM sessions s WHERE json_extract(s.json,'\$.pool_id')=p.id AND s.state='running'),0) AS db_running,
       COALESCE((SELECT pu.used FROM pool_usage pu WHERE pu.pool_id=p.id AND pu.window='active'),0) AS pu_used
FROM pools p
WHERE p.enabled=1;" 2>/dev/null || true)
```

### Rule 10 — approved_plan_no_execute

Issues in `in_progress` column that have a plan file with `ready_to_execute=true` whose mtime is older than 5 minutes, and no `execute`-type session row linked to that issue in the last hour.

Severity: **medium**.

```bash
NOW_EPOCH_R10="$(date +%s)"
while IFS='|' read -r ws_id num; do
  [ -z "$ws_id" ] && continue
  # Look for a plan file: <repo>/.prismconductor/plans/<wsid>-<num>-rev*.json or similar
  # The canonical location is relative to the workspace's repo_path
  repo_path=""
  if [ -f "$WORKSPACES_JSON" ]; then
    repo_path="$(jq -r --arg id "$ws_id" '.[] | select(.id==$id) | .repo_path // ""' "$WORKSPACES_JSON" 2>/dev/null || true)"
  fi
  plan_found=0
  if [ -n "$repo_path" ] && [ -d "$repo_path/.prismconductor/plans" ]; then
    while IFS= read -r plan_file; do
      rte="$(jq -r '.ready_to_execute // false' "$plan_file" 2>/dev/null || echo false)"
      if [ "$rte" = "true" ]; then
        plan_mtime="$(stat -f '%m' "$plan_file" 2>/dev/null || stat -c '%Y' "$plan_file" 2>/dev/null || echo 0)"
        age=$(( NOW_EPOCH_R10 - plan_mtime ))
        if [ "$age" -gt 300 ]; then
          # Check for any execute session in the last hour
          exec_count="$(q "SELECT COUNT(*) FROM sessions WHERE workspace_id='$ws_id' AND issue_number=$num AND json_extract(json,'\$.skill')='execute' AND json_extract(json,'\$.started_at') > $(( NOW_EPOCH_R10 - 3600 ));" 2>/dev/null || echo 0)"
          if [ "${exec_count:-0}" -eq 0 ]; then
            mins=$(( age / 60 ))
            add_finding "approved_plan_no_execute" "medium" \
              "issue #$num (workspace=$ws_id) has ready_to_execute=true plan (${mins}m old) but no execute session in the last hour" \
              "Check if the orchestrator missed the event; drag the card back to TODO and re-approve to re-trigger."
          fi
          plan_found=1
        fi
      fi
    done < <(find "$repo_path/.prismconductor/plans" -name "*.json" -newer /dev/null 2>/dev/null | grep -E "[0-9]+-rev[0-9]+\.json$" || true)
  fi
done < <(q "SELECT workspace_id, number FROM issues WHERE column_name='in_progress' AND archived_at IS NULL;" 2>/dev/null || true)
```

### Rule 11 — worktree_no_session

Worktree directories under `DATA_DIR/worktrees/` whose mtime is older than 1 hour and whose `(workspace_id, issue_number)` pair has no session row created in the last hour. Leftover worktrees consume disk and may hold stale branches.

Severity: **low**.

```bash
if [ -d "$WORKTREES_DIR" ]; then
  NOW_EPOCH_R11="$(date +%s)"
  while IFS= read -r wt_dir; do
    wt_name="$(basename "$wt_dir")"
    # Expect name like: <workspace_id>-<issue_number>  e.g.  ws_abc-42
    # Extract issue number: last hyphen-delimited numeric segment
    issue_num="$(echo "$wt_name" | grep -oE '[0-9]+$' || true)"
    [ -z "$issue_num" ] && continue
    # Workspace id: everything before the last -<num>
    ws_id="${wt_name%-$issue_num}"
    [ -z "$ws_id" ] && continue
    # Check mtime (older than 1h)
    wt_mtime="$(stat -f '%m' "$wt_dir" 2>/dev/null || stat -c '%Y' "$wt_dir" 2>/dev/null || echo 0)"
    age=$(( NOW_EPOCH_R11 - wt_mtime ))
    if [ "$age" -lt 3600 ]; then continue; fi
    # Check for a session in the last hour
    recent_session="$(q "SELECT COUNT(*) FROM sessions WHERE workspace_id='$ws_id' AND issue_number=$issue_num AND json_extract(json,'\$.started_at') > $(( NOW_EPOCH_R11 - 3600 ));" 2>/dev/null || echo 0)"
    if [ "${recent_session:-0}" -eq 0 ]; then
      hrs=$(( age / 3600 ))
      add_finding "worktree_no_session" "low" \
        "worktree $wt_dir (${hrs}h old) has no session in the last hour for workspace=$ws_id issue=#$issue_num" \
        "If the branch was merged or abandoned: git worktree remove --force $wt_dir"
    fi
  done < <(find "$WORKTREES_DIR" -mindepth 1 -maxdepth 1 -type d 2>/dev/null || true)
fi
```

### Rule 12 — transcript_missing

Session rows with a non-null `transcript_path` whose file does not exist on disk. Lost transcripts prevent the SessionDrawer from rendering and block post-mortem analysis.

Severity: **low**.

```bash
while IFS='|' read -r sess_id ws_id issue_num tpath; do
  [ -z "$tpath" ] && continue
  if [ ! -f "$tpath" ]; then
    add_finding "transcript_missing" "low" \
      "session $sess_id (workspace=$ws_id issue=#$issue_num) references transcript_path='$tpath' which does not exist" \
      "The transcript was likely deleted or the session was from a different machine; acknowledge this session in the UI to clear the red badge."
  fi
done < <(q "SELECT id, workspace_id, issue_number, transcript_path FROM sessions WHERE transcript_path IS NOT NULL;" 2>/dev/null || true)
```

### Emit report

```bash
# Build findings JSON array from accumulated ndjson
FINDINGS_JSON="$(jq -s '.' "$FINDINGS_FILE" 2>/dev/null || echo '[]')"
FINDING_COUNT="$(echo "$FINDINGS_JSON" | jq 'length')"

# Write JSON report
jq -n \
  --arg scanned_at "$SCANNED_AT" \
  --arg data_dir "$DATA_DIR" \
  --argjson findings "$FINDINGS_JSON" \
  '{scanned_at:$scanned_at,data_dir:$data_dir,finding_count:($findings|length),findings:$findings}' \
  > "$REPORT_JSON"

# Write Markdown summary
{
  echo "# Bug Hunter Report — $SCANNED_AT"
  echo ""
  echo "Data dir: \`$DATA_DIR\`"
  echo ""
  echo "**$FINDING_COUNT finding(s)**"
  echo ""
  if [ "$FINDING_COUNT" -eq 0 ]; then
    echo "_No findings. Conductor state looks healthy._"
  else
    # Group by rule
    echo "$FINDINGS_JSON" | jq -r '
      group_by(.rule)[] |
      "## \(.[0].rule) [\(.[0].severity)] — \(length) finding(s)\n" +
      (.[0:3] | map("- **evidence**: \(.evidence)\n  **fix**: \(.suggested_fix)") | join("\n")) +
      (if length > 3 then "\n_(and \(length-3) more — see JSON report)_" else "" end)
    '
  fi
  echo ""
  echo "Full report: \`$REPORT_JSON\`"
} > "$REPORT_MD"

# Update latest.json symlink (or copy on platforms without symlink support)
LATEST="$REPORT_DIR/latest.json"
ln -sf "$REPORT_JSON" "$LATEST" 2>/dev/null || cp "$REPORT_JSON" "$LATEST"

echo "$REPORT_JSON"
```

## Output shape

```json
{
  "scanned_at": "2026-05-06T03:00:00Z",
  "data_dir": "/Users/you/Library/Application Support/PrismConductor",
  "finding_count": 2,
  "findings": [
    {
      "rule": "orphan_subprocess",
      "severity": "high",
      "evidence": "PID 4426 (127 min elapsed): /path/to/claude ...",
      "suggested_fix": "kill 4426   # then verify the linked worktree is cleaned up"
    }
  ]
}
```

The Markdown summary (`<timestamp>.md`) is a one-page human-readable version of the same findings. The `latest.json` symlink always points to the most recent run's JSON.

## Scheduling

Run on demand or via the built-in `/loop` skill:

```
/loop 24h /bug-hunter-state
```

No conductor-side scheduler required. v2 can add an in-app indicator reading `latest.json`.

## Notes

- Rule 9 is DB-only in v1 (in-memory pool counter unreachable from outside the running process). v2 will add `--include-runtime` via a conductor IPC socket.
- Rule 1 uses `lsof -p` and macOS `ps -eo` flags — Linux support is a v2 concern.
- Reports accumulate in `.prismconductor/bug-hunter/`; old reports are not auto-pruned. Add a cron to remove files older than 30 days if disk space matters.
