package harness_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"prismconductor/internal/harness"
	"prismconductor/internal/llm"
	"prismconductor/internal/session"
	"prismconductor/internal/skills/bundle"
	"prismconductor/internal/types"
)

// fakeProvider is a programmable Provider used to drive the harness loop in
// tests without any HTTP. responses is consumed in order; once exhausted,
// ToolChat returns nextErr (or, by default, an empty Stop response).
type fakeProvider struct {
	responses []llm.ChatResponse
	calls     []llm.ChatRequest
	nextErr   error
}

func (fakeProvider) Kind() types.Provider                                { return "fake" }
func (fakeProvider) DisplayName() string                                 { return "fake" }
func (fakeProvider) DefaultEndpoint() string                             { return "" }
func (fakeProvider) NeedsAPIKey() bool                                   { return false }
func (fakeProvider) CanSpawn() bool                                      { return true }
func (fakeProvider) ListModels(context.Context, types.Pool) ([]string, error) {
	return nil, llm.ErrNotSupported
}
func (fakeProvider) SpawnArgs(types.Pool, string) ([]string, error) {
	return nil, llm.ErrNotSupported
}
func (fakeProvider) ChatJSON(context.Context, types.Pool, string, string) (string, error) {
	return "", llm.ErrNotSupported
}
func (f *fakeProvider) ToolChat(_ context.Context, _ types.Pool, req llm.ChatRequest) (llm.ChatResponse, error) {
	f.calls = append(f.calls, req)
	if len(f.responses) == 0 {
		if f.nextErr != nil {
			return llm.ChatResponse{}, f.nextErr
		}
		return llm.ChatResponse{Stop: true}, nil
	}
	resp := f.responses[0]
	f.responses = f.responses[1:]
	return resp, nil
}

// pipeBuf is an io.Writer + reader-style sink that captures emitted bytes
// for assertions without a goroutine race on a real os.Pipe.
type pipeBuf struct {
	b strings.Builder
}

func (p *pipeBuf) Write(b []byte) (int, error) {
	p.b.Write(b)
	return len(b), nil
}

func (p *pipeBuf) String() string { return p.b.String() }

