// Package harness drives the in-process tool-calling loop for non-Claude
// pools (PRISMCONDUCTOR_PLAN.md §10.5, issue #58).
//
// Today's session manager spawns a Claude Code CLI subprocess and consumes
// its stream-json over a PTY. For OpenAI-compatible providers there's no
// equivalent CLI binary the conductor can pty.Start, so the conductor drives
// the agent loop itself: assemble a system prompt from the bundled skill +
// repo conventions, send the chat turn through Provider.ToolChat, dispatch
// any tool calls against the in-process tool registry, feed the results
// back, and synthesize a stream-json byte stream into a pipe so the existing
// session.tailAndParse + StreamParser machinery sees identical input shape
// regardless of strategy.
//
// The seam in the session manager is `errors.Is(SpawnArgs-err,
// ErrNotSupported)`: providers either return argv (subprocess strategy) or
// ErrNotSupported (harness strategy). Adding a sixth provider doesn't touch
// internal/session.
package harness

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"

	"prismconductor/internal/llm"
	"prismconductor/internal/types"
)

// Run is the per-session input to Execute. Constructed by the session
// manager; the harness never reads the SQLite store directly.
type Run struct {
	SessionID     string
	RepoPath      string // ws.RepoPath — used for system-prompt enrichment.
	WorktreeDir   string // tool cwd; defaults to RepoPath when empty.
	Mode          types.SessionMode
	SkillMode     types.SkillMode // DEPRECATED: use SkillMarkdown (issue #92).
	SkillMarkdown string          // explicit skill markdown; empty falls back to Mode-based lookup.
	Pool          types.Pool
	Provider      llm.Provider
	UserPrompt    string   // already rendered by planPrompt / executePrompt.
	Skills        fs.FS    // bundle.FS
	EnvVars       []string // ws.AgentEnv flattened for the Bash tool.
	Budget        Budget
	Out           io.Writer // synthetic stream-json sink (the session pipe writer).
}

// Execute drives the agent loop until: (1) the model emits a sentinel that
// flips the session state via matchPatterns, (2) the budget is exhausted,
// (3) the model stops without tool calls, or (4) ctx is cancelled. Errors
// returned here flow back to the session as `harnessErr`, which
// mapTerminalState turns into StateFailed unless matchPatterns already saw a
// sentinel. Clean-stop sentinel exits return nil so the conductor sees the
// matchPatterns-driven terminal state.
func Execute(ctx context.Context, r Run) error {
	em := newEmitter(r.Out)
	em.systemInit()

	var system string
	var err error
	if r.SkillMarkdown != "" {
		// Per-stage skill selected (issue #92): use the provided markdown directly.
		system, err = assembleSystemPromptWithMarkdown(r.SkillMarkdown, r.RepoPath)
	} else {
		// Legacy path: derive skill from Mode. SkillMode is a Claude-CLI-era
		// concept — Hybrid and Native both expect a repo-side CLI binary.
		// Non-Claude pools have no such CLI; silently fall back to bundled.
		if r.SkillMode != "" && r.SkillMode != types.SkillModeBundled {
			log.Printf("harness: workspace SkillMode=%q has no harness analog; using bundled skill (SessionID=%s, Pool=%s)",
				r.SkillMode, r.SessionID, r.Pool.ID)
		}
		system, err = assembleSystemPrompt(r.Skills, string(r.Mode), r.RepoPath)
	}
	if err != nil {
		em.assistantMessage("BLOCKED: harness skill bundle missing — "+err.Error(), nil)
		em.done()
		return nil
	}

	cwd := r.WorktreeDir
	if cwd == "" {
		cwd = r.RepoPath
	}
	e := &env{
		Cwd:     cwd,
		EnvVars: r.EnvVars,
		Budget:  r.Budget,
	}

	history := []llm.Message{
		{Role: "user", Content: r.UserPrompt},
	}
	tools := Definitions()
	state := budgetState{}

	for state.Turns() < r.Budget.MaxTurns {
		if err := ctx.Err(); err != nil {
			return err
		}
		state.tickTurn()
		resp, err := r.Provider.ToolChat(ctx, r.Pool, llm.ChatRequest{
			System:  system,
			History: history,
			Tools:   tools,
		})
		if err != nil {
			if errors.Is(err, llm.ErrNotSupported) {
				em.assistantMessage("BLOCKED: provider does not implement ToolChat", nil)
				em.done()
				return nil
			}
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return err
			}
			em.assistantMessage("BLOCKED: provider error — "+err.Error(), nil)
			em.done()
			return nil
		}
		state.addUsage(resp.Usage.InputTokens, resp.Usage.OutputTokens)

		callEvents := make([]toolCallEvent, len(resp.ToolCalls))
		for i, c := range resp.ToolCalls {
			callEvents[i] = toolCallEvent{ID: c.ID, Name: c.Name, Args: c.Args}
		}
		em.assistantMessage(resp.Content, callEvents)

		history = append(history, llm.Message{
			Role:      "assistant",
			Content:   resp.Content,
			ToolCalls: resp.ToolCalls,
		})

		if len(resp.ToolCalls) == 0 {
			em.done()
			return nil
		}

		for _, call := range resp.ToolCalls {
			if err := ctx.Err(); err != nil {
				return err
			}
			result, toolErr := dispatch(ctx, e, call.Name, call.Args)
			content := result
			isError := false
			if toolErr != nil {
				content = "error: " + toolErr.Error()
				if result != "" {
					content += "\n" + result
				}
				isError = true
			}
			em.toolResult(call.ID, content, isError)
			history = append(history, llm.Message{
				Role:       "tool",
				Content:    content,
				ToolCallID: call.ID,
			})
		}

		if r.Budget.MaxInputTokens > 0 && state.InputTokens() >= r.Budget.MaxInputTokens {
			em.assistantMessage(fmt.Sprintf(
				"BLOCKED: harness input-token cap reached (%d of %d cumulative). This is PrismConductor's local safety budget, NOT a provider rate-limit or account balance issue. Raise harness.DefaultBudget().MaxInputTokens or per-pool budget if the model genuinely needs more context.",
				state.InputTokens(), r.Budget.MaxInputTokens), nil)
			em.done()
			return nil
		}
	}

	em.assistantMessage(fmt.Sprintf(
		"BLOCKED: harness turn cap reached (%d turns). This is PrismConductor's local safety budget, NOT a provider issue. Raise harness.DefaultBudget().MaxTurns if the model needs more iterations.",
		r.Budget.MaxTurns), nil)
	em.done()
	return nil
}

// EncodeArgs is a small helper so callers can build raw json.RawMessage args
// without sprinkling json.Marshal noise. Used by tests and any future caller
// that needs to construct ChatResponse.ToolCalls.Args inline.
func EncodeArgs(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}
