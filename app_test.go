package main

import (
	"reflect"
	"testing"

	"prismconductor/internal/types"
)

func TestPullNumberFromURL(t *testing.T) {
	cases := []struct {
		in     string
		want   int
		wantOK bool
	}{
		{"https://github.com/o/r/pull/123", 123, true},
		{"https://github.com/o/r/pull/1", 1, true},
		{"https://github.com/foo/bar/pull/42#issuecomment-1", 42, true},
		{"https://github.com/o/r/issues/9", 0, false},
		{"", 0, false},
		{"PR_OPENED:", 0, false},
	}
	for _, c := range cases {
		got, ok := pullNumberFromURL(c.in)
		if ok != c.wantOK || got != c.want {
			t.Errorf("pullNumberFromURL(%q) = (%d,%v), want (%d,%v)", c.in, got, ok, c.want, c.wantOK)
		}
	}
}

// Issue #24: AutoApplyLabelsEnabled defaults to true when the field is absent
// (nil) so legacy workspace JSON keeps the new behavior without a migration.
func TestAutoApplyLabelsEnabled(t *testing.T) {
	tt := true
	ff := false
	cases := []struct {
		name string
		ptr  *bool
		want bool
	}{
		{"nil defaults to true (legacy JSON)", nil, true},
		{"explicit true", &tt, true},
		{"explicit false disables", &ff, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			sp := types.SkillProfile{AutoApplyLabels: c.ptr}
			if got := sp.AutoApplyLabelsEnabled(); got != c.want {
				t.Fatalf("AutoApplyLabelsEnabled() = %v, want %v", got, c.want)
			}
		})
	}
}

// Issue #24 q2 A: re-plan reconciliation drops the prior axis label only,
// preserves topical/manual labels, and unions the new suggestions.
func TestReconcileAutoLabels(t *testing.T) {
	cases := []struct {
		name      string
		current   []string
		keep      []string
		priorAxis string
		want      []string
	}{
		{
			name:      "first plan: union with empty current",
			current:   nil,
			keep:      []string{"enhancement", "frontend"},
			priorAxis: "",
			want:      []string{"enhancement", "frontend"},
		},
		{
			name:      "no-op when keep already on issue",
			current:   []string{"enhancement", "frontend"},
			keep:      []string{"enhancement", "frontend"},
			priorAxis: "",
			want:      []string{"enhancement", "frontend"},
		},
		{
			name:      "first plan: preserves manual labels",
			current:   []string{"manual-tag"},
			keep:      []string{"enhancement"},
			priorAxis: "",
			want:      []string{"manual-tag", "enhancement"},
		},
		{
			name:      "re-plan axis swap: drops prior axis, keeps topical+manual, adds new axis",
			current:   []string{"enhancement", "frontend", "manual-tag"},
			keep:      []string{"bug"},
			priorAxis: "enhancement",
			want:      []string{"frontend", "manual-tag", "bug"},
		},
		{
			name:      "re-plan: prior axis still suggested, do not drop",
			current:   []string{"enhancement", "frontend"},
			keep:      []string{"enhancement", "backend"},
			priorAxis: "enhancement",
			want:      []string{"enhancement", "frontend", "backend"},
		},
		{
			name:      "re-plan: prior axis missing from issue (already removed manually) is a no-op for drop",
			current:   []string{"frontend"},
			keep:      []string{"bug"},
			priorAxis: "enhancement",
			want:      []string{"frontend", "bug"},
		},
		{
			name:      "duplicate suggestions and labels are deduped",
			current:   []string{"frontend", "frontend"},
			keep:      []string{"frontend", "bug", "bug"},
			priorAxis: "",
			want:      []string{"frontend", "bug"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := reconcileAutoLabels(c.current, c.keep, c.priorAxis)
			if !reflect.DeepEqual(got, c.want) {
				t.Fatalf("reconcileAutoLabels(%v, %v, %q)\n  got:  %v\n  want: %v",
					c.current, c.keep, c.priorAxis, got, c.want)
			}
		})
	}
}

func TestLabelSetEqual(t *testing.T) {
	cases := []struct {
		a, b []string
		want bool
	}{
		{nil, nil, true},
		{[]string{}, nil, true},
		{[]string{"a"}, []string{"a"}, true},
		{[]string{"a", "b"}, []string{"b", "a"}, true},
		{[]string{"a"}, []string{"a", "b"}, false},
		{[]string{"a", "b"}, []string{"a", "c"}, false},
	}
	for _, c := range cases {
		if got := labelSetEqual(c.a, c.b); got != c.want {
			t.Errorf("labelSetEqual(%v, %v) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}
