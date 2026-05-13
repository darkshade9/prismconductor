package llm

import "testing"

func TestIsFreeTierProvider(t *testing.T) {
	cases := []struct {
		provider string
		want     bool
	}{
		{"ollama", true},
		{"lmstudio", true},
		{"codex", true},
		{"claude", false},
		{"openai", false},
		{"gemini", false},
		{"litellm", false},
		{"", false},
	}
	for _, tc := range cases {
		got := IsFreeTierProvider(tc.provider)
		if got != tc.want {
			t.Errorf("IsFreeTierProvider(%q) = %v, want %v", tc.provider, got, tc.want)
		}
	}
}
