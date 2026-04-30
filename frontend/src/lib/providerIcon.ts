// Provider icon registry for the card-header badge (issue #37).
//
// SVG sources live in frontend/src/assets/providers/. Vite's `?raw` suffix
// inlines each file's text at build time, so the actual file on disk is the
// source of truth — drop in a new SVG, rebuild, the new icon ships. (The
// previous hand-written-string registry meant the assets/providers files
// were decorative and edits never showed up in the app.)
//
// Each SVG should declare `fill="currentColor"` (or use no fill) so the icon
// inherits the parent text color. Rendered via dangerouslySetInnerHTML in
// a <span>.

import SVG_CLAUDE from "../assets/providers/claude.svg?raw";
import SVG_OPENAI from "../assets/providers/openai.svg?raw";
import SVG_OLLAMA from "../assets/providers/ollama.svg?raw";
import SVG_LMSTUDIO from "../assets/providers/lmstudio.svg?raw";
import SVG_GEMINI from "../assets/providers/gemini.svg?raw";
import SVG_OPENROUTER from "../assets/providers/openrouter.svg?raw";
import SVG_GENERIC from "../assets/providers/generic.svg?raw";

export type ProviderIconInfo = {
  svgContent: string;
  label: string;
};

const registry: Record<string, ProviderIconInfo> = {
  claude:     { svgContent: SVG_CLAUDE,     label: "Anthropic Claude" },
  openai:     { svgContent: SVG_OPENAI,     label: "OpenAI" },
  litellm:    { svgContent: SVG_OPENAI,     label: "LiteLLM" },
  ollama:     { svgContent: SVG_OLLAMA,     label: "Ollama" },
  lmstudio:   { svgContent: SVG_LMSTUDIO,   label: "LM Studio" },
  gemini:     { svgContent: SVG_GEMINI,     label: "Google Gemini" },
  openrouter: { svgContent: SVG_OPENROUTER, label: "OpenRouter" },
  generic:    { svgContent: SVG_GENERIC,    label: "Unknown provider" },
};

export function resolveProviderIcon(provider: string): ProviderIconInfo {
  return registry[provider] ?? registry.generic;
}
