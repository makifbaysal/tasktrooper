// Package urlguard vets outbound HTTP destinations chosen by untrusted input —
// tool arguments, MCP config rows, API request bodies. The model's context is
// full of attacker-influenced text, so every such destination is treated as
// attacker-chosen.
//
// The check runs on the address that gets dialled: Client and ClientFor vet
// inside Transport.DialContext and connect to that literal IP, closing the DNS
// rebinding window with no second lookup to poison; redirect hops are re-vetted.
//
// Loopback, RFC1918 and CGNAT are refused unless AllowLoopback is set; Default
// reads it from an environment variable, never a config row an agent can reach.
package urlguard

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// AllowLoopbackEnv re-enables loopback destinations; read from the process
// environment, which no agent or config row can write.
const AllowLoopbackEnv = "ALLOW_LOOPBACK_TOOL_URLS"

// defaultMaxRedirects caps a redirect chain; five is well past real needs and
// well short of Go's default ten, which is only a loop breaker.
const defaultMaxRedirects = 5

// ErrBlocked distinguishes a rejection from "host down" so both can read the
// same outward while being logged differently; errors.Is reaches it through
// net/http's url.Error.
var ErrBlocked = errors.New("destination not permitted")

// Error is one rejection; its message is for logs only, never for a model.
type Error struct {
	Reason string
	Host   string
	IP     net.IP
}

func (e *Error) Error() string {
	switch {
	case e.IP != nil && e.Host != "":
		return fmt.Sprintf("%s (%s): %s", e.Host, e.IP, e.Reason)
	case e.Host != "":
		return fmt.Sprintf("%s: %s", e.Host, e.Reason)
	default:
		return e.Reason
	}
}

func (e *Error) Is(target error) bool { return target == ErrBlocked }

// Resolver lets tests script DNS answers deterministically; production uses
// net.DefaultResolver.
type Resolver interface {
	LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error)
}

// Policy is what a destination is judged against; the zero value allows no
// scheme, so a forgotten field fails closed.
type Policy struct {
	// Schemes is the scheme allowlist; anything outside it is refused — the odd
	// schemes are the interesting half of an SSRF payload.
	Schemes []string

	AllowLoopback bool

	// AllowPrivate permits RFC1918, IPv6 ULA and CGNAT, for a destination a
	// person configured (HumanConfigured). Cloud metadata addresses inside
	// those ranges stay refused.
	AllowPrivate bool

	// RequireLoopback inverts the whole policy: only 127.0.0.0/8 and ::1 dial,
	// everything else (public, private, link-local) is refused. For a tool
	// that must stay on this machine (http_request) — not an environment
	// switch like AllowLoopback, so ALLOW_LOOPBACK_TOOL_URLS has no effect on it.
	RequireLoopback bool

	// DenyHosts are names refused before any lookup, whatever they resolve
	// to (lowercase, no trailing dot).
	DenyHosts []string

	MaxRedirects int

	Resolver Resolver

	// Dialer replaces the plain net.Dialer below the guard, not around it.
	// Tests only.
	Dialer func(ctx context.Context, network, address string) (net.Conn, error)

	// TLSClientConfig swaps who is trusted (private CA), never where the
	// connection may go — the address rules run first.
	TLSClientConfig *tls.Config
}

// PublicOnly is the policy for anything an LLM or API caller can steer:
// http/https to public addresses only.
func PublicOnly() Policy {
	return Policy{
		Schemes:      []string{"http", "https"},
		MaxRedirects: defaultMaxRedirects,
	}
}

// Default is PublicOnly plus the environment loopback switch; call at
// construction, the answer is a deployment property.
func Default() Policy {
	p := PublicOnly()
	p.AllowLoopback = loopbackAllowed(os.Getenv)
	return p
}

// LoopbackOnly is the policy for a tool that must never leave this machine
// (http_request): http/https to 127.0.0.0/8 or ::1 only, every other
// destination refused — including private and link-local addresses that
// AllowPrivate/AllowLoopback would otherwise admit elsewhere.
func LoopbackOnly() Policy {
	return Policy{
		Schemes:         []string{"http", "https"},
		RequireLoopback: true,
		MaxRedirects:    defaultMaxRedirects,
	}
}

// MetadataHosts are the cloud metadata service names. They resolve to
// link-local addresses, which every policy refuses anyway; naming them refuses
// them even behind a resolver that answers otherwise.
var MetadataHosts = []string{"metadata.google.internal", "metadata.goog", "metadata"}

