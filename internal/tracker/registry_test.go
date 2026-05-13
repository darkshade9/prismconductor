package tracker_test

import (
	"context"
	"testing"

	"prismconductor/internal/tracker"
	"prismconductor/internal/types"
)

// fakeTracker implements tracker.Tracker for testing.
type fakeTracker struct{ kind tracker.TrackerKind }

func (f *fakeTracker) Kind() tracker.TrackerKind { return f.kind }
func (f *fakeTracker) ListIssues(context.Context, types.Workspace) ([]types.Issue, error) {
	return nil, nil
}
func (f *fakeTracker) FetchIssue(context.Context, types.Workspace, tracker.IssueRef) (types.Issue, error) {
	return types.Issue{}, nil
}
func (f *fakeTracker) UpdateIssueStatus(context.Context, types.Workspace, tracker.IssueRef, tracker.IssueStatus) error {
	return nil
}
func (f *fakeTracker) PostComment(context.Context, types.Workspace, tracker.IssueRef, string) error {
	return nil
}
func (f *fakeTracker) LinkPRToIssue(context.Context, types.Workspace, tracker.IssueRef, string) error {
	return nil
}
func (f *fakeTracker) ListAvailableStatuses(context.Context, types.Workspace, tracker.IssueRef) ([]tracker.IssueStatus, error) {
	return nil, nil
}
func (f *fakeTracker) ListLabels(context.Context, types.Workspace) ([]string, error) {
	return nil, nil
}
func (f *fakeTracker) SetIssueLabels(context.Context, types.Workspace, tracker.IssueRef, []string) error {
	return nil
}
func (f *fakeTracker) CreateIssue(context.Context, types.Workspace, string, string, []string) (int, string, error) {
	return 0, "", tracker.ErrCreateIssueUnsupported
}

func TestRegistry_RegisterAndGet(t *testing.T) {
	// Use a unique kind so parallel tests don't conflict.
	const testKind tracker.TrackerKind = "test-registry-kind"

	ft := &fakeTracker{kind: testKind}
	tracker.Register(ft)

	got, err := tracker.Get(testKind)
	if err != nil {
		t.Fatalf("Get(%q) error: %v", testKind, err)
	}
	if got.Kind() != testKind {
		t.Errorf("expected kind %q, got %q", testKind, got.Kind())
	}
}

func TestRegistry_GetUnknown(t *testing.T) {
	_, err := tracker.Get("nonexistent-tracker-xyz")
	if err == nil {
		t.Fatal("expected error for unknown tracker kind, got nil")
	}
}
