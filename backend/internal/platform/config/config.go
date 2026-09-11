// Package config loads typed application configuration from environment
// variables (optionally backed by a local .env file for development).
package config

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config holds all application configuration. Fields are populated from
// environment variables; see .env.example at the repo root for the full
// set of supported variables and their defaults.
type Config struct {
	AppEnv   string
	HTTPPort string

	MongoURI      string
	MongoDatabase string

	// JWTAccessSecret and JWTRefreshSecret are reserved for Milestone 1
	// authentication and are not used by Milestone 0.
	JWTAccessSecret  string
	JWTRefreshSecret string

	SMTPHost string
	SMTPPort string
	SMTPFrom string

	StorageLocalPath string

	AIProvider string

	// AIServiceURL, AIInternalToken, and AIServiceTimeout configure the
	// authenticated Go -> Python AI service HTTP client (M8.5B-A design doc
	// §21.1). AIServiceURL/AIInternalToken are empty together when the AI
	// feature is not configured; LoadFromEnv requires both or neither.
	// AIInternalToken never reaches Next.js, MongoDB, public OpenAPI, or
	// Gemini — it is a Go<->Python-only secret.
	AIServiceURL     string
	AIInternalToken  string
	AIServiceTimeout time.Duration

	// ExternalAPIBaseURL is the absolute base URL used to build the client
	// access API link returned alongside a freshly minted token. It addresses
	// this API's own external endpoints for development and integration
	// testing — it is NOT a finished client portal link (M6 design spec §4.3).
	ExternalAPIBaseURL string

	// InvitationSecretActiveVersion and InvitationSecretKeys configure the M8
	// Supplier Invitation secret keyring (M8 design spec §6.1A).
	//
	// Keys are base64-encoded and keyed by version. Multiple versions coexist
	// deliberately: an invitation records the version that derived its secret,
	// so an old key must stay configured while any invitation still references
	// it. Zero/empty means unconfigured — LoadFromEnv leaves the keyring absent
	// rather than inventing one, because a key generated per boot would make
	// every previously issued invitation link unreproducible.
	InvitationSecretActiveVersion int
	InvitationSecretKeys          map[int]string

	// Supplier access credentials use dedicated key material for each purpose.
	// Empty values mean Phase D is not configured; the composition root refuses
	// to construct the Phase D service in that state, preserving older tooling
	// while still making a shipping API fail startup.
	SupplierVerificationCodeActiveVersion int
	SupplierVerificationCodeKeys          map[int]string
	SupplierSessionTokenActiveVersion     int
	SupplierSessionTokenKeys              map[int]string
	SupplierRateLimitFingerprintKey       string

	// VisualAssetCapabilityActiveVersion/Keys sign RP4D's short-lived
	// asset-read bearer capabilities. Unlike the supplier keyrings above,
	// this is NOT optional once any code path calls
	// Service.CreateVisualAssetAccess — a missing/malformed key fails
	// LoadFromEnv outright (composition/services.go refuses to wire the
	// local read-access provider without it), matching
	// InvitationSecretKeys's "never silently serve unsigned content"
	// precedent rather than the supplier keyrings' "absent means the
	// feature isn't configured yet" precedent.
	VisualAssetCapabilityActiveVersion int
	VisualAssetCapabilityKeys          map[int]string
	// VisualAssetAccessTTL is how long a minted asset-read capability
	// remains valid. Defaults to 10 minutes if unset (RP4D §22).
	VisualAssetAccessTTL time.Duration

	// HuggingFaceSpaceURL/HuggingFaceToken configure the authenticated
	// Go -> Hugging Face Gradio HTTP client for the private
	// renovex-hunyuan3d-runtime Space (RP4E0). Empty together when asset
	// generation is not configured; LoadFromEnv requires both or
	// neither. HuggingFaceToken never reaches the browser — server-side
	// only, exactly like AIInternalToken.
	HuggingFaceSpaceURL    string
	HuggingFaceToken       string
	HunyuanProviderTimeout time.Duration

	// AssetGenerationSourceCapabilityActiveVersion/Keys sign RP4E0's
	// short-lived source-image-read bearer capabilities the Go backend
	// hands to the Hunyuan provider — same optional-until-referenced
	// loading as VisualAssetCapabilityKeys, fails closed only at the
	// point the local provider would actually be constructed.
	AssetGenerationSourceCapabilityActiveVersion int
	AssetGenerationSourceCapabilityKeys          map[int]string

	// AssetGenerationLeaseTTL/HeartbeatInterval configure
	// ProcessOneAssetGenerationJob's worker lease lifecycle. Zero means
	// "use the documented default" (spatial.defaultAssetGeneration*).
	AssetGenerationLeaseTTL          time.Duration
	AssetGenerationHeartbeatInterval time.Duration

	// AISpatialServiceTimeout/SpatialDesignContextRadiusMeters/
	// AssetGenerationRuntimeNotice* configure RP4E1's spatial design
	// reasoning surface. Deliberately no GLM_API_KEY field here — Go never
	// reads the GLM provider key; only the Python ai-service owns it
	// (RP4E1 plan Task 8 Step 2, mirroring GEMINI_API_KEY's existing "never
	// a Go config field" precedent).
	AISpatialServiceTimeout             time.Duration
	SpatialDesignContextRadiusMeters    float64
	AssetGenerationRuntimeNoticeEnabled bool
	AssetGenerationRuntimeNoticeText    string

	// TrustedProxyCIDRs controls whether forwarding headers may influence the
	// client-address rate scope. An empty list means the direct peer is always
	// authoritative.
	TrustedProxyCIDRs []netip.Prefix

	// AllowedOrigins is the exact set of browser origins permitted to make
	// credentialed CORS requests and to pass the refresh/logout Origin guard.
	// Empty is valid outside production (no browser frontend configured yet);
	// production requires at least one (see LoadFromEnv).
	AllowedOrigins AllowedOrigins

	// RefreshCookieSecure controls the refresh-token cookie's Secure
	// attribute. Defaults to true; only development/test environments may set
	// it false (over plain http://localhost), and production may not.
	RefreshCookieSecure bool

	// RefreshCookieSameSite controls the refresh-token cookie's SameSite
	// attribute. It defaults to Lax for local development, while deployments
	// with a cross-site browser frontend must explicitly select None.
	RefreshCookieSameSite http.SameSite

	// ObjectStoreProvider selects "local" (LocalObjectStore, three physical
	// roots preserved for dev) or "r2" (one shared R2ObjectStore instance
	// across every spatial adapter) — M8.5C plan: "R2 is the sole durable
	// object store in the tester deployment." R2* fields are required
	// together only when ObjectStoreProvider=r2.
	ObjectStoreProvider string
	R2AccountID         string
	R2AccessKeyID       string
	R2SecretAccessKey   string
	R2Bucket            string
	R2Endpoint          string

	// ReferenceImageMaxBytes bounds both the Go-side ReferenceImageClient's
	// response-read budget and (mirrored) the Python service's own
	// REFERENCE_IMAGE_MAX_BYTES — kept as separate configuration on each
	// side deliberately (Go never trusts Python's own self-reported bound).
	ReferenceImageMaxBytes int64

	// EmailProvider selects "smtp" (local Mailpit) or "resend" (tester/
	// production). EmailDeliveryMode selects "direct" or "test_sink" — the
	// M8.5C plan's central safety invariant: APP_ENV=production with
	// EmailDeliveryMode=test_sink is a FATAL configuration error, checked
	// unconditionally in LoadFromEnv below, never left to a caller to
	// remember.
	EmailProvider        string
	EmailDeliveryMode    string
	EmailTestSinkAddress string
	ResendAPIKey         string
	ResendFrom           string

	// SpatialWorkerToken protects the internal RP4E2/RP4E0 bounded-worker
	// routes (process-one endpoints) — a separate secret from
	// AIInternalToken (Go<->Python) since this guards Queue-consumer-or-
	// local-ticker<->Go instead.
	SpatialWorkerToken string
	// SpatialQueueEnqueueURL/Token configure Go's call into the Web
	// project's internal enqueue bridge (Gate 4) — both optional together;
	// their absence simply means Vercel Queue wake publishing is disabled
	// (the local in-process ticker remains the dev-mode dispatch path).
	SpatialQueueEnqueueURL   string
	SpatialQueueEnqueueToken string

	// ServerlessMode, when true, tells the composition root never to start
	// the local in-process asset-generation dispatch ticker — Vercel's
	// bounded runtime cannot host a long-lived background loop (M8.5C
	// plan's Vercel-safety requirement). cmd/api's own binary always sets
	// this false; api/index.go's Vercel entrypoint sets it true.
	ServerlessMode bool
}

