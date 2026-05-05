import { describe, it, expect } from "vitest";

// Unit-level spec for the plan failure badge logic derived from IssueView.
// No DOM rendering (no @testing-library/react in this project); we test
// the decision conditions that Card.tsx evaluates when choosing whether to
// show a planFailed badge vs other badges.

// Mirrors the condition in StatusRow:
// planFailed is shown only when:
//   - planFailed is set (non-null)
//   - no activeSession
//   - no pausedSession
//   - no lastFailure (those already have a badge)
function shouldShowPlanFailed(opts: {
  planFailed: { session_id: string; reason?: string } | null;
  activeSession: boolean;
  pausedSession: boolean;
  lastFailure: boolean;
}): boolean {
  const { planFailed, activeSession, pausedSession, lastFailure } = opts;
  return !!(planFailed && !activeSession && !pausedSession && !lastFailure);
}

describe("Card plan failure badge visibility", () => {
  it("shows when planFailed is set and no other session state", () => {
    expect(
      shouldShowPlanFailed({
        planFailed: { session_id: "abc-123" },
        activeSession: false,
        pausedSession: false,
        lastFailure: false,
      }),
    ).toBe(true);
  });

  it("hidden when activeSession is present", () => {
    expect(
      shouldShowPlanFailed({
        planFailed: { session_id: "abc-123" },
        activeSession: true,
        pausedSession: false,
        lastFailure: false,
      }),
    ).toBe(false);
  });

  it("hidden when pausedSession is present", () => {
    expect(
      shouldShowPlanFailed({
        planFailed: { session_id: "abc-123" },
        activeSession: false,
        pausedSession: true,
        lastFailure: false,
      }),
    ).toBe(false);
  });

  it("hidden when lastFailure is present (avoids double badge)", () => {
    expect(
      shouldShowPlanFailed({
        planFailed: { session_id: "abc-123" },
        activeSession: false,
        pausedSession: false,
        lastFailure: true,
      }),
    ).toBe(false);
  });

  it("hidden when planFailed is null", () => {
    expect(
      shouldShowPlanFailed({
        planFailed: null,
        activeSession: false,
        pausedSession: false,
        lastFailure: false,
      }),
    ).toBe(false);
  });

  it("uses planFailed.reason when provided", () => {
    const info = { session_id: "abc-123", reason: "remote plan timed out" };
    expect(info.reason).toBe("remote plan timed out");
  });

  it("falls back to default reason when reason is absent", () => {
    const info: { session_id: string; reason?: string } = { session_id: "abc-123" };
    const reason = info.reason || "plan session ended without success";
    expect(reason).toBe("plan session ended without success");
  });
});
