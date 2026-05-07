// Package complexity defines the workspace-level complexity scales the planner
// emits and the validator enforces (issue #233).
package complexity

import (
	"fmt"
	"strings"
)

// Scale name constants mirror types.ComplexityScale to avoid a cycle.
const (
	ScaleTShirt    = "tshirt"
	ScaleFibonacci = "fibonacci"
	ScaleLinear10  = "linear10"
)

var tshirtValues = []string{"XS", "S", "M", "L", "XL"}
var fibonacciValues = []string{"1", "2", "3", "5", "8", "13", "21", "?"}
var linear10Values = []string{"1", "2", "3", "4", "5", "6", "7", "8", "9", "10"}

// legacyNorm maps historical free-form strings to their T-shirt equivalents.
var legacyNorm = map[string]string{
	"xs":          "XS",
	"extra-small": "XS",
	"s":           "S",
	"small":       "S",
	"m":           "M",
	"medium":      "M",
	"l":           "L",
	"large":       "L",
	"xl":          "XL",
	"extra-large": "XL",
}

func scaleValues(scale string) []string {
	switch scale {
	case ScaleFibonacci:
		return fibonacciValues
	case ScaleLinear10:
		return linear10Values
	default: // tshirt or ""
		return tshirtValues
	}
}

// IsValid returns true if value is a canonical member of the given scale.
func IsValid(scale, value string) bool {
	for _, v := range scaleValues(scale) {
		if v == value {
			return true
		}
	}
	return false
}

// Normalize maps raw to a canonical form for scale. For the T-shirt scale it
// also applies legacy mappings (e.g. "small" → "S"). Returns the normalized
// value and nil on success, or the original value and a descriptive error when
// no mapping exists.
func Normalize(scale, raw string) (string, error) {
	if IsValid(scale, raw) {
		return raw, nil
	}
	if scale == ScaleTShirt || scale == "" {
		if mapped, ok := legacyNorm[strings.ToLower(raw)]; ok {
			return mapped, nil
		}
	}
	return raw, fmt.Errorf("complexity value %q is not valid for scale %q", raw, scale)
}

// PromptFragment returns the guidance string injected into the planner prompt
// so the model knows which values to emit for estimated_complexity.
func PromptFragment(scale string) string {
	switch scale {
	case ScaleFibonacci:
		return `**Complexity scale for this workspace: Fibonacci**
Use one of: 1, 2, 3, 5, 8, 13, 21, ? for the estimated_complexity field.
Use "?" when the scope cannot be reliably estimated without further breakdown.
Do not use T-shirt sizes (XS/S/M/L/XL) — the validator will reject them.`
	case ScaleLinear10:
		return `**Complexity scale for this workspace: Linear 1–10**
Use an integer string from "1" to "10" for the estimated_complexity field.
1 = trivial one-liner change, 10 = multi-week multi-team effort.
Do not use T-shirt sizes or Fibonacci numbers — the validator will reject them.`
	default: // tshirt or ""
		return `**Complexity scale for this workspace: T-shirt (default)**
Use one of: XS, S, M, L, XL for the estimated_complexity field.
XS = < 1 hour, S = a few hours, M = 1–2 days, L = 3–5 days, XL = > 1 week.
Do not use legacy spellings like "small", "medium", or "large" — use the uppercase abbreviations.`
	}
}