// invitationSecretKeyPrefix is the environment-variable prefix carrying one
// base64-encoded key per version, e.g. INVITATION_SECRET_KEY_V1.
const invitationSecretKeyPrefix = "INVITATION_SECRET_KEY_V"

// maxInvitationSecretKeyVersion bounds the version scan. Versions advance only
// on deliberate key rotation, so this ceiling is far beyond any realistic
// deployment while keeping the scan finite.
const maxInvitationSecretKeyVersion = 64

// minInvitationSecretKeyBytes mirrors secrets.MinInvitationKeyBytes. It is
// duplicated rather than imported because config must not depend on any other
// platform package (import cycle risk at the configuration leaf).
const minInvitationSecretKeyBytes = 32

const (
	supplierVerificationCodeKeyPrefix        = "SUPPLIER_VERIFICATION_CODE_KEY_V"
	supplierSessionTokenKeyPrefix            = "SUPPLIER_SESSION_TOKEN_KEY_V"
	visualAssetCapabilityKeyPrefix           = "VISUAL_ASSET_CAPABILITY_KEY_V"
	assetGenerationSourceCapabilityKeyPrefix = "ASSET_GENERATION_SOURCE_CAPABILITY_KEY_V"
)

// defaultVisualAssetAccessTTL is used when VISUAL_ASSET_ACCESS_TTL is
// unset (RP4D §22's recommended local-verification default).
const defaultVisualAssetAccessTTL = 10 * time.Minute

