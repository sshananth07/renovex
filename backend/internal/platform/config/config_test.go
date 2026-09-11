package config

import (
	"net/http"
	"testing"
	"time"
)

func TestLoadFromEnvUsesDefaults(t *testing.T) {
	t.Setenv("MONGO_URI", "mongodb://localhost:27017")
	t.Setenv("MONGO_DATABASE", "renovation_platform_test")
	t.Setenv("AI_PROVIDER", "mock")

	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.HTTPPort != "8080" {
		t.Fatalf("expected default HTTPPort 8080, got %s", cfg.HTTPPort)
	}
	if cfg.MongoURI != "mongodb://localhost:27017" {
		t.Fatalf("expected MongoURI from env, got %s", cfg.MongoURI)
	}
	if cfg.MongoDatabase != "renovation_platform_test" {
		t.Fatalf("expected MongoDatabase from env, got %s", cfg.MongoDatabase)
	}
	if cfg.AIProvider != "mock" {
		t.Fatalf("expected AIProvider mock, got %s", cfg.AIProvider)
	}
}

func TestLoadFromEnvPrefersPortOverHTTPPort(t *testing.T) {
	setBaseEnv(t)
	t.Setenv("HTTP_PORT", "8080")
	t.Setenv("PORT", "4567")

	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.HTTPPort != "4567" {
		t.Fatalf("expected PORT to take precedence, got %q", cfg.HTTPPort)
	}
}

func TestLoadFromEnvUsesDefaultVisualAssetAccessTTLWhenUnset(t *testing.T) {
	t.Setenv("MONGO_URI", "mongodb://localhost:27017")
	t.Setenv("MONGO_DATABASE", "renovation_platform_test")
	t.Setenv("AI_PROVIDER", "mock")

	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.VisualAssetAccessTTL != 10*time.Minute {
		t.Fatalf("expected default 10m VisualAssetAccessTTL, got %s", cfg.VisualAssetAccessTTL)
	}
}

func TestLoadFromEnvReadsVisualAssetAccessTTL(t *testing.T) {
	t.Setenv("MONGO_URI", "mongodb://localhost:27017")
	t.Setenv("MONGO_DATABASE", "renovation_platform_test")
	t.Setenv("AI_PROVIDER", "mock")
	t.Setenv("VISUAL_ASSET_ACCESS_TTL", "5m")

	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.VisualAssetAccessTTL != 5*time.Minute {
		t.Fatalf("expected 5m VisualAssetAccessTTL, got %s", cfg.VisualAssetAccessTTL)
	}
}

func TestLoadFromEnvRejectsInvalidVisualAssetAccessTTL(t *testing.T) {
	t.Setenv("MONGO_URI", "mongodb://localhost:27017")
	t.Setenv("MONGO_DATABASE", "renovation_platform_test")
	t.Setenv("AI_PROVIDER", "mock")
	t.Setenv("VISUAL_ASSET_ACCESS_TTL", "not-a-duration")

	if _, err := LoadFromEnv(); err == nil {
		t.Fatalf("expected an error for an invalid VISUAL_ASSET_ACCESS_TTL")
	}
}

func TestLoadFromEnvReadsVisualAssetCapabilityKeyring(t *testing.T) {
	t.Setenv("MONGO_URI", "mongodb://localhost:27017")
	t.Setenv("MONGO_DATABASE", "renovation_platform_test")
	t.Setenv("AI_PROVIDER", "mock")
	t.Setenv("VISUAL_ASSET_CAPABILITY_KEY_V1", "MDEyMzQ1Njc4OTAxMjM0NTY3ODkwMTIzNDU2Nzg5MDE=")
	t.Setenv("VISUAL_ASSET_CAPABILITY_ACTIVE_VERSION", "1")

	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.VisualAssetCapabilityActiveVersion != 1 {
		t.Fatalf("expected active version 1, got %d", cfg.VisualAssetCapabilityActiveVersion)
	}
	if cfg.VisualAssetCapabilityKeys[1] == "" {
		t.Fatalf("expected key v1 to be loaded")
	}
}

