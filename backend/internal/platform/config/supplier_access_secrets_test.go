package config

import (
	"net/netip"
	"strings"
	"testing"
)

func configureSupplierAccessSecrets(t *testing.T) {
	t.Helper()
	t.Setenv("SUPPLIER_VERIFICATION_CODE_ACTIVE_VERSION", "2")
	t.Setenv("SUPPLIER_VERIFICATION_CODE_KEY_V1", validKey())
	t.Setenv("SUPPLIER_VERIFICATION_CODE_KEY_V2", validKey())
	t.Setenv("SUPPLIER_SESSION_TOKEN_ACTIVE_VERSION", "1")
	t.Setenv("SUPPLIER_SESSION_TOKEN_KEY_V1", validKey())
	t.Setenv("SUPPLIER_RATE_LIMIT_FINGERPRINT_KEY", validKey())
}

func TestLoadFromEnvReadsDedicatedSupplierAccessKeysAndTrustedProxies(t *testing.T) {
	requiredEnv(t)
	configureSupplierAccessSecrets(t)
	t.Setenv("TRUSTED_PROXY_CIDRS", "10.0.0.0/8, 2001:db8::/32")

	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("loading config: %v", err)
	}

	if cfg.SupplierVerificationCodeActiveVersion != 2 ||
		len(cfg.SupplierVerificationCodeKeys) != 2 {
		t.Fatalf("verification-code keyring = %d/%d keys",
			cfg.SupplierVerificationCodeActiveVersion,
			len(cfg.SupplierVerificationCodeKeys))
	}
	if cfg.SupplierSessionTokenActiveVersion != 1 ||
		len(cfg.SupplierSessionTokenKeys) != 1 {
		t.Fatalf("session-token keyring = %d/%d keys",
			cfg.SupplierSessionTokenActiveVersion,
			len(cfg.SupplierSessionTokenKeys))
	}
	if cfg.SupplierRateLimitFingerprintKey != validKey() {
		t.Fatal("rate-limit fingerprint key was not loaded")
	}
	want := []netip.Prefix{
		netip.MustParsePrefix("10.0.0.0/8"),
		netip.MustParsePrefix("2001:db8::/32"),
	}
	if len(cfg.TrustedProxyCIDRs) != len(want) {
		t.Fatalf("trusted proxies = %v, want %v", cfg.TrustedProxyCIDRs, want)
	}
	for i := range want {
		if cfg.TrustedProxyCIDRs[i] != want[i] {
			t.Fatalf("trusted proxy %d = %v, want %v", i, cfg.TrustedProxyCIDRs[i], want[i])
		}
	}
}

func TestLoadFromEnvRejectsMalformedSupplierAccessSecrets(t *testing.T) {
	tests := map[string]func(*testing.T){
		"code key without active version": func(t *testing.T) {
			t.Setenv("SUPPLIER_VERIFICATION_CODE_KEY_V1", validKey())
		},
		"missing active code key": func(t *testing.T) {
			t.Setenv("SUPPLIER_VERIFICATION_CODE_ACTIVE_VERSION", "2")
			t.Setenv("SUPPLIER_VERIFICATION_CODE_KEY_V1", validKey())
		},
		"malformed session key": func(t *testing.T) {
			t.Setenv("SUPPLIER_SESSION_TOKEN_ACTIVE_VERSION", "1")
			t.Setenv("SUPPLIER_SESSION_TOKEN_KEY_V1", "not-base64")
		},
		"undersized fingerprint key": func(t *testing.T) {
			t.Setenv("SUPPLIER_RATE_LIMIT_FINGERPRINT_KEY", "c2hvcnQ=")
		},
		"malformed trusted proxy": func(t *testing.T) {
			t.Setenv("TRUSTED_PROXY_CIDRS", "10.0.0.0/8,not-a-cidr")
		},
	}
	for name, configure := range tests {
		t.Run(name, func(t *testing.T) {
			requiredEnv(t)
			configure(t)
			if _, err := LoadFromEnv(); err == nil {
				t.Fatal("expected startup configuration error")
			}
		})
	}
}

func TestLoadFromEnvLeavesSupplierAccessSecretsAbsentWhenPhaseDIsUnconfigured(t *testing.T) {
	requiredEnv(t)

	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("loading unconfigured Phase D values: %v", err)
	}
	if cfg.SupplierVerificationCodeActiveVersion != 0 ||
		cfg.SupplierSessionTokenActiveVersion != 0 ||
		len(cfg.SupplierVerificationCodeKeys) != 0 ||
		len(cfg.SupplierSessionTokenKeys) != 0 ||
		cfg.SupplierRateLimitFingerprintKey != "" ||
		len(cfg.TrustedProxyCIDRs) != 0 {
		t.Fatalf("unexpected Phase D configuration: %#v", cfg)
	}
}

func TestSupplierAccessKeyEnvironmentNamesRemainPurposeSeparated(t *testing.T) {
	requiredEnv(t)
	configureSupplierAccessSecrets(t)

	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("loading config: %v", err)
	}
	if strings.HasPrefix(cfg.SupplierRateLimitFingerprintKey, "SUPPLIER_") {
		t.Fatal("configuration retained an environment variable name instead of its value")
	}
	if cfg.SupplierVerificationCodeKeys == nil || cfg.SupplierSessionTokenKeys == nil {
		t.Fatal("dedicated keyrings were not loaded independently")
	}
}
