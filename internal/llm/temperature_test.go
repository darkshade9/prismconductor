package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"prismconductor/internal/types"
)

// captureBody is a minimal httptest.Server that records the last request body
// and returns a valid chat-completions response.
func temperatureTestServer(t *testing.T) (*httptest.Server, *[]byte) {
	t.Helper()
	var captured []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"},"finish_reason":"stop"}],"usage":{}}`))
	}))
	return srv, &captured
}

// TestOpenAICompatChatOmitsTemperatureWhenNil asserts that when Pool.Temperature
// is nil, the outgoing JSON body does not contain a "temperature" key.
func TestOpenAICompatChatOmitsTemperatureWhenNil(t *testing.T) {
	srv, body := temperatureTestServer(t)
	defer srv.Close()

	client := srv.Client()
	_, err := openAICompatChat(context.Background(), client, srv.URL, "", "model-x", "sys", "user", nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var req map[string]any
	if err := json.Unmarshal(*body, &req); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, ok := req["temperature"]; ok {
		t.Errorf("temperature key present in request when Pool.Temperature is nil; body: %s", *body)
	}
}

// TestOpenAICompatChatIncludesTemperatureWhenSet asserts that when
// Pool.Temperature is non-nil, its exact value appears in the request JSON.
func TestOpenAICompatChatIncludesTemperatureWhenSet(t *testing.T) {
	srv, body := temperatureTestServer(t)
	defer srv.Close()

	client := srv.Client()
	temp := 0.0
	_, err := openAICompatChat(context.Background(), client, srv.URL, "", "model-x", "sys", "user", &temp, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var req map[string]any
	if err := json.Unmarshal(*body, &req); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	got, ok := req["temperature"]
	if !ok {
		t.Fatalf("temperature key missing in request; body: %s", *body)
	}
	if got != 0.0 {
		t.Errorf("temperature = %v, want 0.0", got)
	}
}

// TestOpenAICompatChatIncludesTemperatureOne asserts temperature=1 round-trips correctly.
func TestOpenAICompatChatIncludesTemperatureOne(t *testing.T) {
	srv, body := temperatureTestServer(t)
	defer srv.Close()

	client := srv.Client()
	temp := 1.0
	_, err := openAICompatChat(context.Background(), client, srv.URL, "", "model-x", "sys", "user", &temp, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var req map[string]any
	if err := json.Unmarshal(*body, &req); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	got, ok := req["temperature"]
	if !ok {
		t.Fatalf("temperature key missing; body: %s", *body)
	}
	if got != 1.0 {
		t.Errorf("temperature = %v, want 1.0", got)
	}
}

// TestOpenAIToolChatOmitsTemperatureWhenNil mirrors the CompatChat test for
// the tool-chat path.
func TestOpenAIToolChatOmitsTemperatureWhenNil(t *testing.T) {
	srv, body := temperatureTestServer(t)
	defer srv.Close()

	client := srv.Client()
	_, err := openAIToolChat(context.Background(), client, srv.URL, "", "model-x", ChatRequest{System: "s"}, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var req map[string]any
	if err := json.Unmarshal(*body, &req); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, ok := req["temperature"]; ok {
		t.Errorf("temperature key present in tool-chat request when nil; body: %s", *body)
	}
}

// TestOpenAIToolChatIncludesTemperatureZero asserts temperature=0.0 reaches
// the wire in the tool-chat path (preserving determinism opt-in).
func TestOpenAIToolChatIncludesTemperatureZero(t *testing.T) {
	srv, body := temperatureTestServer(t)
	defer srv.Close()

	client := srv.Client()
	temp := 0.0
	_, err := openAIToolChat(context.Background(), client, srv.URL, "", "model-x", ChatRequest{System: "s"}, &temp, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var req map[string]any
	if err := json.Unmarshal(*body, &req); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	got, ok := req["temperature"]
	if !ok {
		t.Fatalf("temperature missing in tool-chat request; body: %s", *body)
	}
	if got != 0.0 {
		t.Errorf("tool-chat temperature = %v, want 0.0", got)
	}
}

// TestPoolTemperatureRoundTrip asserts that types.Pool.Temperature pointer
// semantics work as expected: nil vs explicit 0.0.
func TestPoolTemperatureRoundTrip(t *testing.T) {
	p1 := types.Pool{Model: "m"}
	if p1.Temperature != nil {
		t.Error("Temperature should be nil by default")
	}

	zero := 0.0
	p2 := types.Pool{Model: "m", Temperature: &zero}
	if p2.Temperature == nil || *p2.Temperature != 0.0 {
		t.Error("Temperature should be 0.0")
	}

	one := 1.0
	p3 := types.Pool{Model: "m", Temperature: &one}
	if p3.Temperature == nil || *p3.Temperature != 1.0 {
		t.Error("Temperature should be 1.0")
	}
}
