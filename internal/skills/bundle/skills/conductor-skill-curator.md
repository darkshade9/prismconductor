---
name: conductor-skill-curator
description: Reads the skill_outcomes log, detects repeated failure patterns for a named skill, and proposes targeted markdown edits to fix them. Read-only — never mutates skill files. Phase A of issue #293.
---

# conductor-skill-curator

Bundled by PrismConductor (PRISMCONDUCTOR_PLAN.md §15.12).

Reads the `skill_outcomes` table for a named skill, detects repeated failure patterns, reads the current skill markdown, and proposes concrete `{old_text, new_text}` patches that would have prevented those failures. Writes findings to `.prismconductor/skill-curator/<RFC3339>.{json,md}` and updates `latest.json`. Never mutates skill files — edits are gated on user approval (phase B).

## Inputs

- `--skill <name>` (required) — the skill to analyse. Use the `bundled:<name>` form for bundled skills (e.g. `bundled:conductor-plan`) or an absolute path for repo skills.
- `--window-days <N>` (optional, default 30) — how many days back to look in the outcome log.
- `--data-dir <path>` (optional) — override the data directory. Resolution order: flag → `$PRISMCONDUCTOR_DATA_DIR` → `~/Library/Application Support/PrismConductor` (darwin default).

## Invocation

```
/conductor-skill-curator --skill bundled:conductor-plan
/conductor-skill-curator --skill bundled:conductor-execute --window-days 14
/conductor-skill-curator --skill bundled:conductor-plan --data-dir /tmp/test-dir
```

## Behavior

```bash
set -euo pipefail

# ── 0. Parse args ─────────────────────────────────────────────────────────────
SKILL_ARG=""
WINDOW_DAYS=30
DATA_DIR=""
args=("$@")
i=0
while [ $i -lt ${#args[@]} ]; do
  case "${args[$i]}" in
    --skill)       i=$((i+1)); SKILL_ARG="${args[$i]}" ;;
    --window-days) i=$((i+1)); WINDOW_DAYS="${args[$i]}" ;;
    --data-dir)    i=$((i+1)); DATA_DIR="${args[$i]}" ;;
  esac
  i=$((i+1))
done

if [ -z "$SKILL_ARG" ]; then
  echo "BLOCKED: --skill is required (e.g. --skill bundled:conductor-plan)"
  exit 1
fi

# ── 1. Resolve data dir ───────────────────────────────────────────────────────
if [ -z "$DATA_DIR" ]; then
  DATA_DIR="${PRISMCONDUCTOR_DATA_DIR:-}"
fi
if [ -z "$DATA_DIR" ]; then
  DATA_DIR="$HOME/Library/Application Support/PrismConductor"
fi

DB="$DATA_DIR/conductor.db"
if [ ! -f "$DB" ]; then
  echo "BLOCKED: conductor.db not found at $DB — is DATA_DIR correct?"
  exit 1
fi

# ── 2. Compute time window ────────────────────────────────────────────────────
if command -v python3 &>/dev/null; then
  SINCE_UNIX=$(python3 -c "import time; print(int(time.time()) - ${WINDOW_DAYS}*86400)")
else
  SINCE_UNIX=$(($(date +%s) - WINDOW_DAYS * 86400))
fi

# ── 3. Query outcome log ──────────────────────────────────────────────────────
if ! command -v sqlite3 &>/dev/null; then
  echo "BLOCKED: sqlite3 CLI not found — install it to run the curator"
  exit 1
fi

OUTCOMES_JSON=$(sqlite3 -json "$DB" \
  "SELECT session_id, outcome, blocked_reason, transcript_path, captured_at \
   FROM skill_outcomes \
   WHERE skill_path = '${SKILL_ARG}' AND captured_at >= ${SINCE_UNIX} \
   ORDER BY captured_at DESC" 2>/dev/null || echo "[]")

TOTAL=$(sqlite3 "$DB" \
  "SELECT COUNT(*) FROM skill_outcomes WHERE skill_path = '${SKILL_ARG}' AND captured_at >= ${SINCE_UNIX}" \
  2>/dev/null || echo "0")

# Per-outcome counts
for OUTCOME in success failed blocked needs_pr; do
  COUNT=$(sqlite3 "$DB" \
    "SELECT COUNT(*) FROM skill_outcomes WHERE skill_path = '${SKILL_ARG}' AND outcome = '${OUTCOME}' AND captured_at >= ${SINCE_UNIX}" \
    2>/dev/null || echo "0")
  eval "COUNT_${OUTCOME^^}=$COUNT"
done

echo "Outcome log: total=$TOTAL success=$COUNT_SUCCESS failed=$COUNT_FAILED blocked=$COUNT_BLOCKED needs_pr=$COUNT_NEEDS_PR (window: last ${WINDOW_DAYS} days)"

if [ "$TOTAL" -eq 0 ]; then
  echo "No outcomes recorded for skill '${SKILL_ARG}' in the last ${WINDOW_DAYS} days."
  echo "Spawn some sessions first, then re-run the curator."
  exit 0
fi
```