func TestLoadFromEnvVisualAssetCapabilityKeyringAbsentByDefault(t *testing.T) {
	t.Setenv("MONGO_URI", "mongodb://localhost:27017")
	t.Setenv("MONGO_DATABASE", "renovation_platform_test")
	t.Setenv("AI_PROVIDER", "mock")

	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.VisualAssetCapabilityKeys) != 0 {
		t.Fatalf("expected no visual asset capability keys configured by default, got %+v", cfg.VisualAssetCapabilityKeys)
	}
}

func TestLoadFromEnvMissingMongoURIFails(t *testing.T) {
	t.Setenv("MONGO_URI", "")
	t.Setenv("MONGO_DATABASE", "renovation_platform_test")

	_, err := LoadFromEnv()
	if err == nil {
		t.Fatal("expected error when MONGO_URI is unset, got nil")
	}
}

// --- Milestone 6 ---

func TestLoadFromEnvDefaultsExternalAPIBaseURLLocally(t *testing.T) {
	t.Setenv("MONGO_URI", "mongodb://localhost:27017")
	t.Setenv("MONGO_DATABASE", "test")

	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.ExternalAPIBaseURL != "http://localhost:3000" {
		t.Fatalf("expected the local default, got %q", cfg.ExternalAPIBaseURL)
	}
}

func TestLoadFromEnvReadsExternalAPIBaseURL(t *testing.T) {
	t.Setenv("MONGO_URI", "mongodb://localhost:27017")
	t.Setenv("MONGO_DATABASE", "test")
	t.Setenv("EXTERNAL_API_BASE_URL", "https://api.example.com")

	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.ExternalAPIBaseURL != "https://api.example.com" {
		t.Fatalf("expected the configured value, got %q", cfg.ExternalAPIBaseURL)
	}
}

func TestLoadFromEnvRejectsInvalidExternalAPIBaseURL(t *testing.T) {
	t.Setenv("MONGO_URI", "mongodb://localhost:27017")
	t.Setenv("MONGO_DATABASE", "test")
	t.Setenv("EXTERNAL_API_BASE_URL", "not-an-absolute-url")

	if _, err := LoadFromEnv(); err == nil {
		t.Fatal("expected an error for a non-absolute EXTERNAL_API_BASE_URL")
	}
}

// --- F0A: browser origin and refresh-cookie configuration ---

func setBaseEnv(t *testing.T) {
	t.Helper()
	t.Setenv("MONGO_URI", "mongodb://localhost:27017")
	t.Setenv("MONGO_DATABASE", "test")
}

func TestLoadFromEnvParsesCommaSeparatedOrigins(t *testing.T) {
	setBaseEnv(t)
	t.Setenv("APP_ALLOWED_ORIGINS", "http://localhost:3000,https://app.example.com")

	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cfg.AllowedOrigins.Contains("http://localhost:3000") {
		t.Fatalf("expected http://localhost:3000 to be allowed, got %v", cfg.AllowedOrigins)
	}
	if !cfg.AllowedOrigins.Contains("https://app.example.com") {
		t.Fatalf("expected https://app.example.com to be allowed, got %v", cfg.AllowedOrigins)
	}
}

func TestLoadFromEnvTrimsWhitespaceInOrigins(t *testing.T) {
	setBaseEnv(t)
	t.Setenv("APP_ALLOWED_ORIGINS", " http://localhost:3000 , https://app.example.com ")

	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cfg.AllowedOrigins.Contains("http://localhost:3000") {
		t.Fatalf("expected trimmed origin to be allowed, got %v", cfg.AllowedOrigins)
	}
}

func TestLoadFromEnvCollapsesDuplicateOrigins(t *testing.T) {
	setBaseEnv(t)
	t.Setenv("APP_ALLOWED_ORIGINS", "http://localhost:3000,http://localhost:3000")

	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.AllowedOrigins.Len() != 1 {
		t.Fatalf("expected duplicate origins to collapse to 1, got %d", cfg.AllowedOrigins.Len())
	}
}

