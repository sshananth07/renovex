package config

import (
	"encoding/base64"
	"strings"
	"testing"
)

// M8 design spec §6.1A: the invitation-secret keyring is loaded from a
// versioned set of environment variables, and a missing, malformed or
// undersized key must FAIL APPLICATION STARTUP rather than being replaced by a
// generated key — a per-boot key would make every previously issued invitation
// link unreproducible.

func validKey() string {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	return base64.StdEncoding.EncodeToString(key)
}

// requiredEnv sets the variables every LoadFromEnv call needs, so these tests
// exercise only the keyring rules.
func requiredEnv(t *testing.T) {
	t.Helper()
	t.Setenv("MONGO_URI", "mongodb://localhost:27017")
	t.Setenv("MONGO_DATABASE", "renovation_platform_test")
}

func TestLoadFromEnvReadsTheActiveVersionAndKeys(t *testing.T) {
	requiredEnv(t)
	t.Setenv("INVITATION_SECRET_ACTIVE_VERSION", "1")
	t.Setenv("INVITATION_SECRET_KEY_V1", validKey())

	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.InvitationSecretActiveVersion != 1 {
		t.Errorf("InvitationSecretActiveVersion = %d, want 1", cfg.InvitationSecretActiveVersion)
	}
	if got := cfg.InvitationSecretKeys[1]; got != validKey() {
		t.Errorf("InvitationSecretKeys[1] = %q, want the configured key", got)
	}
}

// Multiple versions must load together: an old key stays configured while any
// invitation still references its version (§6.1A).
func TestLoadFromEnvReadsMultipleKeyVersions(t *testing.T) {
	requiredEnv(t)
	t.Setenv("INVITATION_SECRET_ACTIVE_VERSION", "2")
	t.Setenv("INVITATION_SECRET_KEY_V1", validKey())
	t.Setenv("INVITATION_SECRET_KEY_V2", validKey())

	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.InvitationSecretActiveVersion != 2 {
		t.Errorf("InvitationSecretActiveVersion = %d, want 2", cfg.InvitationSecretActiveVersion)
	}
	if len(cfg.InvitationSecretKeys) != 2 {
		t.Errorf("expected 2 configured key versions, got %d", len(cfg.InvitationSecretKeys))
	}
}

// The active version must have a key. Starting without one would let the
// application boot and then fail on the first invitation created.
func TestLoadFromEnvFailsWhenTheActiveVersionHasNoKey(t *testing.T) {
	requiredEnv(t)
	t.Setenv("INVITATION_SECRET_ACTIVE_VERSION", "2")
	t.Setenv("INVITATION_SECRET_KEY_V1", validKey())

	_, err := LoadFromEnv()

	if err == nil {
		t.Fatal("expected an error when the active version has no configured key")
	}
	if !strings.Contains(err.Error(), "INVITATION_SECRET_KEY_V2") {
		t.Errorf("error should name the missing variable, got %q", err)
	}
}

func TestLoadFromEnvFailsOnUndersizedKey(t *testing.T) {
	requiredEnv(t)
	t.Setenv("INVITATION_SECRET_ACTIVE_VERSION", "1")
	t.Setenv("INVITATION_SECRET_KEY_V1", base64.StdEncoding.EncodeToString(make([]byte, 16)))

	_, err := LoadFromEnv()

	if err == nil {
		t.Fatal("expected an error for a key shorter than 32 decoded bytes")
	}
}

func TestLoadFromEnvFailsOnMalformedKey(t *testing.T) {
	requiredEnv(t)
	t.Setenv("INVITATION_SECRET_ACTIVE_VERSION", "1")
	t.Setenv("INVITATION_SECRET_KEY_V1", "not!valid!base64!")

	_, err := LoadFromEnv()

	if err == nil {
		t.Fatal("expected an error for a malformed base64 key")
	}
}

func TestLoadFromEnvFailsOnNonNumericActiveVersion(t *testing.T) {
	requiredEnv(t)
	t.Setenv("INVITATION_SECRET_ACTIVE_VERSION", "latest")
	t.Setenv("INVITATION_SECRET_KEY_V1", validKey())

	_, err := LoadFromEnv()

	if err == nil {
		t.Fatal("expected an error for a non-numeric active version")
	}
}

// The keyring is only required once it is in use. An unconfigured keyring loads
// as absent so existing M0-M7 tooling and tests keep booting; the composition
// root is what refuses to construct M8 without it.
func TestLoadFromEnvLeavesTheKeyringAbsentWhenUnconfigured(t *testing.T) {
	requiredEnv(t)
	t.Setenv("INVITATION_SECRET_ACTIVE_VERSION", "")
	t.Setenv("INVITATION_SECRET_KEY_V1", "")

	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(cfg.InvitationSecretKeys) != 0 {
		t.Errorf("expected no configured keys, got %d", len(cfg.InvitationSecretKeys))
	}
	if cfg.InvitationSecretActiveVersion != 0 {
		t.Errorf("expected active version 0 when unconfigured, got %d",
			cfg.InvitationSecretActiveVersion)
	}
}

// A configured key with no active version is a misconfiguration, not an
// intentional opt-out — it must not silently load as absent.
func TestLoadFromEnvFailsWhenKeysExistWithoutAnActiveVersion(t *testing.T) {
	requiredEnv(t)
	t.Setenv("INVITATION_SECRET_ACTIVE_VERSION", "")
	t.Setenv("INVITATION_SECRET_KEY_V1", validKey())

	_, err := LoadFromEnv()

	if err == nil {
		t.Fatal("expected an error when keys are configured without an active version")
	}
}
