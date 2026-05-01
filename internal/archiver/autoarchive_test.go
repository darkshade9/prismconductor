package archiver

import (
	"context"
	"testing"

	"prismconductor/internal/types"
)

type fakeStore struct {
	calls    []fakeCall
	returnN  int
	returnErr error
}

type fakeCall struct {
	workspaceID string
	daysClosed  int
}

func (f *fakeStore) ArchiveClosedByAge(workspaceID string, daysClosed int) (int, error) {
	f.calls = append(f.calls, fakeCall{workspaceID, daysClosed})
	return f.returnN, f.returnErr
}

func TestRunAutoArchive_Disabled(t *testing.T) {
	fs := &fakeStore{returnN: 5}
	ws := types.Workspace{ID: "ws1", AutoArchive: types.AutoArchive{Enabled: false, DaysClosed: 7}}
	n, err := RunAutoArchive(context.Background(), fs, ws)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("expected 0 when disabled, got %d", n)
	}
	if len(fs.calls) != 0 {
		t.Errorf("expected no store calls when disabled, got %d", len(fs.calls))
	}
}

func TestRunAutoArchive_Enabled(t *testing.T) {
	fs := &fakeStore{returnN: 3}
	ws := types.Workspace{ID: "ws2", AutoArchive: types.AutoArchive{Enabled: true, DaysClosed: 14}}
	n, err := RunAutoArchive(context.Background(), fs, ws)
	if err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Errorf("expected 3, got %d", n)
	}
	if len(fs.calls) != 1 {
		t.Fatalf("expected 1 store call, got %d", len(fs.calls))
	}
	if fs.calls[0].workspaceID != "ws2" {
		t.Errorf("wrong workspace: %q", fs.calls[0].workspaceID)
	}
	if fs.calls[0].daysClosed != 14 {
		t.Errorf("wrong days: %d", fs.calls[0].daysClosed)
	}
}

func TestRunAutoArchive_DefaultDays(t *testing.T) {
	fs := &fakeStore{returnN: 1}
	// DaysClosed=0 should fall back to 7.
	ws := types.Workspace{ID: "ws3", AutoArchive: types.AutoArchive{Enabled: true, DaysClosed: 0}}
	_, err := RunAutoArchive(context.Background(), fs, ws)
	if err != nil {
		t.Fatal(err)
	}
	if len(fs.calls) != 1 {
		t.Fatalf("expected 1 store call, got %d", len(fs.calls))
	}
	if fs.calls[0].daysClosed != 7 {
		t.Errorf("expected default 7 days, got %d", fs.calls[0].daysClosed)
	}
}

func TestRunAutoArchive_MaxDays(t *testing.T) {
	fs := &fakeStore{returnN: 0}
	// DaysClosed=999 should be clamped to 365.
	ws := types.Workspace{ID: "ws4", AutoArchive: types.AutoArchive{Enabled: true, DaysClosed: 999}}
	_, err := RunAutoArchive(context.Background(), fs, ws)
	if err != nil {
		t.Fatal(err)
	}
	if fs.calls[0].daysClosed != 365 {
		t.Errorf("expected max 365 days, got %d", fs.calls[0].daysClosed)
	}
}
