package httpx

import (
	"net"
	"net/http"
	"net/netip"
	"strings"

	"github.com/0xsj/overwatch-backend/pkg/errors"
)

// TrustedProxies is the set of hops whose X-Forwarded-For entries are believed.
// The zero value trusts nothing, which is the correct default: with no proxy in
// front, the header is pure caller input.
type TrustedProxies []netip.Prefix

func ParseTrustedProxies(cidrs []string) (TrustedProxies, error) {
	out := make(TrustedProxies, 0, len(cidrs))
	for _, s := range cidrs {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if p, err := netip.ParsePrefix(s); err == nil {
			out = append(out, p)
			continue
		}
		// A bare address is a /32 or /128. Operators write both, and refusing
		// one of them moves a configuration decision into a syntax rule.
		a, err := netip.ParseAddr(s)
		if err != nil {
			return nil, errors.Newf(errors.Invalid,
				"trusted proxy %q is neither a CIDR block nor an address", s)
		}
		out = append(out, netip.PrefixFrom(a, a.BitLen()))
	}
	return out, nil
}

func (t TrustedProxies) trusts(a netip.Addr) bool {
	a = a.Unmap()
	for _, p := range t {
		if p.Contains(a) {
			return true
		}
	}
	return false
}

// ClientIP walks right to left from the peer and stops at the first hop that is
// not trusted. That hop is the client. Walking the other way returns the first
// value the caller wrote, which is the one value they fully control.
func ClientIP(r *http.Request, trusted TrustedProxies) netip.Addr {
	peer := peerAddr(r)
	if len(trusted) == 0 || !peer.IsValid() || !trusted.trusts(peer) {
		return peer
	}

	hops := forwardedFor(r)
	for i := len(hops) - 1; i >= 0; i-- {
		a, ok := parseHop(hops[i])
		if !ok {
			// A malformed hop ends the walk. Skipping it would let a caller
			// hide the hop before it behind a value we could not read.
			return peer
		}
		if !trusted.trusts(a) {
			return a
		}
		peer = a
	}
	return peer
}

func forwardedFor(r *http.Request) []string {
	var out []string
	// Several headers are one chain, in order. net/http keeps them separate.
	for _, v := range r.Header.Values("X-Forwarded-For") {
		for _, part := range strings.Split(v, ",") {
			if part = strings.TrimSpace(part); part != "" {
				out = append(out, part)
			}
		}
	}
	return out
}

func parseHop(s string) (netip.Addr, bool) {
	if a, err := netip.ParseAddr(s); err == nil {
		return a.Unmap(), true
	}
	if h, _, err := net.SplitHostPort(s); err == nil {
		if a, err := netip.ParseAddr(h); err == nil {
			return a.Unmap(), true
		}
	}
	return netip.Addr{}, false
}

func peerAddr(r *http.Request) netip.Addr {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	a, err := netip.ParseAddr(host)
	if err != nil {
		return netip.Addr{}
	}
	return a.Unmap()
}
