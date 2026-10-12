package msauth

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rlrghb/olkcli/internal/secrets"
)

// lockedStore is a secrets.Store safe for the concurrent use these tests make.
type lockedStore struct {
	mu sync.Mutex
	m  map[string]string
}

func newLockedStore() *lockedStore { return &lockedStore{m: map[string]string{}} }

func (s *lockedStore) Set(k, v string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[k] = v
	return nil
}
func (s *lockedStore) Delete(k string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.m, k)
	return nil
}
func (s *lockedStore) Get(k string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.m[k]
	if !ok {
		return "", fmt.Errorf("key %q not found", k)
	}
	return v, nil
}
func (s *lockedStore) Keys() ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	keys := make([]string, 0, len(s.m))
	for k := range s.m {
		keys = append(keys, k)
	}
	return keys, nil
}

func TestAccountLockExcludesAndTimesOut(t *testing.T) {
	t.Setenv("OLK_CONFIG_DIR", t.TempDir())
	prev := lockWait
	lockWait = 200 * time.Millisecond
	t.Cleanup(func() { lockWait = prev })

	unlock, err := lockAccount(context.Background(), "a@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lockAccount(context.Background(), "A@example.com"); err == nil || !strings.Contains(err.Error(), "another olk process") {
		t.Errorf("second lock on the same account = %v, want a wait that times out", err)
	}
	other, err := lockAccount(context.Background(), "b@example.com")
	if err != nil {
		t.Errorf("another account's lock: %v", err)
	} else {
		other()
	}
	unlock()
	again, err := lockAccount(context.Background(), "a@example.com")
	if err != nil {
		t.Fatalf("lock after unlock: %v", err)
	}
	again()
}

// A refresh in flight when the account signs out must not store its token
// after the sign-out deleted it.
func TestLogoutDuringRefreshStaysLoggedOut(t *testing.T) {
	t.Setenv("OLK_CONFIG_DIR", t.TempDir())
	const email = "user@example.com"
	refreshing := make(chan struct{})
	finish := make(chan struct{})
	var release sync.Once
	finished := func() { release.Do(func() { close(finish) }) }
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(refreshing)
		<-finish
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"access_token":"at2","refresh_token":"rt2","expires_in":3600,"token_type":"Bearer"}`)
	}))
	defer srv.Close()
	defer finished() // a failed run must not leave the handler blocked
	overrideAuthority(t, srv.URL)

	store := newLockedStore()
	if err := StoreToken(store, email, &TokenData{RefreshToken: "rt1", Email: email}); err != nil {
		t.Fatal(err)
	}
	a := NewAuthenticator(store, testClientID, testTenantID)

	refreshed := make(chan error, 1)
	go func() {
		_, err := a.GetCredential(context.Background(), email)
		refreshed <- err
	}()
	<-refreshing

	loggedOut := make(chan error, 1)
	go func() { loggedOut <- a.Logout(email) }()
	select {
	case err := <-loggedOut:
		t.Fatalf("logout finished during the refresh: %v", err)
	case <-time.After(150 * time.Millisecond):
	}
	finished()
	if err := <-refreshed; err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if err := <-loggedOut; err != nil {
		t.Fatalf("logout: %v", err)
	}
	if _, err := store.Get(secrets.TokenKey(email)); err == nil {
		t.Error("the refresh restored a token after sign-out")
	}
	if _, err := a.GetCredential(context.Background(), email); err == nil {
		t.Error("credentials still load after sign-out")
	}
}
