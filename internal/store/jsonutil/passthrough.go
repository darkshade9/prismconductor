// Package jsonutil provides a round-trip-preserving JSON helper for the store layer.
// It keeps unknown fields from a newer-binary-written row alive when an older binary
// reads and re-saves the same row, preventing downgrade-induced field loss.
package jsonutil

import (
	"encoding/json"
	"reflect"
	"strings"
)

// Load decodes raw into v and returns the original decoded map so unknown fields
// survive a future Save call. Errors from either decode are returned.
func Load(raw []byte, v any) (map[string]any, error) {
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(raw, v); err != nil {
		return nil, err
	}
	return m, nil
}

// Save marshals v and merges unknown fields from prior (the map returned by a
// previous Load call) onto the marshaled result. "Unknown" means a key present
// in prior that does not belong to v's struct schema (i.e., it was written by a
// newer binary and the current struct definition doesn't have that field yet).
//
// Known struct fields that were intentionally cleared (zero / nil with omitempty)
// are NOT restored from prior — the struct's marshaled form is authoritative for
// all fields the struct knows about.
//
// If prior is nil, Save falls back to plain json.Marshal(v).
func Save(prior map[string]any, v any) ([]byte, error) {
	if prior == nil {
		return json.Marshal(v)
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var typed map[string]any
	if err := json.Unmarshal(b, &typed); err != nil {
		return nil, err
	}

	// Determine which keys belong to v's struct schema (known fields).
	// Only preserve prior keys that are NOT in the schema (future/unknown fields).
	schema := structJSONKeys(v)
	for k, val := range prior {
		if _, inSchema := schema[k]; !inSchema {
			// Unknown future field — preserve it only if the current binary
			// didn't write a value for it.
			if _, inTyped := typed[k]; !inTyped {
				typed[k] = val
			}
		}
		// Known schema key absent from typed = intentionally zeroed; do not restore.
	}
	return json.Marshal(typed)
}

// structJSONKeys returns the set of JSON key names for v's struct type,
// following embedded structs. Only works for struct types; returns empty map otherwise.
func structJSONKeys(v any) map[string]struct{} {
	t := reflect.TypeOf(v)
	if t == nil {
		return nil
	}
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return nil
	}
	keys := make(map[string]struct{})
	collectKeys(t, keys)
	return keys
}

func collectKeys(t reflect.Type, out map[string]struct{}) {
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		tag := f.Tag.Get("json")
		if tag == "-" {
			continue
		}
		name, _, _ := strings.Cut(tag, ",")
		if name == "" {
			name = f.Name
		}
		ft := f.Type
		for ft.Kind() == reflect.Ptr {
			ft = ft.Elem()
		}
		// Inline anonymous (embedded) structs.
		if f.Anonymous && ft.Kind() == reflect.Struct {
			collectKeys(ft, out)
			continue
		}
		out[name] = struct{}{}
	}
}
