import { describe, it, expect } from "vitest";
import { getPoolSummaryByRole } from "./usePoolsStore";

type FakePool = {
  pool: {
    id: string;
    name: string;
    provider: string;
    role: string;
    capacity: number;
    enabled: boolean;
    max_input_tokens?: number;
    max_turns?: number;
  };
  active: number;
};

// Cast to the expected type to keep tests decoupled from Wails model imports.
// eslint-disable-next-line @typescript-eslint/no-explicit-any
const make = (rows: FakePool[]) => getPoolSummaryByRole(rows as any);

describe("getPoolSummaryByRole", () => {
  it("returns empty array when no pools", () => {
    expect(make([])).toEqual([]);
  });

  it("omits disabled pools entirely", () => {
    const result = make([
      { pool: { id: "a", name: "p1", provider: "claude", role: "work", capacity: 3, enabled: false }, active: 1 },
    ]);
    expect(result).toEqual([]);
  });

  it("returns single role with one provider", () => {
    const result = make([
      { pool: { id: "a", name: "main", provider: "claude", role: "work", capacity: 3, enabled: true }, active: 1 },
    ]);
    expect(result).toHaveLength(1);
    expect(result[0].role).toBe("work");
    expect(result[0].label).toBe("Workers");
    expect(result[0].providers).toHaveLength(1);
    expect(result[0].providers[0]).toMatchObject({ kind: "claude", active: 1, capacity: 3, poolNames: ["main"] });
  });

  it("aggregates multiple pools with same role+provider", () => {
    const result = make([
      { pool: { id: "a", name: "claude-main", provider: "claude", role: "work", capacity: 3, enabled: true }, active: 1 },
      { pool: { id: "b", name: "claude-aux", provider: "claude", role: "work", capacity: 2, enabled: true }, active: 2 },
    ]);
    expect(result).toHaveLength(1);
    const entry = result[0].providers[0];
    expect(entry.kind).toBe("claude");
    expect(entry.active).toBe(3);
    expect(entry.capacity).toBe(5);
    expect(entry.poolNames).toContain("claude-main");
    expect(entry.poolNames).toContain("claude-aux");
  });

  it("groups by role correctly", () => {
    const result = make([
      { pool: { id: "a", name: "orch", provider: "claude", role: "orchestrator", capacity: 1, enabled: true }, active: 0 },
      { pool: { id: "b", name: "plan", provider: "openai", role: "plan", capacity: 2, enabled: true }, active: 1 },
      { pool: { id: "c", name: "work", provider: "ollama", role: "work", capacity: 4, enabled: true }, active: 2 },
    ]);
    const roles = result.map((r) => r.role);
    expect(roles).toEqual(["orchestrator", "plan", "work"]);
  });

  it("orders providers alphabetically within a role", () => {
    const result = make([
      { pool: { id: "a", name: "z-pool", provider: "openai", role: "work", capacity: 2, enabled: true }, active: 1 },
      { pool: { id: "b", name: "a-pool", provider: "claude", role: "work", capacity: 3, enabled: true }, active: 0 },
    ]);
    const kinds = result[0].providers.map((p) => p.kind);
    expect(kinds).toEqual(["claude", "openai"]);
  });

  it("omits roles with zero enabled pools", () => {
    const result = make([
      { pool: { id: "a", name: "p", provider: "claude", role: "plan", capacity: 2, enabled: true }, active: 1 },
    ]);
    const roles = result.map((r) => r.role);
    expect(roles).not.toContain("orchestrator");
    expect(roles).not.toContain("work");
    expect(roles).toContain("plan");
  });

  it("does not color amber (zero capacity should not show saturated)", () => {
    const result = make([
      { pool: { id: "a", name: "p", provider: "claude", role: "work", capacity: 0, enabled: true }, active: 0 },
    ]);
    const entry = result[0].providers[0];
    expect(entry.active).toBe(0);
    expect(entry.capacity).toBe(0);
  });

  it("falls back to 'work' role when role is missing", () => {
    const result = make([
      { pool: { id: "a", name: "p", provider: "claude", role: "", capacity: 2, enabled: true }, active: 1 },
    ]);
    expect(result[0].role).toBe("work");
  });

  it("includes budgets array with one entry per pool", () => {
    const result = make([
      { pool: { id: "a", name: "main", provider: "claude", role: "work", capacity: 3, enabled: true, max_input_tokens: 100000, max_turns: 50 }, active: 1 },
    ]);
    expect(result[0].providers[0].budgets).toHaveLength(1);
    expect(result[0].providers[0].budgets[0]).toMatchObject({ name: "main", max_input_tokens: 100000, max_turns: 50 });
  });

  it("accumulates budgets across multiple pools in the same provider group", () => {
    const result = make([
      { pool: { id: "a", name: "claude-a", provider: "claude", role: "work", capacity: 3, enabled: true, max_input_tokens: 80000 }, active: 1 },
      { pool: { id: "b", name: "claude-b", provider: "claude", role: "work", capacity: 2, enabled: true, max_turns: 40 }, active: 0 },
    ]);
    const budgets = result[0].providers[0].budgets;
    expect(budgets).toHaveLength(2);
    expect(budgets.find((b) => b.name === "claude-a")).toMatchObject({ max_input_tokens: 80000 });
    expect(budgets.find((b) => b.name === "claude-b")).toMatchObject({ max_turns: 40 });
  });

  it("includes budget entry even when no budget fields are set", () => {
    const result = make([
      { pool: { id: "a", name: "p", provider: "claude", role: "work", capacity: 2, enabled: true }, active: 1 },
    ]);
    expect(result[0].providers[0].budgets).toHaveLength(1);
    expect(result[0].providers[0].budgets[0].name).toBe("p");
    expect(result[0].providers[0].budgets[0].max_input_tokens).toBeUndefined();
    expect(result[0].providers[0].budgets[0].max_turns).toBeUndefined();
  });
});
