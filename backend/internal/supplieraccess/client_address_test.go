package supplieraccess_test

import (
	"net/netip"
	"testing"

	"github.com/shananth/renovation-platform/backend/internal/supplieraccess"
)

func TestResolveSupplierClientAddressTrustsForwardingOnlyFromConfiguredProxy(t *testing.T) {
	trusted := []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}

	address, err := supplieraccess.ResolveSupplierClientAddress(
		"198.51.100.10:443", "203.0.113.9, 10.1.2.3", trusted)
	if err != nil {
		t.Fatalf("untrusted direct peer: %v", err)
	}
	if address.String() != "198.51.100.10" {
		t.Fatalf("untrusted peer resolved to %v", address)
	}

	address, err = supplieraccess.ResolveSupplierClientAddress(
		"10.9.8.7:443", "203.0.113.9, 10.1.2.3", trusted)
	if err != nil {
		t.Fatalf("trusted proxy chain: %v", err)
	}
	if address.String() != "203.0.113.9" {
		t.Fatalf("trusted chain resolved to %v, want first untrusted hop", address)
	}
}

func TestResolveSupplierClientAddressRejectsMalformedAuthoritativeChain(t *testing.T) {
	trusted := []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}
	for _, test := range []struct {
		direct, forwarded string
	}{
		{"not-a-peer", ""},
		{"10.1.2.3:443", "203.0.113.9, malformed"},
		{"[2001:db8::1", "203.0.113.9"},
	} {
		if _, err := supplieraccess.ResolveSupplierClientAddress(
			test.direct, test.forwarded, trusted); err == nil {
			t.Errorf("accepted direct=%q forwarded=%q", test.direct, test.forwarded)
		}
	}
}

func TestResolveSupplierClientAddressNormalizesMappedIPv4(t *testing.T) {
	address, err := supplieraccess.ResolveSupplierClientAddress(
		"[::ffff:192.0.2.44]:443", "", nil)
	if err != nil {
		t.Fatalf("resolving mapped address: %v", err)
	}
	if address.String() != "192.0.2.44" {
		t.Fatalf("mapped address normalized to %v", address)
	}
}
