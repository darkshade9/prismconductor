package store

import (
	"reflect"
	"testing"
)

func TestGetLabelFilterDefaults(t *testing.T) {
	s := newTestStore(t)
	labels, mode, err := s.GetLabelFilter("ws1")
	if err != nil {
		t.Fatalf("GetLabelFilter: %v", err)
	}
	if mode != "or" {
		t.Errorf("default mode = %q, want %q", mode, "or")
	}
	if len(labels) != 0 {
		t.Errorf("default labels = %v, want empty", labels)
	}
}

func TestSetAndGetLabelFilter(t *testing.T) {
	s := newTestStore(t)

	want := []string{"bug", "enhancement"}
	if err := s.SetLabelFilter("ws1", want, "and"); err != nil {
		t.Fatalf("SetLabelFilter: %v", err)
	}

	got, mode, err := s.GetLabelFilter("ws1")
	if err != nil {
		t.Fatalf("GetLabelFilter: %v", err)
	}
	if mode != "and" {
		t.Errorf("mode = %q, want %q", mode, "and")
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("labels = %v, want %v", got, want)
	}
}

func TestSetLabelFilterNilSlice(t *testing.T) {
	s := newTestStore(t)

	if err := s.SetLabelFilter("ws2", nil, "or"); err != nil {
		t.Fatalf("SetLabelFilter nil: %v", err)
	}

	got, mode, err := s.GetLabelFilter("ws2")
	if err != nil {
		t.Fatalf("GetLabelFilter: %v", err)
	}
	if mode != "or" {
		t.Errorf("mode = %q, want or", mode)
	}
	if len(got) != 0 {
		t.Errorf("labels = %v, want empty", got)
	}
}

func TestSetLabelFilterWorkspaceIsolation(t *testing.T) {
	s := newTestStore(t)

	_ = s.SetLabelFilter("ws1", []string{"bug"}, "and")
	_ = s.SetLabelFilter("ws2", []string{"feature"}, "or")

	labels1, mode1, _ := s.GetLabelFilter("ws1")
	labels2, mode2, _ := s.GetLabelFilter("ws2")

	if !reflect.DeepEqual(labels1, []string{"bug"}) || mode1 != "and" {
		t.Errorf("ws1 = %v/%s, want [bug]/and", labels1, mode1)
	}
	if !reflect.DeepEqual(labels2, []string{"feature"}) || mode2 != "or" {
		t.Errorf("ws2 = %v/%s, want [feature]/or", labels2, mode2)
	}
}
