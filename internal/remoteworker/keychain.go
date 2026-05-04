package remoteworker

import (
	"fmt"
	"os"
	"path/filepath"
)

const keychainServiceName = "PrismConductor"

// keyFilePath returns the path to the per-workspace key file in the user
// config directory, e.g. ~/Library/Application Support/PrismConductor/secrets/<wsID>.key
func keyFilePath(workspaceID string) (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("user config dir: %w", err)
	}
	return filepath.Join(dir, keychainServiceName, "secrets", workspaceID+".key"), nil
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
