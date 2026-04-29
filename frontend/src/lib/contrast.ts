// WCAG relative luminance per https://www.w3.org/WAI/GL/wiki/Relative_luminance.
// Returns "#000" or "#fff" — whichever yields higher contrast against a hex
// color. Accepts 6 hex chars with or without leading '#'. Falls back to white
// for malformed input.

function toLinear(c: number): number {
  const s = c / 255;
  return s <= 0.03928 ? s / 12.92 : Math.pow((s + 0.055) / 1.055, 2.4);
}

export function getContrastText(hex: string): "#000" | "#fff" {
  const clean = hex.replace(/^#/, "").trim();
  if (!/^[0-9a-fA-F]{6}$/.test(clean)) return "#fff";
  const r = parseInt(clean.slice(0, 2), 16);
  const g = parseInt(clean.slice(2, 4), 16);
  const b = parseInt(clean.slice(4, 6), 16);
  const luminance = 0.2126 * toLinear(r) + 0.7152 * toLinear(g) + 0.0722 * toLinear(b);
  return luminance > 0.5 ? "#000" : "#fff";
}

export function normalizeHex(hex: string): string {
  return hex.replace(/^#/, "").toLowerCase();
}
