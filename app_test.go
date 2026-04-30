package main

import (
	"fmt"
	"reflect"
	"testing"
	"time"

	"prismconductor/internal/eventbus"
	"prismconductor/internal/store"
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

// Issue #49: pin the Archive-DONE chain end-to-end at the App layer. The bug
// report claimed the 📦 Archive N button was dead; rev1's static walk showed
// every link wired correctly. This test fails loudly if the SQL filter, the
// bus publish, or the ListIssues exclusion ever regress.
func TestApp_ArchiveDone_PublishesEventAndExcludesFromListIssues(t *testing.T) {
	s, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	bus := eventbus.New()
	a := &App{store: s, bus: bus}

	seed := func(num int, col types.BoardColumn) {
		if _, err := s.SaveIssue(types.Issue{
			WorkspaceID: "ws1",
			Number:      num,
			Title:       fmt.Sprintf("#%d", num),
			State:       "open",
			Column:      col,
		}); err != nil {
			t.Fatalf("SaveIssue %d: %v", num, err)
		}
	}
	seed(101, types.ColDone)
	seed(102, types.ColDone)
	seed(103, types.ColDone)
	seed(200, types.ColInProgress)

	captured := make(chan eventbus.Event, 4)
	bus.Subscribe(func(e eventbus.Event) {
		if e.Type == eventbus.EvtIssuesArchived {
			captured <- e
		}
	})

	n, err := a.ArchiveDone("ws1")
	if err != nil {
		t.Fatalf("ArchiveDone: %v", err)
	}
	if n != 3 {
		t.Errorf("count = %d, want 3", n)
	}

	select {
	case evt := <-captured:
		p, ok := evt.Payload.(map[string]any)
		if !ok {
			t.Fatalf("payload type = %T, want map[string]any", evt.Payload)
		}
		if p["workspace_id"] != "ws1" {
			t.Errorf("workspace_id = %v, want ws1", p["workspace_id"])
		}
		if p["count"] != 3 {
			t.Errorf("count = %v, want 3", p["count"])
		}
	case <-time.After(time.Second):
		t.Fatal("EvtIssuesArchived not published within 1s")
	}

	live, err := a.ListIssues("ws1")
	if err != nil {
		t.Fatalf("ListIssues: %v", err)
	}
	if len(live) != 1 || live[0].Number != 200 {
		t.Errorf("ListIssues after archive = %v, want only #200", live)
	}

	arch, err := a.ListArchivedIssues("ws1")
	if err != nil {
		t.Fatalf("ListArchivedIssues: %v", err)
	}
	if len(arch) != 3 {
		t.Errorf("archived count = %d, want 3", len(arch))
	}
}

// FetchIssueDetail falls back to the local mirror when the github client is unavailable.
func TestApp_FetchIssueDetail_FallbackToLocal(t *testing.T) {
	s, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	if _, err := s.SaveIssue(types.Issue{
		WorkspaceID: "ws1",
		Number:      42,
		Title:       "test issue",
		Body:        "body text",
		State:       "open",
		Column:      types.ColTodo,
	}); err != nil {
		t.Fatalf("SaveIssue: %v", err)
	}

	a := &App{store: s, gh: nil}
	iss, err := a.FetchIssueDetail("ws1", 42)
	if err != nil {
		t.Fatalf("FetchIssueDetail: %v", err)
	}
	if iss.Number != 42 || iss.Body != "body text" {
		t.Errorf("unexpected issue: %+v", iss)
	}
}

// FetchIssueDetail returns a cached result on the second call without hitting GitHub.
func TestApp_FetchIssueDetail_Cache(t *testing.T) {
	s, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	a := &App{store: s, gh: nil}
	cached := types.Issue{WorkspaceID: "ws1", Number: 7, Title: "cached", Body: "cached body", State: "open"}
	a.issueDetailCache.Store("ws1#7", issueDetailEntry{issue: cached, expiresAt: time.Now().Add(60 * time.Second)})

	iss, err := a.FetchIssueDetail("ws1", 7)
	if err != nil {
		t.Fatalf("FetchIssueDetail: %v", err)
	}
	if iss.Title != "cached" {
		t.Errorf("expected cached title, got %q", iss.Title)
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