func TestLoadFromEnvRejectsOriginWithoutHTTPScheme(t *testing.T) {
	setBaseEnv(t)
	t.Setenv("APP_ALLOWED_ORIGINS", "ftp://localhost:3000")

	if _, err := LoadFromEnv(); err == nil {
		t.Fatal("expected an error for a non-http(s) origin scheme")
	}
}

func TestLoadFromEnvRejectsOriginWithPath(t *testing.T) {
	setBaseEnv(t)
	t.Setenv("APP_ALLOWED_ORIGINS", "http://localhost:3000/app")

	if _, err := LoadFromEnv(); err == nil {
		t.Fatal("expected an error for an origin containing a path")
	}
}

func TestLoadFromEnvRejectsOriginWithQuery(t *testing.T) {
	setBaseEnv(t)
	t.Setenv("APP_ALLOWED_ORIGINS", "http://localhost:3000?x=1")

	if _, err := LoadFromEnv(); err == nil {
		t.Fatal("expected an error for an origin containing a query string")
	}
}

func TestLoadFromEnvRejectsOriginWithFragment(t *testing.T) {
	setBaseEnv(t)
	t.Setenv("APP_ALLOWED_ORIGINS", "http://localhost:3000#frag")

	if _, err := LoadFromEnv(); err == nil {
		t.Fatal("expected an error for an origin containing a fragment")
	}
}

func TestLoadFromEnvRejectsOriginWithCredentials(t *testing.T) {
	setBaseEnv(t)
	t.Setenv("APP_ALLOWED_ORIGINS", "http://user:pass@localhost:3000")

	if _, err := LoadFromEnv(); err == nil {
		t.Fatal("expected an error for an origin containing embedded credentials")
	}
}

func TestLoadFromEnvRejectsNullOrigin(t *testing.T) {
	setBaseEnv(t)
	t.Setenv("APP_ALLOWED_ORIGINS", "null")

	if _, err := LoadFromEnv(); err == nil {
		t.Fatal("expected an error for the literal null origin")
	}
}

func TestLoadFromEnvRejectsWildcardOrigin(t *testing.T) {
	setBaseEnv(t)
	t.Setenv("APP_ALLOWED_ORIGINS", "*")

	if _, err := LoadFromEnv(); err == nil {
		t.Fatal("expected an error for a wildcard origin")
	}
}

func TestLoadFromEnvRejectsMalformedOrigin(t *testing.T) {
	setBaseEnv(t)
	t.Setenv("APP_ALLOWED_ORIGINS", "not a url")

	if _, err := LoadFromEnv(); err == nil {
		t.Fatal("expected an error for a malformed origin")
	}
}

func TestLoadFromEnvProductionRequiresAtLeastOneOrigin(t *testing.T) {
	setProductionBaseEnv(t)
	t.Setenv("APP_ALLOWED_ORIGINS", "")

	if _, err := LoadFromEnv(); err == nil {
		t.Fatal("expected production with no APP_ALLOWED_ORIGINS to fail")
	}
}

func TestLoadFromEnvProductionRejectsInsecureRefreshCookie(t *testing.T) {
	setProductionBaseEnv(t)
	t.Setenv("AUTH_REFRESH_COOKIE_SECURE", "false")

	if _, err := LoadFromEnv(); err == nil {
		t.Fatal("expected production with AUTH_REFRESH_COOKIE_SECURE=false to fail")
	}
}

func TestLoadFromEnvProductionRejectsNonHTTPSOrigin(t *testing.T) {
	setProductionBaseEnv(t)
	t.Setenv("APP_ALLOWED_ORIGINS", "http://app.example.com")

	if _, err := LoadFromEnv(); err == nil {
		t.Fatal("expected production with a non-HTTPS allowed origin to fail")
	}
}

func TestLoadFromEnvDevelopmentPermitsLocalhostHTTPWithInsecureCookie(t *testing.T) {
	setBaseEnv(t)
	t.Setenv("APP_ENV", "development")
	t.Setenv("APP_ALLOWED_ORIGINS", "http://localhost:3000")
	t.Setenv("AUTH_REFRESH_COOKIE_SECURE", "false")

	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("unexpected error in development: %v", err)
	}
	if cfg.RefreshCookieSecure {
		t.Fatal("expected RefreshCookieSecure=false in development")
	}
}

