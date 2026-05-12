import { describe, it, expect } from "vitest";
import { fitForRole, fitOptionSuffix, costRank, meetsThreshold } from "./fitGlyph";
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
  it("returns (?) for null hint", () => {
    expect(fitOptionSuffix(null, "plan")).toBe(" (?)");
  });
});
