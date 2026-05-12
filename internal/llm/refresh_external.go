//go:build ignore

// refresh_external fetches the hermesguide.xyz model capability index and
// writes a normalized snapshot to internal/llm/external_models.json.
//
// Run with:
//
//	go run internal/llm/refresh_external.go [--output PATH]
//
// On parse failure the command exits non-zero but leaves the existing
// snapshot in place so runtime lookups are not disrupted.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// externalModel is the normalized shape written to the snapshot.
type externalModel struct {
	Provider      string `json:"provider"`
	ModelID       string `json:"model_id"`
	PlanFit       string `json:"plan_fit,omitempty"`
	WorkFit       string `json:"work_fit,omitempty"`
	OrchFit       string `json:"orch_fit,omitempty"`
	ArchitectFit  string `json:"architect_fit,omitempty"`
	ToolSupport   string `json:"tool_support,omitempty"`
	ContextWindow int    `json:"context_window,omitempty"`
	CostTier      string `json:"cost_tier,omitempty"`
	Notes         string `json:"notes,omitempty"`
	UpdatedAt     string `json:"updated_at,omitempty"`
	Source        string `json:"source"`
}

type snapshot struct {
	FetchedAt string          `json:"fetched_at"`
	Models    []externalModel `json:"models"`
}

func main() {
	// Default output path: internal/llm/external_models.json relative to the
	// repo root (two levels up from this file's directory).
	_, thisFile, _, _ := runtime.Caller(0)
	defaultOut := filepath.Join(filepath.Dir(thisFile), "external_models.json")

	out := flag.String("output", defaultOut, "path to write the snapshot JSON")
	flag.Parse()

	if err := run(*out); err != nil {
		fmt.Fprintf(os.Stderr, "refresh-model-hints: %v\n", err)
		os.Exit(1)
	}
}

func run(outputPath string) error {
	fmt.Fprintln(os.Stderr, "fetching hermesguide.xyz model index…")

	client := &http.Client{Timeout: 30 * time.Second}

	// Try a JSON endpoint first, fall back to the HTML index.
	models, err := fetchJSON(client)
	if err != nil {
		fmt.Fprintf(os.Stderr, "JSON endpoint unavailable (%v); trying HTML parse…\n", err)
		models, err = fetchHTML(client)
		if err != nil {
			return fmt.Errorf("all fetch strategies failed: %w", err)
		}
	}

	if len(models) == 0 {
		return errors.New("parse returned 0 models — refusing to overwrite snapshot with empty list")
	}

	snap := snapshot{
		FetchedAt: time.Now().UTC().Format("2006-01-02"),
		Models:    models,
	}
	data, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}

	if err := os.WriteFile(outputPath, append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", outputPath, err)
	}

	fmt.Fprintf(os.Stderr, "wrote %d models to %s\n", len(models), outputPath)
	return nil
}

// fetchJSON attempts to retrieve a structured JSON list from hermesguide.xyz.
func fetchJSON(c *http.Client) ([]externalModel, error) {
	resp, err := c.Get("https://hermesguide.xyz/api/models")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}

	// Accept either a top-level array or {"models":[…]}.
	var raw []json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		var wrapper struct {
			Models []json.RawMessage `json:"models"`
		}
		if err2 := json.Unmarshal(body, &wrapper); err2 != nil {
			return nil, fmt.Errorf("unrecognised JSON shape: %w", err)
		}
		raw = wrapper.Models
	}

	var out []externalModel
	for _, r := range raw {
		m, err := normalizeJSON(r)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  skip entry: %v\n", err)
			continue
		}
		out = append(out, m)
	}
	return out, nil
}

// normalizeJSON maps an arbitrary JSON object into externalModel.
// Fields are accepted under several common naming conventions.
func normalizeJSON(raw json.RawMessage) (externalModel, error) {
	var kv map[string]any
	if err := json.Unmarshal(raw, &kv); err != nil {
		return externalModel{}, err
	}
	get := func(keys ...string) string {
		for _, k := range keys {
			if v, ok := kv[k]; ok {
				return fmt.Sprintf("%v", v)
			}
		}
		return ""
	}
	getInt := func(keys ...string) int {
		s := get(keys...)
		if s == "" {
			return 0
		}
		var n int
		fmt.Sscan(s, &n)
		return n
	}

	provider := get("provider", "vendor")
	modelID := get("model_id", "id", "name", "model")
	if provider == "" || modelID == "" {
		return externalModel{}, fmt.Errorf("missing provider or model_id in %s", raw)
	}

	return externalModel{
		Provider:      strings.ToLower(provider),
		ModelID:       modelID,
		PlanFit:       get("plan_fit", "planFit"),
		WorkFit:       get("work_fit", "workFit"),
		OrchFit:       get("orch_fit", "orchFit"),
		ArchitectFit:  get("architect_fit", "architectFit"),
		ToolSupport:   get("tool_support", "toolSupport", "tools"),
		ContextWindow: getInt("context_window", "contextWindow", "context"),
		CostTier:      get("cost_tier", "costTier", "cost"),
		Notes:         get("notes", "description"),
		UpdatedAt:     get("updated_at", "updatedAt"),
		Source:        "hermesguide.xyz",
	}, nil
}

// fetchHTML is a best-effort HTML scraper for hermesguide.xyz's main page.
// It looks for <tr> rows that contain a provider column and a model column.
func fetchHTML(c *http.Client) ([]externalModel, error) {
	resp, err := c.Get("https://hermesguide.xyz")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}

	// Very conservative parse: look for lines containing known provider names.
	// This is intentionally fragile — if the site changes layout, the JSON
	// endpoint should be preferred; the HTML path is a last resort.
	knownProviders := []string{"claude", "openai", "gemini", "ollama", "litellm", "mistral"}
	var models []externalModel
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(line)
		for _, prov := range knownProviders {
			if !strings.Contains(strings.ToLower(line), prov) {
				continue
			}
			// Try to extract a model-id-like token from the line.
			parts := strings.Fields(stripHTML(line))
			for _, p := range parts {
				if looksLikeModelID(p) {
					models = append(models, externalModel{
						Provider: prov,
						ModelID:  p,
						Source:   "hermesguide.xyz",
					})
					break
				}
			}
		}
	}
	if len(models) == 0 {
		return nil, errors.New("HTML parse found no recognizable model rows")
	}
	return models, nil
}

func stripHTML(s string) string {
	var b strings.Builder
	inTag := false
	for _, c := range s {
		switch {
		case c == '<':
			inTag = true
		case c == '>':
			inTag = false
		case !inTag:
			b.WriteRune(c)
		}
	}
	return b.String()
}

func looksLikeModelID(s string) bool {
	// Model IDs usually contain hyphens or colons and at least one digit.
	return (strings.Contains(s, "-") || strings.Contains(s, ":")) &&
		strings.ContainsAny(s, "0123456789") &&
		len(s) >= 5
}
