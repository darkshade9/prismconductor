package remoteworker

import (
	"encoding/json"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// replaceHTTPClient swaps the package-level httpClient for a test server
// client, and restores it when the test ends.
func replaceHTTPClient(t *testing.T, client *http.Client) {
	t.Helper()
	orig := httpClient
	httpClient = client
	t.Cleanup(func() { httpClient = orig })
}

func TestVerifyToken_success(t *testing.T) {
	// CF API base includes the /client/v4 prefix in the path.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/client/v4/user/tokens/verify":
			json.NewEncoder(w).Encode(cfResponse{Success: true, Result: json.RawMessage(`{"id":"tok1","status":"active"}`)})
		case "/client/v4/accounts":
			json.NewEncoder(w).Encode(cfResponse{
				Success: true,
				Result:  json.RawMessage(`[{"id":"acct123","name":"My CF Account"}]`),
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	// Rewrite api.cloudflare.com → test server, preserving the full path.
	replaceHTTPClient(t, &http.Client{Transport: &rewriteTransport{base: srv.URL}})

	res, err := VerifyToken("testtoken")
	if err != nil {
		t.Fatalf("VerifyToken: %v", err)
	}
	if res.AccountID != "acct123" {
		t.Errorf("AccountID = %q, want acct123", res.AccountID)
	}
	if res.AccountName != "My CF Account" {
		t.Errorf("AccountName = %q, want My CF Account", res.AccountName)
	}
}

func TestVerifyToken_invalidToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(cfResponse{
			Success: false,
			Errors:  []cfError{{Code: 1000, Message: "Invalid API Token"}},
		})
	}))
	defer srv.Close()
	replaceHTTPClient(t, &http.Client{Transport: &rewriteTransport{base: srv.URL}})

	_, err := VerifyToken("badtoken")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestSanitizeID(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"my-workspace", "my-workspace"},
		{"My Workspace!", "my-workspace"},
		{"abc_123", "abc-123"},
		{"UPPERCASE", "uppercase"},
		{"a-very-long-workspace-id-that-exceeds-thirty-chars", "a-very-long-workspace-id-that-"},
	}
	for _, c := range cases {
		got := sanitizeID(c.in)
		// Trim trailing hyphens for the length test case.
		if c.in == "a-very-long-workspace-id-that-exceeds-thirty-chars" {
			if len(got) > 30 {
				t.Errorf("sanitizeID(%q) = %q len=%d, want ≤30", c.in, got, len(got))
			}
			continue
		}
		if got != c.want {
			t.Errorf("sanitizeID(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestVerifyGitHubPAT_noPush(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"full_name":   "octocat/my-repo",
			"permissions": map[string]bool{"push": false, "pull": true},
		})
	}))
	defer srv.Close()
	replaceHTTPClient(t, &http.Client{Transport: &rewriteGHTransport{base: srv.URL}})

	err := VerifyGitHubPAT("ghp_fake", "octocat", "my-repo")
	if err == nil {
		t.Fatal("expected error for no-push, got nil")
	}
}

func TestVerifyGitHubPAT_withPush(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"full_name":   "octocat/my-repo",
			"permissions": map[string]bool{"push": true, "pull": true},
		})
	}))
	defer srv.Close()
	replaceHTTPClient(t, &http.Client{Transport: &rewriteGHTransport{base: srv.URL}})

	if err := VerifyGitHubPAT("ghp_fake", "octocat", "my-repo"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestDeployWorker_noBindingsInMetadata verifies that the multipart metadata
// sent to the CF upload endpoint does NOT contain a "bindings" key.
// Cloudflare rejects secret_text bindings that lack a "text" value (error 10021);
// secrets are created separately via UpsertSecret.
func TestDeployWorker_noBindingsInMetadata(t *testing.T) {
	var capturedMeta map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		// Parse the multipart body and capture the metadata part.
		ct := r.Header.Get("Content-Type")
		mediaType, params, err := mime.ParseMediaType(ct)
		if err != nil || !strings.HasPrefix(mediaType, "multipart/") {
			http.Error(w, "expected multipart", http.StatusBadRequest)
			return
		}
		mr := multipart.NewReader(r.Body, params["boundary"])
		for {
			part, err := mr.NextPart()
			if err != nil {
				break
			}
			if part.FormName() == "metadata" {
				if err := json.NewDecoder(part).Decode(&capturedMeta); err != nil {
					http.Error(w, "bad metadata json", http.StatusBadRequest)
					return
				}
			}
		}

		json.NewEncoder(w).Encode(cfResponse{
			Success: true,
			Result:  json.RawMessage(`{"etag":"v1"}`),
		})
	}))
	defer srv.Close()

	replaceHTTPClient(t, &http.Client{Transport: &rewriteTransport{base: srv.URL}})

	_, err := DeployWorker("acct123", "tok", "ws1", []byte("export default {}"))
	if err != nil {
		t.Fatalf("DeployWorker: %v", err)
	}
	if capturedMeta == nil {
		t.Fatal("metadata part not captured")
	}
	if _, ok := capturedMeta["bindings"]; ok {
		t.Error("metadata must NOT contain a \"bindings\" key (causes CF API error 10021)")
	}
}

// ---------------------------------------------------------------------------
// Test helpers: transports that rewrite the scheme+host to a test server
// ---------------------------------------------------------------------------

type rewriteTransport struct {
	base    string // "http://127.0.0.1:PORT"
	wrapped http.RoundTripper
}

func (rt *rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	clone.URL.Scheme = "http"
	// Strip the authority and replace with test server.
	clone.URL.Host = rt.base[len("http://"):]
	tr := rt.wrapped
	if tr == nil {
		tr = http.DefaultTransport
	}
	return tr.RoundTrip(clone)
}

type rewriteGHTransport struct {
	base string
}

func (rt *rewriteGHTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	clone.URL.Scheme = "http"
	clone.URL.Host = rt.base[len("http://"):]
	return http.DefaultTransport.RoundTrip(clone)
}
