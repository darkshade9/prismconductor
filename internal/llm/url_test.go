package llm

import "testing"

func TestChatCompletionsURL(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"bare host", "http://localhost:1234", "http://localhost:1234/v1/chat/completions"},
		{"trailing slash", "http://localhost:1234/", "http://localhost:1234/v1/chat/completions"},
		{"openai with v1", "https://api.openai.com/v1", "https://api.openai.com/v1/chat/completions"},
		{"openai bare", "https://api.openai.com", "https://api.openai.com/v1/chat/completions"},
		{"gemini openai compat", "https://generativelanguage.googleapis.com/v1beta/openai", "https://generativelanguage.googleapis.com/v1beta/openai/chat/completions"},
		{"gemini trailing slash", "https://generativelanguage.googleapis.com/v1beta/openai/", "https://generativelanguage.googleapis.com/v1beta/openai/chat/completions"},
		{"v2 only", "https://example.com/v2", "https://example.com/v2/chat/completions"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := chatCompletionsURL(c.in); got != c.want {
				t.Errorf("chatCompletionsURL(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

func TestModelsURL(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"bare", "http://localhost:1234", "http://localhost:1234/v1/models"},
		{"openai with v1", "https://api.openai.com/v1", "https://api.openai.com/v1/models"},
		{"gemini openai compat", "https://generativelanguage.googleapis.com/v1beta/openai", "https://generativelanguage.googleapis.com/v1beta/openai/models"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := modelsURL(c.in); got != c.want {
				t.Errorf("modelsURL(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}