// HumanConfigured is the policy for a destination a person typed into a
// settings form rather than one an agent or a request body can steer (a data
// source URL): a self-hosted Prometheus usually lives on this machine or the
// local network, so loopback and private ranges dial. Cloud metadata never
// does — link-local is refused by every policy, and so are MetadataHosts and
// the AWS IPv6 metadata address inside the ULA range AllowPrivate opens.
func HumanConfigured() Policy {
	return Policy{
		Schemes:       []string{"http", "https"},
		AllowLoopback: true,
		AllowPrivate:  true,
		DenyHosts:     MetadataHosts,
		MaxRedirects:  defaultMaxRedirects,
	}
}

// loopbackAllowed takes getenv so tests do not mutate the process environment.
func loopbackAllowed(getenv func(string) string) bool {
	v, err := strconv.ParseBool(strings.TrimSpace(getenv(AllowLoopbackEnv)))
	return err == nil && v
}

type Target struct {
	URL  *url.URL
	Host string
	IPs  []net.IP
}

// Precheck is the offline half of Validate (no DNS): scheme, shape and
// literal-IP rules. Resolving at write time is flaky and pointless — the row is
// dialled later — so it only keeps obvious literals out of the data.
func (p Policy) Precheck(raw string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return nil, &Error{Reason: "not a valid URL"}
	}
	if !p.schemeAllowed(u.Scheme) {
		return nil, &Error{Reason: fmt.Sprintf("scheme %q is not allowed", u.Scheme)}
	}
	host := u.Hostname()
	if host == "" {
		return nil, &Error{Reason: "URL has no host"}
	}
	if p.hostDenied(host) {
		return nil, &Error{Host: host, Reason: "denied host name"}
	}
	if ip := net.ParseIP(host); ip != nil {
		if reason := p.blockedReason(ip); reason != "" {
			return nil, &Error{Host: host, IP: ip, Reason: reason}
		}
	}
	return u, nil
}

func (p Policy) hostDenied(host string) bool {
	host = strings.TrimSuffix(strings.ToLower(host), ".")
	for _, d := range p.DenyHosts {
		if host == d {
			return true
		}
	}
	return false
}

func (p Policy) Validate(ctx context.Context, raw string) (*Target, error) {
	u, err := p.Precheck(raw)
	if err != nil {
		return nil, err
	}
	return p.ValidateURL(ctx, u)
}

func (p Policy) ValidateURL(ctx context.Context, u *url.URL) (*Target, error) {
	if u == nil {
		return nil, &Error{Reason: "not a valid URL"}
	}
	if !p.schemeAllowed(u.Scheme) {
		return nil, &Error{Reason: fmt.Sprintf("scheme %q is not allowed", u.Scheme)}
	}
	host := u.Hostname()
	if host == "" {
		return nil, &Error{Reason: "URL has no host"}
	}
	ips, err := p.resolve(ctx, host)
	if err != nil {
		return nil, err
	}
	return &Target{URL: u, Host: host, IPs: ips}, nil
}

func (p Policy) schemeAllowed(scheme string) bool {
	scheme = strings.ToLower(scheme)
	for _, s := range p.Schemes {
		if scheme == strings.ToLower(s) {
			return true
		}
	}
	return false
}

func (p Policy) resolver() Resolver {
	if p.Resolver != nil {
		return p.Resolver
	}
	return net.DefaultResolver
}

// resolve refuses the whole destination if any answer is blocked, since the
// transport is free to pick any of them.
func (p Policy) resolve(ctx context.Context, host string) ([]net.IP, error) {
	if p.hostDenied(host) {
		return nil, &Error{Host: host, Reason: "denied host name"}
	}
	if ip := net.ParseIP(host); ip != nil {
		if reason := p.blockedReason(ip); reason != "" {
			return nil, &Error{Host: host, IP: ip, Reason: reason}
		}
		return []net.IP{ip}, nil
	}
	addrs, err := p.resolver().LookupIPAddr(ctx, host)
	if err != nil {
		return nil, &Error{Host: host, Reason: "host could not be resolved"}
	}
	if len(addrs) == 0 {
		return nil, &Error{Host: host, Reason: "host resolved to no addresses"}
	}
	ips := make([]net.IP, 0, len(addrs))
	for _, a := range addrs {
		if reason := p.blockedReason(a.IP); reason != "" {
			return nil, &Error{Host: host, IP: a.IP, Reason: reason}
		}
		ips = append(ips, a.IP)
	}
	return ips, nil
}

