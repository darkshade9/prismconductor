// Provider icon registry for the card-header badge (issue #37).
// SVG strings use fill="currentColor" so the icon inherits the parent's
// text color. Rendered via dangerouslySetInnerHTML in a <span>.
// Source SVG files live in frontend/src/assets/providers/ per the plan.

const SVG_CLAUDE = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 16 16" fill="currentColor" width="16" height="16"><path d="M8 1L14.5 8L8 15L1.5 8Z"/></svg>`;

const SVG_OPENAI = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 16 16" fill="currentColor" width="16" height="16"><path fill-rule="evenodd" clip-rule="evenodd" d="M8 1.5a6.5 6.5 0 100 13 6.5 6.5 0 000-13zM0 8a8 8 0 1116 0A8 8 0 010 8z"/><path d="M8 5a.75.75 0 01.75.75v2.5h2.5a.75.75 0 010 1.5h-2.5v2.5a.75.75 0 01-1.5 0v-2.5h-2.5a.75.75 0 010-1.5h2.5v-2.5A.75.75 0 018 5z"/></svg>`;

const SVG_OLLAMA = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 16 16" fill="currentColor" width="16" height="16"><rect x="1" y="1" width="14" height="14" rx="3"/></svg>`;

const SVG_LMSTUDIO = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 16 16" fill="currentColor" width="16" height="16"><path d="M8 1L11.5 4.5 15 8 11.5 11.5 8 15 4.5 11.5 1 8 4.5 4.5Z"/></svg>`;

const SVG_GENERIC = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 16 16" fill="currentColor" width="16" height="16"><circle cx="8" cy="8" r="7"/></svg>`;

export type ProviderIconInfo = {
  svgContent: string;
  label: string;
};

const registry: Record<string, ProviderIconInfo> = {
  claude:   { svgContent: SVG_CLAUDE,   label: "Anthropic Claude" },
  openai:   { svgContent: SVG_OPENAI,   label: "OpenAI" },
  litellm:  { svgContent: SVG_OPENAI,   label: "LiteLLM" },
  ollama:   { svgContent: SVG_OLLAMA,   label: "Ollama" },
  lmstudio: { svgContent: SVG_LMSTUDIO, label: "LM Studio" },
  gemini:   { svgContent: SVG_OPENAI,   label: "Google Gemini" },
  generic:  { svgContent: SVG_GENERIC,  label: "Unknown provider" },
};

export function resolveProviderIcon(provider: string): ProviderIconInfo {
  return registry[provider] ?? registry.generic;
}
