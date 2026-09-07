package egress

import (
	"fmt"
	"net"
	"net/netip"
	"syscall"

	"github.com/0xsj/overwatch-backend/pkg/errors"
)

var (
	cgnat       = netip.MustParsePrefix("100.64.0.0/10")
	benchmark   = netip.MustParsePrefix("198.18.0.0/15")
	nat64       = netip.MustParsePrefix("64:ff9b::/96")
	uniqueLocal = netip.MustParsePrefix("fc00::/7")
)

type Policy struct {
	AllowLoopback bool
	AllowPrivate  bool
	Allow         []netip.Prefix
	Deny          []netip.Prefix
}

type Guard struct {
	policy Policy
}

func NewGuard(p Policy) *Guard { return &Guard{policy: p} }

func (g *Guard) Allows(addr netip.Addr) error {
	if !addr.IsValid() {
		return fmt.Errorf("egress: %w: not an address", ErrBlocked)
	}
	addr = addr.Unmap()

	for _, deny := range g.policy.Deny {
		if deny.Contains(addr) {
			return refuse(addr, "denied by policy")
		}
	}
	for _, allow := range g.policy.Allow {
		if allow.Contains(addr) {
			return nil
		}
	}

	switch {
	case addr.IsUnspecified():
		return refuse(addr, "unspecified")
	case addr.IsLoopback():
		if g.policy.AllowLoopback {
			return nil
		}
		return refuse(addr, "loopback")
	case addr.IsLinkLocalUnicast() || addr.IsLinkLocalMulticast():
		return refuse(addr, "link-local — cloud metadata lives here")
	case addr.IsMulticast() || addr.IsInterfaceLocalMulticast():
		return refuse(addr, "multicast")
	case nat64.Contains(addr):
		return refuse(addr, "NAT64 — an IPv4 address wearing IPv6")
	case addr.IsPrivate() || uniqueLocal.Contains(addr):
		if g.policy.AllowPrivate {
			return nil
		}
		return refuse(addr, "private")
	case cgnat.Contains(addr):
		return refuse(addr, "carrier-grade NAT")
	case benchmark.Contains(addr):
		return refuse(addr, "benchmarking range")
	}
	return nil
}

func (g *Guard) Control(_, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("egress: %w: %s is not an address and a port", ErrBlocked, address)
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		// Control is handed a RESOLVED address. A name here means something
		// dialled without resolving, which this guard cannot judge.
		return fmt.Errorf("egress: %w: %s was not resolved before dialling", ErrBlocked, host)
	}
	return g.Allows(addr)
}

func refuse(addr netip.Addr, why string) error {
	return errors.Wrap(fmt.Errorf("egress: %w", ErrBlocked), errors.Forbidden,
		"refused "+addr.String()).WithDetail("reason", why).WithDetail("address", addr.String())
}
