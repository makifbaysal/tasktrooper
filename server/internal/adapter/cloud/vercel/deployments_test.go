package vercel

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// TestAPIErrorMatchesTheUnauthorizedSentinel is the seam the application layer
// stands on: it must be able to tell a refused token from an outage WITHOUT
// importing this package, which is what lets Provider.Verify turn it into
// port.ErrCloudAuth and everything else surface as a plain provider error.
func TestAPIErrorMatchesTheUnauthorizedSentinel(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden} {
		c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(status)
			_, _ = w.Write([]byte(`{"error":{"code":"forbidden","message":"Not authorized"}}`))
		}))
		_, err := c.User(context.Background(), "bad")
		if !errors.Is(err, port.ErrVercelUnauthorized) {
			t.Fatalf("status %d: errors.Is(err, port.ErrVercelUnauthorized) = false for %v", status, err)
		}
	}

	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`{"error":{"code":"bad_gateway","message":"upstream"}}`))
	}))
	_, err := c.User(context.Background(), "tok")
	if errors.Is(err, port.ErrVercelUnauthorized) {
		t.Fatalf("a 502 is an outage, not a refused token: %v", err)
	}
	if err == nil {
		t.Fatal("a 502 must still be an error")
	}
}
