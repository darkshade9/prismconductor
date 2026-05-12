import { useCallback, useEffect, useRef } from "react";
import { GetIssueView } from "../../wailsjs/go/main/App";
import { issueview } from "../../wailsjs/go/models";
import { useIssueViewStore } from "./useIssueViewStore";
import { useIssueStore } from "./issueStore";
import { useWorkspaceStore } from "./workspaceStore";
import { useSettingsStore } from "./useSettingsStore";

export type ReconcilerDriftEntry = {
  kind: "reconciler_drift";
  workspaceID: string;
  issueNumber: number;
  missedFields: string[];
  ts: number;
};

const DRIFT_CAP = 200;
const driftBuffer: ReconcilerDriftEntry[] = [];

export function logDrift(entry: ReconcilerDriftEntry): void {
  driftBuffer.unshift(entry);
  if (driftBuffer.length > DRIFT_CAP) driftBuffer.pop();
}

export function getDriftBuffer(): ReconcilerDriftEntry[] {
  return driftBuffer.slice();
}

/** Compare load-bearing fields; return names of fields that differ. */
export function diffIssueView(
  cached: issueview.IssueView,
  fresh: issueview.IssueView,
): string[] {
  const missed: string[] = [];

  if (cached.card_state !== fresh.card_state) missed.push("card_state");
  if (cached.derived_column !== fresh.derived_column) missed.push("derived_column");

  const cachedActiveState = cached.active_session?.state ?? null;
  const freshActiveState = fresh.active_session?.state ?? null;
  if (cachedActiveState !== freshActiveState) missed.push("active_session.state");

  const cachedFailureID = cached.last_failure?.id ?? null;
  const freshFailureID = fresh.last_failure?.id ?? null;
  if (cachedFailureID !== freshFailureID) missed.push("last_failure");

  const cachedTestsFailing = cached.tests_failing_info != null;
  const freshTestsFailing = fresh.tests_failing_info != null;
  if (cachedTestsFailing !== freshTestsFailing) missed.push("tests_failing_info");

  const cachedConflicts = cached.conflicts_info != null;
  const freshConflicts = fresh.conflicts_info != null;
  if (cachedConflicts !== freshConflicts) missed.push("conflicts_info");

  const cachedNeedsPR = cached.needs_pr_info != null;
  const freshNeedsPR = fresh.needs_pr_info != null;
  if (cachedNeedsPR !== freshNeedsPR) missed.push("needs_pr_info");

  const cachedPlanReady = cached.latest_plan?.ready_to_execute ?? null;
  const freshPlanReady = fresh.latest_plan?.ready_to_execute ?? null;
  if (cachedPlanReady !== freshPlanReady) missed.push("latest_plan.ready_to_execute");

  return missed;
}

const RECONCILER_INTERVAL_MS = 5000;

export function useReconciler(): void {
  const reconcilerEnabled = useSettingsStore((s) => s.reconcilerEnabled);
  const workspaceID = useWorkspaceStore((s) => s.selectedID);
  const issues = useIssueStore((s) => s.issues);

  // Refs so the interval/focus handler always see latest values without re-wiring.
  const enabledRef = useRef(reconcilerEnabled);
  const workspaceIDRef = useRef(workspaceID);
  const issuesRef = useRef(issues);

  useEffect(() => { enabledRef.current = reconcilerEnabled; }, [reconcilerEnabled]);
  useEffect(() => { workspaceIDRef.current = workspaceID; }, [workspaceID]);
  useEffect(() => { issuesRef.current = issues; }, [issues]);

  const runReconcile = useCallback(async () => {
    if (!enabledRef.current) return;
    const wsID = workspaceIDRef.current;
    if (!wsID) return;
    const visibleIssues = issuesRef.current.filter((i) => i.workspace_id === wsID);
    for (const issue of visibleIssues) {
      try {
        const fresh = await GetIssueView(wsID, issue.number);
        if (!fresh) continue;
        const cached = useIssueViewStore.getState().get(wsID, issue.number);
        if (!cached) {
          useIssueViewStore.setState((s) => ({
            views: { ...s.views, [`${wsID}#${issue.number}`]: fresh },
          }));
          continue;
        }
        const missed = diffIssueView(cached, fresh);
        if (missed.length > 0) {
          useIssueViewStore.setState((s) => ({
            views: { ...s.views, [`${wsID}#${issue.number}`]: fresh },
          }));
          logDrift({
            kind: "reconciler_drift",
            workspaceID: wsID,
            issueNumber: issue.number,
            missedFields: missed,
            ts: Date.now(),
          });
        }
      } catch {
        // IPC errors are transient; skip and try next issue.
      }
    }
  }, []);

  useEffect(() => {
    const tick = () => { void runReconcile(); };
    const id = setInterval(tick, RECONCILER_INTERVAL_MS);
    window.addEventListener("focus", tick);
    return () => {
      clearInterval(id);
      window.removeEventListener("focus", tick);
    };
  }, [runReconcile]);

  // Re-run immediately on workspace switch.
  useEffect(() => {
    if (workspaceID) void runReconcile();
  }, [workspaceID, runReconcile]);
}
