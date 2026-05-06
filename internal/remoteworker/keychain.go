package remoteworker

import (
	"fmt"
	"os"
	"path/filepath"
)

const keychainServiceName = "PrismConductor"

// testKeychainDir, when non-empty, redirects all key-file operations to the
// given directory instead of os.UserConfigDir(). Set via OverrideKeychainDir.
var testKeychainDir string

// OverrideKeychainDir redirects key-file I/O to dir for the duration of a
// test. Call it only from test code; the returned function restores the
// previous value and must be deferred or passed to t.Cleanup.
func OverrideKeychainDir(dir string) func() {
	prev := testKeychainDir
	testKeychainDir = dir
	return func() { testKeychainDir = prev }
}

// keyFilePath returns the path to the per-workspace key file in the user
// config directory, e.g. ~/Library/Application Support/PrismConductor/secrets/<wsID>.key
func keyFilePath(workspaceID string) (string, error) {
	base := testKeychainDir
	if base == "" {
		dir, err := os.UserConfigDir()
		if err != nil {
			return "", fmt.Errorf("user config dir: %w", err)
		}
		base = filepath.Join(dir, keychainServiceName)
	}
	return filepath.Join(base, "secrets", workspaceID+".key"), nil
}

// GetKey retrieves the conductor API key for workspaceID from the local key
// store. Returns ("", nil) when no key has been stored yet.
func GetKey(workspaceID string) (string, error) {
	path, err := keyFilePath(workspaceID)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("read key file: %w", err)
	}
	return string(data), nil
}

// SetKey persists the conductor API key for workspaceID with mode 0600.
// Returns a non-empty human-readable warning message that callers should
// surface to the user (the key is stored in a plain file, not the OS keychain).
func SetKey(workspaceID, key string) (string, error) {
	path, err := keyFilePath(workspaceID)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", fmt.Errorf("create key dir: %w", err)
	}
	if err := os.WriteFile(path, []byte(key), 0o600); err != nil {
		return "", fmt.Errorf("write key file: %w", err)
	}
	return "Worker API key stored in a protected file (" + path + "). Keep this file secure — it grants access to your remote workspace.", nil
}

// DeleteKey removes the stored conductor API key for workspaceID. Safe to
// call when no key is stored (returns nil).
func DeleteKey(workspaceID string) error {
	path, err := keyFilePath(workspaceID)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("delete key file: %w", err)
	}
	return nil
}
