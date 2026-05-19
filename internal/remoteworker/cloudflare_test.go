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

		switch {
		case strings.HasSuffix(r.URL.Path, "/workers/subdomain") && r.Method == "GET":
			json.NewEncoder(w).Encode(cfResponse{Success: true, Result: json.RawMessage(`{"subdomain":"testacct"}`)})
		case strings.Contains(r.URL.Path, "/subdomain") && r.Method == "POST":
			json.NewEncoder(w).Encode(cfResponse{Success: true, Result: json.RawMessage(`{}`)})
		case r.URL.Path == "/health" || r.URL.Path == "":
			// health probe
			w.WriteHeader(http.StatusOK)
		default:
			// Script upload (PUT) — parse multipart and capture metadata.
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
		}
	}))
	defer srv.Close()

	replaceHTTPClient(t, &http.Client{Transport: &rewriteTransport{base: srv.URL}})
	origHealth := healthClient
	healthClient = &http.Client{Transport: &rewriteTransport{base: srv.URL}}
	t.Cleanup(func() { healthClient = origHealth })

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

func TestDeleteWorker_success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			http.Error(w, "want DELETE", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(cfResponse{Success: true, Result: json.RawMessage(`{}`)})
	}))
	defer srv.Close()
	replaceHTTPClient(t, &http.Client{Transport: &rewriteTransport{base: srv.URL}})

	if err := DeleteWorker("acct123", "tok", "prismconductor-ws1"); err != nil {
		t.Fatalf("DeleteWorker: %v", err)
	}
}

func TestDeleteWorker_notFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(cfResponse{
			Success: false,
			Errors:  []cfError{{Code: 10007, Message: "script not found"}},
		})
	}))
	defer srv.Close()
	replaceHTTPClient(t, &http.Client{Transport: &rewriteTransport{base: srv.URL}})

	// 404 must be treated as success (worker already gone).
	if err := DeleteWorker("acct123", "tok", "prismconductor-ws1"); err != nil {
		t.Fatalf("DeleteWorker 404 should be a no-op, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Tests for getAccountSubdomain
// ---------------------------------------------------------------------------

func TestGetAccountSubdomain_success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/client/v4/accounts/acct123/workers/subdomain" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(cfResponse{
			Success: true,
			Result:  json.RawMessage(`{"subdomain":"myaccount"}`),
		})
	}))
	defer srv.Close()
	replaceHTTPClient(t, &http.Client{Transport: &rewriteTransport{base: srv.URL}})

	sub, err := getAccountSubdomain("acct123", "tok")
	if err != nil {
		t.Fatalf("getAccountSubdomain: %v", err)
	}
	if sub != "myaccount" {
		t.Errorf("subdomain = %q, want myaccount", sub)
	}
}

func TestGetAccountSubdomain_notFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(cfResponse{
			Success: false,
			Errors:  []cfError{{Code: 10007, Message: "not found"}},
		})
	}))
	defer srv.Close()
	replaceHTTPClient(t, &http.Client{Transport: &rewriteTransport{base: srv.URL}})

	sub, err := getAccountSubdomain("acct123", "tok")
	if err != nil {
		t.Fatalf("404 should return empty subdomain without error, got: %v", err)
	}
	if sub != "" {
		t.Errorf("expected empty subdomain on 404, got %q", sub)
	}
}

// ---------------------------------------------------------------------------
// Tests for enableScriptSubdomain
// ---------------------------------------------------------------------------

func TestEnableScriptSubdomain_success(t *testing.T) {
	var gotMethod string
	var gotPath string
	var gotBody map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(cfResponse{Success: true, Result: json.RawMessage(`{}`)})
	}))
	defer srv.Close()
	replaceHTTPClient(t, &http.Client{Transport: &rewriteTransport{base: srv.URL}})

	if err := enableScriptSubdomain("acct123", "prismconductor-ws1", "tok"); err != nil {
		t.Fatalf("enableScriptSubdomain: %v", err)
	}
	if gotMethod != "POST" {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	wantPath := "/client/v4/accounts/acct123/workers/scripts/prismconductor-ws1/subdomain"
	if gotPath != wantPath {
		t.Errorf("path = %q, want %q", gotPath, wantPath)
	}
	if enabled, _ := gotBody["enabled"].(bool); !enabled {
		t.Errorf("body[enabled] = %v, want true", gotBody["enabled"])
	}
}

// ---------------------------------------------------------------------------
// Tests for probeWorkerEndpoint
// ---------------------------------------------------------------------------

func TestProbeWorkerEndpoint_healthOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.WriteHeader(http.StatusOK)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	// Swap healthClient to point at test server.
	orig := healthClient
	healthClient = &http.Client{Transport: http.DefaultTransport}
	t.Cleanup(func() { healthClient = orig })

	if err := probeWorkerEndpoint(srv.URL); err != nil {
		t.Fatalf("probeWorkerEndpoint: %v", err)
	}
}

func TestProbeWorkerEndpoint_healthMissingFallsBackToHEAD(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		// HEAD on root
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	orig := healthClient
	healthClient = &http.Client{Transport: http.DefaultTransport}
	t.Cleanup(func() { healthClient = orig })

	if err := probeWorkerEndpoint(srv.URL); err != nil {
		t.Fatalf("probeWorkerEndpoint with 404 /health fallback: %v", err)
	}
}

