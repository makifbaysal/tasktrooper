// Package live is what the direct broker kinds share. They read a broker's
// live state rather than a metrics store, so a read answers one point per
// series at the instant it is made (Instant); the HTTP ones (RabbitMQ's
// management API, NATS's monitoring endpoint) call it through Conn: every
// request is judged by the URL policy and pinned to the address it vetted, a
// redirect may change neither host nor scheme, and no error carries a secret
// or the address a host resolved to.
package live

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/urlguard"
)

const (
	maxUpstreamText = 300
	maskMinLen      = 4
)

// ErrNotFound is a 404: the entity a path names (a queue, a vhost) is not
// there.
var ErrNotFound = errors.New("not found")

// Endpoint is one API: Base is the URL paths hang off, Headers go on every
// request, Masks are the values no returned error may carry, and API names
// it in errors ("the RabbitMQ management API").
type Endpoint struct {
	Base    string
	Headers []domain.HeaderLine
	Masks   []string
	API     string
}

type Conn struct {
	base    *url.URL
	ips     []net.IP
	client  *http.Client
	headers []domain.HeaderLine
	masks   []string
	api     string
}

// Open checks ep.Base against policy; the caller closes the answer.
func Open(ctx context.Context, policy urlguard.Policy, ep Endpoint, timeout time.Duration) (*Conn, error) {
	raw := strings.TrimSpace(ep.Base)
	if raw == "" {
		return nil, errors.New("no URL is configured")
	}
	target, err := policy.Validate(ctx, raw)
	if err != nil {
		return nil, Refused(err, raw)
	}
	if target.URL.User != nil {
		return nil, errors.New("the URL must not carry credentials; use the username and password fields")
	}
	client := policy.ClientFor(target, timeout)
	client.CheckRedirect = policy.SameHostRedirect(target.URL)
	// Longest first: a secret inside another (a key's id inside the key file)
	// would otherwise leave the rest of the longer one showing. A header value
	// comes after them, so "Bearer " stays readable around a masked token.
	masks := append([]string(nil), ep.Masks...)
	slices.SortStableFunc(masks, func(a, b string) int { return len(b) - len(a) })
	for _, h := range ep.Headers {
		masks = append(masks, h.Value)
	}
	return &Conn{base: target.URL, ips: target.IPs, client: client, headers: ep.Headers, masks: masks, api: ep.API}, nil
}

func (c *Conn) Close() { c.client.CloseIdleConnections() }

// Get reads base + "/" + the path segments (each escaped on its own, so a
// RabbitMQ vhost "/" travels as %2F) into out. A 404 is ErrNotFound; any
// other answer but 200 is an error naming the status. A "." or ".." segment
// is refused: escaping leaves it as it is, and a server or proxy resolving it
// would read another path than the one named.
func (c *Conn) Get(ctx context.Context, segments []string, query url.Values, limit int64, out any) error {
	u := *c.base
	op := "/" + strings.Join(segments, "/")
	escaped := make([]string, len(segments))
	for i, s := range segments {
		if s == "." || s == ".." {
			return fmt.Errorf("%s: %q cannot be read as a name", c.api, s)
		}
		escaped[i] = url.PathEscape(s)
	}
	u.Path = strings.TrimRight(c.base.Path, "/") + op
	u.RawPath = strings.TrimRight(c.base.EscapedPath(), "/") + "/" + strings.Join(escaped, "/")
	u.RawQuery = query.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return errors.New("the URL cannot be requested")
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "TaskTrooper")
	for _, h := range c.headers {
		req.Header.Set(h.Name, h.Value)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return c.transportError(err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return c.transportError(err)
	}
	if int64(len(raw)) > limit {
		return fmt.Errorf("%s answered more than %d MiB", c.api, limit>>20)
	}
	switch {
	case resp.StatusCode == http.StatusOK:
		if err := json.Unmarshal(raw, out); err != nil {
			return fmt.Errorf("the URL answered, but not as %s — is it the right base URL?", c.api)
		}
		return nil
	case resp.StatusCode == http.StatusNotFound:
		return ErrNotFound
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		if d := detail(raw); d != "" {
			return fmt.Errorf("%s: authentication failed (HTTP %d): %s", c.api, resp.StatusCode, c.Scrub(d))
		}
		return fmt.Errorf("%s: authentication failed (HTTP %d)", c.api, resp.StatusCode)
	default:
		if d := detail(raw); d != "" {
			return fmt.Errorf("%s: HTTP %d: %s", c.api, resp.StatusCode, c.Scrub(d))
		}
		return fmt.Errorf("%s: HTTP %d", c.api, resp.StatusCode)
	}
}

// detail is the message of a JSON error body: RabbitMQ's {"error",
// "reason"}, or a proxy's {"error": {"message"}}.
func detail(raw []byte) string {
	var flat struct {
		Error  string `json:"error"`
		Reason string `json:"reason"`
	}
	if json.Unmarshal(raw, &flat) == nil && (flat.Error != "" || flat.Reason != "") {
		if flat.Reason != "" && flat.Error != "" {
			return flat.Error + ": " + flat.Reason
		}
		return flat.Error + flat.Reason
	}
	var nested struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(raw, &nested) == nil {
		return nested.Error.Message
	}
	return ""
}

// transportError drops the request URL net/http puts in its message and
// names the host as the person typed it: the guard dials the address it
// resolved, so a raw dial error would tell them which internal IP a name
// points at.
func (c *Conn) transportError(err error) error {
	if errors.Is(err, urlguard.ErrBlocked) {
		return Refused(err, c.base.String())
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("%s: timed out", c.api)
	}
	var ue *url.Error
	if errors.As(err, &ue) {
		if ue.Timeout() {
			return fmt.Errorf("%s: timed out", c.api)
		}
		err = ue.Err
	}
	var opErr *net.OpError
	if errors.As(err, &opErr) && opErr.Op == "dial" && opErr.Err != nil {
		return fmt.Errorf("%s: cannot connect to %s: %s", c.api, c.base.Host, c.Scrub(opErr.Err.Error()))
	}
	return fmt.Errorf("%s: %s", c.api, c.Scrub(err.Error()))
}

// Scrub bounds upstream text, masks any secret a server echoed back, and puts
// the typed host back where a resolved address of it appears.
func (c *Conn) Scrub(s string) string {
	for _, v := range c.masks {
		if len(v) >= maskMinLen {
			s = strings.ReplaceAll(s, v, "[redacted]")
		}
	}
	if host := c.base.Hostname(); host != "" {
		for _, ip := range c.ips {
			s = strings.ReplaceAll(s, ip.String(), host)
		}
	}
	if r := []rune(s); len(r) > maxUpstreamText {
		s = string(r[:maxUpstreamText]) + "…"
	}
	return s
}

// Refused logs why and answers only that: the reason names the address a
// host resolved to, which is not the caller's to learn.
func Refused(err error, raw string) error {
	log.Warn().Err(err).Str("url", urlguard.LogRaw(raw)).Msg("data source: destination refused")
	return refusedError{}
}

type refusedError struct{}

func (refusedError) Error() string        { return "the URL is not an allowed destination" }
func (refusedError) Is(target error) bool { return target == urlguard.ErrBlocked }