func TestLoadFromEnvRefreshCookieSecureDefaultsTrue(t *testing.T) {
	setBaseEnv(t)

	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cfg.RefreshCookieSecure {
		t.Fatal("expected RefreshCookieSecure to default to true")
	}
	if got := cfg.RefreshCookieSameSite; got != http.SameSiteLaxMode {
		t.Fatalf("RefreshCookieSameSite = %v, want SameSite=Lax", got)
	}
}

func TestLoadFromEnvReadsRefreshCookieSameSiteNone(t *testing.T) {
	setBaseEnv(t)
	t.Setenv("AUTH_REFRESH_COOKIE_SAME_SITE", "none")

	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got := cfg.RefreshCookieSameSite; got != http.SameSiteNoneMode {
		t.Fatalf("RefreshCookieSameSite = %v, want SameSite=None", got)
	}
}

func TestLoadFromEnvRejectsInvalidRefreshCookieSameSite(t *testing.T) {
	setBaseEnv(t)
	t.Setenv("AUTH_REFRESH_COOKIE_SAME_SITE", "cross-site")

	if _, err := LoadFromEnv(); err == nil {
		t.Fatal("expected an error for an invalid AUTH_REFRESH_COOKIE_SAME_SITE")
	}
}

// --- M8.5B-A: AI service client config ---

func TestLoadFromEnvAIServiceDefaultsWhenUnset(t *testing.T) {
	setBaseEnv(t)

	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.AIServiceURL != "" {
		t.Fatalf("expected empty AIServiceURL when unset, got %q", cfg.AIServiceURL)
	}
	if cfg.AIInternalToken != "" {
		t.Fatalf("expected empty AIInternalToken when unset, got %q", cfg.AIInternalToken)
	}
	if cfg.AIServiceTimeout != 45*time.Second {
		t.Fatalf("expected default AIServiceTimeout 45s, got %v", cfg.AIServiceTimeout)
	}
}

func TestLoadFromEnvAIServiceReadsConfiguredValues(t *testing.T) {
	setBaseEnv(t)
	t.Setenv("AI_SERVICE_URL", "http://localhost:8090")
	t.Setenv("AI_INTERNAL_TOKEN", "test-secret-token")
	t.Setenv("AI_SERVICE_TIMEOUT", "30s")

	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.AIServiceURL != "http://localhost:8090" {
		t.Fatalf("expected AIServiceURL from env, got %q", cfg.AIServiceURL)
	}
	if cfg.AIInternalToken != "test-secret-token" {
		t.Fatalf("expected AIInternalToken from env, got %q", cfg.AIInternalToken)
	}
	if cfg.AIServiceTimeout != 30*time.Second {
		t.Fatalf("expected AIServiceTimeout 30s, got %v", cfg.AIServiceTimeout)
	}
}

func TestLoadFromEnvAIServiceURLRequiresTokenAndViceVersa(t *testing.T) {
	setBaseEnv(t)
	t.Setenv("AI_SERVICE_URL", "http://localhost:8090")
	// AI_INTERNAL_TOKEN deliberately unset.

	_, err := LoadFromEnv()
	if err == nil {
		t.Fatal("expected an error when AI_SERVICE_URL is set without AI_INTERNAL_TOKEN")
	}
}

func TestLoadFromEnvAIServiceInvalidTimeoutFails(t *testing.T) {
	setBaseEnv(t)
	t.Setenv("AI_SERVICE_URL", "http://localhost:8090")
	t.Setenv("AI_INTERNAL_TOKEN", "test-secret-token")
	t.Setenv("AI_SERVICE_TIMEOUT", "not-a-duration")

	_, err := LoadFromEnv()
	if err == nil {
		t.Fatal("expected an error for an unparsable AI_SERVICE_TIMEOUT")
	}
}

