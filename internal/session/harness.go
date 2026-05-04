package session

import (
	"context"
	"fmt"
	"log"
	"runtime/debug"

	"prismconductor/internal/harness"
	"prismconductor/internal/llm"
	"prismconductor/internal/skills/bundle"
	"prismconductor/internal/types"
)

// spawnHarness launches the in-process harness goroutine for a harness-strategy
// session (issue #58, #152). The goroutine wraps harness.Execute with a
// top-level panic-recovery defer so a misbehaving provider or tool cannot crash
// the conductor. Any recovered panic is written to rs.harnessErr so that
// mapTerminalState reports StateFailed rather than silently treating the panicked
// session as completed.
//
// The returned channel is closed when the goroutine exits (clean, panic, or
// context cancel). Wire it into synthCmd.done before passing rs to tailAndParse.
//
// Callers must ensure m.providers.Get(pool.Provider) succeeds before calling;
// the provider is passed explicitly (prov) to avoid a second registry lookup.
func (m *Manager) spawnHarness(
	ctx context.Context,
	rs *runtimeSession,
	prov llm.Provider,
	ws types.Workspace,
	pool types.Pool,
	prompt, skillMarkdown string,
) chan struct{} {
	done := make(chan struct{})
	run := harness.Run{
		SessionID:     rs.sess.ID,
		RepoPath:      ws.RepoPath,
		WorktreeDir:   rs.worktreeDir,
		Mode:          rs.sess.Mode,
		SkillMode:     ws.SkillProfile.Mode,
		SkillMarkdown: skillMarkdown,
		Pool:          pool,
		Provider:      prov,
		UserPrompt:    prompt,
		Skills:        bundle.FS,
		EnvVars:       envSpecToSlice(ws.AgentEnv),
		Budget:        poolBudget(pool),
		Out:           rs.transcriptFile,
		OnTurnUsage: func(in, out int) {
			rs.actMu.Lock()
			rs.harnessInputTokens = int64(in)
			rs.harnessOutputTokens = int64(out)
			rs.actMu.Unlock()
		},
	}
	go func() {
		defer func() {
			// Safety-net: recover panics so a misbehaving provider or tool does
			// not crash the conductor process. The panic text is embedded in
			// harnessErr so mapTerminalState resolves to StateFailed and the UI
			// shows a blocked card instead of a zombie running card.
			if r := recover(); r != nil {
				stack := debug.Stack()
				rs.actMu.Lock()
				rs.harnessErr = fmt.Errorf("harness panic: %v\n%s", r, stack)
				rs.actMu.Unlock()
				log.Printf("session %s: harness panic recovered: %v", rs.sess.ID, r)
			}
			close(done)
		}()
		rs.harnessErr = harness.Execute(ctx, run)
	}()
	return done
}
