package types

import (
	"encoding/json"
	"testing"
)

// TestQuestionAudienceDefaultsToEmpty verifies that questions written without
// the audience field (pre-issue-#211 format) deserialize to audience="" so
// the conductor treats them as user-facing (backwards compatible).
func TestQuestionAudienceDefaultsToEmpty(t *testing.T) {
	legacy := []byte(`{"id":"q1","type":"yes_no","prompt":"Add tests?","required":true}`)
	var q Question
	if err := json.Unmarshal(legacy, &q); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if q.Audience != "" {
		t.Errorf("expected empty audience on legacy question; got %q", q.Audience)
	}
}

// TestQuestionAudiencePeerAgent verifies the peer_agent value round-trips.
func TestQuestionAudiencePeerAgent(t *testing.T) {
	q := Question{
		ID:       "q2",
		Type:     QuestionSingleChoice,
		Prompt:   "Which pattern?",
		Audience: "peer_agent",
		Required: true,
	}
	b, err := json.Marshal(q)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got Question
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Audience != "peer_agent" {
		t.Errorf("audience round-trip: got %q want %q", got.Audience, "peer_agent")
	}
}

// TestValidRoleArchitect verifies that the new architect role passes
// ValidRole and that an unknown role still fails.
func TestValidRoleArchitect(t *testing.T) {
	cases := []struct {
		role  Role
		valid bool
	}{
		{RolePlan, true},
		{RoleWork, true},
		{RoleOrchestrator, true},
		{RoleArchitect, true},
		{"unknown", false},
		{"", false},
	}
	for _, c := range cases {
		got := ValidRole(c.role)
		if got != c.valid {
			t.Errorf("ValidRole(%q) = %v, want %v", c.role, got, c.valid)
		}
	}
}
