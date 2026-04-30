//go:build integration
// +build integration

package harness_test

import (
	"context"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"prismconductor/internal/harness"
	"prismconductor/internal/llm"
	"prismconductor/internal/skills/bundle"
	"prismconductor/internal/types"
)

// TestHarness_LiveSmokePlan exercises the harness end-to-end against a real
// OpenAI-compatible endpoint. Gated on PC_HARNESS_SMOKE_ENDPOINT and
// PC_HARNESS_SMOKE_MODEL — CI never runs this; the maintainer runs it
// before merging issue #58 (e.g. against LM Studio + a tool-capable model).
//
// Build/run:
//
//	go test -tags integration ./internal/harness/... -run LiveSmokePlan -v
//
// On a clean run the model writes a plan JSON to a temp dir and emits the
// §10.3 sentinel; we assert the sentinel reaches the harness's transcript
// stream.
func TestHarness_LiveSmokePlan(t *testing.T) {
	endpoint := os.Getenv("PC_HARNESS_SMOKE_ENDPOINT")
	model := os.Getenv("PC_HARNESS_SMOKE_MODEL")
	if endpoint == "" || model == "" {
		t.Skip("set PC_HARNESS_SMOKE_ENDPOINT and PC_HARNESS_SMOKE_MODEL to run")
	}

	// Resolve the provider via Kind. Default is openai-style (works for
	// OpenAI / LM Studio / generic OpenAI-compat); override with
	// PC_HARNESS_SMOKE_PROVIDER=ollama|lmstudio|litellm to use a different
	// resolver.
	kind := os.Getenv("PC_HARNESS_SMOKE_PROVIDER")
	if kind == "" {
		kind = "lmstudio"
	}
	var prov llm.Provider
	switch kind {
	case "openai":
		prov = llm.NewOpenAIProvider()
	case "ollama":
		prov = llm.NewOllamaProvider()
	case "lmstudio":
		prov = llm.NewLMStudioProvider()
	case "litellm":
		prov = llm.NewLiteLLMProvider()
	default:
		t.Fatalf("unknown PC_HARNESS_SMOKE_PROVIDER %q", kind)
	}

	// Sanity check the endpoint is reachable before we burn turns.
	hc := &http.Client{Timeout: 5 * time.Second}
	if _, err := hc.Get(endpoint + "/v1/models"); err != nil {
		t.Skipf("smoke endpoint %s unreachable: %v", endpoint, err)
	}

	repo := t.TempDir()
	pool := types.Pool{
		ID:       "smoke",
		Provider: types.Provider(kind),
		Endpoint: endpoint,
		Model:    model,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer pr.Close()

	done := make(chan error, 1)
	go func() {
		done <- harness.Execute(ctx, harness.Run{
			SessionID:  "smoke",
			RepoPath:   repo,
			Mode:       types.ModePlan,
			SkillMode:  types.SkillModeBundled,
			Pool:       pool,
			Provider:   prov,
			UserPrompt: "/conductor-plan --issue 9999 --repo " + repo,
			Skills:     bundle.FS,
			Budget:     harness.DefaultBudget(),
			Out:        pw,
		})
		_ = pw.Close()
	}()

	buf := make([]byte, 0, 64*1024)
	tmp := make([]byte, 8*1024)
	for {
		n, err := pr.Read(tmp)
		if n > 0 {
			buf = append(buf, tmp[:n]...)
		}
		if err != nil {
			break
		}
	}
	if err := <-done; err != nil {
		t.Logf("harness terminal error: %v", err)
	}
	out := string(buf)
	if !strings.Contains(out, "Plan written to .prismconductor/plans/") {
		t.Errorf("smoke run produced no plan-written sentinel; transcript:\n%s", out)
	}
}
