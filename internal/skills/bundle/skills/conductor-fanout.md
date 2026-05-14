---
name: conductor-fanout
description: Analyzes a review PR diff, identifies public cross-repo surface changes, searches sibling repositories in a PrismConductor collection, and writes proposed follow-up issue JSON for impacted repos.
---

# conductor-fanout

Bundled skill invoked by PrismConductor to analyze a REVIEW PR's diff and propose coordinating follow-up issues on sibling repositories in the same collection.

## Inputs (from prompt)

- Source repository path (working directory)
- Source issue number
- Source PR number
- Output JSON path (absolute)

## Behavior

### 1. Fetch the PR diff

```bash
gh pr diff <pr_number>
```

Capture stdout. If the diff exceeds 200 KB, truncate to 200 KB and note in the analysis.

### 2. Extract surface-area changes

Scan the diff for:
- **Go**: exported symbols whose signatures changed (functions, types, interface methods, constants). A line starting with `+func `, `+type `, `+var `, `+const ` in a non-test file is a candidate.
- **TypeScript/JavaScript**: exported function/class/type changes.
- **JSON/YAML schemas**: top-level key additions or removals.
- **REST API paths**: route registration lines in common frameworks.
- **Protocol Buffers**: message or service field changes.

Keep only **exported / public** changes — unexported Go identifiers (`foo` vs `Foo`) are not a cross-repo concern.

Build a list: `changed_symbols` = array of `{file, symbol, kind, change}`.

### 3. Identify sibling repos

Read `.prismconductor/collection-context.json` if present (written by the conductor when the workspace belongs to a collection). Each entry has `repo_path` and `workspace_id`.

If the file is absent, print `⚠ No sibling repos found — fanout analysis complete with 0 proposals.` then emit an empty proposals JSON (see §5) and exit.

### 4. Grep sibling repos for callers

For each sibling repo and each changed symbol:
- Run `grep -r --include="*.go" --include="*.ts" --include="*.tsx" --include="*.js" -l "<symbol>"` in the sibling repo.
- Collect matching file paths.

A symbol is a **cross-repo caller candidate** when ≥1 file in the sibling repo references it.

### 5. Build proposals

For each sibling repo that has ≥1 caller candidate, generate at most **3** proposals. Proposals must be **conservative**: only propose work that is clearly necessary given the observed callers (do not speculate). Each proposal has:

```json
{
  "id": "<uuid>",
  "source_workspace_id": "<source ws id from prompt>",
  "source_issue_number": <n>,
  "source_pr_number": <pr>,
  "target_workspace_id": "<sibling ws id>",
  "title": "<concise action title, ≤72 chars>",
  "body": "<markdown body describing what changed and why the sibling must update>",
  "labels": ["cross-repo"],
  "status": "pending"
}
```

Keep the body factual: quote the changed symbol, show which files in the sibling reference it, and state the minimum change required. Do not include boilerplate like "this issue was auto-generated".

### 6. Write output

Write the following JSON to the output path (create parent dirs with `mkdir -p`):

```json
{
  "source_workspace_id": "<ws id>",
  "source_issue_number": <n>,
  "source_pr_number": <pr>,
  "proposals": [ ... ],
  "analyzed_at": "<RFC3339 timestamp>"
}
```

Then print exactly:

```
FANOUT_WRITTEN: <absolute output path>
Work complete.
```

## Constraints

- Read-only: do NOT modify any file in any sibling repo.
- Do NOT open PRs or file issues — proposals are filed only when the user approves them in the UI.
- If `gh pr diff` returns an error (e.g. PR is private and credentials lack access), print `BLOCKED: <reason>` and exit.
- If there are no surface-area changes in the diff, write an empty proposals JSON and print `FANOUT_WRITTEN: <path>` then `Work complete.` — do NOT block.
- Skip test files (files ending `_test.go`, `.test.ts`, `.spec.ts`) when building `changed_symbols`.
- Generate UUIDs for proposal IDs using `uuidgen` or by reading `/proc/sys/kernel/random/uuid`.
