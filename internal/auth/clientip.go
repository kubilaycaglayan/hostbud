package auth

import (
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strings"
)

// ParsePrefixes parses a comma-separated list of CIDRs or addresses.
func ParsePrefixes(s string) ([]netip.Prefix, error) {
	var out []netip.Prefix
	for f := range strings.SplitSeq(s, ",") {
		f = strings.TrimSpace(f)
		if f == "" {
			continue
		}
		if !strings.Contains(f, "/") {
			a, err := netip.ParseAddr(f)
			if err != nil {
				return nil, fmt.Errorf("invalid address %q", f)
			}
			out = append(out, netip.PrefixFrom(a, a.BitLen()))
			continue
		}
		p, err := netip.ParsePrefix(f)
		if err != nil {
			return nil, fmt.Errorf("invalid CIDR %q", f)
		}
		out = append(out, p.Masked())
	}
	return out, nil
}

func trusted(a netip.Addr, proxies []netip.Prefix) bool {
	for _, p := range proxies {
		if p.Contains(a.Unmap()) {
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
	return a.Unmap(), err == nil
}

// ClientIP returns the client's address. X-Forwarded-For is honoured only
// when the direct peer is a trusted proxy (Caddy), and then the rightmost
// entry that isn't itself a trusted proxy wins; clients can't spoof it.
func ClientIP(r *http.Request, proxies []netip.Prefix) string {
	peer, ok := remoteAddr(r)
	if !ok {
		return r.RemoteAddr
	}
	if !trusted(peer, proxies) {
		return peer.String()
	}
	hops := strings.Split(strings.Join(r.Header.Values("X-Forwarded-For"), ","), ",")
	for i := len(hops) - 1; i >= 0; i-- {
		a, err := netip.ParseAddr(strings.TrimSpace(hops[i]))
		if err != nil {
			break
		}
		if !trusted(a, proxies) {
			return a.Unmap().String()
		}
	}
	return peer.String()
}

// IsHTTPS reports whether the client connection is HTTPS (directly, or as
// reported by a trusted proxy), which decides the cookie's Secure flag.
func IsHTTPS(r *http.Request, proxies []netip.Prefix) bool {
	if r.TLS != nil {
		return true
	}
	peer, ok := remoteAddr(r)
	return ok && trusted(peer, proxies) && strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}
