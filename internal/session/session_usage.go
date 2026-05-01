package session

import "prismconductor/internal/llm"

// ResolveUsage merges token data from two sources — the Claude stream-parser
// result event (CostData) and the harness budgetState accumulators — into a
// single authoritative set of counts plus an estimated cost in cents.
//
// Resolution rules (issue #101):
//  1. If CostData is present, use its InputTokens/OutputTokens (model-accurate
//     counts from the provider's own billing counters).
//  2. If CostData has TotalCostUSD > 0, use that directly as the cost
//     (provider-computed, most accurate).
//  3. Otherwise estimate cost from token counts using the model's rate table.
//  4. Fall back to harness-tracked counts when CostData has none.
func ResolveUsage(cd *CostData, harnessInput, harnessOutput int64, model string) (inputTok, outputTok int64, costCents float64) {
	if cd != nil {
		inputTok = cd.InputTokens
		outputTok = cd.OutputTokens
		if cd.TotalCostUSD > 0 {
			costCents = cd.TotalCostUSD * 100
		}
	}
	// Supplement with harness-tracked counts when Claude parser had none.
	if inputTok == 0 {
		inputTok = harnessInput
	}
	if outputTok == 0 {
		outputTok = harnessOutput
	}
	// Compute estimated cost from token counts when not already set.
	if costCents == 0 && (inputTok > 0 || outputTok > 0) {
		if rates, ok := llm.LookupRates(model); ok {
			inCents := float64(inputTok) * rates.InputPerMillion / 1_000_000 * 100
			outCents := float64(outputTok) * rates.OutputPerMillion / 1_000_000 * 100
			costCents = inCents + outCents
		}
	}
	return inputTok, outputTok, costCents
}
