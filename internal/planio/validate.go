package planio

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// ValidatePlanJSON enforces the §9.1 plan contract on a raw JSON document
// (issue #71). The harness's Write tool calls this *before* writing a file
// matching `.prismconductor/plans/<num>-rev<N>.json`, so a misshapen plan
// never lands on disk and the model gets immediate, actionable feedback.
//
// The validator is intentionally strict: no field-name aliases, no per-model
// fallbacks, no "we'll figure it out later" lenience. Unknown extra fields
// are tolerated (the schema may grow); known fields with the wrong key
// (`issue` instead of `issue_number`) are rejected with a specific message.
//
// Errors are formatted to be self-explanatory to a model reading them in a
// tool_result: every message starts with the offending field name and gives
// the rule that was violated.
func ValidatePlanJSON(b []byte) error {
	// Stage 1: parse as a generic map so we can detect unknown vs aliased
	// keys before Go's strict-types decoder rejects on a type mismatch.
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	var raw map[string]any
	if err := dec.Decode(&raw); err != nil {
		return fmt.Errorf("plan body is not a valid JSON object: %w", err)
	}

	if _, hasAlias := raw["issue"]; hasAlias {
		if _, ok := raw["issue_number"]; !ok {
			return fmt.Errorf("field `issue` is not the contract — use `issue_number` (an integer matching the issue you were dispatched against)")
		}
	}
	if _, hasAlias := raw["rev"]; hasAlias {
		if _, ok := raw["revision"]; !ok {
			return fmt.Errorf("field `rev` is not the contract — use `revision` (an integer; first plan is 1)")
		}
	}

	if err := requireInt(raw, "issue_number", 1); err != nil {
		return err
	}
	if err := requireInt(raw, "revision", 1); err != nil {
		return err
	}
	if err := requireString(raw, "plan_markdown"); err != nil {
		return err
	}
	if err := requireString(raw, "estimated_complexity"); err != nil {
		return err
	}
	if err := requireBool(raw, "ready_to_execute"); err != nil {
		return err
	}

	if err := validateFilesToModify(raw); err != nil {
		return err
	}
	if err := validateDependenciesDetected(raw); err != nil {
		return err
	}
	if err := validateSuggestedLabels(raw); err != nil {
		return err
	}
	if err := validateQuestions(raw); err != nil {
		return err
	}
	return nil
}

// requireInt asserts that key holds a JSON number >= min.
func requireInt(raw map[string]any, key string, min int) error {
	v, ok := raw[key]
	if !ok {
		return fmt.Errorf("required field `%s` is missing", key)
	}
	n, ok := v.(json.Number)
	if !ok {
		return fmt.Errorf("`%s` must be an integer, got %T", key, v)
	}
	i, err := n.Int64()
	if err != nil {
		return fmt.Errorf("`%s` must be an integer, got %s", key, n.String())
	}
	if int(i) < min {
		return fmt.Errorf("`%s` must be >= %d, got %d", key, min, i)
	}
	return nil
}

// requireString asserts that key holds a non-empty JSON string.
func requireString(raw map[string]any, key string) error {
	v, ok := raw[key]
	if !ok {
		return fmt.Errorf("required field `%s` is missing", key)
	}
	s, ok := v.(string)
	if !ok {
		return fmt.Errorf("`%s` must be a string, got %T", key, v)
	}
	if strings.TrimSpace(s) == "" {
		return fmt.Errorf("`%s` must be a non-empty string", key)
	}
	return nil
}

// requireBool asserts that key holds a JSON boolean (presence required;
// false is acceptable). Plans must always make a yes/no decision on
// ready-to-execute even when the answer is no.
func requireBool(raw map[string]any, key string) error {
	v, ok := raw[key]
	if !ok {
		return fmt.Errorf("required field `%s` is missing", key)
	}
	if _, ok := v.(bool); !ok {
		return fmt.Errorf("`%s` must be a boolean, got %T", key, v)
	}
	return nil
}

