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
 * Returns empty string when no hint is available or fit is unknown.
 */
export function fitOptionSuffix(hint: llm.ModelHint | null, role: string): string {
  const fit = fitForRole(hint, role);
  switch (fit) {
    case "excellent": return " ✓";
    case "good":      return " ✓";
    case "fair":      return " ◦";
    case "poor":      return " ⚠ (Poor fit)";
    case "unsuitable": return " ✗ (Unsuitable)";
    default:          return "";
  }
}

/** Numeric fit rank — higher is better. Unknown/empty → 0. */
const FIT_RANK: Record<string, number> = {
  excellent: 5,
  good: 4,
  fair: 3,
  poor: 2,
  unsuitable: 1,
};

export function fitRank(fit: string): number {
  return FIT_RANK[fit] ?? 0;
}

/** Leading glyph prefix for use at the start of a dropdown option label. */
export function fitOptionPrefix(hint: llm.ModelHint | null, role: string): string {
  const fit = fitForRole(hint, role);
  switch (fit) {
    case "excellent": return "✓ ";
    case "good":      return "✓ ";
    case "fair":      return "· ";
    case "poor":      return "⚠ ";
    case "unsuitable": return "✗ ";
    default:          return "";
  }
}

/** Parenthetical descriptor appended after the model name: " (Excellent · high)". */
export function fitOptionDescriptor(hint: llm.ModelHint | null, role: string): string {
  const fit = fitForRole(hint, role);
  if (!fit) return "";
  const label = fit.charAt(0).toUpperCase() + fit.slice(1);
  const cost = hint?.cost_tier ?? "";
  return cost ? ` (${label} · ${cost})` : ` (${label})`;
}

/**
 * Sorts a model list by descending fit rank, then ascending cost rank, then
 * alphabetical. Models with no hint data sort to the end.
 */
export function sortModelsByFit(
  models: string[],
  hints: Record<string, llm.ModelHint | null>,
  role: string,
): string[] {
  return [...models].sort((a, b) => {
    const hintA = hints[a] ?? null;
    const hintB = hints[b] ?? null;
    const rankDiff = fitRank(fitForRole(hintB, role)) - fitRank(fitForRole(hintA, role));
    if (rankDiff !== 0) return rankDiff;
    const costDiff = costRank(hintA?.cost_tier ?? "") - costRank(hintB?.cost_tier ?? "");
    if (costDiff !== 0) return costDiff;
    return a.toLowerCase().localeCompare(b.toLowerCase());
  });
}
