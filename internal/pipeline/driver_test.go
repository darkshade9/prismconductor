package pipeline_test

import (
	"testing"

	"prismconductor/internal/pipeline"
	"prismconductor/internal/types"
)

// helpers

func step(id, name, onSuccess, onFail string, maxLoops int) types.PipelineStep {
	return types.PipelineStep{
		ID:        id,
		Name:      name,
		OnSuccess: onSuccess,
		OnFail:    onFail,
		MaxLoops:  maxLoops,
	}
}

func pip(steps ...types.PipelineStep) *types.WorkspacePipeline {
	return &types.WorkspacePipeline{Steps: steps}
}

// --- Next() tests ---

func TestNext_NilPipeline(t *testing.T) {
	got := pipeline.Next(nil, "x", pipeline.OutcomeSuccess, nil)
	if got.Type != pipeline.ActionDone {
		t.Fatalf("nil pipeline: want ActionDone, got %s", got.Type)
	}
}

func TestNext_SuccessToNextStep(t *testing.T) {
	p := pip(
		step("a", "A", "b", "", 0),
		step("b", "B", "", "", 0),
	)
	got := pipeline.Next(p, "a", pipeline.OutcomeSuccess, nil)
	if got.Type != pipeline.ActionSpawnStep || got.StepID != "b" {
		t.Fatalf("want ActionSpawnStep b, got %+v", got)
	}
}

func TestNext_SuccessEmptyMeansDone(t *testing.T) {
	p := pip(step("a", "A", "", "", 0))
	got := pipeline.Next(p, "a", pipeline.OutcomeSuccess, nil)
	if got.Type != pipeline.ActionDone {
		t.Fatalf("want ActionDone, got %s", got.Type)
	}
}

func TestNext_FailEmptyMeansBlocked(t *testing.T) {
	p := pip(step("a", "A", "", "", 0))
	got := pipeline.Next(p, "a", pipeline.OutcomeFail, nil)
	if got.Type != pipeline.ActionBlocked {
		t.Fatalf("want ActionBlocked, got %s", got.Type)
	}
}

func TestNext_FailRouteToOtherStep(t *testing.T) {
	p := pip(
		step("review", "Review", "close", "execute", 3),
		step("execute", "Execute", "review", "", 0),
		step("close", "Close", "", "", 0),
	)
	got := pipeline.Next(p, "review", pipeline.OutcomeFail, map[string]int{})
	if got.Type != pipeline.ActionSpawnStep || got.StepID != "execute" {
		t.Fatalf("want ActionSpawnStep execute, got %+v", got)
	}
}

func TestNext_LoopCapEnforced(t *testing.T) {
	p := pip(
		step("review", "Review", "close", "execute", 3),
		step("execute", "Execute", "review", "", 0),
		step("close", "Close", "", "", 0),
	)
	// review has run 3 times; routing to it again should be blocked
	loops := map[string]int{"review": 3}
	got := pipeline.Next(p, "execute", pipeline.OutcomeSuccess, loops)
	if got.Type != pipeline.ActionBlocked {
		t.Fatalf("want ActionBlocked when loops==cap, got %+v", got)
	}
}

func TestNext_LoopBelowCapAllowed(t *testing.T) {
	p := pip(
		step("review", "Review", "close", "execute", 3),
		step("execute", "Execute", "review", "", 0),
		step("close", "Close", "", "", 0),
	)
	loops := map[string]int{"review": 2}
	got := pipeline.Next(p, "execute", pipeline.OutcomeSuccess, loops)
	if got.Type != pipeline.ActionSpawnStep || got.StepID != "review" {
		t.Fatalf("want ActionSpawnStep review (loops=2 < cap=3), got %+v", got)
	}
}

func TestNext_MissingCurrentStep(t *testing.T) {
	p := pip(step("a", "A", "", "", 0))
	got := pipeline.Next(p, "missing", pipeline.OutcomeSuccess, nil)
	if got.Type != pipeline.ActionBlocked {
		t.Fatalf("want ActionBlocked for missing step, got %s", got.Type)
	}
}

func TestNext_DeletedTargetBlocked(t *testing.T) {
	// on_success points to "b" but "b" is not in the pipeline (deleted)
	p := pip(step("a", "A", "b", "", 0))
	got := pipeline.Next(p, "a", pipeline.OutcomeSuccess, nil)
	if got.Type != pipeline.ActionBlocked {
		t.Fatalf("want ActionBlocked for deleted target, got %s", got.Type)
	}
}

// --- Validate() tests ---

func TestValidate_NilOK(t *testing.T) {
	if err := pipeline.Validate(nil); err != nil {
		t.Fatal(err)
	}
}

func TestValidate_EmptyOK(t *testing.T) {
	if err := pipeline.Validate(&types.WorkspacePipeline{}); err != nil {
		t.Fatal(err)
	}
}

func TestValidate_MissingID(t *testing.T) {
	p := &types.WorkspacePipeline{Steps: []types.PipelineStep{{Name: "no-id"}}}
	if err := pipeline.Validate(p); err == nil {
		t.Fatal("want error for missing step ID")
	}
}

func TestValidate_DuplicateID(t *testing.T) {
	p := pip(step("a", "A", "", "", 0), step("a", "B", "", "", 0))
	if err := pipeline.Validate(p); err == nil {
		t.Fatal("want error for duplicate step ID")
	}
}

func TestValidate_UnknownOnSuccess(t *testing.T) {
	p := pip(step("a", "A", "missing", "", 0))
	if err := pipeline.Validate(p); err == nil {
		t.Fatal("want error for unknown on_success")
	}
}

func TestValidate_UnknownOnFail(t *testing.T) {
	p := pip(step("a", "A", "", "missing", 0))
	if err := pipeline.Validate(p); err == nil {
		t.Fatal("want error for unknown on_fail")
	}
}

func TestValidate_CycleWithCap(t *testing.T) {
	// review → execute → review (cycle); review has max_loops=3 — valid
	p := pip(
		step("review", "Review", "close", "execute", 3),
		step("execute", "Execute", "review", "", 0),
		step("close", "Close", "", "", 0),
	)
	if err := pipeline.Validate(p); err != nil {
		t.Fatalf("want no error for capped cycle, got %v", err)
	}
}

func TestValidate_CycleNoCap(t *testing.T) {
	// a → b → a, neither has max_loops — invalid
	p := pip(
		step("a", "A", "b", "", 0),
		step("b", "B", "a", "", 0),
	)
	if err := pipeline.Validate(p); err == nil {
		t.Fatal("want error for uncapped cycle")
	}
}

func TestValidate_LinearNoCap(t *testing.T) {
	// a → b → (end); no cycle so no max_loops needed
	p := pip(
		step("a", "A", "b", "", 0),
		step("b", "B", "", "", 0),
	)
	if err := pipeline.Validate(p); err != nil {
		t.Fatalf("want no error for linear pipeline without max_loops, got %v", err)
	}
}
