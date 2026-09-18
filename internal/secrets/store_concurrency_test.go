package secrets

import (
	"errors"
	"sync"
	"testing"

	"github.com/99designs/keyring"
)

// errDuplicate is the message the keyring library produces when the macOS
// keychain rejects an update because another process wrote the same item.
var errDuplicate = errors.New("failed to update item in keychain: " +
	"The specified item already exists in the keychain. (-25299)")

// racingRing stands in for the keychain. Its first `failures` Set calls report
// errSecDuplicateItem, as the real keychain does to the loser of a concurrent
// write; later calls succeed.
type racingRing struct {
	keyring.Keyring
	mu       sync.Mutex
	failures int
	calls    int
	stored   map[string]string
}

func (r *racingRing) Set(item keyring.Item) error { //nolint:gocritic // signature fixed by keyring.Keyring
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls++
	if r.failures > 0 {
		r.failures--
		return errDuplicate
	}
	r.stored[item.Key] = string(item.Data)
	return nil
}

func TestSetRetriesConcurrentKeychainDuplicate(t *testing.T) {
	duplicateRetryDelay = 0
	const writers = 6
	ring := &racingRing{failures: 1, stored: map[string]string{}}
	store := &KeyringStore{ring: ring}
	key := TokenKey("someone@example.com")

	var wg sync.WaitGroup
	errs := make(chan error, writers)
	for range writers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- store.Set(key, "refresh-token")
		}()
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		if err != nil {
			t.Errorf("Set: %v; a concurrent writer's duplicate error should be retried", err)
		}
	}
	if ring.stored[key] != "refresh-token" {
		t.Errorf("stored = %q, want the token", ring.stored[key])
	}
}

func TestSetGivesUpAfterOneRetry(t *testing.T) {
	duplicateRetryDelay = 0
	ring := &racingRing{failures: 2, stored: map[string]string{}}
	store := &KeyringStore{ring: ring}

	if err := store.Set("k", "v"); err == nil {
		t.Fatal("Set succeeded, want the second duplicate error returned")
	}
	if ring.calls != 2 {
		t.Errorf("Set attempts = %d, want 2", ring.calls)
	}
}

func TestSetDoesNotRetryOtherErrors(t *testing.T) {
	duplicateRetryDelay = 0
	ring := &failingRing{err: errors.New("user interaction is not allowed (-25308)")}
	store := &KeyringStore{ring: ring}

	if err := store.Set("k", "v"); err == nil {
		t.Fatal("Set succeeded, want the error returned")
	}
	if ring.calls != 1 {
		t.Errorf("Set attempts = %d, want 1: only a duplicate is worth retrying", ring.calls)
	}
}

type failingRing struct {
	keyring.Keyring
	err   error
	calls int
}

func (r *failingRing) Set(keyring.Item) error {
	r.calls++
	return r.err
}
