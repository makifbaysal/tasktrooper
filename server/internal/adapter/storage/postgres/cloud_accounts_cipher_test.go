package postgres

import (
	"errors"
	"testing"

	"github.com/makifbaysal/tasktrooper/server/internal/domain/secrets"
)

// The secret-bearing stores used to derive their own cipher lazily, on first
// use — the same bug SettingsStore and RepositoryStore were fixed for (see
// settings_cipher_test.go, repository_cipher_test.go). Boot unsets
// MCP_SECRETS_KEY right after deriving the cipher
// (runtime.scrubProcessSecrets), and a cloud account's first real use is
// always a later HTTP request, so "first use" was always after the key was
// gone. These pin the injection that replaces it.
func TestCloudAccountStoreSetCipherWinsOverTheScrubbedEnvironment(t *testing.T) {
	t.Setenv("MCP_SECRETS_KEY", "")
	t.Setenv("SERVER_API_KEY", "")

	want, err := secrets.NewCipher(make([]byte, 32))
	if err != nil {
		t.Fatalf("build cipher: %v", err)
	}

	var s CloudAccountStore
	s.SetCipher(want, nil)

	got, gotErr := s.getCipher()
	if gotErr != nil {
		t.Fatalf("getCipher after SetCipher: %v", gotErr)
	}
	if got != want {
		t.Fatal("getCipher returned a different cipher than the one injected at boot")
	}
}

// A degraded boot (no key configured) has to stay degraded rather than
// silently re-deriving from an environment that may still hold a key for some
// other component.
func TestCloudAccountStoreSetCipherPropagatesTheBootError(t *testing.T) {
	t.Setenv("MCP_SECRETS_KEY", "")

	bootErr := errors.New("no key at boot")

	var s CloudAccountStore
	s.SetCipher(nil, bootErr)

	got, gotErr := s.getCipher()
	if !errors.Is(gotErr, bootErr) {
		t.Fatalf("getCipher error = %v, want %v", gotErr, bootErr)
	}
	if got != nil {
		t.Fatal("getCipher returned a cipher despite a failed boot derivation")
	}
}
