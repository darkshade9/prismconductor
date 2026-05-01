import { describe, it, expect, beforeEach, vi } from "vitest";

// Mock the Wails bindings before importing the store.
vi.mock("../../wailsjs/go/main/App", () => ({
  GetLabelFilter: vi.fn().mockResolvedValue({ labels: [], mode: "or" }),
  SetLabelFilter: vi.fn().mockResolvedValue(undefined),
}));

// Import after mock setup so the module uses our stubs.
import { useLabelFilterStore, toggleLabel, setFilterMode, clearFilter } from "./useLabelFilterStore";
import { GetLabelFilter, SetLabelFilter } from "../../wailsjs/go/main/App";

const wsID = "ws-test";

beforeEach(() => {
  useLabelFilterStore.setState({ selected: [], mode: "or" });
  vi.clearAllMocks();
});

describe("useLabelFilterStore", () => {
  it("starts with empty selection and or mode", () => {
    const { selected, mode } = useLabelFilterStore.getState();
    expect(selected).toEqual([]);
    expect(mode).toBe("or");
  });

  it("toggle adds a label", () => {
    useLabelFilterStore.getState().toggle("bug");
    expect(useLabelFilterStore.getState().selected).toEqual(["bug"]);
  });

  it("toggle removes a label that is already selected", () => {
    useLabelFilterStore.setState({ selected: ["bug", "feature"] });
    useLabelFilterStore.getState().toggle("bug");
    expect(useLabelFilterStore.getState().selected).toEqual(["feature"]);
  });

  it("setMode changes the mode", () => {
    useLabelFilterStore.getState().setMode("and");
    expect(useLabelFilterStore.getState().mode).toBe("and");
  });

  it("clear resets selection and mode to or", () => {
    useLabelFilterStore.setState({ selected: ["bug"], mode: "and" });
    useLabelFilterStore.getState().clear();
    expect(useLabelFilterStore.getState().selected).toEqual([]);
    expect(useLabelFilterStore.getState().mode).toBe("or");
  });

  it("loadForWorkspace seeds state from GetLabelFilter", async () => {
    (GetLabelFilter as ReturnType<typeof vi.fn>).mockResolvedValueOnce({ labels: ["bug"], mode: "and" });
    await useLabelFilterStore.getState().loadForWorkspace(wsID);
    const { selected, mode } = useLabelFilterStore.getState();
    expect(selected).toEqual(["bug"]);
    expect(mode).toBe("and");
    expect(GetLabelFilter).toHaveBeenCalledWith(wsID);
  });

  it("loadForWorkspace resets state on empty workspaceID", async () => {
    useLabelFilterStore.setState({ selected: ["bug"], mode: "and" });
    await useLabelFilterStore.getState().loadForWorkspace("");
    expect(useLabelFilterStore.getState().selected).toEqual([]);
    expect(useLabelFilterStore.getState().mode).toBe("or");
  });

  it("loadForWorkspace falls back to empty on error", async () => {
    (GetLabelFilter as ReturnType<typeof vi.fn>).mockRejectedValueOnce(new Error("fail"));
    useLabelFilterStore.setState({ selected: ["bug"], mode: "and" });
    await useLabelFilterStore.getState().loadForWorkspace(wsID);
    expect(useLabelFilterStore.getState().selected).toEqual([]);
    expect(useLabelFilterStore.getState().mode).toBe("or");
  });
});

describe("toggleLabel action helper", () => {
  it("calls toggle and schedules SetLabelFilter", async () => {
    vi.useFakeTimers();
    toggleLabel(wsID, "bug");
    expect(useLabelFilterStore.getState().selected).toEqual(["bug"]);
    vi.advanceTimersByTime(300);
    expect(SetLabelFilter).toHaveBeenCalledWith(wsID, ["bug"], "or");
    vi.useRealTimers();
  });
});

describe("clearFilter action helper", () => {
  it("resets and persists", async () => {
    vi.useFakeTimers();
    useLabelFilterStore.setState({ selected: ["bug"], mode: "and" });
    clearFilter(wsID);
    expect(useLabelFilterStore.getState().selected).toEqual([]);
    vi.advanceTimersByTime(300);
    expect(SetLabelFilter).toHaveBeenCalledWith(wsID, [], "or");
    vi.useRealTimers();
  });
});

describe("setFilterMode action helper", () => {
  it("changes mode and persists", () => {
    vi.useFakeTimers();
    setFilterMode(wsID, "and");
    expect(useLabelFilterStore.getState().mode).toBe("and");
    vi.advanceTimersByTime(300);
    expect(SetLabelFilter).toHaveBeenCalledWith(wsID, [], "and");
    vi.useRealTimers();
  });
});

describe("board label filter logic (pure)", () => {
  type FakeIssue = { labels: string[] };
  function applyFilter(issues: FakeIssue[], selected: string[], mode: "and" | "or"): FakeIssue[] {
    if (selected.length === 0) return issues;
    const labelSet = new Set(selected);
    return issues.filter((i) => {
      const cardLabels = i.labels ?? [];
      return mode === "and"
        ? [...labelSet].every((l) => cardLabels.includes(l))
        : cardLabels.some((l) => labelSet.has(l));
    });
  }

  it("passthrough when no labels selected", () => {
    const issues = [{ labels: ["bug"] }, { labels: [] }];
    expect(applyFilter(issues, [], "or")).toHaveLength(2);
  });

  it("OR: passes card matching any selected label", () => {
    const issues = [{ labels: ["bug"] }, { labels: ["feature"] }, { labels: [] }];
    expect(applyFilter(issues, ["bug"], "or")).toEqual([{ labels: ["bug"] }]);
  });

  it("OR: multi-select matches either label", () => {
    const issues = [{ labels: ["bug"] }, { labels: ["feature"] }, { labels: ["docs"] }];
    expect(applyFilter(issues, ["bug", "feature"], "or")).toHaveLength(2);
  });

  it("AND: requires all selected labels", () => {
    const issues = [
      { labels: ["bug", "feature"] },
      { labels: ["bug"] },
      { labels: ["feature"] },
    ];
    expect(applyFilter(issues, ["bug", "feature"], "and")).toEqual([{ labels: ["bug", "feature"] }]);
  });

  it("AND with single label is equivalent to OR", () => {
    const issues = [{ labels: ["bug"] }, { labels: ["feature"] }];
    const orResult = applyFilter(issues, ["bug"], "or");
    const andResult = applyFilter(issues, ["bug"], "and");
    expect(orResult).toEqual(andResult);
  });
});
