import { describe, it, expect } from "vitest";
import { toQuotedBlock } from "./CopyMenu";

describe("toQuotedBlock", () => {
  it("prefixes single-line text with '> '", () => {
    expect(toQuotedBlock("hello")).toBe("> hello");
  });

  it("prefixes each line of multi-line text", () => {
    expect(toQuotedBlock("line1\nline2\nline3")).toBe("> line1\n> line2\n> line3");
  });

  it("trims leading/trailing whitespace before quoting", () => {
    expect(toQuotedBlock("  hello  ")).toBe("> hello");
  });

  it("preserves internal whitespace in each line", () => {
    expect(toQuotedBlock("BLOCKED: tool denied: wails build\n  detail here")).toBe(
      "> BLOCKED: tool denied: wails build\n>   detail here",
    );
  });

  it("handles empty string after trim", () => {
    expect(toQuotedBlock("")).toBe("> ");
  });
});
