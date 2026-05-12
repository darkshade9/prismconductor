import { llm } from "../../wailsjs/go/models";

export type Fit = "excellent" | "good" | "fair" | "poor" | "unsuitable" | "";

export function fitForRole(hint: llm.ModelHint | null, role: string): Fit {
  if (!hint) return "";
  switch (role) {
    case "plan":        return (hint.plan_fit as Fit) || "";
    case "work":        return (hint.work_fit as Fit) || "";
    case "orchestrator": return (hint.orch_fit as Fit) || "";
    case "architect":   return (hint.architect_fit as Fit) || "";
    default:            return (hint.work_fit as Fit) || "";
  }
}

/** Numeric cost rank — lower is cheaper. */
const COST_RANK: Record<string, number> = {
  micro: 0,
  low: 1,
  medium: 2,
  high: 3,
  very_high: 4,
};

export function costRank(tier: string): number {
  return COST_RANK[tier] ?? 99;
}

/** Returns true when the fit level meets the threshold for a role. */
export function meetsThreshold(fit: Fit, role: string): boolean {
  if (!fit) return false;
  switch (role) {
    case "orchestrator":
      return fit === "excellent" || fit === "good" || fit === "fair";
    case "plan":
    case "work":
    case "architect":
    default:
      return fit === "excellent" || fit === "good";
  }
}

/**
 * Short text label appended to a native <select> option for the given hint.
 * Returns empty string when no hint is available.
 */
export function fitOptionSuffix(hint: llm.ModelHint | null, role: string): string {
  const fit = fitForRole(hint, role);
  switch (fit) {
    case "excellent": return " ✓";
    case "good":      return " ✓";
    case "fair":      return " ◦";
    case "poor":      return " ⚠ (Poor fit)";
    case "unsuitable": return " ✗ (Unsuitable)";
    default:          return " (?)";
  }
}