// loadInvitationSecretKeyring reads and validates the keyring variables.
//
// Every validation failure is returned to the caller, which fails application
// startup (§6.1A). Silently accepting a malformed or undersized key would
// weaken every invitation link the deployment issues.
func loadInvitationSecretKeyring() (int, map[int]string, error) {
	keys := make(map[int]string)
	for version := 1; version <= maxInvitationSecretKeyVersion; version++ {
		name := fmt.Sprintf("%s%d", invitationSecretKeyPrefix, version)
		encoded := os.Getenv(name)
		if encoded == "" {
			continue
		}
		decoded, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil {
			return 0, nil, fmt.Errorf("config: %s is not valid base64: %w", name, err)
		}
		if len(decoded) < minInvitationSecretKeyBytes {
			return 0, nil, fmt.Errorf("config: %s decodes to %d bytes; at least %d random "+
				"bytes are required", name, len(decoded), minInvitationSecretKeyBytes)
		}
		keys[version] = encoded
	}

	rawActive := os.Getenv("INVITATION_SECRET_ACTIVE_VERSION")
	if rawActive == "" {
		// Configured keys with no active version is a misconfiguration, not an
		// intentional opt-out: the deployment clearly meant to enable invitation
		// secrets and would otherwise boot unable to derive any.
		if len(keys) > 0 {
			return 0, nil, fmt.Errorf("config: INVITATION_SECRET_ACTIVE_VERSION is required " +
				"when any INVITATION_SECRET_KEY_V* is configured")
		}
		return 0, keys, nil
	}

	active, err := strconv.Atoi(rawActive)
	if err != nil {
		return 0, nil, fmt.Errorf("config: INVITATION_SECRET_ACTIVE_VERSION must be an integer, got %q",
			rawActive)
	}
	if active < 1 {
		return 0, nil, fmt.Errorf("config: INVITATION_SECRET_ACTIVE_VERSION must be >= 1, got %d", active)
	}
	if _, ok := keys[active]; !ok {
		return 0, nil, fmt.Errorf("config: INVITATION_SECRET_ACTIVE_VERSION is %d but %s%d is not set",
			active, invitationSecretKeyPrefix, active)
	}

	return active, keys, nil
}

// loadOptionalVersionedKeyring applies the same bounded scan and startup
// validation to each Phase D keyring. Fully absent is allowed at this leaf;
// the API composition root requires the resulting constructors to succeed.
func loadOptionalVersionedKeyring(activeName, keyPrefix string) (
	int, map[int]string, error) {

	keys := make(map[int]string)
	for version := 1; version <= maxInvitationSecretKeyVersion; version++ {
		name := fmt.Sprintf("%s%d", keyPrefix, version)
		encoded := os.Getenv(name)
		if encoded == "" {
			continue
		}
		decoded, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil {
			return 0, nil, fmt.Errorf("config: %s is not valid base64: %w", name, err)
		}
		if len(decoded) < minInvitationSecretKeyBytes {
			return 0, nil, fmt.Errorf("config: %s decodes to %d bytes; at least %d random "+
				"bytes are required", name, len(decoded), minInvitationSecretKeyBytes)
		}
		keys[version] = encoded
	}

	rawActive := os.Getenv(activeName)
	if rawActive == "" {
		if len(keys) > 0 {
			return 0, nil, fmt.Errorf("config: %s is required when any %s* is configured",
				activeName, keyPrefix)
		}
		return 0, keys, nil
	}
	active, err := strconv.Atoi(rawActive)
	if err != nil {
		return 0, nil, fmt.Errorf("config: %s must be an integer, got %q",
			activeName, rawActive)
	}
	if active < 1 {
		return 0, nil, fmt.Errorf("config: %s must be >= 1, got %d", activeName, active)
	}
	if _, ok := keys[active]; !ok {
		return 0, nil, fmt.Errorf("config: %s is %d but %s%d is not set",
			activeName, active, keyPrefix, active)
	}
	return active, keys, nil
}

func loadSupplierRateLimitFingerprintKey() (string, error) {
	const name = "SUPPLIER_RATE_LIMIT_FINGERPRINT_KEY"
	encoded := os.Getenv(name)
	if encoded == "" {
		return "", nil
	}
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", fmt.Errorf("config: %s is not valid base64: %w", name, err)
	}
	if len(decoded) < minInvitationSecretKeyBytes {
		return "", fmt.Errorf("config: %s decodes to %d bytes; at least %d random "+
			"bytes are required", name, len(decoded), minInvitationSecretKeyBytes)
	}
	return encoded, nil
}

// defaultAIServiceTimeout matches the design doc's documented default
// (M8.5B-A design doc §21.1, AI_SERVICE_TIMEOUT=45s).
const defaultAIServiceTimeout = 45 * time.Second

