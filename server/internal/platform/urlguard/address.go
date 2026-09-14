package urlguard

import "net"

// blockedReason returns "" for an address this policy will dial, or a short
// human reason for one it will not. Every reason string is log-only; the model
// and the API caller see one flat message from the call site.
func (p Policy) blockedReason(ip net.IP) string {
	if ip == nil {
		return "not an IP address"
	}

	// v4-mapped IPv6 (::ffff:127.0.0.1) collapses to its v4 form here, which is
	// why it needs no rule of its own: To4 unwraps it and every v4 rule below
	// then applies. net.IP's own predicates do exactly the same unwrap, so a
	// mapped loopback would be caught either way — it is spelled out because
	// this is the encoding people reach for first when probing an SSRF filter.
	if v4 := ip.To4(); v4 != nil {
		return p.reasonV4(v4)
	}

	ip16 := ip.To16()
	if ip16 == nil {
		return "not an IP address"
	}

	// The other IPv6 encodings that carry a v4 address inside are judged by
	// their payload as well. Unlike v4-mapped these are NOT unwrapped by net.IP,
	// so ::127.0.0.1 answers false to IsLoopback and would otherwise sail
	// through on a technicality about a deprecated address format.
	if embedded := embeddedV4(ip16); embedded != nil {
		if reason := p.reasonV4(embedded); reason != "" {
			return reason
		}
	}
	return p.reasonV6(ip16)
}

func (p Policy) reasonV4(ip net.IP) string {
	switch {
	case ip.IsUnspecified():
		// 0.0.0.0 is not a destination; the kernel turns a connect to it into a
		// connect to a local listener, which makes it loopback wearing a hat.
		if p.AllowLoopback {
			return ""
		}
		return "unspecified address"
	case ip.IsLoopback(): // 127.0.0.0/8
		if p.AllowLoopback {
			return ""
		}
		return "loopback address"
	case ip.IsLinkLocalUnicast(): // 169.254.0.0/16 — cloud metadata lives here
		return "link-local address"
	case ip.IsInterfaceLocalMulticast(), ip.IsLinkLocalMulticast(), ip.IsMulticast():
		return "multicast address"
	case ip.IsPrivate(): // 10/8, 172.16/12, 192.168/16
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
	case ip.IsUnspecified(): // ::
		if p.AllowLoopback {
			return ""
		}
		return "unspecified address"
	case ip.IsLoopback(): // ::1
		if p.AllowLoopback {
			return ""
		}
		return "loopback address"
	case ip.IsLinkLocalUnicast(): // fe80::/10
		return "link-local address"
	case ip.IsInterfaceLocalMulticast(), ip.IsLinkLocalMulticast(), ip.IsMulticast(): // ff00::/8
		return "multicast address"
	case ip.IsPrivate(): // fc00::/7 unique local addresses
		if p.AllowPrivate {
			return ""
		}
		return "unique local address"
	}
	return ""
}

// blockedV4 covers the ranges net.IP has no predicate for. CGNAT is the one
// that matters operationally — it is where a cloud provider puts internal
// service endpoints that RFC1918 checks miss. The rest are ranges no reachable
// public host can live in, so rejecting them costs nothing and removes a set of
// addresses that only ever show up in a probe.
var blockedV4 = []struct {
	net     *net.IPNet
	reason  string
	private bool
}{
	{cidr("100.64.0.0/10"), "carrier-grade NAT address", true},
	{cidr("192.0.0.0/24"), "IETF protocol assignment address", false},
	{cidr("198.18.0.0/15"), "benchmarking address", false},
	{cidr("240.0.0.0/4"), "reserved address", false}, // includes 255.255.255.255
}

var (
	// nat64 is the well-known prefix an IPv6-only network uses to reach IPv4;
	// the last 32 bits are the v4 address verbatim.
	nat64 = cidr("64:ff9b::/96")
)

// embeddedV4 extracts the IPv4 address carried inside an IPv6 one, for the
// encodings net.IP does not unwrap by itself. Returns nil when there is none.
func embeddedV4(ip net.IP) net.IP {
	if len(ip) != net.IPv6len {
		return nil
	}
	switch {
	case isZeros(ip[:12]):
		// ::a.b.c.d, the deprecated IPv4-compatible form. Includes :: and ::1,
		// whose payloads (0.0.0.0 and 0.0.0.1) are handled or harmless.
		return net.IPv4(ip[12], ip[13], ip[14], ip[15]).To4()
	case nat64.Contains(ip):
		return net.IPv4(ip[12], ip[13], ip[14], ip[15]).To4()
	case ip[0] == 0x20 && ip[1] == 0x02:
		// 2002::/16, 6to4: the v4 address sits in the next four bytes.
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

// cidr parses a constant prefix, panicking on a typo. Every caller is a package
// level literal, so the panic can only fire at init of a broken build.
func cidr(s string) *net.IPNet {
	_, n, err := net.ParseCIDR(s)
	if err != nil {
		panic("urlguard: bad CIDR " + s + ": " + err.Error())
	}
	return n
}
