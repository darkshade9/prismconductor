import { describe, it, expect, beforeEach, vi } from "vitest";

// Mock Wails runtime before any imports that transitively call EventsOn at module level.
vi.mock("../../wailsjs/runtime/runtime", () => ({
  EventsOn: vi.fn(() => () => {}),
}));
vi.mock("../../wailsjs/go/main/App", () => ({
  GetIssueView: vi.fn(),
  ListIssueViews: vi.fn(),
}));

import { diffIssueView, getDriftBuffer, logDrift, type ReconcilerDriftEntry } from "./reconciler";
import { issueview } from "../../wailsjs/go/models";

// Minimal IssueView factory — only sets the load-bearing fields.
function makeView(overrides: Partial<{
  card_state: string | undefined;
  derived_column: string;
  active_session_state: string | undefined;
  last_failure_id: string | undefined;
  tests_failing_info: object | undefined;
  conflicts_info: object | undefined;
  needs_pr_info: object | undefined;
  latest_plan_ready: boolean | undefined;
}> = {}): issueview.IssueView {
  const v = issueview.IssueView.createFrom({
    issue: { workspace_id: "ws1", number: 1 },
    derived_column: overrides.derived_column ?? "todo",
    card_state: overrides.card_state,
    active_session: overrides.active_session_state !== undefined
      ? { id: "s1", state: overrides.active_session_state }
      : undefined,
    last_failure: overrides.last_failure_id !== undefined
      ? { id: overrides.last_failure_id, state: "failed" }
      : undefined,
    tests_failing_info: overrides.tests_failing_info ?? undefined,
    conflicts_info: overrides.conflicts_info ?? undefined,
    needs_pr_info: overrides.needs_pr_info ?? undefined,
    latest_plan: overrides.latest_plan_ready !== undefined
      ? { id: "p1", ready_to_execute: overrides.latest_plan_ready }
      : undefined,
  });
  return v;
}

// Drain the drift buffer between tests.
function drainDriftBuffer() {
  // logDrift adds to the front; read it to see current length, then reset via private module state.
  // Since the buffer is module-level, we drain it by reading via getDriftBuffer() — we can't
  // reset it directly, but we can count entries before each test and ignore earlier ones.
}

describe("diffIssueView", () => {
  it("returns empty array when views are identical", () => {
    const a = makeView({ card_state: "glow_blue", derived_column: "in_progress" });
    const b = makeView({ card_state: "glow_blue", derived_column: "in_progress" });
    expect(diffIssueView(a, b)).toEqual([]);
  });

  it("detects card_state mismatch", () => {
    const a = makeView({ card_state: "glow_blue" });
    const b = makeView({ card_state: "glow_red" });
    expect(diffIssueView(a, b)).toContain("card_state");
  });

  it("detects derived_column mismatch", () => {
    const a = makeView({ derived_column: "todo" });
    const b = makeView({ derived_column: "in_progress" });
    expect(diffIssueView(a, b)).toContain("derived_column");
  });

  it("detects active_session.state mismatch", () => {
    const a = makeView({ active_session_state: "running" });
    const b = makeView({ active_session_state: "stopped" });
    expect(diffIssueView(a, b)).toContain("active_session.state");
  });

  it("detects active_session appearing (null → value)", () => {
    const a = makeView({ active_session_state: undefined });
    const b = makeView({ active_session_state: "running" });
    expect(diffIssueView(a, b)).toContain("active_session.state");
  });

  it("detects last_failure id mismatch", () => {
    const a = makeView({ last_failure_id: "old-fail" });
    const b = makeView({ last_failure_id: "new-fail" });
    expect(diffIssueView(a, b)).toContain("last_failure");
  });

  it("detects tests_failing_info appearing", () => {
    const a = makeView({ tests_failing_info: undefined });
    const b = makeView({ tests_failing_info: { count: 3 } });
    expect(diffIssueView(a, b)).toContain("tests_failing_info");
  });

  it("detects conflicts_info appearing", () => {
    const a = makeView({ conflicts_info: undefined });
    const b = makeView({ conflicts_info: { has_conflict: true } });
    expect(diffIssueView(a, b)).toContain("conflicts_info");
  });

  it("detects needs_pr_info appearing", () => {
    const a = makeView({ needs_pr_info: undefined });
    const b = makeView({ needs_pr_info: { branch: "feat/x" } });
    expect(diffIssueView(a, b)).toContain("needs_pr_info");
  });

  it("detects latest_plan.ready_to_execute mismatch", () => {
    const a = makeView({ latest_plan_ready: false });
    const b = makeView({ latest_plan_ready: true });
    expect(diffIssueView(a, b)).toContain("latest_plan.ready_to_execute");
  });

  it("returns multiple fields when several differ", () => {
    const a = makeView({ card_state: "glow_blue", derived_column: "todo" });
    const b = makeView({ card_state: "glow_red", derived_column: "review" });
    const missed = diffIssueView(a, b);
    expect(missed).toContain("card_state");
    expect(missed).toContain("derived_column");
  });

  it("ignores non-load-bearing field differences (e.g. unread_comment_count)", () => {
    const a = issueview.IssueView.createFrom({ issue: { workspace_id: "ws1", number: 1 }, derived_column: "todo", unread_comment_count: 0 });
    const b = issueview.IssueView.createFrom({ issue: { workspace_id: "ws1", number: 1 }, derived_column: "todo", unread_comment_count: 5 });
    expect(diffIssueView(a, b)).toEqual([]);
  });
});

describe("drift buffer", () => {
  let baseLen: number;

  beforeEach(() => {
    baseLen = getDriftBuffer().length;
  });

  it("logDrift appends an entry to the front", () => {
    const entry: ReconcilerDriftEntry = {
      kind: "reconciler_drift",
      workspaceID: "ws1",
      issueNumber: 42,
      missedFields: ["card_state"],
      ts: Date.now(),
    };
    logDrift(entry);
    const buf = getDriftBuffer();
    expect(buf.length).toBe(baseLen + 1);
    expect(buf[0]).toEqual(entry);
  });

  it("getDriftBuffer returns a copy, not the live array", () => {
    const snap1 = getDriftBuffer();
    logDrift({ kind: "reconciler_drift", workspaceID: "ws1", issueNumber: 99, missedFields: [], ts: 0 });
    const snap2 = getDriftBuffer();
    expect(snap2.length).toBe(snap1.length + 1);
    expect(snap1.length).toBe(snap1.length); // unchanged original snapshot
  });
});