// loadAIServiceConfig reads the AI service HTTP client's configuration.
// AIServiceURL and AIInternalToken are required together — either both are
// set (the AI feature is enabled) or neither is (AI generation is simply
// unavailable, matching M8.5B-A design doc §21.1). GEMINI_API_KEY is
// deliberately not read here: it is a Go config field never, existing only
// in the Python ai-service's own runtime configuration.
func loadAIServiceConfig() (url, token string, timeout time.Duration, err error) {
	url = os.Getenv("AI_SERVICE_URL")
	token = os.Getenv("AI_INTERNAL_TOKEN")

	if (url == "") != (token == "") {
		return "", "", 0, fmt.Errorf(
			"config: AI_SERVICE_URL and AI_INTERNAL_TOKEN must be set together (both or neither)")
	}

	rawTimeout := os.Getenv("AI_SERVICE_TIMEOUT")
	if rawTimeout == "" {
		return url, token, defaultAIServiceTimeout, nil
	}
	parsed, parseErr := time.ParseDuration(rawTimeout)
	if parseErr != nil {
		return "", "", 0, fmt.Errorf("config: AI_SERVICE_TIMEOUT is not a valid duration: %w", parseErr)
	}
	if parsed <= 0 {
		return "", "", 0, fmt.Errorf("config: AI_SERVICE_TIMEOUT must be positive, got %v", parsed)
	}
	return url, token, parsed, nil
}

// defaultAISpatialServiceTimeout must exceed Python's GLM timeout (default
// 30s, GLM_TIMEOUT_SECONDS) plus a response-validation margin (RP4E1 plan
// Task 8 Step 1) — 45s matches AI_SERVICE_TIMEOUT's own existing default
// for the Copilot suggestion routes, comfortably above that floor.
const defaultAISpatialServiceTimeout = 45 * time.Second

// defaultSpatialDesignContextRadiusMeters matches the RP4E1 plan's
// documented context-minimization radius floor (§"Context minimization").
const defaultSpatialDesignContextRadiusMeters = 3.0

// defaultAssetGenerationRuntimeNoticeText is the fixed, contractor-safe
// default copy shown whenever a design plan's cumulative
// hunyuanRequired=true (RP4E1 plan's public turn DTO example).
const defaultAssetGenerationRuntimeNoticeText = "3D generation currently runs in a limited test GPU environment. Very complex generations may not complete within the available execution window."

// maxAssetGenerationRuntimeNoticeTextBytes bounds an operator-overridden
// notice — RP4E1 plan's "bounded if overridden" requirement.
const maxAssetGenerationRuntimeNoticeTextBytes = 500

func loadAISpatialServiceTimeout() (time.Duration, error) {
	raw := os.Getenv("AI_SPATIAL_SERVICE_TIMEOUT")
	if raw == "" {
		return defaultAISpatialServiceTimeout, nil
	}
	parsed, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("config: AI_SPATIAL_SERVICE_TIMEOUT is not a valid duration: %w", err)
	}
	if parsed <= 0 {
		return 0, fmt.Errorf("config: AI_SPATIAL_SERVICE_TIMEOUT must be positive, got %v", parsed)
	}
	return parsed, nil
}

func loadSpatialDesignContextRadiusMeters() (float64, error) {
	raw := os.Getenv("SPATIAL_DESIGN_CONTEXT_RADIUS_METERS")
	if raw == "" {
		return defaultSpatialDesignContextRadiusMeters, nil
	}
	parsed, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0, fmt.Errorf("config: SPATIAL_DESIGN_CONTEXT_RADIUS_METERS is not a valid number: %w", err)
	}
	if parsed <= 0 {
		return 0, fmt.Errorf("config: SPATIAL_DESIGN_CONTEXT_RADIUS_METERS must be positive, got %v", parsed)
	}
	return parsed, nil
}

func loadAssetGenerationRuntimeNotice() (enabled bool, text string, err error) {
	enabled, err = getEnvBoolOrDefault("ASSET_GENERATION_RUNTIME_NOTICE_ENABLED", true)
	if err != nil {
		return false, "", err
	}
	text = getEnvOrDefault("ASSET_GENERATION_RUNTIME_NOTICE_TEXT", defaultAssetGenerationRuntimeNoticeText)
	if len(text) > maxAssetGenerationRuntimeNoticeTextBytes {
		return false, "", fmt.Errorf("config: ASSET_GENERATION_RUNTIME_NOTICE_TEXT exceeds %d bytes", maxAssetGenerationRuntimeNoticeTextBytes)
	}
	return enabled, text, nil
}

// defaultHunyuanProviderTimeout allows roughly a 15-30s transport/queue/
// serialization margin beyond the deployed HF Space's actual GPU execution
// budget (@spaces.GPU(duration=90) — 90s), rather than the previous 9m
// default which vastly overshot the real upstream budget. Durable
// JobExecution lease/retry behavior (see spatial.defaultAssetGenerationLeaseTTL)
// is unaffected by this value — a shorter provider timeout simply surfaces a
// stalled call sooner, in line with the existing retry-on-next-wake model.
const defaultHunyuanProviderTimeout = 110 * time.Second

