// Package secretstore wraps the OS-native secret store (macOS Keychain,
// Windows Credential Vault, Linux Secret Service) for PrismConductor secrets.
// On Linux systems without a working keyring backend the caller can opt in to a
// file-based fallback stored at a restricted-permission path.
package secretstore

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/zalando/go-keyring"
)

const (
	// service is the keyring service name used for all PrismConductor secrets.
	service = "PrismConductor"

	// KeyPrefix is the namespace prefix for all secret keys.
	KeyPrefix = "prismconductor."

	// FileFallbackPrefix is prepended to values stored in workspace metadata
	// when the file-based fallback was used instead of the OS keyring.
	FileFallbackPrefix = "file://"
)

// ErrKeyringUnavailable is returned by KeychainStore when no OS keyring
// backend is available (common on headless Linux systems).
var ErrKeyringUnavailable = errors.New("keyring backend unavailable")

// ErrNotFound is returned when the requested secret does not exist.
var ErrNotFound = errors.New("secret not found")

// Store is the narrow interface all secretstore backends satisfy.
type Store interface {
	Set(key, value string) error
	Get(key string) (string, error)
	Delete(key string) error
}

// CFTokenKey returns the namespaced keyring key for a workspace's CF API token.
func CFTokenKey(workspaceID string) string {
	return KeyPrefix + workspaceID + ".cf_api_token"
}

// KeychainStore persists secrets in the OS-native keyring.
type KeychainStore struct{}

// NewKeychainStore returns a KeychainStore backed by the OS keyring.
func NewKeychainStore() *KeychainStore { return &KeychainStore{} }

func (k *KeychainStore) Set(key, value string) error {
	if err := keyring.Set(service, key, value); err != nil {
		if isKeyringUnavailable(err) {
			return ErrKeyringUnavailable
		}
		return fmt.Errorf("keyring set %q: %w", key, err)
	}
	return nil
}

func (k *KeychainStore) Get(key string) (string, error) {
	val, err := keyring.Get(service, key)
	if err != nil {
		if errors.Is(err, keyring.ErrNotFound) {
			return "", ErrNotFound
		}
		if isKeyringUnavailable(err) {
			return "", ErrKeyringUnavailable
		}
		return "", fmt.Errorf("keyring get %q: %w", key, err)
	}
	return val, nil
}

func (k *KeychainStore) Delete(key string) error {
	err := keyring.Delete(service, key)
	if err != nil && !errors.Is(err, keyring.ErrNotFound) {
		if isKeyringUnavailable(err) {
			return ErrKeyringUnavailable
		}
		return fmt.Errorf("keyring delete %q: %w", key, err)
	}
	return nil
}

// isKeyringUnavailable detects errors from go-keyring that indicate no backend
// is reachable (e.g., no Secret Service daemon on headless Linux).
func isKeyringUnavailable(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "no keyring") ||
		strings.Contains(msg, "secret service") ||
		strings.Contains(msg, "dbus") ||
		strings.Contains(msg, "SecKeychainItem") ||
		strings.Contains(msg, "org.freedesktop.secrets")
}

// FileStore persists secrets as 0600-mode files under a directory. Used as an
// explicit, user-consented fallback when the OS keyring is unavailable.
type FileStore struct {
	dir string
}

// NewFileStore returns a FileStore rooted at dir.
func NewFileStore(dir string) *FileStore { return &FileStore{dir: dir} }

// DefaultSecretsDir returns the default directory for the file-based fallback:
// ~/.config/PrismConductor/secrets (or %APPDATA%\PrismConductor\secrets on Windows).
func DefaultSecretsDir() string {
	if d, err := os.UserConfigDir(); err == nil {
		return filepath.Join(d, "PrismConductor", "secrets")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "PrismConductor", "secrets")
}

func (f *FileStore) Set(key, value string) error {
	if err := os.MkdirAll(f.dir, 0700); err != nil {
		return fmt.Errorf("create secrets dir: %w", err)
	}
	path := filepath.Join(f.dir, sanitizeKey(key)+".key")
	return os.WriteFile(path, []byte(value), 0600)
}

func (f *FileStore) Get(key string) (string, error) {
	path := filepath.Join(f.dir, sanitizeKey(key)+".key")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", ErrNotFound
		}
		return "", fmt.Errorf("read secret file: %w", err)
	}
	return string(data), nil
}

func (f *FileStore) Delete(key string) error {
	path := filepath.Join(f.dir, sanitizeKey(key)+".key")
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("delete secret file: %w", err)
	}
	return nil
}

// FilePath returns the full path where the file store would write key.
func (f *FileStore) FilePath(key string) string {
	return filepath.Join(f.dir, sanitizeKey(key)+".key")
}

// sanitizeKey converts a key to a safe filename component.
func sanitizeKey(key string) string {
	return strings.NewReplacer("/", "_", "\\", "_", ":", "_", " ", "_").Replace(key)
}

// GetFromRef retrieves a secret using the ref string stored in workspace
// metadata. If the ref starts with FileFallbackPrefix the FileStore is used;
// otherwise the KeychainStore is used.
func GetFromRef(ref string, fileDir string) (string, error) {
	if ref == "" {
		return "", ErrNotFound
	}
	if strings.HasPrefix(ref, FileFallbackPrefix) {
		path := strings.TrimPrefix(ref, FileFallbackPrefix)
		data, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				return "", ErrNotFound
			}
			return "", fmt.Errorf("read file secret: %w", err)
		}
		return string(data), nil
	}
	return NewKeychainStore().Get(ref)
}

// DeleteFromRef removes a secret using the ref string. If the ref starts with
// FileFallbackPrefix the file is removed; otherwise the keyring entry is removed.
func DeleteFromRef(ref string) error {
	if ref == "" {
		return nil
	}
	if strings.HasPrefix(ref, FileFallbackPrefix) {
		path := strings.TrimPrefix(ref, FileFallbackPrefix)
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("delete file secret: %w", err)
		}
		return nil
	}
	return NewKeychainStore().Delete(ref)
}
