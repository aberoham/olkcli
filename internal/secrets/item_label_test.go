package secrets

import (
	"testing"

	"github.com/99designs/keyring"
)

func TestItemLabelNamesTheAccountForTokens(t *testing.T) {
	tests := []struct{ key, want string }{
		{TokenKey("Someone@Example.com"), "olk token for someone@example.com"},
		{"olk:other", "olk olk:other"},
	}
	for _, tc := range tests {
		if got := ItemLabel(tc.key); got != tc.want {
			t.Fatalf("ItemLabel(%q) = %q, want %q", tc.key, got, tc.want)
		}
	}
}

func newFileStore(t *testing.T) *KeyringStore {
	t.Helper()
	ring, err := keyring.Open(keyring.Config{
		ServiceName:      serviceName,
		AllowedBackends:  []keyring.BackendType{keyring.FileBackend},
		FileDir:          t.TempDir(),
		FilePasswordFunc: keyring.FixedStringPrompt("test"),
	})
	if err != nil {
		t.Fatalf("open file keyring: %v", err)
	}
	return &KeyringStore{ring: ring}
}

func TestSetStoresTheLabelAndUpdatesInPlace(t *testing.T) {
	store := newFileStore(t)
	key := TokenKey("someone@example.com")
	if err := store.ring.Set(keyring.Item{Key: key, Data: []byte("old"), Label: ""}); err != nil {
		t.Fatalf("seed unlabelled item: %v", err)
	}

	if err := store.Set(key, "new"); err != nil {
		t.Fatalf("Set: %v", err)
	}

	item, err := store.ring.Get(key)
	if err != nil {
		t.Fatalf("Get after Set: %v", err)
	}
	if item.Label != ItemLabel(key) || string(item.Data) != "new" {
		t.Fatalf("item = label %q data %q, want the current label and new data", item.Label, item.Data)
	}
	if got, err := store.Get(key); err != nil || got != "new" {
		t.Fatalf("store.Get = %q, %v", got, err)
	}
}