// loadHuggingFaceConfig mirrors loadAIServiceConfig's optional-together
// pattern exactly (RP4E0): HUGGINGFACE_SPACE_URL and HUGGINGFACE_TOKEN
// are required together or neither is set (asset generation simply
// unavailable).
func loadHuggingFaceConfig() (spaceURL, token string, timeout time.Duration, err error) {
	spaceURL = os.Getenv("HUGGINGFACE_SPACE_URL")
	token = os.Getenv("HUGGINGFACE_TOKEN")

	if (spaceURL == "") != (token == "") {
		return "", "", 0, fmt.Errorf(
			"config: HUGGINGFACE_SPACE_URL and HUGGINGFACE_TOKEN must be set together (both or neither)")
	}

	rawTimeout := os.Getenv("HUNYUAN_PROVIDER_TIMEOUT")
	if rawTimeout == "" {
		return spaceURL, token, defaultHunyuanProviderTimeout, nil
	}
	parsed, parseErr := time.ParseDuration(rawTimeout)
	if parseErr != nil {
		return "", "", 0, fmt.Errorf("config: HUNYUAN_PROVIDER_TIMEOUT is not a valid duration: %w", parseErr)
	}
	if parsed <= 0 {
		return "", "", 0, fmt.Errorf("config: HUNYUAN_PROVIDER_TIMEOUT must be positive, got %v", parsed)
	}
	return spaceURL, token, parsed, nil
}

// defaultReferenceImageMaxBytes mirrors the RP4E2 plan's documented default
// (REFERENCE_IMAGE_MAX_BYTES=8388608, 8 MiB).
const defaultReferenceImageMaxBytes = 8 * 1024 * 1024

// loadObjectStoreConfig validates ObjectStoreProvider is one of the two
// known values and, only for "r2", requires every R2* variable together —
// a partially configured R2 (e.g. bucket set but no credentials) fails
// startup rather than booting into a store that will fail on first use.
func loadObjectStoreConfig() (provider, accountID, accessKeyID, secretAccessKey, bucket, endpoint string, err error) {
	provider = getEnvOrDefault("OBJECT_STORE_PROVIDER", "local")
	if provider != "local" && provider != "r2" {
		return "", "", "", "", "", "", fmt.Errorf("config: OBJECT_STORE_PROVIDER must be \"local\" or \"r2\", got %q", provider)
	}
	accountID = os.Getenv("R2_ACCOUNT_ID")
	accessKeyID = os.Getenv("R2_ACCESS_KEY_ID")
	secretAccessKey = os.Getenv("R2_SECRET_ACCESS_KEY")
	bucket = os.Getenv("R2_BUCKET")
	endpoint = os.Getenv("R2_ENDPOINT")
	if provider != "r2" {
		return provider, accountID, accessKeyID, secretAccessKey, bucket, endpoint, nil
	}
	if accountID == "" || accessKeyID == "" || secretAccessKey == "" || bucket == "" || endpoint == "" {
		return "", "", "", "", "", "", fmt.Errorf(
			"config: R2_ACCOUNT_ID, R2_ACCESS_KEY_ID, R2_SECRET_ACCESS_KEY, R2_BUCKET, and R2_ENDPOINT are all required when OBJECT_STORE_PROVIDER=r2")
	}
	return provider, accountID, accessKeyID, secretAccessKey, bucket, endpoint, nil
}

func loadReferenceImageMaxBytes() (int64, error) {
	raw := os.Getenv("REFERENCE_IMAGE_MAX_BYTES")
	if raw == "" {
		return defaultReferenceImageMaxBytes, nil
	}
	parsed, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("config: REFERENCE_IMAGE_MAX_BYTES must be an integer, got %q", raw)
	}
	if parsed <= 0 {
		return 0, fmt.Errorf("config: REFERENCE_IMAGE_MAX_BYTES must be positive, got %d", parsed)
	}
	return parsed, nil
}

// loadEmailConfig validates EmailProvider ("smtp"|"resend") and
// EmailDeliveryMode ("direct"|"test_sink"), and requires every Resend
// variable together when EmailProvider=resend. The production+test_sink
// fatal check happens separately in LoadFromEnv (it needs AppEnv, which
// this function does not have).
func loadEmailConfig() (provider, deliveryMode, sinkAddress, apiKey, from string, err error) {
	provider = getEnvOrDefault("EMAIL_PROVIDER", "smtp")
	if provider != "smtp" && provider != "resend" {
		return "", "", "", "", "", fmt.Errorf("config: EMAIL_PROVIDER must be \"smtp\" or \"resend\", got %q", provider)
	}
	deliveryMode = getEnvOrDefault("EMAIL_DELIVERY_MODE", "direct")
	if deliveryMode != "direct" && deliveryMode != "test_sink" {
		return "", "", "", "", "", fmt.Errorf("config: EMAIL_DELIVERY_MODE must be \"direct\" or \"test_sink\", got %q", deliveryMode)
	}
	sinkAddress = os.Getenv("EMAIL_TEST_SINK_ADDRESS")
	apiKey = os.Getenv("RESEND_API_KEY")
	from = os.Getenv("RESEND_FROM")

	if deliveryMode == "test_sink" && sinkAddress == "" {
		return "", "", "", "", "", fmt.Errorf("config: EMAIL_TEST_SINK_ADDRESS is required when EMAIL_DELIVERY_MODE=test_sink")
	}
	if provider == "resend" && (apiKey == "" || from == "") {
		return "", "", "", "", "", fmt.Errorf("config: RESEND_API_KEY and RESEND_FROM are required when EMAIL_PROVIDER=resend")
	}
	return provider, deliveryMode, sinkAddress, apiKey, from, nil
}

