package session

import (
	"strings"
	"testing"
)

func TestPatternPROpenedMatches(t *testing.T) {
	const sample = "@asst PR_OPENED: https://github.com/o/r/pull/123"
	if !strings.Contains(sample, PatternPROpened) {
		t.Fatalf("expected sample to contain PatternPROpened (%q)", PatternPROpened)
	}
	idx := strings.Index(sample, PatternPROpened)
	url := strings.TrimSpace(sample[idx+len(PatternPROpened):])
	if url != "https://github.com/o/r/pull/123" {
		t.Errorf("extracted url = %q", url)
	}
}

func TestPatternPROpenedEmptyURLGuard(t *testing.T) {
	const empty = "PR_OPENED: "
	idx := strings.Index(empty, PatternPROpened)
	url := strings.TrimSpace(empty[idx+len(PatternPROpened):])
	if url != "" {
		t.Errorf("expected empty url, got %q", url)
	}
}
