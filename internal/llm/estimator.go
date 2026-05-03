package llm

// EstimatePromptCost returns a cheap pre-flight estimate for a plain-text
// prompt against a given model (issue #101, Q1=chars/4 heuristic, Q2=static
// pricing table via LookupRates).
//
// Token count uses the chars/4 heuristic (no external tokenizer dependency).
// The estimate is conservative: it counts only input tokens (output volume is
// unknown before execution) and uses the model's input rate. Returns
// (tokens, costCents) where costCents=0 when the model is unknown.
func EstimatePromptCost(promptText, model string) (tokens int64, costCents float64) {
	tokens = int64(len([]rune(promptText)) / 4)
	if tokens < 50 {
		tokens = 50 // floor for very short prompts
	}
	rates, ok := LookupRates(model)
	if !ok {
		return tokens, 0
	}
	costCents = float64(tokens) * rates.InputPerMillion / 1_000_000 * 100
	return tokens, costCents
}
