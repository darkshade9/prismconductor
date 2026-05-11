import { create } from "zustand";
import { ProbeProviderModels } from "../../wailsjs/go/main/App";

const TTL_MS = 5 * 60 * 1000;

export type CacheEntry = {
  models: string[];
  fetchedAt: number;
  loading: boolean;
  error: string | null;
};

type State = {
  cache: Record<string, CacheEntry>;
  getEntry: (provider: string, endpoint: string) => CacheEntry | null;
  fetch: (provider: string, endpoint: string, apiKey: string) => Promise<string[]>;
  invalidate: (provider: string, endpoint: string) => void;
};

function key(provider: string, endpoint: string): string {
  return `${provider}:${endpoint}`;
}

export const useModelsStore = create<State>((set, get) => ({
  cache: {},

  getEntry: (provider, endpoint) => get().cache[key(provider, endpoint)] ?? null,

  invalidate: (provider, endpoint) => {
    const k = key(provider, endpoint);
    set((s) => {
      const next = { ...s.cache };
      delete next[k];
      return { cache: next };
    });
  },

  fetch: async (provider, endpoint, apiKey) => {
    const k = key(provider, endpoint);
    const existing = get().cache[k];
    if (existing && !existing.loading && Date.now() - existing.fetchedAt < TTL_MS) {
      return existing.models;
    }
    set((s) => ({
      cache: {
        ...s.cache,
        [k]: {
          models: existing?.models ?? [],
          fetchedAt: existing?.fetchedAt ?? 0,
          loading: true,
          error: null,
        },
      },
    }));
    try {
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      const list = (await ProbeProviderModels(provider as any, endpoint, apiKey)) ?? [];
      set((s) => ({
        cache: {
          ...s.cache,
          [k]: { models: list, fetchedAt: Date.now(), loading: false, error: null },
        },
      }));
      return list;
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : String(err);
      set((s) => ({
        cache: {
          ...s.cache,
          [k]: {
            models: existing?.models ?? [],
            fetchedAt: existing?.fetchedAt ?? 0,
            loading: false,
            error: msg,
          },
        },
      }));
      throw err;
    }
  },
}));
