package urlguard

import "net"

// blockedReason returns "" to dial ip, else a short human reason. Reasons are
// log-only; forwarding one to a model turns this package into a port scanner.
func (p Policy) blockedReason(ip net.IP) string {
	if ip == nil {
		return "not an IP address"
	}
	if p.RequireLoopback {
		return p.loopbackOnlyReason(ip)
	}

	// To4 unwraps v4-mapped v6, so the v4 rules below cover them.
	if v4 := ip.To4(); v4 != nil {
		return p.reasonV4(v4)
	}

	ip16 := ip.To16()
	if ip16 == nil {
		return "not an IP address"
	}

	// Payloads of encodings net.IP does not unwrap are judged explicitly.
	if embedded := embeddedV4(ip16); embedded != nil {
		if reason := p.reasonV4(embedded); reason != "" {
			return reason
		}
	}
	return p.reasonV6(ip16)
}

// loopbackOnlyReason is RequireLoopback's whole policy: everything that is not
// 127.0.0.0/8 or ::1 is refused, deliberately bypassing reasonV4/reasonV6 (which
// exist to find exceptions in an otherwise-public-only policy, the opposite
// shape from "only loopback dials").
func (p Policy) loopbackOnlyReason(ip net.IP) string {
	if v4 := ip.To4(); v4 != nil {
		if v4.IsLoopback() {
			return ""
		}
		return "not a loopback address"
	}
	ip16 := ip.To16()
	if ip16 == nil {
		return "not an IP address"
	}
	if embedded := embeddedV4(ip16); embedded != nil {
		if embedded.IsLoopback() {
			return ""
		}
		return "not a loopback address"
	}
	if ip16.IsLoopback() {
		return ""
	}
	return "not a loopback address"
}

func (p Policy) reasonV4(ip net.IP) string {
	switch {
	case ip.IsUnspecified():
		// 0.0.0.0 connects to a local listener.
		if p.AllowLoopback {
			return ""
		}
		return "unspecified address"
	case ip.IsLoopback():
		if p.AllowLoopback {
			return ""
		}
		return "loopback address"
	case ip.IsLinkLocalUnicast(): // 169.254.0.0/16: cloud metadata lives here
		return "link-local address"
	case ip.IsInterfaceLocalMulticast(), ip.IsLinkLocalMulticast(), ip.IsMulticast():
		return "multicast address"
	case isMetadataIP(ip):
		return "cloud metadata address"
	case ip.IsPrivate():
		if p.AllowPrivate {
			return ""
		}
		return "private address"
	}
	for _, b := range blockedV4 {
		if b.net.Contains(ip) {
			if b.private && p.AllowPrivate {
				return ""
			}
			return b.reason
		}
	}
	return ""
}

func (p Policy) reasonV6(ip net.IP) string {
	switch {
	case ip.IsUnspecified():
		if p.AllowLoopback {
			return ""
		}
		return "unspecified address"
	case ip.IsLoopback():
		if p.AllowLoopback {
			return ""
		}
		return "loopback address"
	case ip.IsLinkLocalUnicast():
		return "link-local address"
	case ip.IsInterfaceLocalMulticast(), ip.IsLinkLocalMulticast(), ip.IsMulticast():
		return "multicast address"
	case isMetadataIP(ip):
		return "cloud metadata address"
	case ip.IsPrivate():
		if p.AllowPrivate {
			return ""
		}
		return "unique local address"
	}
	return ""
}

// metadataIPs are cloud instance metadata endpoints. Some sit inside ranges
// AllowPrivate opens (Alibaba's in CGNAT, EC2's and GCE's IPv6 ones in ULA), so
// every policy refuses them before it consults that switch. 169.254.169.254 is
// link-local and refused earlier still; it is listed so the set is whole.
var metadataIPs = []net.IP{
	net.ParseIP("169.254.169.254"),
	net.ParseIP("100.100.100.200"),
	net.ParseIP("fd00:ec2::254"),
	net.ParseIP("fd20:ce::254"),
}

func isMetadataIP(ip net.IP) bool {
	for _, m := range metadataIPs {
		if ip.Equal(m) {
			return true
		}
	}
	return false
}

// blockedV4 covers ranges net.IP has no predicate for. CGNAT (100.64.0.0/10)
// is the operational one: cloud providers park internal endpoints there, unseen
// by RFC1918 checks.
var blockedV4 = []struct {
	net     *net.IPNet
	reason  string
	private bool
}{
	{cidr("100.64.0.0/10"), "carrier-grade NAT address", true},
	{cidr("192.0.0.0/24"), "IETF protocol assignment address", false},
	{cidr("198.18.0.0/15"), "benchmarking address", false},
	{cidr("240.0.0.0/4"), "reserved address", false},
}

var (
	// 64:ff9b::/96, the NAT64 well-known prefix; the last 32 bits of the address
	// are the v4 payload verbatim.
	nat64 = cidr("64:ff9b::/96")
)

// embeddedV4 extracts the IPv4 payload from encodings net.IP does not unwrap.
func embeddedV4(ip net.IP) net.IP {
	if len(ip) != net.IPv6len {
		return nil
	}
	switch {
	case isZeros(ip[:12]):
		// Also matches :: and ::1; their payloads (0.0.0.0, 0.0.0.1) are
		// handled or harmless.
		return net.IPv4(ip[12], ip[13], ip[14], ip[15]).To4()
	case nat64.Contains(ip):
		return net.IPv4(ip[12], ip[13], ip[14], ip[15]).To4()
	case ip[0] == 0x20 && ip[1] == 0x02:
		return net.IPv4(ip[2], ip[3], ip[4], ip[5]).To4()
	}
	return nil
}

func isZeros(b []byte) bool {
	for _, c := range b {
		if c != 0 {
			return false
		}
	}
	return true
}

func cidr(s string) *net.IPNet {
	_, n, err := net.ParseCIDR(s)
	if err != nil {
		panic("urlguard: bad CIDR " + s + ": " + err.Error())
	}
	return n
}
