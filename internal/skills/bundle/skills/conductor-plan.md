---
name: conductor-plan
description: Universal `/start-issue` analogue. Reads a GitHub issue, greps the repo, reads any CLAUDE.md and .claude/rules/ present, and emits a structured plan JSON to .prismconductor/plans/<issue>-rev<N>.json. Stops at the proposal gate. No code mutation.
---

# conductor-plan

Bundled by PrismConductor. Used in Bundled and Hybrid skill modes (PRISMCONDUCTOR_PLAN.md §15.7).

## Inputs

- `--issue <number>` (required)
- `--repo <path>` (defaults to cwd)
- `--native-cmd <command>` (Hybrid mode: wraps a repo's own planning skill)

## Behavior

1. Fetch the issue body via `gh issue view <number> --json title,body,labels`.
2. Read `CLAUDE.md` and `.claude/rules/*.md` if present (silently skip if absent — see §15.8).
3. Grep the repo for terms in the issue title and body that name files/symbols.
4. Run `gh label list -R <owner>/<repo> --json name,color,description --limit 200` to see the
   repo's label vocabulary. Then classify the issue (NON-NEGOTIABLE):

   - **Axis** (pick exactly one — never multi-axis):
     - `feature` (new capability) → map to existing `enhancement` / `feature` / `feat` (in
       that priority order). If none of those exist in the repo, omit the axis label.
     - `bug` (defect fix) → map to `bug` / `defect` / `fix`.
     - `refactor` (no behavior change) → map to `refactor` / `cleanup` / `chore`.
     - `docs` (markdown-only) → map to `documentation` / `docs`.
     - `test` (test-only) → map to `test` / `tests` / `testing`.
   - **Topical** (up to 4): include component, area, or module names suggested by the file
     paths the plan modifies (e.g. `frontend`, `backend`, `worker`, `orchestrator`,
     `eventbus`). **Suggest these even when they don't exist in the repo's label list yet** —
     the conductor drops them on auto-apply (logging the drop), and the PlanModal renders
     them with a `(create)` chip so the user can opt to create the missing label. This
     surfaces gaps in the repo's label vocabulary.
   - **Cap**: 5 entries total.
   - **Order**: axis FIRST (index 0), then topicals. The conductor's auto-apply reconciler
     reads index 0 as the axis on re-plan to decide whether to drop the prior axis label.
     Any deviation from this ordering breaks re-plan reconciliation.

   Axis names MUST map to existing repo labels via the priority lists above — do not invent
   axis names. The "suggest even if missing" rule applies to topicals only.
5. Compose a plan that lists:
   - **Goal summary** (`goal_summary`, REQUIRED — non-empty): 2-3 paragraphs at medium-high level,
     what the issue is actually trying to accomplish. Intent, not implementation. The planner's
     restatement of the issue body in their own words. If a reader only sees this, they should be
     able to say "yes that's right" or "no, replan with this clarification." If you genuinely cannot
     summarize the goal, the issue is too thin to plan — add a `Question` asking the user to clarify
     intent rather than producing an empty `goal_summary`.
   - **Executive summary** (`executive_summary`, REQUIRED — non-empty): 3-4 plain-language sentences
     a non-coder PM would understand. What does the user get when this ships? Outcome-focused.
     **No file paths, no symbol names, no code references.** If you're tempted to mention
     `Foo.bar()`, write it differently.
   - **Plan markdown** (`plan_markdown`, REQUIRED): the technical body. Step-by-step, file
     rationale, code-level decisions. Lives under the collapsed `The Code` section in the PlanModal,
     so the audience for this content is a reviewer who's already decided to dig in — not a top-line
     summary.
   - Files to add/modify/delete
   - Detected dependencies (other open issue numbers)
   - Suggested labels (from step 4; empty array if nothing matches)
   - Open questions to ask the user (structured per §6.4 Question schema)

   The `goal_summary` and `executive_summary` fields are tagged `omitempty` in the Go struct so old
   on-disk plans deserialize cleanly, but the skill-level requirement is that NEW plans MUST have
   both populated. This is the dual interpretation: schema-level optional, skill-level required.

   **Question authoring rules (NON-NEGOTIABLE — the UI rendering depends on these):**
   - The `prompt` field is the *question only* — one or two sentences. Do NOT inline the answer choices into the prompt text ("A) … B) … C) …"). The prompt should read cleanly without the options.
   - The `options` array is the FULL TEXT of each choice, not letter shorthand. WRONG: `["A", "B", "C"]`. RIGHT: `["Both: app calls Orchestrator.SetAutoPullPaused() AND publishes EvtAutoPullPausedChanged.", "Direct method only — no event.", "Event only — orchestrator subscribes to EvtAutoPullPausedChanged."]`.
   - Use markdown freely in `prompt` — backticks for code, bold for emphasis, inline links. The UI renders prompts as markdown.
   - Keep each option text under ~280 chars where possible. Long options (a paragraph each) signal the question should split into multiple smaller ones.
   - For yes_no questions, omit `options` (or `[]`) — the UI hardcodes "yes"/"no".

   **Every question MUST set `default` to exactly one recommended answer.**

   - `single_choice`: `default` is the full text of one option string — the safest / most-conventional / best-aligned-with-the-issue-body choice.
   - `yes_no`: `default` is the literal string `"yes"` or `"no"`.
   - `multi_choice`: `default` is the full text of **one** option (the single most-likely choice).
   - `free_text`: `default` is a one-or-two-sentence sample answer the user can edit, accept, or replace. Never an empty string.

   **One recommendation per question — always.** If you find yourself wanting to recommend more than one answer for a single question, the question's scope is too large. Split it into multiple `yes_no` questions (or smaller `single_choice` questions), each with exactly one recommendation. This applies to `multi_choice` in particular: if you'd recommend two or more options, replace the `multi_choice` question with one `yes_no` per option.

   Refusing to recommend ("I'm not sure, you decide") is not allowed. If you genuinely cannot pick, the question should be split into smaller yes_no questions or removed.
6. Write the plan to `.prismconductor/plans/<issue>-rev1.json` using **exactly** these top-level keys (verbatim — `issue_number`, `revision`, etc., not `issue` / `rev`):

   ```json
   {
     "issue_number": 53,
     "revision": 1,
     "goal_summary": "...",
     "executive_summary": "...",
     "plan_markdown": "...",
     "files_to_modify": [
       { "path": "frontend/src/components/Card.tsx", "intent": "modify" }
     ],
     "dependencies_detected": [],
     "suggested_labels": ["enhancement"],
     "questions": [
       {
         "id": "q1",
         "type": "single_choice",
         "prompt": "...",
         "options": ["A", "B"],
         "default": "A",
         "required": true
       }
     ],
     "estimated_complexity": "S",
     "ready_to_execute": false
   }
   ```

   Field name rules (strict — the conductor's validator requires the canonical names):
   - `issue_number` (NOT `issue`).
   - `revision` (NOT `rev`).
   - `files_to_modify` is an array of `{path, intent}` objects, NOT `{add, modify, delete}` lists.
   - `suggested_labels` is an array of plain label strings (`"enhancement"`), NOT label-with-action strings (`"enhancement (create)"`).
   - Omit `suggested_labels` entirely if empty.
   - Do NOT include `workspace_id` — the conductor attaches it on read.

7. Print `Plan written to .prismconductor/plans/<issue>-rev1.json` so the conductor's PTY parser picks it up (§10.3).
8. Stop. Do not edit code.

## Revisions

If `.prismconductor/answers/<issue>-rev<N>.json` exists, regenerate as rev<N+1> consuming the answers.