func loadTrustedProxyCIDRs() ([]netip.Prefix, error) {
	raw := strings.TrimSpace(os.Getenv("TRUSTED_PROXY_CIDRS"))
	if raw == "" {
		return nil, nil
	}
	parts := strings.Split(raw, ",")
	prefixes := make([]netip.Prefix, 0, len(parts))
	for _, part := range parts {
		value := strings.TrimSpace(part)
		prefix, err := netip.ParsePrefix(value)
		if err != nil {
			return nil, fmt.Errorf("config: TRUSTED_PROXY_CIDRS contains invalid CIDR %q: %w",
				value, err)
		}
		prefixes = append(prefixes, prefix.Masked())
	}
	return prefixes, nil
}

// LoadFromEnv reads configuration from process environment variables,
// applying defaults where documented in .env.example. It returns an error
// if a required variable is missing.
func LoadFromEnv() (Config, error) {
	cfg := Config{
		AppEnv:   getEnvOrDefault("APP_ENV", "development"),
		HTTPPort: getEnvOrDefault("PORT", getEnvOrDefault("HTTP_PORT", "8080")),

		MongoURI:      os.Getenv("MONGO_URI"),
		MongoDatabase: os.Getenv("MONGO_DATABASE"),

		JWTAccessSecret:  os.Getenv("JWT_ACCESS_SECRET"),
		JWTRefreshSecret: os.Getenv("JWT_REFRESH_SECRET"),

		SMTPHost: getEnvOrDefault("SMTP_HOST", "localhost"),
		SMTPPort: getEnvOrDefault("SMTP_PORT", "1025"),
		SMTPFrom: getEnvOrDefault("SMTP_FROM", "no-reply@renovation-platform.local"),

		StorageLocalPath: getEnvOrDefault("STORAGE_LOCAL_PATH", "./data/uploads"),

		AIProvider: getEnvOrDefault("AI_PROVIDER", "mock"),

		ExternalAPIBaseURL: getEnvOrDefault("EXTERNAL_API_BASE_URL", "http://localhost:3000"),
	}

	if cfg.MongoURI == "" {
		return Config{}, fmt.Errorf("config: MONGO_URI is required")
	}
	if cfg.MongoDatabase == "" {
		return Config{}, fmt.Errorf("config: MONGO_DATABASE is required")
	}
	// Fail fast on a malformed base URL: a relative or scheme-less value would
	// silently produce unusable client links.
	if parsed, err := url.Parse(cfg.ExternalAPIBaseURL); err != nil || !parsed.IsAbs() || parsed.Host == "" {
		return Config{}, fmt.Errorf("config: EXTERNAL_API_BASE_URL must be an absolute URL, got %q", cfg.ExternalAPIBaseURL)
	}

	activeVersion, keys, err := loadInvitationSecretKeyring()
	if err != nil {
		return Config{}, err
	}
	cfg.InvitationSecretActiveVersion = activeVersion
	cfg.InvitationSecretKeys = keys

	codeActive, codeKeys, err := loadOptionalVersionedKeyring(
		"SUPPLIER_VERIFICATION_CODE_ACTIVE_VERSION",
		supplierVerificationCodeKeyPrefix,
	)
	if err != nil {
		return Config{}, err
	}
	cfg.SupplierVerificationCodeActiveVersion = codeActive
	cfg.SupplierVerificationCodeKeys = codeKeys

	sessionActive, sessionKeys, err := loadOptionalVersionedKeyring(
		"SUPPLIER_SESSION_TOKEN_ACTIVE_VERSION",
		supplierSessionTokenKeyPrefix,
	)
	if err != nil {
		return Config{}, err
	}
	cfg.SupplierSessionTokenActiveVersion = sessionActive
	cfg.SupplierSessionTokenKeys = sessionKeys

	fingerprintKey, err := loadSupplierRateLimitFingerprintKey()
	if err != nil {
		return Config{}, err
	}
	cfg.SupplierRateLimitFingerprintKey = fingerprintKey

	// VisualAssetCapabilityKeys uses the SAME optional-until-referenced
	// loading as the supplier keyrings above (empty is valid — RP4D's
	// asset-access routes simply are not available yet) — but unlike
	// those, composition/services.go refuses to construct the LOCAL
	// read-access provider at all without at least one key configured,
	// rather than silently booting a provider that would mint unsigned
	// or trivially-forgeable capabilities (RP4D §6: "missing signing
	// configuration fails closed; never expose unsigned content").
	visualAssetCapabilityActive, visualAssetCapabilityKeys, err := loadOptionalVersionedKeyring(
		"VISUAL_ASSET_CAPABILITY_ACTIVE_VERSION",
		visualAssetCapabilityKeyPrefix,
	)
	if err != nil {
		return Config{}, err
	}
	cfg.VisualAssetCapabilityActiveVersion = visualAssetCapabilityActive
	cfg.VisualAssetCapabilityKeys = visualAssetCapabilityKeys

	cfg.VisualAssetAccessTTL = defaultVisualAssetAccessTTL
	if raw := os.Getenv("VISUAL_ASSET_ACCESS_TTL"); raw != "" {
		ttl, err := time.ParseDuration(raw)
		if err != nil {
			return Config{}, fmt.Errorf("config: VISUAL_ASSET_ACCESS_TTL is not a valid duration: %w", err)
		}
		cfg.VisualAssetAccessTTL = ttl
	}

	huggingFaceSpaceURL, huggingFaceToken, hunyuanProviderTimeout, err := loadHuggingFaceConfig()
	if err != nil {
		return Config{}, err
	}
	cfg.HuggingFaceSpaceURL = huggingFaceSpaceURL
	cfg.HuggingFaceToken = huggingFaceToken
	cfg.HunyuanProviderTimeout = hunyuanProviderTimeout

	// AssetGenerationSourceCapabilityKeys uses the SAME optional-until-
	// referenced loading as VisualAssetCapabilityKeys — empty is valid
	// (asset generation simply isn't configured yet); fails closed only
	// at the point composition/services.go would construct the local
	// provider without at least one key.
	assetGenerationSourceCapabilityActive, assetGenerationSourceCapabilityKeys, err := loadOptionalVersionedKeyring(
		"ASSET_GENERATION_SOURCE_CAPABILITY_ACTIVE_VERSION",
		assetGenerationSourceCapabilityKeyPrefix,
	)
	if err != nil {
		return Config{}, err
	}
	cfg.AssetGenerationSourceCapabilityActiveVersion = assetGenerationSourceCapabilityActive
	cfg.AssetGenerationSourceCapabilityKeys = assetGenerationSourceCapabilityKeys

	if raw := os.Getenv("ASSET_GENERATION_LEASE_TTL"); raw != "" {
		ttl, err := time.ParseDuration(raw)
		if err != nil {
			return Config{}, fmt.Errorf("config: ASSET_GENERATION_LEASE_TTL is not a valid duration: %w", err)
		}
		cfg.AssetGenerationLeaseTTL = ttl
	}
	if raw := os.Getenv("ASSET_GENERATION_HEARTBEAT_INTERVAL"); raw != "" {
		interval, err := time.ParseDuration(raw)
		if err != nil {
			return Config{}, fmt.Errorf("config: ASSET_GENERATION_HEARTBEAT_INTERVAL is not a valid duration: %w", err)
		}
		cfg.AssetGenerationHeartbeatInterval = interval
	}

	trustedProxyCIDRs, err := loadTrustedProxyCIDRs()
	if err != nil {
		return Config{}, err
	}
	cfg.TrustedProxyCIDRs = trustedProxyCIDRs

	allowedOrigins, err := parseAllowedOrigins(os.Getenv("APP_ALLOWED_ORIGINS"))
	if err != nil {
		return Config{}, err
	}
	cfg.AllowedOrigins = allowedOrigins

	aiServiceURL, aiInternalToken, aiServiceTimeout, err := loadAIServiceConfig()
	if err != nil {
		return Config{}, err
	}
	cfg.AIServiceURL = aiServiceURL
	cfg.AIInternalToken = aiInternalToken
	cfg.AIServiceTimeout = aiServiceTimeout

	aiSpatialServiceTimeout, err := loadAISpatialServiceTimeout()
	if err != nil {
		return Config{}, err
	}
	cfg.AISpatialServiceTimeout = aiSpatialServiceTimeout

	spatialDesignContextRadiusMeters, err := loadSpatialDesignContextRadiusMeters()
	if err != nil {
		return Config{}, err
	}
	cfg.SpatialDesignContextRadiusMeters = spatialDesignContextRadiusMeters

	assetGenerationRuntimeNoticeEnabled, assetGenerationRuntimeNoticeText, err := loadAssetGenerationRuntimeNotice()
	if err != nil {
		return Config{}, err
	}
	cfg.AssetGenerationRuntimeNoticeEnabled = assetGenerationRuntimeNoticeEnabled
	cfg.AssetGenerationRuntimeNoticeText = assetGenerationRuntimeNoticeText

	refreshCookieSecure, err := getEnvBoolOrDefault("AUTH_REFRESH_COOKIE_SECURE", true)
	if err != nil {
		return Config{}, err
	}
	cfg.RefreshCookieSecure = refreshCookieSecure

	refreshCookieSameSite, err := getRefreshCookieSameSite()
	if err != nil {
		return Config{}, err
	}
	cfg.RefreshCookieSameSite = refreshCookieSameSite

	objectStoreProvider, r2AccountID, r2AccessKeyID, r2SecretAccessKey, r2Bucket, r2Endpoint, err := loadObjectStoreConfig()
	if err != nil {
		return Config{}, err
	}
	cfg.ObjectStoreProvider = objectStoreProvider
	cfg.R2AccountID = r2AccountID
	cfg.R2AccessKeyID = r2AccessKeyID
	cfg.R2SecretAccessKey = r2SecretAccessKey
	cfg.R2Bucket = r2Bucket
	cfg.R2Endpoint = r2Endpoint

	referenceImageMaxBytes, err := loadReferenceImageMaxBytes()
	if err != nil {
		return Config{}, err
	}
	cfg.ReferenceImageMaxBytes = referenceImageMaxBytes

	emailProvider, emailDeliveryMode, emailTestSinkAddress, resendAPIKey, resendFrom, err := loadEmailConfig()
	if err != nil {
		return Config{}, err
	}
	cfg.EmailProvider = emailProvider
	cfg.EmailDeliveryMode = emailDeliveryMode
	cfg.EmailTestSinkAddress = emailTestSinkAddress
	cfg.ResendAPIKey = resendAPIKey
	cfg.ResendFrom = resendFrom

	cfg.SpatialWorkerToken = os.Getenv("SPATIAL_WORKER_TOKEN")
	cfg.SpatialQueueEnqueueURL = os.Getenv("SPATIAL_QUEUE_ENQUEUE_URL")
	cfg.SpatialQueueEnqueueToken = os.Getenv("SPATIAL_QUEUE_ENQUEUE_TOKEN")

	serverlessMode, err := getEnvBoolOrDefault("SERVERLESS_MODE", false)
	if err != nil {
		return Config{}, err
	}
	cfg.ServerlessMode = serverlessMode || os.Getenv("VERCEL") == "1"

	if cfg.AppEnv == "production" {
		if cfg.AllowedOrigins.Len() == 0 {
			return Config{}, fmt.Errorf("config: APP_ALLOWED_ORIGINS is required when APP_ENV=production")
		}
		if !cfg.RefreshCookieSecure {
			return Config{}, fmt.Errorf("config: AUTH_REFRESH_COOKIE_SECURE must be true when APP_ENV=production")
		}
		for _, origin := range cfg.AllowedOrigins.set {
			if !strings.HasPrefix(origin, "https://") {
				return Config{}, fmt.Errorf(
					"config: APP_ALLOWED_ORIGINS origin %q must use https in production", origin)
			}
		}
		// M8.5C plan's central safety invariant: production must reject
		// test_sink unconditionally — a tester-only escape hatch must never
		// be reachable in a real deployment, checked here rather than left
		// to a caller to remember (or a Web-side guard that could be
		// bypassed by calling the Go API directly).
		if cfg.EmailDeliveryMode == "test_sink" {
			return Config{}, fmt.Errorf("config: EMAIL_DELIVERY_MODE=test_sink is not permitted when APP_ENV=production")
		}
		// T2D: production must never silently boot onto ephemeral local
		// filesystem storage — R2 is the sole durable object store the
		// tester/production deployment is provisioned with (M8.5C plan),
		// and Vercel's filesystem does not even persist across
		// invocations. OBJECT_STORE_PROVIDER defaults to "local" for
		// local/dev convenience; that default must never reach production
		// unnoticed.
		if cfg.ObjectStoreProvider != "r2" {
			return Config{}, fmt.Errorf("config: OBJECT_STORE_PROVIDER must be \"r2\" when APP_ENV=production")
		}
		// A convincing-but-broken production deploy that still points at a
		// local/dev MongoDB is exactly the failure mode this task exists to
		// close — fail fast rather than boot against localhost/127.0.0.1
		// and silently operate on the wrong (or no) real data.
		if isLocalMongoURI(cfg.MongoURI) {
			return Config{}, fmt.Errorf("config: MONGO_URI must not point at localhost/127.0.0.1 when APP_ENV=production")
		}
	}

	return cfg, nil
}

