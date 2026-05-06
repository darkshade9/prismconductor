package a

type storeType struct{}

func (s *storeType) SaveErr() error           { return nil }
func (s *storeType) GetStr() (string, error)  { return "", nil }
func (s *storeType) GetName() string          { return "" }

// App is the type under scrutiny (mirrors *App in the real codebase).
type App struct{ st *storeType }

// Other has no special status — swallows on *Other must not be flagged.
type Other struct{ st *storeType }

// silently swallows single-result error — MUST flag
func (a *App) BadSingle() {
	_ = a.st.SaveErr() // want `silent error swallow in \*App method`
}

// silently swallows second result (error) in multi-return — MUST flag
func (a *App) BadMulti() {
	_, _ = a.st.GetStr() // want `silent error swallow in \*App method`
}

// justified annotation — MUST NOT flag
func (a *App) AllowedWithJustification() {
	// silenterr:ok fire-and-forget telemetry; failure surfaces via store logs
	_ = a.st.SaveErr()
}

// justified with em-dash separator — MUST NOT flag
func (a *App) AllowedEmDash() {
	// silenterr:ok — probe-delete is best-effort; keyring may already be gone
	_ = a.st.SaveErr()
}

// empty justification — MUST flag "without justification"
func (a *App) EmptyJustification() {
	// silenterr:ok
	_ = a.st.SaveErr() // want `silent error swallow allow-listed without justification`
}

// em-dash only (no text after) — MUST flag "without justification"
func (a *App) EmDashNoText() {
	// silenterr:ok —
	_ = a.st.SaveErr() // want `silent error swallow allow-listed without justification`
}

// handles error — MUST NOT flag (ReturnStmt, not AssignStmt)
func (a *App) GoodReturn() error {
	return a.st.SaveErr()
}

// swallows non-error result — MUST NOT flag
func (a *App) NonErrorSwallow() {
	_ = a.st.GetName()
}

// non-*App receiver swallows error — MUST NOT flag
func (o *Other) OtherBad() {
	_ = o.st.SaveErr()
}
