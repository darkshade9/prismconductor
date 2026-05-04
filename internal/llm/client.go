package llm

import (
	"net/http"
	"time"
)

// DefaultHTTPTimeout is the per-LLM-call timeout applied to all provider HTTP
// clients. Five minutes is generous for slow local models while bounding
// runaway HTTP hangs that would otherwise leave harness sessions as zombies.
const DefaultHTTPTimeout = 5 * time.Minute

// NewHTTPClient returns an *http.Client with the given timeout. Use this
// instead of a bare &http.Client{} so every provider inherits a safety timeout.
func NewHTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{Timeout: timeout}
}