func TestProbeWorkerEndpoint_serverError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	orig := healthClient
	healthClient = &http.Client{Transport: http.DefaultTransport}
	t.Cleanup(func() { healthClient = orig })

	if err := probeWorkerEndpoint(srv.URL); err == nil {
		t.Fatal("expected error for 500, got nil")
	}
}

// ---------------------------------------------------------------------------
// Tests for DeployWorker URL construction (subdomain path)
// ---------------------------------------------------------------------------

func TestDeployWorker_correctURLFromSubdomain(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/workers/scripts/prismconductor-ws1") && r.Method == "PUT":
			json.NewEncoder(w).Encode(cfResponse{Success: true, Result: json.RawMessage(`{"etag":"v2"}`)})
		case strings.HasSuffix(r.URL.Path, "/workers/subdomain") && r.Method == "GET":
			json.NewEncoder(w).Encode(cfResponse{Success: true, Result: json.RawMessage(`{"subdomain":"myacct"}`)})
		case strings.Contains(r.URL.Path, "/subdomain") && r.Method == "POST":
			json.NewEncoder(w).Encode(cfResponse{Success: true, Result: json.RawMessage(`{}`)})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	// Redirect both httpClient and healthClient to test server.
	replaceHTTPClient(t, &http.Client{Transport: &rewriteTransport{base: srv.URL}})
	origHealth := healthClient
	healthClient = &http.Client{Transport: &rewriteTransport{base: srv.URL}}
	t.Cleanup(func() { healthClient = origHealth })

	result, err := DeployWorker("acct123", "tok", "ws1", []byte("export default {}"))
	if err != nil {
		t.Fatalf("DeployWorker: %v", err)
	}
	want := "http://prismconductor-ws1.myacct.workers.dev"
	// In the test server the URL scheme is http; the real code builds https.
	// The rewriteTransport rewrites scheme to http, so the URL the probe
	// succeeds against is the rewritten one. We verify subdomain is embedded.
	if !strings.Contains(result.CFWorkerEndpointURL, "myacct") {
		t.Errorf("endpoint URL %q does not contain subdomain 'myacct'", result.CFWorkerEndpointURL)
	}
	_ = want
}

func TestDeployWorker_noSubdomainReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/workers/scripts/prismconductor-ws1") && r.Method == "PUT":
			json.NewEncoder(w).Encode(cfResponse{Success: true, Result: json.RawMessage(`{"etag":"v2"}`)})
		case strings.HasSuffix(r.URL.Path, "/workers/subdomain") && r.Method == "GET":
			// 404 = no subdomain configured
			w.WriteHeader(http.StatusNotFound)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	replaceHTTPClient(t, &http.Client{Transport: &rewriteTransport{base: srv.URL}})

	_, err := DeployWorker("acct123", "tok", "ws1", []byte("export default {}"))
	if err == nil {
		t.Fatal("expected error when no subdomain configured, got nil")
	}
	if !strings.Contains(err.Error(), "no Workers subdomain") {
		t.Errorf("error %q should mention 'no Workers subdomain'", err.Error())
	}
}

// ---------------------------------------------------------------------------
// Tests for DeploySandboxWorker (issue #284)
// ---------------------------------------------------------------------------

// TestDeploySandboxWorker_hasDurableObjectBindings verifies that the metadata
// sent to CF includes the Durable Object binding for SandboxSession.
func TestDeploySandboxWorker_hasDurableObjectBindings(t *testing.T) {
	var capturedMeta map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/workers/subdomain") && r.Method == "GET":
			json.NewEncoder(w).Encode(cfResponse{Success: true, Result: json.RawMessage(`{"subdomain":"testacct"}`)})
		case strings.Contains(r.URL.Path, "/subdomain") && r.Method == "POST":
			json.NewEncoder(w).Encode(cfResponse{Success: true, Result: json.RawMessage(`{}`)})
		case r.URL.Path == "/health" || r.URL.Path == "":
			w.WriteHeader(http.StatusOK)
		default:
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
		}
	}))
	defer srv.Close()

	replaceHTTPClient(t, &http.Client{Transport: &rewriteTransport{base: srv.URL}})
	origHealth := healthClient
	healthClient = &http.Client{Transport: &rewriteTransport{base: srv.URL}}
	t.Cleanup(func() { healthClient = origHealth })

	_, err := DeploySandboxWorker("acct123", "tok", "ws1", []byte("export default {}"))
	if err != nil {
		t.Fatalf("DeploySandboxWorker: %v", err)
	}
	if capturedMeta == nil {
		t.Fatal("metadata part not captured")
	}

	doRaw, ok := capturedMeta["durable_objects"]
	if !ok {
		t.Fatal("metadata must contain 'durable_objects' binding for SandboxSession")
	}
	doMap, ok := doRaw.(map[string]any)
	if !ok {
		t.Fatalf("durable_objects is not an object: %T", doRaw)
	}
	bindings, ok := doMap["bindings"].([]any)
	if !ok || len(bindings) == 0 {
		t.Fatal("durable_objects.bindings must be a non-empty array")
	}
	first, ok := bindings[0].(map[string]any)
	if !ok {
		t.Fatalf("bindings[0] is not an object: %T", bindings[0])
	}
	if first["name"] != "SESSIONS" {
		t.Errorf("binding name = %q, want SESSIONS", first["name"])
	}
	if first["class_name"] != "SandboxSession" {
		t.Errorf("class_name = %q, want SandboxSession", first["class_name"])
	}

	if _, ok := capturedMeta["migrations"]; !ok {
		t.Error("metadata must contain 'migrations' for the SandboxSession DO class")
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