func (p Policy) Client(timeout time.Duration) *http.Client {
	return p.client(nil, timeout)
}

// ClientFor pins the first hop to the addresses Validate vetted for t, leaving
// no second lookup between check and connect to poison.
func (p Policy) ClientFor(t *Target, timeout time.Duration) *http.Client {
	var pin map[string][]net.IP
	if t != nil && len(t.IPs) > 0 {
		pin = map[string][]net.IP{strings.ToLower(t.Host): t.IPs}
	}
	return p.client(pin, timeout)
}

func (p Policy) client(pin map[string][]net.IP, timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			// Proxy is nil on purpose: a proxy would carry the real destination
			// to a host this guard never sees, making every check below moot.
			// Egress through a proxy requires building a custom transport.
			Proxy:                 nil,
			DialContext:           p.dialContext(pin),
			TLSClientConfig:       p.TLSClientConfig,
			ForceAttemptHTTP2:     true,
			MaxIdleConns:          16,
			IdleConnTimeout:       30 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
		},
		CheckRedirect: p.CheckRedirect,
	}
}

func (p Policy) CheckRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= p.maxRedirects() {
		return &Error{Reason: fmt.Sprintf("redirect chain longer than %d hops", p.maxRedirects())}
	}
	_, err := p.ValidateURL(req.Context(), req.URL)
	return err
}

// SameHostRedirect is CheckRedirect plus a refusal to leave endpoint's host or
// scheme. Credentials survive a host change when a RoundTripper or a rename
// sets them, so the cross-host hop itself must be refused; and an https
// endpoint redirecting to http would send them in the clear.
func (p Policy) SameHostRedirect(endpoint *url.URL) func(*http.Request, []*http.Request) error {
	var want, wantScheme string
	if endpoint != nil {
		want = strings.ToLower(endpoint.Host)
		wantScheme = strings.ToLower(endpoint.Scheme)
	}
	return func(req *http.Request, via []*http.Request) error {
		if want != "" && !strings.EqualFold(req.URL.Host, want) {
			return &Error{
				Host:   req.URL.Hostname(),
				Reason: fmt.Sprintf("redirect leaves the configured host %q", want),
			}
		}
		if wantScheme != "" && !strings.EqualFold(req.URL.Scheme, wantScheme) {
			return &Error{
				Host:   req.URL.Hostname(),
				Reason: fmt.Sprintf("redirect changes the scheme from %s to %s", wantScheme, strings.ToLower(req.URL.Scheme)),
			}
		}
		return p.CheckRedirect(req, via)
	}
}

func (p Policy) maxRedirects() int {
	if p.MaxRedirects > 0 {
		return p.MaxRedirects
	}
	return defaultMaxRedirects
}

// dialContext is the load-bearing half: it vets the address net/http is about
// to reach and then dials that exact IP rather than re-resolving the name.
func (p Policy) dialContext(pin map[string][]net.IP) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, &Error{Reason: "malformed dial address"}
		}
		ips, ok := pin[strings.ToLower(host)]
		if !ok {
			if ips, err = p.resolve(ctx, host); err != nil {
				return nil, err
			}
		}
		var lastErr error
		for _, ip := range ips {
			// Pinned addresses are re-checked; a pin is not a way around the policy.
			if reason := p.blockedReason(ip); reason != "" {
				return nil, &Error{Host: host, IP: ip, Reason: reason}
			}
			conn, dialErr := p.baseDial(ctx, network, net.JoinHostPort(ip.String(), port))
			if dialErr == nil {
				return conn, nil
			}
			lastErr = dialErr
		}
		if lastErr == nil {
			lastErr = &Error{Host: host, Reason: "host resolved to no usable address"}
		}
		return nil, lastErr
	}
}

func (p Policy) baseDial(ctx context.Context, network, address string) (net.Conn, error) {
	if p.Dialer != nil {
		return p.Dialer(ctx, network, address)
	}
	d := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	return d.DialContext(ctx, network, address)
}

// LogValue drops query and fragment — agent-supplied URLs routinely carry the
// caller's own tokens — and redacts the password.
func LogValue(u *url.URL) string {
	if u == nil {
		return ""
	}
	c := *u
	c.RawQuery = ""
	c.ForceQuery = false
	c.Fragment = ""
	c.RawFragment = ""
	return c.Redacted()
}

// LogRaw is LogValue for input that may not parse; unparseable input becomes a
// fixed placeholder instead of being logged verbatim.
func LogRaw(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "<unparseable url>"
	}
	return LogValue(u)
}
