package supplieraccess

import (
	"fmt"
	"net/netip"
	"strings"
)

// ResolveSupplierClientAddress selects the authoritative client address for
// rate limiting. Forwarding data is ignored unless the direct TCP peer belongs
// to an explicitly trusted proxy network.
func ResolveSupplierClientAddress(directPeer, forwardedFor string,
	trustedProxyCIDRs []netip.Prefix) (netip.Addr, error) {

	direct, err := parsePeerAddress(directPeer)
	if err != nil {
		return netip.Addr{}, err
	}
	if !addressInPrefixes(direct, trustedProxyCIDRs) ||
		strings.TrimSpace(forwardedFor) == "" {
		return direct, nil
	}

	parts := strings.Split(forwardedFor, ",")
	chain := make([]netip.Addr, 0, len(parts))
	for _, part := range parts {
		value := strings.TrimSpace(part)
		address, err := netip.ParseAddr(value)
		if err != nil {
			return netip.Addr{}, fmt.Errorf(
				"supplieraccess: malformed trusted forwarding address %q", value)
		}
		chain = append(chain, address.Unmap())
	}

	// Walk from the application outward. A proxy may identify only the hop that
	// connected to it; stop as soon as that hop is not in a trusted network.
	current := direct
	for index := len(chain) - 1; index >= 0 &&
		addressInPrefixes(current, trustedProxyCIDRs); index-- {
		current = chain[index]
	}
	return current.Unmap(), nil
}

func parsePeerAddress(value string) (netip.Addr, error) {
	value = strings.TrimSpace(value)
	if addressPort, err := netip.ParseAddrPort(value); err == nil {
		return addressPort.Addr().Unmap(), nil
	}
	if address, err := netip.ParseAddr(value); err == nil {
		return address.Unmap(), nil
	}
	return netip.Addr{}, fmt.Errorf(
		"supplieraccess: malformed direct peer address %q", value)
}

func addressInPrefixes(address netip.Addr, prefixes []netip.Prefix) bool {
	for _, prefix := range prefixes {
		if prefix.Contains(address) {
			return true
		}
	}
	return false
}
