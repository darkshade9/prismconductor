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
});
