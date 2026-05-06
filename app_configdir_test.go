package main

import (
	"strings"
	"testing"
)

func TestConfigDir_RespectsEnvOverride(t *testing.T) {
	want := t.TempDir()
	t.Setenv("PRISMCONDUCTOR_DATA_DIR", want)
	got, err := configDir()
	if err != nil {
		t.Fatalf("configDir: %v", err)
	}
	if got != want {
		t.Errorf("configDir() = %q, want %q", got, want)
	}
}

func TestConfigDir_FallbackUnchanged(t *testing.T) {
	t.Setenv("PRISMCONDUCTOR_DATA_DIR", "")
	got, err := configDir()
	if err != nil {
		t.Fatalf("configDir: %v", err)
	}
	lower := strings.ToLower(got)
	if !strings.Contains(lower, "prismconductor") {
		t.Errorf("configDir() = %q, expected it to contain 'prismconductor'", got)
	}
}
