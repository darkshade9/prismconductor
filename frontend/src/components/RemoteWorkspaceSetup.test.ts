import { describe, it, expect } from "vitest";
import { STEPS, COLOR_PALETTE, CF_REQUIRED_SCOPES, GITHUB_FINE_GRAINED_SCOPES } from "./RemoteWorkspaceSetup";

describe("STEPS", () => {
  it("has exactly 5 steps in wizard order", () => {
    expect(STEPS.map((s) => s.id)).toEqual(["cf-token", "github-pat", "repo", "deploy", "done"]);
  });

  it("every step has a non-empty label", () => {
    for (const step of STEPS) {
      expect(step.label.length).toBeGreaterThan(0);
    }
  });
});

describe("COLOR_PALETTE", () => {
  it("has 8 colors", () => {
    expect(COLOR_PALETTE).toHaveLength(8);
  });

  it("all entries are valid hex colors", () => {
    for (const color of COLOR_PALETTE) {
      expect(color).toMatch(/^#[0-9a-f]{6}$/i);
    }
  });
});

describe("CF_REQUIRED_SCOPES", () => {
  it("includes Workers Scripts Edit", () => {
    const found = CF_REQUIRED_SCOPES.find(
      (s) => s.category.includes("Workers Scripts") && s.permission === "Edit",
    );
    expect(found).toBeDefined();
  });

  it("includes Account Settings Read", () => {
    const found = CF_REQUIRED_SCOPES.find(
      (s) => s.category.includes("Account Settings") && s.permission === "Read",
    );
    expect(found).toBeDefined();
  });

  it("has exactly 2 required scopes", () => {
    expect(CF_REQUIRED_SCOPES).toHaveLength(2);
  });
});

describe("GITHUB_FINE_GRAINED_SCOPES", () => {
  it("includes Contents Read and write", () => {
    const found = GITHUB_FINE_GRAINED_SCOPES.find(
      (s) => s.permission === "Contents" && s.level.includes("Read and write"),
    );
    expect(found).toBeDefined();
  });

  it("includes Pull requests Read and write", () => {
    const found = GITHUB_FINE_GRAINED_SCOPES.find(
      (s) => s.permission === "Pull requests" && s.level.includes("Read and write"),
    );
    expect(found).toBeDefined();
  });

  it("includes Metadata Read", () => {
    const found = GITHUB_FINE_GRAINED_SCOPES.find(
      (s) => s.permission === "Metadata",
    );
    expect(found).toBeDefined();
  });

  it("has exactly 3 scopes", () => {
    expect(GITHUB_FINE_GRAINED_SCOPES).toHaveLength(3);
  });
});
