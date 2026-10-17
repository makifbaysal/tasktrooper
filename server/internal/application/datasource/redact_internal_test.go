package datasource

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRedactMasksTheLongestSecretFirst(t *testing.T) {
	key := `{"private_key_id":"0123abcd","client_email":"x@y"}`
	secrets := map[string]string{"a": "0123abcd", "b": key}

	for range 20 {
		got := redact("echoed "+key, secrets)
		assert.Equal(t, "echoed [redacted]", got, "the id inside the key file must not split it")
	}
}

func TestRedactMasksTheKeyMaterialInsideAPastedServiceAccountKey(t *testing.T) {
	pem := "-----BEGIN PRIVATE KEY-----\nMIIEvQIBADANBgkqhkiG9w0BAQEFAASCBKcwggSjAgEAAoIBAQC\nZZZZsecondline\n-----END PRIVATE KEY-----\n"
	raw, err := json.Marshal(map[string]string{"type": "service_account", "private_key_id": "kid-998877", "private_key": pem})
	assert.NoError(t, err)
	secrets := map[string]string{"service_account_json": string(raw)}

	for _, msg := range []string{
		"bad key kid-998877",
		"cannot parse " + pem,
		`body: "private_key": "` + strings.ReplaceAll(pem, "\n", `\n`) + `"`,
		"line MIIEvQIBADANBgkqhkiG9w0BAQEFAASCBKcwggSjAgEAAoIBAQC rejected",
	} {
		got := redact(msg, secrets)
		assert.NotContains(t, got, "kid-998877")
		assert.NotContains(t, got, "MIIEvQIBADAN")
		assert.NotContains(t, got, "ZZZZsecondline")
	}
}

func TestRedactMasksATailThatBeginsASecretWhenTheMessageWasCut(t *testing.T) {
	secrets := map[string]string{"token": "tok-0123456789"}

	assert.Equal(t, "auth header Bearer [redacted]…", redact("auth header Bearer tok-01…", secrets))
	assert.Equal(t, "upstream says tok…", redact("upstream says tok…", secrets), "shorter than a secret may be is left")
	assert.Equal(t, "auth header Bearer tok-01", redact("auth header Bearer tok-01", secrets), "a message not cut is masked whole values only")
}
