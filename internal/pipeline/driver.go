// Package pipeline provides routing logic for per-workspace pipelines (issue #146).
// The driver is a pure function — it computes the next action from the current
// pipeline state without touching any I/O or spawning sessions.
package pipeline

import (
	"errors"
	"fmt"
	"strings"

	"prismconductor/internal/types"
)

// DefaultMaxLoops is the safety cap applied when max_loops is 0 on a step that
// serves as the loop target. Matches the q1 answer from the issue-146 plan.
const DefaultMaxLoops = 3

// Outcome is the result emitted by a pipeline step worker.
type Outcome string

const (
	OutcomeSuccess Outcome = "success"
	OutcomeFail    Outcome = "fail"
)

// ActionType describes what the caller should do next.
type ActionType string

const (
	ActionSpawnStep ActionType = "spawn_step" // spawn the named step
	ActionDone      ActionType = "done"       // pipeline complete
	ActionBlocked   ActionType = "blocked"    // loop cap exceeded or missing step
)

// NextAction is the driver's routing decision.
type NextAction struct {
	Type   ActionType
	StepID string // non-empty when Type == ActionSpawnStep
	Reason string // non-empty when Type == ActionBlocked
}

// Next returns the routing decision after a pipeline step completes.
// loops maps step ID → number of times that step has already run for this card.
// A nil pipeline returns ActionDone immediately (default Plan→Execute→Close flow).
func Next(
	p *types.WorkspacePipeline,
	currentStepID string,
	outcome Outcome,
	loops map[string]int,
) NextAction {
	if p == nil {
		return NextAction{Type: ActionDone}
	}
	step := findStep(p, currentStepID)
	if step == nil {
		return NextAction{
			Type:   ActionBlocked,
			Reason: fmt.Sprintf("step %q not found in pipeline", currentStepID),
		}
	}

	var targetID string
	if outcome == OutcomeSuccess {
		targetID = step.OnSuccess
	} else {
		targetID = step.OnFail
	}

	if targetID == "" {
		if outcome == OutcomeFail {
			return NextAction{
				Type:   ActionBlocked,
				Reason: fmt.Sprintf("step %q failed with no on_fail route", step.Name),
			}
		}
		return NextAction{Type: ActionDone}
	}

	target := findStep(p, targetID)
	if target == nil {
		return NextAction{
			Type:   ActionBlocked,
			Reason: fmt.Sprintf("next step %q not found (deleted from pipeline?)", targetID),
		}
	}

	if target.MaxLoops > 0 {
		cap := target.MaxLoops
		if loops[targetID] >= cap {
			return NextAction{
				Type:   ActionBlocked,
				Reason: fmt.Sprintf("step %q reached max loop cap (%d)", target.Name, cap),
			}
		}
	}

	return NextAction{Type: ActionSpawnStep, StepID: targetID}
}

// StepByID returns the PipelineStep with the given ID, or nil.
func StepByID(p *types.WorkspacePipeline, id string) *types.PipelineStep {
	return findStep(p, id)
}

// Validate checks the pipeline for structural errors:
//   - all step IDs are non-empty and unique
//   - on_success / on_fail references exist
//   - every cycle has at least one step with max_loops > 0
func Validate(p *types.WorkspacePipeline) error {
	if p == nil {
		return nil
	}
	byID := make(map[string]*types.PipelineStep, len(p.Steps))
	for i := range p.Steps {
		s := &p.Steps[i]
		if s.ID == "" {
			return errors.New("pipeline step missing id")
		}
		if _, dup := byID[s.ID]; dup {
			return fmt.Errorf("duplicate step id %q", s.ID)
		}
		byID[s.ID] = s
	}
	for _, s := range p.Steps {
		if s.OnSuccess != "" && byID[s.OnSuccess] == nil {
			return fmt.Errorf("step %q: on_success references unknown step %q", s.ID, s.OnSuccess)
		}
		if s.OnFail != "" && byID[s.OnFail] == nil {
			return fmt.Errorf("step %q: on_fail references unknown step %q", s.ID, s.OnFail)
		}
	}
	return validateCycles(byID, p.Steps)
}

// validateCycles enforces that every cycle in the pipeline has at least one
// step with max_loops > 0, preventing infinite iteration (q3: no uncapped cycles).
func validateCycles(byID map[string]*types.PipelineStep, steps []types.PipelineStep) error {
	for _, step := range steps {
		if !canReachItself(byID, step.ID) {
			continue
		}
		// step.ID is in a cycle; gather all members of that cycle.
		members := cycleMembers(byID, step.ID)
		hasCap := false
		for id := range members {
			if s := byID[id]; s != nil && s.MaxLoops > 0 {
				hasCap = true
				break
			}
		}
		if !hasCap {
			var names []string
			for id := range members {
				if s := byID[id]; s != nil {
					names = append(names, s.Name)
				}
			}
			return fmt.Errorf(
				"pipeline cycle through steps [%s] has no max_loops cap; set max_loops on at least one step to prevent infinite loops",
				strings.Join(names, ", "),
			)
		}
	}
	return nil
}

// canReachItself reports whether start can reach itself via on_success or on_fail edges.
func canReachItself(byID map[string]*types.PipelineStep, start string) bool {
	s := byID[start]
	if s == nil {
		return false
	}
	visited := map[string]bool{}
	queue := neighbors(s)
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		if cur == start {
			return true
		}
		if visited[cur] {
			continue
		}
		visited[cur] = true
		if node := byID[cur]; node != nil {
			queue = append(queue, neighbors(node)...)
		}
	}
	return false
}

// cycleMembers returns the set of step IDs that are part of the same cycle as start.
func cycleMembers(byID map[string]*types.PipelineStep, start string) map[string]bool {
	reachable := make(map[string]bool)
	bfsVisit(byID, start, reachable)

	members := make(map[string]bool)
	for id := range reachable {
		visited := make(map[string]bool)
		bfsVisit(byID, id, visited)
		if visited[start] {
			members[id] = true
		}
	}
	return members
}

func bfsVisit(byID map[string]*types.PipelineStep, start string, visited map[string]bool) {
	queue := []string{start}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		if visited[cur] {
			continue
		}
		visited[cur] = true
		if node := byID[cur]; node != nil {
			queue = append(queue, neighbors(node)...)
		}
	}
}

func neighbors(s *types.PipelineStep) []string {
	var out []string
	if s.OnSuccess != "" {
		out = append(out, s.OnSuccess)
	}
	if s.OnFail != "" {
		out = append(out, s.OnFail)
	}
	return out
}

func findStep(p *types.WorkspacePipeline, id string) *types.PipelineStep {
	for i := range p.Steps {
		if p.Steps[i].ID == id {
			return &p.Steps[i]
		}
	}
	return nil
}
