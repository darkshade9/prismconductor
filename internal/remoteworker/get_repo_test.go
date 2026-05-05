package remoteworker

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGetRepoDefaultBranch_success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/octocat/hello-world" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"full_name":      "octocat/hello-world",
			"default_branch": "main",
		})
	}))
	defer srv.Close()
	replaceHTTPClient(t, &http.Client{Transport: &rewriteGHTransport{base: srv.URL}})

	branch, err := GetRepoDefaultBranch("ghp_fake", "octocat", "hello-world")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if branch != "main" {
		t.Errorf("branch = %q, want %q", branch, "main")
	}
}

func TestGetRepoDefaultBranch_masterBranch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"default_branch": "master"})
	}))
	defer srv.Close()
	replaceHTTPClient(t, &http.Client{Transport: &rewriteGHTransport{base: srv.URL}})

	branch, err := GetRepoDefaultBranch("ghp_fake", "org", "repo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if branch != "master" {
		t.Errorf("branch = %q, want master", branch)
	}
}

func TestGetRepoDefaultBranch_notFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"message": "Not Found"})
	}))
	defer srv.Close()
	replaceHTTPClient(t, &http.Client{Transport: &rewriteGHTransport{base: srv.URL}})

	_, err := GetRepoDefaultBranch("ghp_fake", "owner", "missing")
	if err == nil {
		t.Fatal("expected error for 404, got nil")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error %q should mention 'not found'", err.Error())
	}
}

func TestGetRepoDefaultBranch_unauthorized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]string{"message": "Bad credentials"})
	}))
	defer srv.Close()
	replaceHTTPClient(t, &http.Client{Transport: &rewriteGHTransport{base: srv.URL}})

	_, err := GetRepoDefaultBranch("bad_pat", "owner", "repo")
	if err == nil {
		t.Fatal("expected error for 401, got nil")
	}
	if !strings.Contains(err.Error(), "PAT") {
		t.Errorf("error %q should mention 'PAT'", err.Error())
	}
}

func TestGetRepoDefaultBranch_forbidden(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		json.NewEncoder(w).Encode(map[string]string{"message": "Forbidden"})
	}))
	defer srv.Close()
	replaceHTTPClient(t, &http.Client{Transport: &rewriteGHTransport{base: srv.URL}})

	_, err := GetRepoDefaultBranch("ghp_fake", "owner", "private")
	if err == nil {
		t.Fatal("expected error for 403, got nil")
	}
}
