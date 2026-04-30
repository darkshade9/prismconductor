import { describe, it, expect } from "vitest";
import { matchesQuery } from "./matchesQuery";

describe("matchesQuery", () => {
  it("returns true for empty query regardless of title", () => {
    expect(matchesQuery("hello", "")).toBe(true);
    expect(matchesQuery(undefined, "")).toBe(true);
    expect(matchesQuery(null, "")).toBe(true);
  });

  it("case-insensitive substring match", () => {
    expect(matchesQuery("Fix Bug", "fix")).toBe(true);
    expect(matchesQuery("fix bug", "Fix")).toBe(true);
    expect(matchesQuery("Fix Bug", "FIX BUG")).toBe(true);
    expect(matchesQuery("some title here", "title")).toBe(true);
  });

  it("returns false for non-matching query", () => {
    expect(matchesQuery("hello world", "xyz")).toBe(false);
  });

  it("returns false for null/undefined title with non-empty query", () => {
    expect(matchesQuery(null, "x")).toBe(false);
    expect(matchesQuery(undefined, "x")).toBe(false);
  });
});
