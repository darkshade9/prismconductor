import { describe, it, expect } from "vitest";
import { resolveGlowClass, resolveCardBorderGlow } from "./glow";
import { GLOW_CLASS_MAP, GLOW_DEFAULTS } from "../stores/useGlowColorsStore";

describe("resolveGlowClass", () => {
  it("returns null when state is null", () => {
    expect(resolveGlowClass(null, GLOW_DEFAULTS.planning)).toBeNull();
  });

  it("returns null when entry is undefined", () => {
    expect(resolveGlowClass("planning", undefined)).toBeNull();
  });

  it("returns null when entry is disabled", () => {
    expect(resolveGlowClass("planning", { enabled: false, color: "#38bdf8" })).toBeNull();
  });

  it("returns the correct CSS class when enabled", () => {
    expect(resolveGlowClass("planning", { enabled: true, color: "#38bdf8" })).toBe(
      GLOW_CLASS_MAP.planning,
    );
  });

  it("review is disabled by default (opt-in)", () => {
    expect(GLOW_DEFAULTS.review.enabled).toBe(false);
    expect(resolveGlowClass("review", GLOW_DEFAULTS.review)).toBeNull();
  });

  it("returns review class when explicitly enabled", () => {
    expect(resolveGlowClass("review", { enabled: true, color: "#22c55e" })).toBe(
      GLOW_CLASS_MAP.review,
    );
  });
});

describe("resolveCardBorderGlow", () => {
  it("returns border-transparent + glow class when customizable glow is active", () => {
    const result = resolveCardBorderGlow("prs-glow-planning", null);
    expect(result).toBe("border-transparent prs-glow-planning");
  });

  it("returns hardcoded class when no custom glow", () => {
    const result = resolveCardBorderGlow(null, "border-pink-500 card-glow-waiting");
    expect(result).toBe("border-pink-500 card-glow-waiting");
  });

  it("prefers custom glow over hardcoded", () => {
    const result = resolveCardBorderGlow("prs-glow-blocked", "border-red-500 card-glow-blocked");
    expect(result).toBe("border-transparent prs-glow-blocked");
  });

  it("returns border-slate-700 when neither active", () => {
    expect(resolveCardBorderGlow(null, null)).toBe("border-slate-700");
  });
});
