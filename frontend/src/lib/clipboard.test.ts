import { describe, it, expect, vi, beforeEach } from "vitest";

vi.mock("../../wailsjs/runtime/runtime", () => ({
  ClipboardSetText: vi.fn(),
}));

import { writeToClipboard } from "./clipboard";
import { ClipboardSetText } from "../../wailsjs/runtime/runtime";

describe("writeToClipboard", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("calls ClipboardSetText with the provided text", async () => {
    vi.mocked(ClipboardSetText).mockResolvedValue(undefined);
    await writeToClipboard("hello");
    expect(ClipboardSetText).toHaveBeenCalledWith("hello");
  });

  it("falls back to navigator.clipboard when ClipboardSetText throws", async () => {
    vi.mocked(ClipboardSetText).mockRejectedValue(new Error("wails unavailable"));
    const writeText = vi.fn().mockResolvedValue(undefined);
    Object.defineProperty(navigator, "clipboard", {
      value: { writeText },
      configurable: true,
    });
    await writeToClipboard("fallback text");
    expect(writeText).toHaveBeenCalledWith("fallback text");
  });

  it("does not throw when both clipboard methods fail", async () => {
    vi.mocked(ClipboardSetText).mockRejectedValue(new Error("wails unavailable"));
    Object.defineProperty(navigator, "clipboard", {
      value: { writeText: vi.fn().mockRejectedValue(new Error("clipboard denied")) },
      configurable: true,
    });
    await expect(writeToClipboard("text")).resolves.not.toThrow();
  });
});
