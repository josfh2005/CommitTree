// Package keys stores one API key per AI provider in the operating system's
// own secret store: Keychain on macOS, Secret Service on Linux, Credential
// Manager on Windows. Keys never touch the settings file, and there is no
// plain-text fallback: on a system without a usable store, saving fails.
package keys

import (
	"errors"
	"fmt"
	"strings"

	"github.com/zalando/go-keyring"
)

// Service is the entry name every key is stored under; the provider name is
// the account.
const Service = "git-ui"

var (
	// ErrNotFound reports a provider with no stored key. It wraps the
	// keyring package's own sentinel so a fake store can return it too.
	ErrNotFound = keyring.ErrNotFound
	// ErrUnavailable reports a system with no usable secret store.
	ErrUnavailable = errors.New("keys: no usable secret store on this system")
)

// Store is the subset of the keyring package this package uses, so tests can
// replace it.
type Store interface {
	Get(service, user string) (string, error)
	Set(service, user, password string) error
	Delete(service, user string) error
}

type osStore struct{}

func (osStore) Get(service, user string) (string, error) { return keyring.Get(service, user) }
func (osStore) Set(service, user, password string) error {
	return keyring.Set(service, user, password)
}
func (osStore) Delete(service, user string) error { return keyring.Delete(service, user) }

var store Store = osStore{}

// UseStore swaps the backing store and returns a function restoring the
// previous one. It exists for tests; production code never calls it.
func UseStore(s Store) (restore func()) {
	previous := store
	store = s
	return func() { store = previous }
}

// known guards what can be used as an account name, so a caller can't reach
// another application's entries.
func known(provider string) error {
	switch provider {
	case "openai", "anthropic":
		return nil
	}
	return fmt.Errorf("keys: unknown provider %q", provider)
}

// Get returns the stored key, or "" when the provider has none.
func Get(provider string) (string, error) {
	if err := known(provider); err != nil {
		return "", err
	}
	key, err := store.Get(Service, provider)
	if errors.Is(err, ErrNotFound) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	return key, nil
}

// Set stores key under provider, replacing any existing value.
func Set(provider, key string) error {
	if err := known(provider); err != nil {
		return err
	}
	if strings.TrimSpace(key) == "" {
		return errors.New("keys: the API key is empty")
	}
	if err := store.Set(Service, provider, strings.TrimSpace(key)); err != nil {
		return fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	return nil
}

// Delete removes the provider's key. Removing one that isn't there succeeds.
func Delete(provider string) error {
	if err := known(provider); err != nil {
		return err
	}
	err := store.Delete(Service, provider)
	if err == nil || errors.Is(err, ErrNotFound) {
		return nil
	}
	return fmt.Errorf("%w: %v", ErrUnavailable, err)
}

// Available reports whether the store can be used at all, so Settings can say
// so before the user types a key.
func Available() error {
	if _, err := store.Get(Service, "openai"); err != nil && !errors.Is(err, ErrNotFound) {
		return fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	return nil
}

// Mask is the hint shown in the UI once a key is stored; the key itself never
// goes back to the frontend.
func Mask(key string) string {
	if key == "" {
		return ""
	}
	if len(key) < 12 {
		return "…"
	}
	return key[:3] + "…" + key[len(key)-4:]
}
