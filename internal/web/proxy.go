package web

import (
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strings"
)

// proxyList holds the addresses of reverse proxies whose forwarding headers
// are believed. Anyone else connecting directly can send these headers freely.
type proxyList []netip.Prefix

func parseProxies(raw string) (proxyList, error) {
	var out proxyList
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if p, err := netip.ParsePrefix(part); err == nil {
			out = append(out, p.Masked())
			continue
		}
		a, err := netip.ParseAddr(part)
		if err != nil {
			return nil, fmt.Errorf("TRUSTED_PROXIES: %q is not an address or range", part)
		}
		out = append(out, netip.PrefixFrom(a.Unmap(), a.Unmap().BitLen()))
	}
	return out, nil
}

func (p proxyList) contains(addr netip.Addr) bool {
	addr = addr.Unmap()
	for _, prefix := range p {
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
}

func remoteAddr(r *http.Request) (netip.Addr, bool) {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	a, err := netip.ParseAddr(host)
	return a, err == nil
}

func (p proxyList) fromProxy(r *http.Request) bool {
	a, ok := remoteAddr(r)
	return ok && p.contains(a)
}

// clientIP returns the visitor's address. Forwarding headers count only when
// the connection comes from a trusted proxy, and the chain is read from the
// right so entries a visitor added themselves are skipped.
func (p proxyList) clientIP(r *http.Request) string {
	direct := r.RemoteAddr
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		direct = host
	}
	if !p.fromProxy(r) {
		return direct
	}
	hops := strings.Split(strings.Join(r.Header.Values("X-Forwarded-For"), ","), ",")
	for i := len(hops) - 1; i >= 0; i-- {
		hop := strings.TrimSpace(hops[i])
		a, err := netip.ParseAddr(hop)
		if err != nil {
			continue
		}
		if p.contains(a) {
			continue
		}
		return a.Unmap().String()
	}
	if ip, err := netip.ParseAddr(strings.TrimSpace(r.Header.Get("X-Real-IP"))); err == nil {
		return ip.Unmap().String()
	}
	return direct
}

func (p proxyList) https(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	if !p.fromProxy(r) {
		return false
	}
	proto := r.Header.Get("X-Forwarded-Proto")
	if i := strings.IndexByte(proto, ','); i >= 0 {
		proto = proto[:i]
	}
	return strings.EqualFold(strings.TrimSpace(proto), "https")
}
