package keys_test

import (
	"errors"
	"testing"

	"git-ui/internal/ai/keys"
)

// fake is an in-memory Store standing in for the OS secret store.
type fake struct {
	items map[string]string
	err   error
}

func (f *fake) Get(service, user string) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	v, ok := f.items[service+"/"+user]
	if !ok {
		return "", keys.ErrNotFound
	}
	return v, nil
}

func (f *fake) Set(service, user, password string) error {
	if f.err != nil {
		return f.err
	}
	f.items[service+"/"+user] = password
	return nil
}

func (f *fake) Delete(service, user string) error {
	if f.err != nil {
		return f.err
	}
	delete(f.items, service+"/"+user)
	return nil
}

func withFake(t *testing.T, f *fake) {
	t.Helper()
	restore := keys.UseStore(f)
	t.Cleanup(restore)
}

func TestSetGetDelete(t *testing.T) {
	withFake(t, &fake{items: map[string]string{}})
	if err := keys.Set("openai", "sk-test-1234"); err != nil {
		t.Fatal(err)
	}
	got, err := keys.Get("openai")
	if err != nil || got != "sk-test-1234" {
		t.Fatalf("Get = %q, %v", got, err)
	}
	if err := keys.Delete("openai"); err != nil {
		t.Fatal(err)
	}
	if got, err := keys.Get("openai"); err != nil || got != "" {
		t.Fatalf("after Delete, Get = %q, %v; want empty and no error", got, err)
	}
}

// A key that was never stored is not an error: the UI shows "no key".
func TestGetUnsetIsEmpty(t *testing.T) {
	withFake(t, &fake{items: map[string]string{}})
	got, err := keys.Get("anthropic")
	if err != nil || got != "" {
		t.Fatalf("Get = %q, %v; want empty and no error", got, err)
	}
}

func TestStoreFailuresSurface(t *testing.T) {
	withFake(t, &fake{items: map[string]string{}, err: errors.New("no dbus")})
	if err := keys.Set("openai", "sk-x"); err == nil {
		t.Error("Set: want an error when the store is unusable")
	}
	if err := keys.Available(); err == nil {
		t.Error("Available: want an error when the store is unusable")
	}
}

func TestSetRejectsUnknownProviderAndEmptyKey(t *testing.T) {
	withFake(t, &fake{items: map[string]string{}})
	if err := keys.Set("acme", "sk-x"); err == nil {
		t.Error("want an error for an unknown provider")
	}
	if err := keys.Set("openai", "   "); err == nil {
		t.Error("want an error for a blank key")
	}
}

func TestMask(t *testing.T) {
	cases := map[string]string{
		"":                  "",
		"sk-abcdefghijklmn": "sk-…klmn",
		"tiny":              "…",
	}
	for in, want := range cases {
		if got := keys.Mask(in); got != want {
			t.Errorf("Mask(%q) = %q, want %q", in, got, want)
		}
	}
}
