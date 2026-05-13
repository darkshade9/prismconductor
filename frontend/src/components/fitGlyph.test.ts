import { describe, it, expect } from "vitest";
import {
  fitForRole,
  fitOptionSuffix,
  fitOptionPrefix,
  fitOptionDescriptor,
  fitRank,
  sortModelsByFit,
  costRank,
  meetsThreshold,
} from "./fitGlyph";
import { llm } from "../../wailsjs/go/models";

function hint(overrides: Partial<llm.ModelHint> = {}): llm.ModelHint {
  return new llm.ModelHint({
    plan_fit: "good",
    work_fit: "fair",
    orch_fit: "fair",
    architect_fit: "excellent",
    tool_support: "full",
    cost_tier: "low",
    ...overrides,
  });
}

describe("fitForRole", () => {
  it("returns plan_fit for role=plan", () => {
    expect(fitForRole(hint({ plan_fit: "excellent" }), "plan")).toBe("excellent");
  });
  it("returns work_fit for role=work", () => {
    expect(fitForRole(hint({ work_fit: "poor" }), "work")).toBe("poor");
  });
  it("returns orch_fit for role=orchestrator", () => {
    expect(fitForRole(hint({ orch_fit: "fair" }), "orchestrator")).toBe("fair");
  });
  it("returns architect_fit for role=architect", () => {
    expect(fitForRole(hint({ architect_fit: "unsuitable" }), "architect")).toBe("unsuitable");
  });
  it("falls back to work_fit for unknown role", () => {
    expect(fitForRole(hint({ work_fit: "good" }), "unknown")).toBe("good");
  });
  it("returns empty string when hint is null", () => {
    expect(fitForRole(null, "plan")).toBe("");
  });
});

describe("meetsThreshold", () => {
  it("plan/work: excellent and good pass, fair fails", () => {
    expect(meetsThreshold("excellent", "plan")).toBe(true);
    expect(meetsThreshold("good", "plan")).toBe(true);
    expect(meetsThreshold("fair", "plan")).toBe(false);
    expect(meetsThreshold("poor", "work")).toBe(false);
    expect(meetsThreshold("unsuitable", "work")).toBe(false);
  });
  it("orchestrator: excellent/good/fair pass, poor fails", () => {
    expect(meetsThreshold("excellent", "orchestrator")).toBe(true);
    expect(meetsThreshold("good", "orchestrator")).toBe(true);
    expect(meetsThreshold("fair", "orchestrator")).toBe(true);
    expect(meetsThreshold("poor", "orchestrator")).toBe(false);
    expect(meetsThreshold("unsuitable", "orchestrator")).toBe(false);
  });
  it("returns false for empty fit", () => {
    expect(meetsThreshold("", "plan")).toBe(false);
  });
});

describe("costRank", () => {
  it("micro < low < medium < high < very_high", () => {
    expect(costRank("micro")).toBeLessThan(costRank("low"));
    expect(costRank("low")).toBeLessThan(costRank("medium"));
    expect(costRank("medium")).toBeLessThan(costRank("high"));
    expect(costRank("high")).toBeLessThan(costRank("very_high"));
  });
  it("unknown tier returns large sentinel", () => {
    expect(costRank("unknown")).toBe(99);
  });
});

describe("fitRank", () => {
  it("excellent > good > fair > poor > unsuitable > unknown", () => {
    expect(fitRank("excellent")).toBeGreaterThan(fitRank("good"));
    expect(fitRank("good")).toBeGreaterThan(fitRank("fair"));
    expect(fitRank("fair")).toBeGreaterThan(fitRank("poor"));
    expect(fitRank("poor")).toBeGreaterThan(fitRank("unsuitable"));
    expect(fitRank("unsuitable")).toBeGreaterThan(fitRank(""));
  });
  it("unknown/empty returns 0", () => {
    expect(fitRank("")).toBe(0);
    expect(fitRank("unknown")).toBe(0);
  });
});