// isLocalMongoURI reports whether uri points at a loopback host — a
// deliberately narrow, string-based check (parsing the mongodb:// scheme's
// full host-list syntax is not required here) that catches the realistic
// "someone copied the local .env.example value into production" mistake
// without trying to be a general MongoDB URI validator.
func isLocalMongoURI(uri string) bool {
	lower := strings.ToLower(uri)
	for _, marker := range []string{"localhost", "127.0.0.1", "0.0.0.0", "[::1]"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

// getEnvBoolOrDefault reads a "true"/"false" environment variable, returning
// fallback when unset. Any other value is a configuration error rather than
// a silently-ignored typo.
func getEnvBoolOrDefault(key string, fallback bool) (bool, error) {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback, nil
	}
	switch raw {
	case "true":
		return true, nil
	case "false":
		return false, nil
	default:
		return false, fmt.Errorf("config: %s must be \"true\" or \"false\", got %q", key, raw)
	}
}

func getRefreshCookieSameSite() (http.SameSite, error) {
	switch getEnvOrDefault("AUTH_REFRESH_COOKIE_SAME_SITE", "lax") {
	case "lax":
		return http.SameSiteLaxMode, nil
	case "strict":
		return http.SameSiteStrictMode, nil
	case "none":
		return http.SameSiteNoneMode, nil
	default:
		return 0, fmt.Errorf("config: AUTH_REFRESH_COOKIE_SAME_SITE must be one of \"lax\", \"strict\", or \"none\"")
	}
}

func getEnvOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
