package secrets

import (
	"errors"
	"testing"

	"github.com/99designs/keyring"
)

// errDuplicate is the message the keyring library produces when the macOS
// keychain rejects an update with errSecDuplicateItem.
var errDuplicate = errors.New("failed to update item in keychain: " +
	"The specified item already exists in the keychain. (-25299)")

var errInteraction = errors.New("user interaction is not allowed (-25308)")

// scriptedRing stands in for the keychain. Each Set call returns the next
// scripted error; a nil entry stores the item.
type scriptedRing struct {
	keyring.Keyring
	errs   []error
	calls  int
	stored map[string]string
}

func (r *scriptedRing) Set(item keyring.Item) error { //nolint:gocritic // signature fixed by keyring.Keyring
	err := r.errs[r.calls]
	r.calls++
	if err == nil {
		r.stored[item.Key] = string(item.Data)
	}
	return err
}

func TestSetRetriesADuplicateItemOnce(t *testing.T) {
	duplicateRetryDelay = 0
	tests := []struct {
		name      string
		errs      []error
		wantErr   error
		wantCalls int
	}{
		{"duplicate then success", []error{errDuplicate, nil}, nil, 2},
		{"duplicate twice", []error{errDuplicate, errDuplicate}, errDuplicate, 2},
		{"duplicate then a different error", []error{errDuplicate, errInteraction}, errInteraction, 2},
		{"other error is not retried", []error{errInteraction}, errInteraction, 1},
		{"success first time", []error{nil}, nil, 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ring := &scriptedRing{errs: tc.errs, stored: map[string]string{}}
			store := &KeyringStore{ring: ring}

			err := store.Set("k", "v")

			if !errors.Is(err, tc.wantErr) || (tc.wantErr == nil) != (err == nil) {
				t.Fatalf("Set error = %v, want %v", err, tc.wantErr)
			}
			if ring.calls != tc.wantCalls {
				t.Errorf("Set attempts = %d, want %d", ring.calls, tc.wantCalls)
			}
			if tc.wantErr == nil && ring.stored["k"] != "v" {
				t.Errorf("stored = %q, want the value", ring.stored["k"])
			}
		})
	}
}
