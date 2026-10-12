package msauth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/rlrghb/olkcli/internal/config"
)

// lockWait bounds how long one olk process waits for another to finish with
// an account's credentials. A refresh holds the lock for one token request,
// whose client times out after 30 seconds.
var lockWait = 60 * time.Second

// lockDir is where account locks live: a per-user directory that does not
// follow OLK_CONFIG_DIR, because the OS credential store they guard does not
// either. Two processes with different config directories still share one
// keychain entry per account. A variable so tests can isolate it.
var lockDir = func() string {
	if dir, err := os.UserCacheDir(); err == nil {
		return filepath.Join(dir, config.Namespace, "locks")
	}
	return filepath.Join(config.ConfigDir(), "locks")
}

// errLocked reports that the lock is held elsewhere; tryLockFile returns it.
var errLocked = errors.New("account lock held")

// lockAccount takes the lock on email's stored credentials that every olk
// process shares. Loading, refreshing and storing a token, signing in and
// signing out all run under it, so two processes never write one account's
// token at once, a reader never sees a half-written one, and a refresh that
// began before a sign-out cannot store its token afterwards.
func lockAccount(ctx context.Context, email string) (unlock func(), err error) {
	dir := lockDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("creating lock directory: %w", err)
	}
	sum := sha256.Sum256([]byte(strings.ToLower(email)))
	path := filepath.Join(dir, hex.EncodeToString(sum[:16])+".lock")
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, fmt.Errorf("opening account lock: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, lockWait)
	defer cancel()
	for {
		err := tryLockFile(f)
		if err == nil {
			return func() {
				_ = unlockFile(f)
				_ = f.Close()
			}, nil
		}
		if !errors.Is(err, errLocked) {
			_ = f.Close()
			return nil, fmt.Errorf("locking account %s: %w", email, err)
		}
		select {
		case <-ctx.Done():
			_ = f.Close()
			return nil, fmt.Errorf("another olk process is using account %s: %w", email, ctx.Err())
		case <-time.After(25 * time.Millisecond):
		}
	}
}
