package urlguard

import (
	"context"
	"net"
	"strings"
)

// DialContext is the guard for a destination that is not a URL: a Kafka
// broker, whose client dials whatever addresses the cluster's metadata
// advertises. It resolves the host once, refuses the whole destination if any
// answer is blocked, and connects to a vetted literal IP, like the HTTP
// clients' dial. A policy that allows no scheme (the zero value) dials
// nothing here either, so a forgotten policy fails closed.
func (p Policy) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	if len(p.Schemes) == 0 {
		return nil, &Error{Reason: "no destination is allowed"}
	}
	return p.dialContext(nil)(ctx, network, address)
}

// PrecheckHost is Precheck for a bare host or host:port: the denied names and
// the literal-IP rules, no DNS.
func (p Policy) PrecheckHost(hostport string) error {
	if len(p.Schemes) == 0 {
		return &Error{Reason: "no destination is allowed"}
	}
	host := strings.TrimSpace(hostport)
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host = strings.TrimSuffix(strings.TrimPrefix(host, "["), "]")
	if host == "" {
		return &Error{Reason: "no host"}
	}
	if p.hostDenied(host) {
		return &Error{Host: host, Reason: "denied host name"}
	}
	if ip := net.ParseIP(host); ip != nil {
		if reason := p.blockedReason(ip); reason != "" {
			return &Error{Host: host, IP: ip, Reason: reason}
		}
	}
	return nil
}
