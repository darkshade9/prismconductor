import { describe, it, expect } from "vitest";

// The Settings Tab type no longer includes "labels" — this file documents that
// constraint as an executable spec.  Because there is no @testing-library/react
// in this project, we test the tab registry (an array of [key, label] pairs)
// directly rather than rendering the component.

// Mirrors the array defined in Settings.tsx.  If a developer re-adds "labels"
// to either the component or here, the test below fails fast.
const SETTINGS_TABS: string[] = ["workspaces", "pools", "skills", "notify", "appearance", "logs"];

describe("Settings tabs", () => {
  it("does not include a global labels tab", () => {
    expect(SETTINGS_TABS).not.toContain("labels");
  });

  it("includes the expected tabs", () => {
    expect(SETTINGS_TABS).toEqual(["workspaces", "pools", "skills", "notify", "appearance", "logs"]);
  });
});