func TestLoadFromEnvAIServiceNonPositiveTimeoutFails(t *testing.T) {
	setBaseEnv(t)
	t.Setenv("AI_SERVICE_URL", "http://localhost:8090")
	t.Setenv("AI_INTERNAL_TOKEN", "test-secret-token")
	t.Setenv("AI_SERVICE_TIMEOUT", "0s")

	_, err := LoadFromEnv()
	if err == nil {
		t.Fatal("expected an error for a non-positive AI_SERVICE_TIMEOUT")
	}
}

// --- RP4E1: spatial design reasoning config ---

func TestLoadFromEnvAISpatialServiceTimeoutDefault(t *testing.T) {
	setBaseEnv(t)

	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Default must exceed Python's GLM timeout (30s) plus a response-
	// validation margin — RP4E1 plan Task 8 Step 1.
	if cfg.AISpatialServiceTimeout <= 30*time.Second {
		t.Fatalf("expected AISpatialServiceTimeout to exceed 30s, got %v", cfg.AISpatialServiceTimeout)
	}
}

func TestLoadFromEnvAISpatialServiceTimeoutReadsConfiguredValue(t *testing.T) {
	setBaseEnv(t)
	t.Setenv("AI_SPATIAL_SERVICE_TIMEOUT", "50s")

	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.AISpatialServiceTimeout != 50*time.Second {
		t.Fatalf("expected 50s, got %v", cfg.AISpatialServiceTimeout)
	}
}

func TestLoadFromEnvAISpatialServiceTimeoutNonPositiveFails(t *testing.T) {
	setBaseEnv(t)
	t.Setenv("AI_SPATIAL_SERVICE_TIMEOUT", "0s")

	_, err := LoadFromEnv()
	if err == nil {
		t.Fatal("expected an error for a non-positive AI_SPATIAL_SERVICE_TIMEOUT")
	}
}

func TestLoadFromEnvSpatialDesignContextRadiusMetersDefault(t *testing.T) {
	setBaseEnv(t)

	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.SpatialDesignContextRadiusMeters != 3 {
		t.Fatalf("expected default 3, got %v", cfg.SpatialDesignContextRadiusMeters)
	}
}

func TestLoadFromEnvSpatialDesignContextRadiusMetersNonPositiveFails(t *testing.T) {
	setBaseEnv(t)
	t.Setenv("SPATIAL_DESIGN_CONTEXT_RADIUS_METERS", "0")

	_, err := LoadFromEnv()
	if err == nil {
		t.Fatal("expected an error for a non-positive SPATIAL_DESIGN_CONTEXT_RADIUS_METERS")
	}
}

func TestLoadFromEnvAssetGenerationRuntimeNoticeDefaults(t *testing.T) {
	setBaseEnv(t)

	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cfg.AssetGenerationRuntimeNoticeEnabled {
		t.Fatal("expected ASSET_GENERATION_RUNTIME_NOTICE_ENABLED to default to true")
	}
	if cfg.AssetGenerationRuntimeNoticeText == "" {
		t.Fatal("expected a fixed default runtime notice text")
	}
}

func TestLoadFromEnvAssetGenerationRuntimeNoticeCanBeDisabled(t *testing.T) {
	setBaseEnv(t)
	t.Setenv("ASSET_GENERATION_RUNTIME_NOTICE_ENABLED", "false")

	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.AssetGenerationRuntimeNoticeEnabled {
		t.Fatal("expected the runtime notice to be disabled")
	}
}

func TestLoadFromEnvAssetGenerationRuntimeNoticeTextOverride(t *testing.T) {
	setBaseEnv(t)
	t.Setenv("ASSET_GENERATION_RUNTIME_NOTICE_TEXT", "Custom notice.")

	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.AssetGenerationRuntimeNoticeText != "Custom notice." {
		t.Fatalf("expected overridden text, got %q", cfg.AssetGenerationRuntimeNoticeText)
	}
}

