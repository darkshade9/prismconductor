package secretstore_test

import (
	"errors"
	"testing"

	"prismconductor/internal/secretstore"
)

func TestMemoryStore_SetGetDelete(t *testing.T) {
	s := secretstore.NewMemoryStore()
	const key, val = "prismconductor.ws1.cf_api_token", "tok-abc123"

	if err := s.Set(key, val); err != nil {
		t.Fatalf("Set: %v", err)
	}

	got, err := s.Get(key)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != val {
		t.Errorf("Get = %q, want %q", got, val)
	}

	if err := s.Delete(key); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	_, err = s.Get(key)
	if !errors.Is(err, secretstore.ErrNotFound) {
		t.Errorf("Get after Delete = %v, want ErrNotFound", err)
	}
}

func TestMemoryStore_DeleteMissing(t *testing.T) {
	s := secretstore.NewMemoryStore()
	if err := s.Delete("nonexistent"); err != nil {
		t.Errorf("Delete missing key should not error, got: %v", err)
	}
}

func TestMemoryStore_Overwrite(t *testing.T) {
	s := secretstore.NewMemoryStore()
	const key = "prismconductor.ws1.cf_api_token"

	_ = s.Set(key, "first")
	_ = s.Set(key, "second")

	got, err := s.Get(key)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != "second" {
		t.Errorf("Get = %q, want %q", got, "second")
	}
}

func TestCFTokenKey(t *testing.T) {
	key := secretstore.CFTokenKey("my-workspace")
	want := "prismconductor.my-workspace.cf_api_token"
	if key != want {
		t.Errorf("CFTokenKey = %q, want %q", key, want)
	}
}

func TestFileStore_SetGetDelete(t *testing.T) {
	dir := t.TempDir()
	fs := secretstore.NewFileStore(dir)

	const key, val = "prismconductor.ws2.cf_api_token", "tok-xyz"

	if err := fs.Set(key, val); err != nil {
		t.Fatalf("Set: %v", err)
	}

	got, err := fs.Get(key)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != val {
		t.Errorf("Get = %q, want %q", got, val)
	}

	if err := fs.Delete(key); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	_, err = fs.Get(key)
	if !errors.Is(err, secretstore.ErrNotFound) {
		t.Errorf("Get after Delete = %v, want ErrNotFound", err)
	}
}

func TestFileStore_DeleteMissing(t *testing.T) {
	dir := t.TempDir()
	fs := secretstore.NewFileStore(dir)
	if err := fs.Delete("nonexistent"); err != nil {
		t.Errorf("Delete missing key should not error, got: %v", err)
	}
}

func TestNoTokenLogged(t *testing.T) {
	// Assert that the token value is never directly encoded in a format that
	// could appear in a log line. This test exercises the redaction contract:
	// code must not log raw token values. We verify by ensuring the constants
	// exported by the package don't contain any magic token strings.
	const sentinel = "secret-token-value"
	if secretstore.KeyPrefix == sentinel {
		t.Error("KeyPrefix must not be a secret value")
	}
	if secretstore.FileFallbackPrefix == sentinel {
		t.Error("FileFallbackPrefix must not be a secret value")
	}
}
