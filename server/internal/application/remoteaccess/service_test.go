package remoteaccess_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/makifbaysal/tasktrooper/server/internal/application/remoteaccess"
	"github.com/makifbaysal/tasktrooper/server/internal/application/remoteaccess/remotetest"
)

type clock struct{ t time.Time }

func (c *clock) now() time.Time          { return c.t }
func (c *clock) advance(d time.Duration) { c.t = c.t.Add(d) }

func newService(t *testing.T) (*remoteaccess.Service, *remotetest.Store, *clock) {
	t.Helper()
	store := remotetest.New()
	clk := &clock{t: time.Date(2026, 11, 11, 10, 0, 0, 0, time.UTC)}
	svc := remoteaccess.New(store, "test")
	svc.SetClock(clk.now)
	return svc, store, clk
}

func TestPairingCodeShapeAndNormalisation(t *testing.T) {
	svc, _, _ := newService(t)
	p, err := svc.CreatePairing(context.Background(), remoteaccess.RoleOperator, 0, "")
	require.NoError(t, err)
	assert.Len(t, p.Code, remoteaccess.CodeLength)
	for _, r := range p.Code {
		assert.True(t, strings.ContainsRune(remoteaccess.CodeAlphabet, r), "%q", r)
	}
	display := remoteaccess.DisplayCode(p.Code)
	assert.Equal(t, p.Code, remoteaccess.NormalizeCode(strings.ToLower(display)+" "))
}

func TestExchangeGivesATokenStoredOnlyAsAHash(t *testing.T) {
	svc, store, _ := newService(t)
	ctx := context.Background()
	p, err := svc.CreatePairing(ctx, remoteaccess.RoleViewer, 7, "")
	require.NoError(t, err)

	token, dev, err := svc.Exchange(ctx, remoteaccess.DisplayCode(p.Code), "Akif's iPhone", "ios")
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(token, remoteaccess.TokenPrefix))
	assert.Equal(t, remoteaccess.RoleViewer, dev.Role)
	require.NotNil(t, dev.ExpiresAt)

	stored := store.Devices[dev.ID]
	assert.NotContains(t, stored.TokenHash, token)
	assert.Equal(t, remoteaccess.HashSecret(token), stored.TokenHash)
	for _, pr := range store.Pairings {
		assert.NotContains(t, pr.CodeHash, p.Code, "the pairing code is hashed too")
	}

	got, err := svc.Authenticate(ctx, token)
	require.NoError(t, err)
	assert.Equal(t, dev.ID, got.ID)
}

func TestAPairingCodeWorksOnce(t *testing.T) {
	svc, _, _ := newService(t)
	ctx := context.Background()
	p, _ := svc.CreatePairing(ctx, remoteaccess.RoleOperator, 0, "")
	_, _, err := svc.Exchange(ctx, p.Code, "one", "ios")
	require.NoError(t, err)
	_, _, err = svc.Exchange(ctx, p.Code, "two", "ios")
	assert.ErrorIs(t, err, remoteaccess.ErrInvalidPairing)
}

func TestAPairingCodeExpiresAfterFiveMinutes(t *testing.T) {
	svc, _, clk := newService(t)
	ctx := context.Background()
	p, _ := svc.CreatePairing(ctx, remoteaccess.RoleOperator, 0, "")
	clk.advance(remoteaccess.PairingTTL + time.Second)
	_, _, err := svc.Exchange(ctx, p.Code, "late", "ios")
	assert.ErrorIs(t, err, remoteaccess.ErrInvalidPairing)

	q, _ := svc.CreatePairing(ctx, remoteaccess.RoleOperator, 0, "")
	clk.advance(remoteaccess.PairingTTL - time.Second)
	_, _, err = svc.Exchange(ctx, q.Code, "in time", "ios")
	assert.NoError(t, err)
}

func TestACancelledPairingCannotBeExchanged(t *testing.T) {
	svc, _, _ := newService(t)
	ctx := context.Background()
	p, _ := svc.CreatePairing(ctx, remoteaccess.RoleOperator, 0, "")
	require.NoError(t, svc.CancelPairing(ctx, p.ID))
	_, _, err := svc.Exchange(ctx, p.Code, "x", "ios")
	assert.ErrorIs(t, err, remoteaccess.ErrInvalidPairing)
}