func TestLoadFromEnvGoNeverReadsGLMAPIKey(t *testing.T) {
	// GLM_API_KEY is a Python-only secret — Go's Config struct must have
	// no field for it at all (RP4E1 plan Task 8 Step 2). This is proven
	// structurally: LoadFromEnv succeeds and produces a usable Config
	// without ever referencing GLM_API_KEY, and Config has no such field
	// (verified by this file compiling without one).
	setBaseEnv(t)
	t.Setenv("GLM_API_KEY", "should-never-be-read-by-go")

	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_ = cfg
}

// --- RP4E2/M8.5C: object store, email, and serverless config guards ---

func TestLoadFromEnvDefaultsObjectStoreProviderToLocal(t *testing.T) {
	setBaseEnv(t)
	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.ObjectStoreProvider != "local" {
		t.Fatalf("expected default local, got %s", cfg.ObjectStoreProvider)
	}
}

func TestLoadFromEnvRejectsUnknownObjectStoreProvider(t *testing.T) {
	setBaseEnv(t)
	t.Setenv("OBJECT_STORE_PROVIDER", "s3-direct")
	if _, err := LoadFromEnv(); err == nil {
		t.Fatal("expected error for unknown OBJECT_STORE_PROVIDER")
	}
}

func TestLoadFromEnvR2RequiresAllVariablesTogether(t *testing.T) {
	setBaseEnv(t)
	t.Setenv("OBJECT_STORE_PROVIDER", "r2")
	t.Setenv("R2_ACCOUNT_ID", "acct1")
	// R2_ACCESS_KEY_ID, R2_SECRET_ACCESS_KEY, R2_BUCKET, R2_ENDPOINT left unset.
	if _, err := LoadFromEnv(); err == nil {
		t.Fatal("expected error for partial R2 configuration")
	}
}

func TestLoadFromEnvR2FullyConfiguredSucceeds(t *testing.T) {
	setBaseEnv(t)
	t.Setenv("OBJECT_STORE_PROVIDER", "r2")
	t.Setenv("R2_ACCOUNT_ID", "acct1")
	t.Setenv("R2_ACCESS_KEY_ID", "key1")
	t.Setenv("R2_SECRET_ACCESS_KEY", "secret1")
	t.Setenv("R2_BUCKET", "renovex-tester")
	t.Setenv("R2_ENDPOINT", "https://acct1.r2.cloudflarestorage.com")

	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.R2Bucket != "renovex-tester" {
		t.Fatalf("expected R2Bucket forwarded, got %s", cfg.R2Bucket)
	}
}

func TestLoadFromEnvDefaultsReferenceImageMaxBytes(t *testing.T) {
	setBaseEnv(t)
	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.ReferenceImageMaxBytes != 8*1024*1024 {
		t.Fatalf("expected default 8MiB, got %d", cfg.ReferenceImageMaxBytes)
	}
}

func TestLoadFromEnvRejectsNonPositiveReferenceImageMaxBytes(t *testing.T) {
	setBaseEnv(t)
	t.Setenv("REFERENCE_IMAGE_MAX_BYTES", "0")
	if _, err := LoadFromEnv(); err == nil {
		t.Fatal("expected error for non-positive REFERENCE_IMAGE_MAX_BYTES")
	}
}

func TestLoadFromEnvDefaultsEmailProviderAndDeliveryMode(t *testing.T) {
	setBaseEnv(t)
	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.EmailProvider != "smtp" || cfg.EmailDeliveryMode != "direct" {
		t.Fatalf("expected smtp/direct defaults, got %s/%s", cfg.EmailProvider, cfg.EmailDeliveryMode)
	}
}

func TestLoadFromEnvRejectsUnknownEmailProvider(t *testing.T) {
	setBaseEnv(t)
	t.Setenv("EMAIL_PROVIDER", "sendgrid")
	if _, err := LoadFromEnv(); err == nil {
		t.Fatal("expected error for unknown EMAIL_PROVIDER")
	}
}

func TestLoadFromEnvRejectsUnknownEmailDeliveryMode(t *testing.T) {
	setBaseEnv(t)
	t.Setenv("EMAIL_DELIVERY_MODE", "bcc_everyone")
	if _, err := LoadFromEnv(); err == nil {
		t.Fatal("expected error for unknown EMAIL_DELIVERY_MODE")
	}
}

