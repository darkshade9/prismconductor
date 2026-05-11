import { describe, it, expect, beforeEach, vi } from "vitest";

vi.mock("../../wailsjs/go/main/App", () => ({
  ProbeProviderModels: vi.fn(),
}));

import { useModelsStore } from "./modelsStore";
import { ProbeProviderModels } from "../../wailsjs/go/main/App";

beforeEach(() => {
  useModelsStore.setState({ cache: {} });
  vi.clearAllMocks();
});

describe("useModelsStore", () => {
  it("getEntry returns null when not cached", () => {
    const entry = useModelsStore.getState().getEntry("ollama", "http://localhost:11434");
    expect(entry).toBeNull();
  });

  it("fetch calls ProbeProviderModels and caches the result", async () => {
    (ProbeProviderModels as ReturnType<typeof vi.fn>).mockResolvedValueOnce(["model-a", "model-b"]);
    const result = await useModelsStore.getState().fetch("ollama", "http://localhost:11434", "");
    expect(result).toEqual(["model-a", "model-b"]);
    expect(ProbeProviderModels).toHaveBeenCalledWith("ollama", "http://localhost:11434", "");
    const entry = useModelsStore.getState().getEntry("ollama", "http://localhost:11434");
    expect(entry).not.toBeNull();
    expect(entry?.models).toEqual(["model-a", "model-b"]);
    expect(entry?.error).toBeNull();
    expect(entry?.loading).toBe(false);
  });

  it("fetch returns cached result within TTL without calling ProbeProviderModels again", async () => {
    (ProbeProviderModels as ReturnType<typeof vi.fn>).mockResolvedValue(["model-a"]);
    await useModelsStore.getState().fetch("ollama", "http://localhost:11434", "");
    vi.clearAllMocks();
    const result = await useModelsStore.getState().fetch("ollama", "http://localhost:11434", "");
    expect(result).toEqual(["model-a"]);
    expect(ProbeProviderModels).not.toHaveBeenCalled();
  });

  it("fetch bypasses cache after TTL expires", async () => {
    vi.useFakeTimers();
    (ProbeProviderModels as ReturnType<typeof vi.fn>).mockResolvedValue(["model-a"]);
    await useModelsStore.getState().fetch("ollama", "http://localhost:11434", "");

    // Advance time past TTL (5 minutes)
    vi.advanceTimersByTime(5 * 60 * 1000 + 1);

    (ProbeProviderModels as ReturnType<typeof vi.fn>).mockResolvedValue(["model-b"]);
    const result = await useModelsStore.getState().fetch("ollama", "http://localhost:11434", "");
    expect(result).toEqual(["model-b"]);
    expect(ProbeProviderModels).toHaveBeenCalledTimes(2);
    vi.useRealTimers();
  });

  it("fetch stores error in cache entry on failure and rethrows", async () => {
    (ProbeProviderModels as ReturnType<typeof vi.fn>).mockRejectedValueOnce(new Error("connection refused"));
    await expect(
      useModelsStore.getState().fetch("ollama", "http://localhost:11434", "")
    ).rejects.toThrow("connection refused");

    const entry = useModelsStore.getState().getEntry("ollama", "http://localhost:11434");
    expect(entry).not.toBeNull();
    expect(entry?.error).toBe("connection refused");
    expect(entry?.loading).toBe(false);
    expect(entry?.models).toEqual([]);
  });

  it("fetch preserves previous models in cache entry on failure", async () => {
    (ProbeProviderModels as ReturnType<typeof vi.fn>).mockResolvedValueOnce(["model-a"]);
    await useModelsStore.getState().fetch("ollama", "http://localhost:11434", "");

    // Force cache expiry
    vi.useFakeTimers();
    vi.advanceTimersByTime(5 * 60 * 1000 + 1);

    (ProbeProviderModels as ReturnType<typeof vi.fn>).mockRejectedValueOnce(new Error("timeout"));
    await expect(
      useModelsStore.getState().fetch("ollama", "http://localhost:11434", "")
    ).rejects.toThrow("timeout");

    const entry = useModelsStore.getState().getEntry("ollama", "http://localhost:11434");
    // Previous models preserved in cache entry on error
    expect(entry?.models).toEqual(["model-a"]);
    expect(entry?.error).toBe("timeout");
    vi.useRealTimers();
  });

  it("invalidate removes the cache entry", async () => {
    (ProbeProviderModels as ReturnType<typeof vi.fn>).mockResolvedValueOnce(["model-a"]);
    await useModelsStore.getState().fetch("ollama", "http://localhost:11434", "");
    expect(useModelsStore.getState().getEntry("ollama", "http://localhost:11434")).not.toBeNull();

    useModelsStore.getState().invalidate("ollama", "http://localhost:11434");
    expect(useModelsStore.getState().getEntry("ollama", "http://localhost:11434")).toBeNull();
  });

  it("invalidate does not affect other cache entries", async () => {
    (ProbeProviderModels as ReturnType<typeof vi.fn>).mockResolvedValue(["model-a"]);
    await useModelsStore.getState().fetch("ollama", "http://localhost:11434", "");
    await useModelsStore.getState().fetch("openai", "https://api.openai.com", "sk-key");

    useModelsStore.getState().invalidate("ollama", "http://localhost:11434");

    expect(useModelsStore.getState().getEntry("ollama", "http://localhost:11434")).toBeNull();
    expect(useModelsStore.getState().getEntry("openai", "https://api.openai.com")).not.toBeNull();
  });

  it("sets loading=true during fetch before resolving", async () => {
    let resolveProbe!: (v: string[]) => void;
    (ProbeProviderModels as ReturnType<typeof vi.fn>).mockReturnValueOnce(
      new Promise<string[]>((resolve) => { resolveProbe = resolve; })
    );

    const fetchPromise = useModelsStore.getState().fetch("ollama", "http://localhost:11434", "");
    const entry = useModelsStore.getState().getEntry("ollama", "http://localhost:11434");
    expect(entry?.loading).toBe(true);

    resolveProbe(["model-x"]);
    await fetchPromise;
    const afterEntry = useModelsStore.getState().getEntry("ollama", "http://localhost:11434");
    expect(afterEntry?.loading).toBe(false);
  });

  it("cache is keyed by provider:endpoint so different combinations are independent", async () => {
    (ProbeProviderModels as ReturnType<typeof vi.fn>).mockResolvedValueOnce(["ollama-model"]);
    await useModelsStore.getState().fetch("ollama", "http://localhost:11434", "");
    (ProbeProviderModels as ReturnType<typeof vi.fn>).mockResolvedValueOnce(["openai-model"]);
    await useModelsStore.getState().fetch("openai", "https://api.openai.com", "key");

    expect(useModelsStore.getState().getEntry("ollama", "http://localhost:11434")?.models).toEqual(["ollama-model"]);
    expect(useModelsStore.getState().getEntry("openai", "https://api.openai.com")?.models).toEqual(["openai-model"]);
  });
});
