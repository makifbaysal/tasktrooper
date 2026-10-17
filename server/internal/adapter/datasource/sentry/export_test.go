package sentry

import "net/url"

// IssueLinkFor is conn.link for a Sentry configured at base, for a test that
// cannot reach that host.
func IssueLinkFor(base, permalink string) string {
	u, _ := url.Parse(base)
	return (&conn{base: u}).link(permalink)
}
