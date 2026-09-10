package composition

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rs/zerolog"

	"github.com/shananth/renovation-platform/backend/internal/platform/config"
	"github.com/shananth/renovation-platform/backend/internal/spatial"
)

// TestNewHTTPHandler_BuildsWithoutListening proves NewHTTPHandler returns a
// usable http.Handler without ever calling ListenAndServe — the Vercel
// serverless entrypoint (api/index.go) needs exactly this: a handler it can
// pass directly to its own request-per-invocation model, never a listening
// server (M8.5C plan's Vercel-safety requirement).
func TestNewHTTPHandler_BuildsWithoutListening(t *testing.T) {
	cfg := config.Config{
		AppEnv: "test", HTTPPort: "8080",
		MongoDatabase: "test", RefreshCookieSecure: true,
	}
	services := &Services{}

	handler := NewHTTPHandler(cfg, zerolog.Nop(), nil, services)
	if handler == nil {
		t.Fatal("expected a non-nil handler")
	}

	// A real HTTP round-trip against the returned handler (via httptest,
	// never a real listener) proves it is a genuinely wired mux, not a
	// stub — /health is registered unauthenticated so this needs no token.
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code == http.StatusNotFound {
		t.Fatalf("expected /health to be registered, got 404")
	}
}

// TestNewHTTPHandler_InternalWorkerRoutesOnlyMountedWhenTokenConfigured
// proves RegisterInternalWorkerHandlers' own "absent config == route
// absent" contract actually holds at the composition-root level, and that
// a configured route genuinely enforces the worker token (never reachable
// by an ordinary unauthenticated or JWT-bearer request).
func TestNewHTTPHandler_InternalWorkerRoutesOnlyMountedWhenTokenConfigured(t *testing.T) {
	baseCfg := config.Config{AppEnv: "test", HTTPPort: "8080", MongoDatabase: "test", RefreshCookieSecure: true}

	t.Run("route absent when SpatialWorkerToken is empty", func(t *testing.T) {
		handler := NewHTTPHandler(baseCfg, zerolog.Nop(), nil, &Services{Spatial: &spatial.Service{}})
		req := httptest.NewRequest(http.MethodPost, "/internal/spatial/design-generation/process-one", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404 when no worker token is configured, got %d", rec.Code)
		}
	})

	t.Run("route present but rejects a missing/wrong token", func(t *testing.T) {
		cfg := baseCfg
		cfg.SpatialWorkerToken = "the-real-token"
		handler := NewHTTPHandler(cfg, zerolog.Nop(), nil, &Services{Spatial: &spatial.Service{}})

		req := httptest.NewRequest(http.MethodPost, "/internal/spatial/design-generation/process-one", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code == http.StatusNotFound {
			t.Fatal("expected the route to be mounted once SpatialWorkerToken is configured")
		}
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401 for a missing worker token, got %d: %s", rec.Code, rec.Body.String())
		}

		req2 := httptest.NewRequest(http.MethodPost, "/internal/spatial/design-generation/process-one", nil)
		req2.Header.Set("X-Spatial-Worker-Token", "wrong-token")
		rec2 := httptest.NewRecorder()
		handler.ServeHTTP(rec2, req2)
		if rec2.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401 for a wrong worker token, got %d: %s", rec2.Code, rec2.Body.String())
		}
	})

	t.Run("route present and reaches the service call with a valid token", func(t *testing.T) {
		cfg := baseCfg
		cfg.SpatialWorkerToken = "the-real-token"
		// A bare &spatial.Service{} has never had SetDesignGenerationSupport
		// called, so ProcessOneDesignGenerationAttempt returns
		// ErrDesignGenerationSupportNotConfigured (503) — a status
		// deliberately distinct from 401, proving the request genuinely
		// passed the token check and reached the real service call rather
		// than being rejected before it.
		handler := NewHTTPHandler(cfg, zerolog.Nop(), nil, &Services{Spatial: &spatial.Service{}})
		req := httptest.NewRequest(http.MethodPost, "/internal/spatial/design-generation/process-one", nil)
		req.Header.Set("X-Spatial-Worker-Token", "the-real-token")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("expected 503 (service call reached but unconfigured) for a valid token, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("internal worker routes are hidden from the public OpenAPI schema", func(t *testing.T) {
		cfg := baseCfg
		cfg.SpatialWorkerToken = "the-real-token"
		handler := NewHTTPHandler(cfg, zerolog.Nop(), nil, &Services{Spatial: &spatial.Service{}})

		req := httptest.NewRequest(http.MethodGet, "/openapi.json", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected /openapi.json to be served, got %d", rec.Code)
		}
		body := rec.Body.String()
		if strings.Contains(body, "/internal/spatial/design-generation/process-one") || strings.Contains(body, "/internal/spatial/asset-generation/process-one") {
			t.Fatal("expected the internal worker routes to be absent from the public OpenAPI document")
		}
	})
}
