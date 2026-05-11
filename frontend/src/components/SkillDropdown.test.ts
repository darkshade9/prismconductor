import { describe, it, expect } from "vitest";
import { placeholderText } from "./SkillDropdown";
import { conductorFirst } from "./SkillProfileEditor";
import { types } from "../../wailsjs/go/models";

function ref(path: string): types.SkillRef {
  return new types.SkillRef({ path, display_name: path, source: "bundled" });
}

describe("placeholderText", () => {
  it("includes the stage name", () => {
    expect(placeholderText("plan")).toBe("(default: bundled conductor-plan)");
    expect(placeholderText("execute")).toBe("(default: bundled conductor-execute)");
    expect(placeholderText("continue")).toBe("(default: bundled conductor-continue)");
    expect(placeholderText("close")).toBe("(default: bundled conductor-close)");
  });
});

describe("conductorFirst", () => {
  it("places conductor-* skills before others", () => {
    const input = [ref("bug-hunter-state"), ref("conductor-plan"), ref("conductor-execute"), ref("simplify")];
    const result = conductorFirst(input);
    expect(result[0].path).toBe("conductor-plan");
    expect(result[1].path).toBe("conductor-execute");
    expect(result[2].path).toBe("bug-hunter-state");
    expect(result[3].path).toBe("simplify");
  });

  it("does not mutate the original array", () => {
    const input = [ref("zzz-skill"), ref("conductor-plan")];
    conductorFirst(input);
    expect(input[0].path).toBe("zzz-skill");
  });

  it("returns stable result when all are conductor-*", () => {
    const input = [ref("conductor-close"), ref("conductor-plan")];
    const result = conductorFirst(input);
    expect(result.map((r) => r.path)).toEqual(["conductor-close", "conductor-plan"]);
  });

  it("returns stable result when none are conductor-*", () => {
    const input = [ref("simplify"), ref("bug-hunter-state")];
    const result = conductorFirst(input);
    expect(result.map((r) => r.path)).toEqual(["simplify", "bug-hunter-state"]);
  });
});