### 4. Read current skill markdown

Resolve the skill content:
- For `bundled:<name>`: use the Read tool to read `internal/skills/bundle/skills/<name>.md` relative to the repo root (determined via `git rev-parse --show-toplevel`).
- For an absolute path: use the Read tool to read it directly.

If the skill file cannot be read, report `BLOCKED: cannot read skill markdown at <path>` and exit.

### 5. Sample failure transcripts

From `OUTCOMES_JSON`, filter rows where `outcome` is `failed` or `blocked`. Take up to 5 samples. For each:
- Use the Read tool to read `transcript_path` (up to last 200 lines).
- Extract the `BLOCKED:` sentinel line if present; otherwise extract the last 20 lines of output.
- Collect the `blocked_reason` text.

### 6. Detect failure patterns

Examine the sampled failure snippets alongside the current skill markdown. For each pattern you detect (a class of failures that appears in ≥2 samples or looks high-impact in 1 sample), ask:

> "What specific instruction in this skill would, if changed, have prevented this class of failure?"

Produce a `failure_patterns` array. Each element:
```json
{
  "pattern_id": "p1",
  "description": "one-line description of the failure class",
  "frequency": <int — how many samples show this pattern>,
  "evidence_session_ids": ["sess-abc", "sess-def"],
  "proposed_edit": {
    "location_hint": "section name or line range in the skill markdown",
    "old_text": "exact substring to replace",
    "new_text": "replacement text",
    "rationale": "why this change would prevent the failure"
  }
}
```

If `old_text` is empty, it means inserting `new_text` at `location_hint`. If no actionable edit can be proposed for a pattern (the failure is environmental, not a skill defect), set `proposed_edit` to `null` and explain in `rationale`.

### 7. Write findings

Determine the report directory: find the git repo root (via `git rev-parse --show-toplevel` from the current working directory) and use `<repo_root>/.prismconductor/skill-curator/`.

```bash
REPO_ROOT=$(git rev-parse --show-toplevel 2>/dev/null || echo ".")
REPORT_DIR="$REPO_ROOT/.prismconductor/skill-curator"
mkdir -p "$REPORT_DIR"
TIMESTAMP=$(date -u +%Y-%m-%dT%H:%M:%SZ)
REPORT_JSON="$REPORT_DIR/${TIMESTAMP}.json"
REPORT_MD="$REPORT_DIR/${TIMESTAMP}.md"
LATEST_JSON="$REPORT_DIR/latest.json"
```

Write `$REPORT_JSON`:
```json
{
  "skill_path": "<SKILL_ARG>",
  "window_days": <WINDOW_DAYS>,
  "scanned_at": "<TIMESTAMP>",
  "summary": {
    "total_sessions": <TOTAL>,
    "success": <COUNT_SUCCESS>,
    "failed": <COUNT_FAILED>,
    "blocked": <COUNT_BLOCKED>,
    "needs_pr": <COUNT_NEEDS_PR>
  },
  "failure_patterns": [ ... ]
}
```

Write `$REPORT_MD` as a human-readable Markdown version of the same findings.

Write `$LATEST_JSON` as a pointer:
```json
{ "path": "<relative path from $REPORT_DIR to $REPORT_JSON>", "scanned_at": "<TIMESTAMP>" }
```

Print: `Curator findings written to $REPORT_JSON`

### Failure paths

- If `sqlite3` is not available: `BLOCKED: sqlite3 CLI not found`
- If `conductor.db` is not found: `BLOCKED: conductor.db not found at <path>`
- If `--skill` is omitted: `BLOCKED: --skill is required`
- If the skill markdown cannot be read: `BLOCKED: cannot read skill markdown at <path>`
- Any other unrecoverable error: `BLOCKED: <reason>`

### Notes

- This skill is **read-only**. It never modifies skill files.
- Proposed edits in `proposed_edit` are for human review. Application is gated on user approval (phase B — `ApplySkillEdit` Wails method).
- Per-workspace skill overrides live at `<repo>/.prismconductor/skills/<name>.md` (q2 answer). Phase B applies edits there, not to the bundled embed.FS copy.
- The curator uses its own judgment to cluster failures into patterns; for ambiguous cases, prefer a conservative edit proposal (tighten an instruction) over a sweeping rewrite.