describe("fitOptionSuffix", () => {
  it("returns checkmark for excellent/good", () => {
    expect(fitOptionSuffix(hint({ plan_fit: "excellent" }), "plan")).toBe(" ✓");
    expect(fitOptionSuffix(hint({ plan_fit: "good" }), "plan")).toBe(" ✓");
  });
  it("returns dot for fair", () => {
    expect(fitOptionSuffix(hint({ plan_fit: "fair" }), "plan")).toBe(" ◦");
  });
  it("returns warning for poor", () => {
    expect(fitOptionSuffix(hint({ plan_fit: "poor" }), "plan")).toBe(" ⚠ (Poor fit)");
  });
  it("returns cross for unsuitable", () => {
    expect(fitOptionSuffix(hint({ plan_fit: "unsuitable" }), "plan")).toBe(" ✗ (Unsuitable)");
  });
  it("returns empty string for null hint", () => {
    expect(fitOptionSuffix(null, "plan")).toBe("");
  });
  it("returns empty string when hint present but role fit is empty", () => {
    expect(fitOptionSuffix(hint({ plan_fit: "" }), "plan")).toBe("");
  });
});

describe("fitOptionPrefix", () => {
  it("returns leading checkmark for excellent", () => {
    expect(fitOptionPrefix(hint({ plan_fit: "excellent" }), "plan")).toBe("✓ ");
  });
  it("returns leading checkmark for good", () => {
    expect(fitOptionPrefix(hint({ plan_fit: "good" }), "plan")).toBe("✓ ");
  });
  it("returns leading dot for fair", () => {
    expect(fitOptionPrefix(hint({ plan_fit: "fair" }), "plan")).toBe("· ");
  });
  it("returns leading warning for poor", () => {
    expect(fitOptionPrefix(hint({ plan_fit: "poor" }), "plan")).toBe("⚠ ");
  });
  it("returns leading cross for unsuitable", () => {
    expect(fitOptionPrefix(hint({ plan_fit: "unsuitable" }), "plan")).toBe("✗ ");
  });
  it("returns empty string for null hint", () => {
    expect(fitOptionPrefix(null, "plan")).toBe("");
  });
  it("returns empty string when hint present but role fit is empty", () => {
    expect(fitOptionPrefix(hint({ plan_fit: "" }), "plan")).toBe("");
  });
});

describe("fitOptionDescriptor", () => {
  it("returns (Fit · cost) when hint present with fit and cost", () => {
    expect(fitOptionDescriptor(hint({ plan_fit: "excellent", cost_tier: "high" }), "plan")).toBe(" (Excellent · high)");
  });
  it("returns (Fit) when hint present with fit but no cost tier", () => {
    expect(fitOptionDescriptor(hint({ plan_fit: "good", cost_tier: "" }), "plan")).toBe(" (Good)");
  });
  it("returns empty string for null hint", () => {
    expect(fitOptionDescriptor(null, "plan")).toBe("");
  });
  it("returns empty string when fit is empty", () => {
    expect(fitOptionDescriptor(hint({ plan_fit: "" }), "plan")).toBe("");
  });
});

describe("sortModelsByFit", () => {
  const modelsHints: Record<string, llm.ModelHint | null> = {
    "model-excellent-high": hint({ plan_fit: "excellent", cost_tier: "high" }),
    "model-excellent-low": hint({ plan_fit: "excellent", cost_tier: "low" }),
    "model-good": hint({ plan_fit: "good", cost_tier: "medium" }),
    "model-fair": hint({ plan_fit: "fair", cost_tier: "low" }),
    "model-unknown-1": null,
    "model-unknown-2": null,
  };

  it("sorts by descending fit rank", () => {
    const models = ["model-fair", "model-good", "model-excellent-low"];
    const sorted = sortModelsByFit(models, modelsHints, "plan");
    expect(sorted[0]).toBe("model-excellent-low");
    expect(sorted[1]).toBe("model-good");
    expect(sorted[2]).toBe("model-fair");
  });

  it("uses cost as tiebreaker within the same fit level", () => {
    const models = ["model-excellent-high", "model-excellent-low"];
    const sorted = sortModelsByFit(models, modelsHints, "plan");
    expect(sorted[0]).toBe("model-excellent-low");
    expect(sorted[1]).toBe("model-excellent-high");
  });

  it("unknown-hint models sort last, then alphabetically", () => {
    const models = ["model-unknown-2", "model-fair", "model-unknown-1"];
    const sorted = sortModelsByFit(models, modelsHints, "plan");
    expect(sorted[0]).toBe("model-fair");
    expect(sorted[1]).toBe("model-unknown-1");
    expect(sorted[2]).toBe("model-unknown-2");
  });

  it("does not mutate the input array", () => {
    const models = ["model-fair", "model-good"];
    const copy = [...models];
    sortModelsByFit(models, modelsHints, "plan");
    expect(models).toEqual(copy);
  });
});