// validateFilesToModify rejects the {add,modify,delete} lists shape some
// models invent and pins the array-of-{path,intent} contract.
func validateFilesToModify(raw map[string]any) error {
	v, ok := raw["files_to_modify"]
	if !ok {
		return fmt.Errorf("required field `files_to_modify` is missing — use [] if no files change")
	}
	if m, isMap := v.(map[string]any); isMap {
		_, hasAdd := m["add"]
		_, hasMod := m["modify"]
		_, hasDel := m["delete"]
		if hasAdd || hasMod || hasDel {
			return fmt.Errorf("`files_to_modify` must be an array of `{path, intent}` objects, not a `{add, modify, delete}` shape")
		}
		return fmt.Errorf("`files_to_modify` must be an array, got an object")
	}
	arr, ok := v.([]any)
	if !ok {
		return fmt.Errorf("`files_to_modify` must be an array, got %T", v)
	}
	for i, item := range arr {
		obj, ok := item.(map[string]any)
		if !ok {
			return fmt.Errorf("`files_to_modify[%d]` must be an object with `path` and `intent`, got %T", i, item)
		}
		path, ok := obj["path"].(string)
		if !ok || strings.TrimSpace(path) == "" {
			return fmt.Errorf("`files_to_modify[%d].path` is required and must be a non-empty string", i)
		}
		intent, ok := obj["intent"].(string)
		if !ok || strings.TrimSpace(intent) == "" {
			return fmt.Errorf("`files_to_modify[%d].intent` is required and must be a non-empty string (e.g. \"modify\", \"add\", \"delete\")", i)
		}
	}
	return nil
}

// validateDependenciesDetected requires the field to exist (so re-planning
// can omit-vs-empty distinguish), but accepts an empty array.
func validateDependenciesDetected(raw map[string]any) error {
	v, ok := raw["dependencies_detected"]
	if !ok {
		return fmt.Errorf("required field `dependencies_detected` is missing — use [] if no dependencies")
	}
	arr, ok := v.([]any)
	if !ok {
		return fmt.Errorf("`dependencies_detected` must be an array of issue numbers, got %T", v)
	}
	for i, item := range arr {
		n, ok := item.(json.Number)
		if !ok {
			return fmt.Errorf("`dependencies_detected[%d]` must be an integer issue number, got %T", i, item)
		}
		if _, err := n.Int64(); err != nil {
			return fmt.Errorf("`dependencies_detected[%d]` must be an integer issue number, got %s", i, n.String())
		}
	}
	return nil
}

// validateSuggestedLabels is optional; if present it must be an array of plain
// label strings (no `(create)` / `(use)` annotations the prose-leaning models
// invent).
func validateSuggestedLabels(raw map[string]any) error {
	v, ok := raw["suggested_labels"]
	if !ok {
		return nil
	}
	arr, ok := v.([]any)
	if !ok {
		return fmt.Errorf("`suggested_labels` must be an array of label strings, got %T", v)
	}
	for i, item := range arr {
		s, ok := item.(string)
		if !ok {
			return fmt.Errorf("`suggested_labels[%d]` must be a string, got %T", i, item)
		}
		if strings.Contains(s, "(") || strings.Contains(s, ")") {
			return fmt.Errorf("`suggested_labels[%d]` (%q) must be a plain label name, not a label-with-annotation", i, s)
		}
		if strings.TrimSpace(s) == "" {
			return fmt.Errorf("`suggested_labels[%d]` must be non-empty", i)
		}
	}
	return nil
}

// validateQuestions enforces the §6.4 Question shape on every entry: id +
// type + prompt + default required, type ∈ enum, options required when type
// is single_choice or multi_choice.
func validateQuestions(raw map[string]any) error {
	v, ok := raw["questions"]
	if !ok {
		return fmt.Errorf("required field `questions` is missing — use [] if no questions")
	}
	arr, ok := v.([]any)
	if !ok {
		return fmt.Errorf("`questions` must be an array, got %T", v)
	}
	validTypes := map[string]bool{
		"single_choice": true,
		"multi_choice":  true,
		"free_text":     true,
		"yes_no":        true,
	}
	for i, item := range arr {
		obj, ok := item.(map[string]any)
		if !ok {
			return fmt.Errorf("`questions[%d]` must be an object, got %T", i, item)
		}
		id, ok := obj["id"].(string)
		if !ok || strings.TrimSpace(id) == "" {
			return fmt.Errorf("`questions[%d].id` is required and must be a non-empty string", i)
		}
		qType, ok := obj["type"].(string)
		if !ok || !validTypes[qType] {
			return fmt.Errorf("`questions[%d].type` (id=%q) must be one of single_choice / multi_choice / free_text / yes_no", i, id)
		}
		if prompt, ok := obj["prompt"].(string); !ok || strings.TrimSpace(prompt) == "" {
			return fmt.Errorf("`questions[%d].prompt` (id=%q) is required and must be a non-empty string", i, id)
		}
		if _, present := obj["default"]; !present {
			return fmt.Errorf("`questions[%d].default` (id=%q) is required — every question must carry one recommended answer", i, id)
		}
		if qType == "single_choice" || qType == "multi_choice" {
			opts, ok := obj["options"].([]any)
			if !ok || len(opts) == 0 {
				return fmt.Errorf("`questions[%d].options` (id=%q, type=%s) must be a non-empty array", i, id, qType)
			}
		}
	}
	return nil
}
