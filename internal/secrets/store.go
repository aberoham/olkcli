package secrets

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/99designs/keyring"
	"golang.org/x/term"

	"github.com/rlrghb/olkcli/internal/config"
)

const (
	serviceName = "olk"
	tokenPrefix = "olk:token:"
)

// Store defines the interface for credential storage.
type Store interface {
	Set(key, value string) error
	Get(key string) (string, error)
	Delete(key string) error
	Keys() ([]string, error)
}

// KeyringStore implements Store using the keyring library for
// cross-platform credential storage (macOS Keychain, Linux Secret Service,
// Windows WinCred).
type KeyringStore struct {
	ring keyring.Keyring
}

// stderrPrompt prompts for a password on stderr and reads it from the
// controlling terminal. Unlike keyring.TerminalPrompt it never writes to
// stdout, so JSON/pipe output is not corrupted.
func stderrPrompt(prompt string) (string, error) {
	if runtime.GOOS == "windows" {
		fmt.Fprintf(os.Stderr, "%s: ", prompt)
		b, err := term.ReadPassword(int(os.Stdin.Fd()))
		if err != nil {
			return "", fmt.Errorf("reading password: %w", err)
		}
		fmt.Fprintln(os.Stderr)
		return string(b), nil
	}

	tty, err := os.Open("/dev/tty")
	if err != nil {
		return "", fmt.Errorf(
			"cannot open terminal for password prompt: %w\n"+
				"Set OLK_KEYRING_PASSWORD to provide the keyring password non-interactively", err)
	}
	defer tty.Close()

	fmt.Fprintf(os.Stderr, "%s: ", prompt)
	b, err := term.ReadPassword(int(tty.Fd()))
	if err != nil {
		return "", fmt.Errorf("reading password: %w", err)
	}
	fmt.Fprintln(os.Stderr)
	return string(b), nil
}

// NewKeyringStore creates a new KeyringStore backed by the OS credential manager.
func NewKeyringStore() (*KeyringStore, error) {
	// Pre-create keyring fallback directory with restrictive permissions
	// to prevent other users from reading tokens on multi-user systems.
	keyringDir := filepath.Join(config.ConfigDir(), "keyring")
	if err := os.MkdirAll(keyringDir, 0o700); err != nil {
		return nil, fmt.Errorf("creating keyring directory: %w", err)
	}
	// Ensure restrictive permissions even if the directory already existed.
	if runtime.GOOS != "windows" {
		if err := os.Chmod(keyringDir, 0o700); err != nil {
			return nil, fmt.Errorf("setting keyring directory permissions: %w", err)
		}
	}

	// Use OLK_KEYRING_PASSWORD for non-interactive/headless environments;
	// otherwise prompt on stderr to avoid corrupting piped output.
	var passwordFunc keyring.PromptFunc
	if pw := os.Getenv("OLK_KEYRING_PASSWORD"); pw != "" {
		passwordFunc = keyring.FixedStringPrompt(pw)
	} else {
		passwordFunc = stderrPrompt
	}

	cfg := keyring.Config{
		ServiceName: serviceName,

		// macOS
		KeychainTrustApplication:       true,
		KeychainSynchronizable:         false,
		KeychainAccessibleWhenUnlocked: true,

		// Linux / FreeBSD
		LibSecretCollectionName: serviceName,

		// Windows
		WinCredPrefix: serviceName,

		// Fall back to an encrypted file store when no native backend is
		// available (e.g. headless Linux without Secret Service).
		FileDir:          keyringDir,
		FilePasswordFunc: passwordFunc,
	}

	// On macOS, prefer Keychain (native UI with "Always Allow") but fall
	// back to file backend when cgo is unavailable (e.g. Homebrew binary).
	if runtime.GOOS == "darwin" {
		cfg.AllowedBackends = []keyring.BackendType{keyring.KeychainBackend, keyring.FileBackend}
	}

	ring, err := keyring.Open(cfg)
	if err != nil {
		return nil, fmt.Errorf("opening keyring: %w", err)
	}
	return &KeyringStore{ring: ring}, nil
}

// Set stores a value under the given key. The item carries a label and a
// description because macOS shows the label in its access prompt: without
// one the dialog reads `olk wants to access key "" in your keychain`, which
// gives the person no way to tell which account or purpose is being asked
// about.
//
// On macOS the name in that prompt is the item's access-control entry,
// which the keychain creates from the label when the item is first added
// and never changes on update, so the name only reaches items created after
// this label existed. Recreating an older item is not an option: the
// keychain lets only the application that created an item delete it, and a
// rebuilt or re-signed binary is a different application (error -25244).
// Signing out and back in recreates the item with the name.
//
// Several olk processes can refresh the same account's token at once, and the
// macOS keychain does not serialize them: the keyring library adds the item,
// falls back to updating it when the add reports a duplicate, and the update
// itself can then fail with errSecDuplicateItem (-25299) while another process
// is writing the same item. That error means the item exists and was just
// written, so the write is retried once after a short pause rather than
// failing the command.
func (s *KeyringStore) Set(key, value string) error {
	item := keyring.Item{
		Key:         key,
		Data:        []byte(value),
		Label:       ItemLabel(key),
		Description: "olk Microsoft 365 credential",
	}
	err := s.ring.Set(item)
	if err != nil && isKeychainDuplicate(err) {
		time.Sleep(duplicateRetryDelay)
		err = s.ring.Set(item)
	}
	return err
}

// duplicateRetryDelay gives a concurrent writer time to finish before the
// retry. It is a variable so tests can remove the wait.
var duplicateRetryDelay = 200 * time.Millisecond

// isKeychainDuplicate reports whether err is the keychain's errSecDuplicateItem.
// The keyring library formats the underlying error with %v, so the typed value
// is lost and only its status code in the message identifies it.
func isKeychainDuplicate(err error) bool {
	return strings.Contains(err.Error(), "(-25299)")
}

// ItemLabel is the human-readable name a stored key gets in the OS
// credential store, e.g. "olk token for someone@example.com".
func ItemLabel(key string) string {
	if IsTokenKey(key) {
		return "olk token for " + strings.TrimPrefix(key, tokenPrefix)
	}
	return "olk " + key
}

// Get retrieves the value stored under the given key.
func (s *KeyringStore) Get(key string) (string, error) {
	item, err := s.ring.Get(key)
	if err != nil {
		return "", fmt.Errorf("retrieving stored credential: %w", err)
	}
	return string(item.Data), nil
}

// Delete removes the entry for the given key.
func (s *KeyringStore) Delete(key string) error {
	return s.ring.Remove(key)
}

// Keys returns all keys currently stored in the keyring.
func (s *KeyringStore) Keys() ([]string, error) {
	keys, err := s.ring.Keys()
	if err != nil {
		return nil, fmt.Errorf("listing keys: %w", err)
	}
	return keys, nil
}

// TokenKey returns the canonical keyring key for a given email address.
// Format: olk:token:<email>
func TokenKey(email string) string {
	return tokenPrefix + strings.ToLower(email)
}

// IsTokenKey reports whether a keyring key is an olk token entry.
func IsTokenKey(key string) bool {
	return strings.HasPrefix(key, tokenPrefix)
}