func runHarness(t *testing.T, prov llm.Provider, mode types.SessionMode, prompt string) string {
	t.Helper()
	out := &pipeBuf{}
	err := harness.Execute(context.Background(), harness.Run{
		SessionID:  "test",
		RepoPath:   t.TempDir(),
		Mode:       mode,
		SkillMode:  types.SkillModeBundled,
		Provider:   prov,
		UserPrompt: prompt,
		Skills:     bundle.FS,
		Budget:     harness.Budget{MaxTurns: 4, MaxInputTokens: 100_000, BashTimeout: 5 * time.Second, OutputCap: 4096},
		Out:        out,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	return out.String()
}

// TestExecute_StopWithoutToolCallsTerminates is the simplest happy path:
// model returns Stop:true with content, harness emits the assistant + done,
// loop terminates without further turns.
func TestExecute_StopWithoutToolCallsTerminates(t *testing.T) {
	prov := &fakeProvider{
		responses: []llm.ChatResponse{
			{Content: "Plan written to .prismconductor/plans/9999-rev1.json", Stop: true},
		},
	}
	out := runHarness(t, prov, types.ModePlan, "/conductor-plan --issue 9999")
	if len(prov.calls) != 1 {
		t.Errorf("expected 1 chat call, got %d", len(prov.calls))
	}
	if !strings.Contains(out, "Plan written to .prismconductor/plans/9999-rev1.json") {
		t.Errorf("missing assistant text in output: %q", out)
	}
}

// TestExecute_SentinelPropagationThroughStreamParser pipes harness output
// through the live session.StreamParser and asserts the §10.3 sentinel lands
// on an @asst line — proving matchPatterns will see it identically to a
// Claude PTY.
func TestExecute_SentinelPropagationThroughStreamParser(t *testing.T) {
	prov := &fakeProvider{
		responses: []llm.ChatResponse{
			{Content: "Plan written to .prismconductor/plans/9999-rev1.json", Stop: true},
		},
	}
	out := runHarness(t, prov, types.ModePlan, "x")
	parser := session.NewStreamParser()
	var lines []string
	for _, raw := range strings.Split(strings.TrimSpace(out), "\n") {
		lines = append(lines, parser.Feed(raw)...)
	}
	found := false
	for _, l := range lines {
		if strings.HasPrefix(l, session.RoleAssistant) && strings.Contains(l, "Plan written to .prismconductor/plans/9999-rev1.json") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("StreamParser never produced an @asst line with the sentinel; lines=%v", lines)
	}
}

// TestExecute_TurnBudgetEmitsBlocked drives the loop past MaxTurns by
// returning tool calls forever; the harness must emit
// "BLOCKED: turn budget exceeded" so matchPatterns flips state to Blocked.
func TestExecute_TurnBudgetEmitsBlocked(t *testing.T) {
	prov := &fakeProvider{}
	for i := 0; i < 10; i++ {
		prov.responses = append(prov.responses, llm.ChatResponse{
			ToolCalls: []llm.ToolCall{{
				ID:   "c1",
				Name: "TodoWrite",
				Args: json.RawMessage(`{"todos":[]}`),
			}},
		})
	}
	out := &pipeBuf{}
	err := harness.Execute(context.Background(), harness.Run{
		SessionID:  "test",
		RepoPath:   t.TempDir(),
		Mode:       types.ModePlan,
		SkillMode:  types.SkillModeBundled,
		Provider:   prov,
		UserPrompt: "x",
		Skills:     bundle.FS,
		Budget:     harness.Budget{MaxTurns: 3, MaxInputTokens: 999_999, BashTimeout: time.Second, OutputCap: 1024},
		Out:        out,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out.String(), "BLOCKED: harness turn cap reached") {
		t.Errorf("expected BLOCKED turn-cap sentinel, got: %s", out.String())
	}
}

// TestExecute_TokenBudgetEmitsBlocked verifies the token-cap arm.
func TestExecute_TokenBudgetEmitsBlocked(t *testing.T) {
	prov := &fakeProvider{
		responses: []llm.ChatResponse{
			{
				ToolCalls: []llm.ToolCall{{
					ID:   "c1",
					Name: "TodoWrite",
					Args: json.RawMessage(`{"todos":[]}`),
				}},
				Usage: llm.UsageCounts{InputTokens: 100, OutputTokens: 1},
			},
			{Stop: true, Content: "should not run"},
		},
	}
	out := &pipeBuf{}
	err := harness.Execute(context.Background(), harness.Run{
		SessionID:  "test",
		RepoPath:   t.TempDir(),
		Mode:       types.ModePlan,
		SkillMode:  types.SkillModeBundled,
		Provider:   prov,
		UserPrompt: "x",
		Skills:     bundle.FS,
		Budget:     harness.Budget{MaxTurns: 10, MaxInputTokens: 50, BashTimeout: time.Second, OutputCap: 1024},
		Out:        out,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out.String(), "BLOCKED: harness input-token cap reached") {
		t.Errorf("expected token-cap BLOCKED sentinel, got: %s", out.String())
	}
}

// TestExecute_NonBundledSkillModeFallsBackToBundled pins the soft-fallback
// behaviour: a non-Claude pool driven by the harness has no CLI to dispatch
// Hybrid/Native slash commands to, so the harness silently treats those modes
// as Bundled instead of BLOCKEDing the user (the prior strict-refuse contract
// was confusing — operators were getting BLOCKED when they hadn't even chosen
// hybrid/native deliberately, just inherited it from a CLAUDE.md detection
// pass).
func TestExecute_NonBundledSkillModeFallsBackToBundled(t *testing.T) {
	prov := &fakeProvider{
		responses: []llm.ChatResponse{{Stop: true, Content: "ok"}},
	}
	out := &pipeBuf{}
	err := harness.Execute(context.Background(), harness.Run{
		SessionID:  "test",
		RepoPath:   t.TempDir(),
		Mode:       types.ModePlan,
		SkillMode:  types.SkillModeHybrid,
		Provider:   prov,
		UserPrompt: "x",
		Skills:     bundle.FS,
		Budget:     harness.DefaultBudget(),
		Out:        out,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if strings.Contains(out.String(), "BLOCKED: harness-v1 supports SkillModeBundled only") {
		t.Errorf("did not expect skill-mode refusal, got: %s", out.String())
	}
	if len(prov.calls) == 0 {
		t.Error("provider should have been called once Hybrid mode falls back to Bundled")
	}
}

// TestExecute_NotSupportedFromProviderEmitsBlocked covers the case where a
// future provider's ToolChat returns ErrNotSupported; the harness must
// surface a BLOCKED: sentinel instead of looping.
func TestExecute_NotSupportedFromProviderEmitsBlocked(t *testing.T) {
	prov := &fakeProvider{nextErr: llm.ErrNotSupported}
	out := runHarness(t, prov, types.ModePlan, "x")
	if !strings.Contains(out, "BLOCKED: provider does not implement ToolChat") {
		t.Errorf("expected provider-not-supported BLOCKED, got: %s", out)
	}
}

// TestExecute_ContextCancelPropagates ensures Kill (via ctx.cancel) returns
// promptly and surfaces context.Canceled as the harness error.
func TestExecute_ContextCancelPropagates(t *testing.T) {
	prov := &fakeProvider{}
	for i := 0; i < 100; i++ {
		prov.responses = append(prov.responses, llm.ChatResponse{
			ToolCalls: []llm.ToolCall{{
				ID:   "c1",
				Name: "TodoWrite",
				Args: json.RawMessage(`{"todos":[]}`),
			}},
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	out := &pipeBuf{}
	err := harness.Execute(ctx, harness.Run{
		SessionID:  "test",
		RepoPath:   t.TempDir(),
		Mode:       types.ModePlan,
		SkillMode:  types.SkillModeBundled,
		Provider:   prov,
		UserPrompt: "x",
		Skills:     bundle.FS,
		Budget:     harness.DefaultBudget(),
		Out:        out,
	})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

// TestExecute_ToolErrorSurfacedAsToolResultNotBlock confirms that a tool
// failure (e.g. bad path) is surfaced to the model as an is_error tool
// result so it can recover, NOT as a harness-level BLOCKED:.
func TestExecute_ToolErrorSurfacedAsToolResultNotBlock(t *testing.T) {
	prov := &fakeProvider{
		responses: []llm.ChatResponse{
			{
				ToolCalls: []llm.ToolCall{{
					ID:   "c1",
					Name: "Read",
					Args: json.RawMessage(`{"file_path":"../../../etc/passwd"}`),
				}},
			},
			{Stop: true, Content: "ok done"},
		},
	}
	out := runHarness(t, prov, types.ModePlan, "x")
	if strings.Contains(out, "BLOCKED:") {
		t.Errorf("tool error must not auto-BLOCK, got: %s", out)
	}
	if !strings.Contains(out, "escapes worktree") {
		t.Errorf("expected tool error text in output, got: %s", out)
	}
	if len(prov.calls) != 2 {
		t.Errorf("expected 2 chat turns (tool + final), got %d", len(prov.calls))
	}
}

// TestExecute_AssistantOnlyEndsLoop covers the second termination path: the
// model returns no tool calls and Stop=false, but content is empty — the
// harness still treats "no calls" as end-of-turn so a model can't wedge.
var _ io.Writer = (*pipeBuf)(nil)
