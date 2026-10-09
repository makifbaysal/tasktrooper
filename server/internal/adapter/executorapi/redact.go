package executorapi

import (
	"bytes"
	"encoding/json"
)

// minSecretLength keeps a short or empty value from scrubbing ordinary text:
// replacing "ab" everywhere would mangle the frame, not protect anything.
const minSecretLength = 8

var redacted = []byte("[redacted]")

// newRedactor scrubs each secret from an encoded body in both the spelling a
// JSON encoder gives it and the raw one, since a provider error can quote it
// either way inside a string field.
func newRedactor(secrets ...string) func([]byte) []byte {
	var needles [][]byte
	for _, secret := range secrets {
		if len(secret) < minSecretLength {
			continue
		}
		needles = append(needles, []byte(secret))
		if quoted, err := json.Marshal(secret); err == nil {
			if escaped := quoted[1 : len(quoted)-1]; !bytes.Equal(escaped, []byte(secret)) {
				needles = append(needles, escaped)
			}
		}
	}
	return func(body []byte) []byte {
		for _, needle := range needles {
			if bytes.Contains(body, needle) {
				body = bytes.ReplaceAll(body, needle, redacted)
			}
		}
		return body
	}
}
