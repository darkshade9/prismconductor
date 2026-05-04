import type { GlowEntry, GlowState } from "../stores/useGlowColorsStore";
import { GLOW_CLASS_MAP } from "../stores/useGlowColorsStore";

export type { GlowState, GlowEntry };

// Returns the injected CSS class name for a glow state, or null when disabled.
export function resolveGlowClass(
  state: GlowState | null,
  entry: GlowEntry | undefined,
): string | null {
  if (!state || !entry?.enabled) return null;
  return GLOW_CLASS_MAP[state] ?? null;
}

// Full border + glow className for a card element.
// Customizable glows use border-transparent because the keyframe drives border-color.
// Hardcoded glows (waitingForPool, needsPR) include their own border class.
export function resolveCardBorderGlow(
  glowClass: string | null,
  hardcodedClass: string | null,
): string {
  if (glowClass) return `border-transparent ${glowClass}`;
  if (hardcodedClass) return hardcodedClass;
  return "border-slate-700";
}