func TestWrongCodesAreRateLimitedForTheWholeListener(t *testing.T) {
	svc, _, clk := newService(t)
	ctx := context.Background()
	p, _ := svc.CreatePairing(ctx, remoteaccess.RoleOperator, 0, "")
	for i := 0; i < remoteaccess.PairFailuresPerMinute; i++ {
		_, _, err := svc.Exchange(ctx, "WRONGWRONG12", "x", "ios")
		require.ErrorIs(t, err, remoteaccess.ErrInvalidPairing)
	}
	_, _, err := svc.Exchange(ctx, p.Code, "x", "ios")
	var rl *remoteaccess.RateLimitError
	require.True(t, errors.As(err, &rl), "even the right code waits once guessing was detected")
	assert.Greater(t, rl.RetryAfter, time.Duration(0))

	clk.advance(time.Minute + time.Second)
	_, _, err = svc.Exchange(ctx, p.Code, "x", "ios")
	assert.NoError(t, err)
}

func TestRevocationIsImmediate(t *testing.T) {
	svc, _, _ := newService(t)
	ctx := context.Background()
	p, _ := svc.CreatePairing(ctx, remoteaccess.RoleOperator, 0, "")
	token, dev, _ := svc.Exchange(ctx, p.Code, "phone", "ios")
	_, err := svc.Authenticate(ctx, token)
	require.NoError(t, err)

	require.NoError(t, svc.Revoke(ctx, dev.ID))
	_, err = svc.Authenticate(ctx, token)
	assert.ErrorIs(t, err, remoteaccess.ErrTokenRevoked)
	assert.ErrorIs(t, svc.Revoke(ctx, dev.ID), remoteaccess.ErrNotFound, "second revoke finds nothing live")
}

func TestADeviceTokenExpires(t *testing.T) {
	svc, _, clk := newService(t)
	ctx := context.Background()
	p, _ := svc.CreatePairing(ctx, remoteaccess.RoleOperator, 1, "")
	token, _, _ := svc.Exchange(ctx, p.Code, "phone", "ios")
	clk.advance(23 * time.Hour)
	_, err := svc.Authenticate(ctx, token)
	require.NoError(t, err)
	clk.advance(2 * time.Hour)
	_, err = svc.Authenticate(ctx, token)
	assert.ErrorIs(t, err, remoteaccess.ErrTokenExpired)
}

func TestNeverExpiringDevicesHaveNoExpiry(t *testing.T) {
	svc, _, _ := newService(t)
	ctx := context.Background()
	p, _ := svc.CreatePairing(ctx, remoteaccess.RoleOperator, 0, "")
	_, dev, _ := svc.Exchange(ctx, p.Code, "phone", "ios")
	assert.Nil(t, dev.ExpiresAt)
}

func TestUnknownAndMalformedTokensAreUnauthorized(t *testing.T) {
	svc, _, _ := newService(t)
	for _, tok := range []string{"", "x", "Bearer", "ttd_", "ttd_" + strings.Repeat("a", 43), strings.Repeat("a", 500)} {
		_, err := svc.Authenticate(context.Background(), tok)
		assert.ErrorIs(t, err, remoteaccess.ErrUnauthorized, tok)
	}
}

func TestTheServerAPIKeyIsNotADeviceToken(t *testing.T) {
	svc, _, _ := newService(t)
	_, err := svc.Authenticate(context.Background(), "some-server-api-key")
	assert.ErrorIs(t, err, remoteaccess.ErrUnauthorized)
}

func TestPerDeviceRateLimit(t *testing.T) {
	svc, _, clk := newService(t)
	p, _ := svc.CreatePairing(context.Background(), remoteaccess.RoleOperator, 0, "")
	_, dev, _ := svc.Exchange(context.Background(), p.Code, "phone", "ios")
	for i := 0; i < remoteaccess.DeviceRequestsPerMinute; i++ {
		ok, _ := svc.Allow(dev.ID)
		require.True(t, ok)
	}
	ok, retry := svc.Allow(dev.ID)
	assert.False(t, ok)
	assert.Greater(t, retry, time.Duration(0))
	clk.advance(61 * time.Second)
	ok, _ = svc.Allow(dev.ID)
	assert.True(t, ok)
}

func TestInvalidPairingArguments(t *testing.T) {
	svc, _, _ := newService(t)
	_, err := svc.CreatePairing(context.Background(), "root", 0, "")
	assert.ErrorIs(t, err, remoteaccess.ErrInvalidArgument)
	_, err = svc.CreatePairing(context.Background(), remoteaccess.RoleOperator, 9999, "")
	assert.ErrorIs(t, err, remoteaccess.ErrInvalidArgument)
	_, _, err = svc.Exchange(context.Background(), "", "", "")
	assert.ErrorIs(t, err, remoteaccess.ErrInvalidArgument)
}