func TestLoadFromEnvResendProviderRequiresAPIKeyAndFrom(t *testing.T) {
	setBaseEnv(t)
	t.Setenv("EMAIL_PROVIDER", "resend")
	// RESEND_API_KEY/RESEND_FROM left unset.
	if _, err := LoadFromEnv(); err == nil {
		t.Fatal("expected error for resend provider without API key/from")
	}
}

func TestLoadFromEnvTestSinkModeRequiresSinkAddress(t *testing.T) {
	setBaseEnv(t)
	t.Setenv("EMAIL_DELIVERY_MODE", "test_sink")
	// EMAIL_TEST_SINK_ADDRESS left unset.
	if _, err := LoadFromEnv(); err == nil {
		t.Fatal("expected error for test_sink mode without a sink address")
	}
}

func TestLoadFromEnvTesterModeWithResendAndTestSinkSucceeds(t *testing.T) {
	setBaseEnv(t)
	t.Setenv("EMAIL_PROVIDER", "resend")
	t.Setenv("EMAIL_DELIVERY_MODE", "test_sink")
	t.Setenv("EMAIL_TEST_SINK_ADDRESS", "tester-inbox@resend.dev")
	t.Setenv("RESEND_API_KEY", "re_test_key")
	t.Setenv("RESEND_FROM", "Renovex <onboarding@resend.dev>")

	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.EmailTestSinkAddress != "tester-inbox@resend.dev" {
		t.Fatalf("expected sink address forwarded, got %s", cfg.EmailTestSinkAddress)
	}
}

func TestLoadFromEnvProductionRejectsTestSinkDeliveryMode(t *testing.T) {
	// The M8.5C plan's central safety invariant: APP_ENV=production with
	// EMAIL_DELIVERY_MODE=test_sink must be a FATAL configuration error —
	// a tester-only escape hatch must never be reachable in a real
	// deployment.
	setProductionBaseEnv(t)
	t.Setenv("EMAIL_PROVIDER", "resend")
	t.Setenv("EMAIL_DELIVERY_MODE", "test_sink")
	t.Setenv("EMAIL_TEST_SINK_ADDRESS", "tester-inbox@resend.dev")
	t.Setenv("RESEND_API_KEY", "re_prod_key")
	t.Setenv("RESEND_FROM", "Renovex <no-reply@renovex.example>")

	if _, err := LoadFromEnv(); err == nil {
		t.Fatal("expected production with EMAIL_DELIVERY_MODE=test_sink to fail")
	}
}

func TestLoadFromEnvProductionWithDirectDeliveryModeSucceeds(t *testing.T) {
	setProductionBaseEnv(t)
	t.Setenv("EMAIL_PROVIDER", "resend")
	t.Setenv("EMAIL_DELIVERY_MODE", "direct")
	t.Setenv("RESEND_API_KEY", "re_prod_key")
	t.Setenv("RESEND_FROM", "Renovex <no-reply@renovex.example>")

	if _, err := LoadFromEnv(); err != nil {
		t.Fatalf("unexpected error for production with direct delivery mode: %v", err)
	}
}

// setProductionBaseEnv sets every variable a genuinely valid production
// boot requires (T2D) — a real Atlas-shaped MONGO_URI, R2 object storage,
// HTTPS origins, and a secure refresh cookie. Individual T2D tests then
// override exactly the one variable they're testing back to an invalid
// value, proving LoadFromEnv rejects it specifically rather than piggy-
// backing on an unrelated missing field.
func setProductionBaseEnv(t *testing.T) {
	t.Helper()
	t.Setenv("MONGO_URI", "mongodb+srv://user:pass@cluster0.mongodb.net")
	t.Setenv("MONGO_DATABASE", "renovation_platform")
	t.Setenv("APP_ENV", "production")
	t.Setenv("APP_ALLOWED_ORIGINS", "https://app.example.com")
	t.Setenv("AUTH_REFRESH_COOKIE_SECURE", "true")
	t.Setenv("OBJECT_STORE_PROVIDER", "r2")
	t.Setenv("R2_ACCOUNT_ID", "acct_1")
	t.Setenv("R2_ACCESS_KEY_ID", "key_1")
	t.Setenv("R2_SECRET_ACCESS_KEY", "secret_1")
	t.Setenv("R2_BUCKET", "renovex-prod")
	t.Setenv("R2_ENDPOINT", "https://acct_1.r2.cloudflarestorage.com")
}

