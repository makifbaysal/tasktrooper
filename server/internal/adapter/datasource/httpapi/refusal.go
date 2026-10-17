package httpapi

import (
	"fmt"
	"net/http"

	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// Refusal names a hosted API's non-OK answer to op the way the hosted kinds
// share: 400 and 422 are a query the API refused (port.ErrDataSourceQuery),
// 401/403 an authentication failure followed by authHint (what the
// credential needs), 404 not found, 429 rate limited, anything else its
// status. detail is what the body says, scrubbed here.
func (c *Conn) Refusal(op string, r Response, detail, authHint string) error {
	suffix := ""
	if d := c.Scrub(detail); d != "" {
		suffix = ": " + d
	}
	switch r.Status {
	case http.StatusBadRequest, http.StatusUnprocessableEntity:
		return fmt.Errorf("%w: %s %s: HTTP %d%s", port.ErrDataSourceQuery, c.api, op, r.Status, suffix)
	case http.StatusUnauthorized, http.StatusForbidden:
		hint := ""
		if authHint != "" {
			hint = "; " + authHint
		}
		return fmt.Errorf("%s %s: authentication failed (HTTP %d)%s%s", c.api, op, r.Status, suffix, hint)
	case http.StatusNotFound:
		return fmt.Errorf("%s %s: not found (HTTP 404)%s", c.api, op, suffix)
	case http.StatusTooManyRequests:
		return fmt.Errorf("%s %s: rate limited (HTTP 429); try again in a minute", c.api, op)
	}
	return fmt.Errorf("%s %s: HTTP %d%s", c.api, op, r.Status, suffix)
}
