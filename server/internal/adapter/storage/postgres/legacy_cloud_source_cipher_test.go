package postgres

import (
	"errors"
	"testing"

	"github.com/makifbaysal/tasktrooper/server/internal/domain/secrets"
)

// LegacyCloudSourceStore's backfill read is the FIRST and only reader of the
// pre-cloud-accounts secrets in a process that has adopted cloud_accounts, and
// it runs from cloud.Service.Boot right after startup — always after
// runtime.scrubProcessSecrets has wiped MCP_SECRETS_KEY from the process
// environment. A lazily-derived cipher would fail every time; this pins the
// boot-time injection that replaces it (see cloud_accounts_cipher_test.go).
func TestLegacyCloudSourceStoreSetCipherWinsOverTheScrubbedEnvironment(t *testing.T) {
	t.Setenv("MCP_SECRETS_KEY", "")
	t.Setenv("SERVER_API_KEY", "")

	want, err := secrets.NewCipher(make([]byte, 32))
	if err != nil {
		t.Fatalf("build cipher: %v", err)
	}

	var s LegacyCloudSourceStore
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
func TestLegacyCloudSourceStoreSetCipherPropagatesTheBootError(t *testing.T) {
	t.Setenv("MCP_SECRETS_KEY", "")

	bootErr := errors.New("no key at boot")

	var s LegacyCloudSourceStore
	s.SetCipher(nil, bootErr)

	got, gotErr := s.getCipher()
	if !errors.Is(gotErr, bootErr) {
		t.Fatalf("getCipher error = %v, want %v", gotErr, bootErr)
	}
	if got != nil {
		t.Fatal("getCipher returned a cipher despite a failed boot derivation")
	}
}
