package kafka

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kerr"

	"github.com/makifbaysal/tasktrooper/server/internal/platform/urlguard"
)

func TestBrokerRefusalsReadAsWhatThePersonCanFix(t *testing.T) {
	s := &session{masks: []string{"s3cret-pass"}}
	tests := []struct {
		name string
		err  error
		want string
	}{
		{"sasl refused", fmt.Errorf("%w: Authentication failed during authentication due to invalid credentials s3cret-pass", kerr.SaslAuthenticationFailed), "group: authentication failed"},
		{"unknown mechanism", kerr.UnsupportedSaslMechanism, "group: authentication failed"},
		{"acl", &kadm.AuthError{Err: kerr.GroupAuthorizationFailed}, "group: not authorized"},
		{"connection dropped", fmt.Errorf("read: %w", io.EOF), "closed the connection"},
		{"deadline", fmt.Errorf("request: %w", context.DeadlineExceeded), "group: timed out"},
		{"advertised broker refused", fmt.Errorf("unable to dial: %w", blockedBroker{address: "10.0.0.9:9092"}), "broker 10.0.0.9:9092 is not an allowed destination"},
		{"other", errors.New(strings.Repeat("x", 400) + "s3cret-pass"), "group: xxx"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := s.fail("group", tt.err)
			assert.Contains(t, got.Error(), tt.want)
			assert.NotContains(t, got.Error(), "s3cret-pass")
			assert.LessOrEqual(t, len([]rune(got.Error())), maxUpstreamText+40)
		})
	}
	assert.True(t, errors.Is(s.fail("group", blockedBroker{address: "b:1"}), urlguard.ErrBlocked))
}

func TestNormalizePEMRestoresTheMarkersLines(t *testing.T) {
	got := string(normalizePEM("-----BEGIN CERTIFICATE----- MIIB AAA -----END CERTIFICATE-----"))
	assert.Equal(t, "\n-----BEGIN CERTIFICATE-----\n MIIB AAA \n-----END CERTIFICATE-----\n", got)
}
