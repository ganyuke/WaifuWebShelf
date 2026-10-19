// Package netguard keeps outgoing fetches to the public internet.
package netguard

import (
	"fmt"
	"net"
	"net/netip"
	"syscall"
)

// special lists reserved ranges that IsGlobalUnicast and IsPrivate let
// through, including IPv6 ranges that carry an IPv4 address inside them.
var special = func() []netip.Prefix {
	var out []netip.Prefix
	for _, p := range []string{
		"0.0.0.0/8", "100.64.0.0/10", "192.0.0.0/24", "192.0.2.0/24", "192.88.99.0/24",
		"198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "240.0.0.0/4",
		"64:ff9b::/96", "64:ff9b:1::/48", "100::/64", "2001::/23", "2001:db8::/32", "2002::/16",
	} {
		out = append(out, netip.MustParsePrefix(p))
	}
	return out
}()

// PublicOnly is a net.Dialer Control function that refuses to connect to
// loopback, private, link-local, and other reserved addresses, so a fetch
// started from a link can never reach the server itself or its network.
func PublicOnly(_, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	ip, err := netip.ParseAddr(host)
	if err != nil {
		return err
	}
	ip = ip.Unmap()
	ok := ip.IsGlobalUnicast() && !ip.IsPrivate()
	for _, p := range special {
		if p.Contains(ip) {
			ok = false
		}
	}
	if !ok {
		return fmt.Errorf("%s is not a public address", host)
	}
	return nil
}
