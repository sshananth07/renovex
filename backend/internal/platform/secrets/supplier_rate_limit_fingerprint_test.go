package secrets_test

import (
	"net/netip"
	"strings"
	"testing"

	"github.com/shananth/renovation-platform/backend/internal/platform/secrets"
)

func TestSupplierRateLimitIdentityFingerprintIsDeterministicAndGenerationBound(t *testing.T) {
	fingerprinter, err := secrets.NewSupplierRateLimitFingerprinter(
		encodedSupplierAccessKey(21))
	if err != nil {
		t.Fatalf("constructing fingerprinter: %v", err)
	}

	first, err := fingerprinter.FingerprintIdentity(
		"company-1", "supplier-1", "recipient@supplier.test", "invitation-1", 3)
	if err != nil {
		t.Fatalf("fingerprinting identity: %v", err)
	}
	second, _ := fingerprinter.FingerprintIdentity(
		"company-1", "supplier-1", "recipient@supplier.test", "invitation-1", 3)
	nextGeneration, _ := fingerprinter.FingerprintIdentity(
		"company-1", "supplier-1", "recipient@supplier.test", "invitation-1", 4)

	if first != second {
		t.Fatal("same identity did not produce a deterministic fingerprint")
	}
	if first == nextGeneration {
		t.Fatal("a new invitation generation reused the previous rate-limit scope")
	}
	if strings.Contains(first, "company") || strings.Contains(first, "recipient") {
		t.Fatalf("fingerprint %q contains raw identity input", first)
	}
}

func TestSupplierClientAddressFingerprintNormalizesIPv4MappedIPv6(t *testing.T) {
	fingerprinter, err := secrets.NewSupplierRateLimitFingerprinter(
		encodedSupplierAccessKey(22))
	if err != nil {
		t.Fatalf("constructing fingerprinter: %v", err)
	}

	ipv4, err := fingerprinter.FingerprintClientAddress(netip.MustParseAddr("192.0.2.10"))
	if err != nil {
		t.Fatalf("fingerprinting IPv4: %v", err)
	}
	mapped, err := fingerprinter.FingerprintClientAddress(
		netip.MustParseAddr("::ffff:192.0.2.10"))
	if err != nil {
		t.Fatalf("fingerprinting mapped IPv6: %v", err)
	}
	ipv6, err := fingerprinter.FingerprintClientAddress(netip.MustParseAddr("2001:db8::10"))
	if err != nil {
		t.Fatalf("fingerprinting IPv6: %v", err)
	}

	if ipv4 != mapped {
		t.Fatalf("IPv4 %q and its mapped form %q produced different fingerprints", ipv4, mapped)
	}
	if ipv4 == ipv6 {
		t.Fatal("unrelated IPv4 and IPv6 addresses produced the same test fingerprint")
	}
}

func TestSupplierRateLimitFingerprinterFailsClosedOnInvalidInputAndKey(t *testing.T) {
	if _, err := secrets.NewSupplierRateLimitFingerprinter("not-base64"); err == nil {
		t.Fatal("malformed key was accepted")
	}
	fingerprinter, err := secrets.NewSupplierRateLimitFingerprinter(
		encodedSupplierAccessKey(23))
	if err != nil {
		t.Fatalf("constructing fingerprinter: %v", err)
	}
	if _, err := fingerprinter.FingerprintIdentity(
		"", "supplier-1", "recipient@supplier.test", "invitation-1", 1); err == nil {
		t.Fatal("incomplete rate-limit identity was accepted")
	}
	if _, err := fingerprinter.FingerprintClientAddress(netip.Addr{}); err == nil {
		t.Fatal("invalid client address was accepted")
	}
}

func TestSupplierResendFingerprintIsChallengeScopedAndPurposeSeparated(t *testing.T) {
	fingerprinter, err := secrets.NewSupplierRateLimitFingerprinter(
		encodedSupplierAccessKey(24))
	if err != nil {
		t.Fatalf("constructing fingerprinter: %v", err)
	}

	first, err := fingerprinter.FingerprintChallengeResend(
		"company-1", "challenge-1")
	if err != nil {
		t.Fatalf("fingerprinting challenge resend: %v", err)
	}
	repeated, _ := fingerprinter.FingerprintChallengeResend(
		"company-1", "challenge-1")
	otherChallenge, _ := fingerprinter.FingerprintChallengeResend(
		"company-1", "challenge-2")
	identity, _ := fingerprinter.FingerprintIdentity(
		"company-1", "supplier-1", "recipient@supplier.test", "challenge-1", 1)

	if first != repeated {
		t.Fatal("same challenge resend scope was not deterministic")
	}
	if first == otherChallenge {
		t.Fatal("different challenges shared one resend rate scope")
	}
	if first == identity {
		t.Fatal("resend and challenge-creation fingerprints lacked domain separation")
	}
}