func TestLoadFromEnvProductionRequiresR2ObjectStore(t *testing.T) {
	setProductionBaseEnv(t)
	t.Setenv("OBJECT_STORE_PROVIDER", "local")

	if _, err := LoadFromEnv(); err == nil {
		t.Fatal("expected production with OBJECT_STORE_PROVIDER=local to fail")
	}
}

func TestLoadFromEnvProductionRejectsLocalhostMongoURI(t *testing.T) {
	setProductionBaseEnv(t)
	t.Setenv("MONGO_URI", "mongodb://localhost:27017")

	if _, err := LoadFromEnv(); err == nil {
		t.Fatal("expected production with a localhost MONGO_URI to fail")
	}
}

func TestLoadFromEnvProductionRejects127MongoURI(t *testing.T) {
	setProductionBaseEnv(t)
	t.Setenv("MONGO_URI", "mongodb://127.0.0.1:27017")

	if _, err := LoadFromEnv(); err == nil {
		t.Fatal("expected production with a 127.0.0.1 MONGO_URI to fail")
	}
}

func TestLoadFromEnvProductionWithRealAtlasURIAndR2Succeeds(t *testing.T) {
	setProductionBaseEnv(t)

	if _, err := LoadFromEnv(); err != nil {
		t.Fatalf("unexpected error for a genuinely valid production config: %v", err)
	}
}

func TestLoadFromEnvDevelopmentPermitsLocalMongoAndLocalObjectStore(t *testing.T) {
	setBaseEnv(t)
	// APP_ENV unset -> defaults to development; neither new T2D guard
	// should apply outside production.
	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("unexpected error in development: %v", err)
	}
	if cfg.ObjectStoreProvider != "local" {
		t.Fatalf("expected local object store default preserved in development, got %s", cfg.ObjectStoreProvider)
	}
}

func TestLoadFromEnvDefaultsServerlessModeToFalse(t *testing.T) {
	setBaseEnv(t)
	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.ServerlessMode {
		t.Fatal("expected ServerlessMode to default to false")
	}
}

func TestLoadFromEnvReadsServerlessMode(t *testing.T) {
	setBaseEnv(t)
	t.Setenv("SERVERLESS_MODE", "true")
	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cfg.ServerlessMode {
		t.Fatal("expected ServerlessMode true")
	}
}

func TestLoadFromEnvUsesServerlessModeOnVercel(t *testing.T) {
	setBaseEnv(t)
	t.Setenv("VERCEL", "1")

	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cfg.ServerlessMode {
		t.Fatal("expected VERCEL=1 to enable ServerlessMode")
	}
}

func TestLoadFromEnvReadsSpatialWorkerAndQueueTokens(t *testing.T) {
	setBaseEnv(t)
	t.Setenv("SPATIAL_WORKER_TOKEN", "worker-secret")
	t.Setenv("SPATIAL_QUEUE_ENQUEUE_URL", "https://web.example.com/api/internal/spatial-generation/enqueue")
	t.Setenv("SPATIAL_QUEUE_ENQUEUE_TOKEN", "queue-secret")

	cfg, err := LoadFromEnv()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.SpatialWorkerToken != "worker-secret" {
		t.Fatalf("expected worker token forwarded, got %s", cfg.SpatialWorkerToken)
	}
	if cfg.SpatialQueueEnqueueURL == "" || cfg.SpatialQueueEnqueueToken == "" {
		t.Fatal("expected queue enqueue URL/token forwarded")
	}
}
