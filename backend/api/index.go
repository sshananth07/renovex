// Package api is the Vercel Go serverless entrypoint for the Renovation
// Project Intelligence Platform backend. Vercel's Go runtime invokes the
// exported Handler function once per HTTP request; it never calls main()
// and never starts a listener. Handler lazily initializes config, the
// Mongo client, and the full service graph EXACTLY ONCE per warm process
// (M8.5C plan: "The Vercel function lazily initializes config, Mongo, and
// services, and the router once per warm process. It never starts the
// local ticker or listens on a port."), caching them across warm
// invocations the same way any long-lived Go process would — a cold start
// pays full initialization cost once; every subsequent invocation on that
// same warm instance reuses the cached client/handler.
package api

import (
	"context"
	"net/http"
	"os"
	"sync"

	"github.com/shananth/renovation-platform/backend/internal/platform/composition"
	"github.com/shananth/renovation-platform/backend/internal/platform/config"
	"github.com/shananth/renovation-platform/backend/internal/platform/logging"
	platformmongo "github.com/shananth/renovation-platform/backend/internal/platform/mongo"
)

var (
	initOnce   sync.Once
	initErr    error
	cachedHTTP http.Handler
)

// buildOnce performs the exact same lazy initialization cmd/api/main.go
// performs at process startup — config load, Mongo connect, service graph
// construction, router construction — but exactly once per warm Vercel
// process (sync.Once), never on every request. It deliberately does NOT
// call composition.StartAssetGenerationDispatchLoop: that loop assumes a
// long-lived process and would either never run (a Vercel invocation ends
// after the response) or, worse, leak a goroutine across invocations on
// the same warm instance with no way to stop it — Gate 2's bounded worker
// steps (invoked by the Vercel Queue consumer) are Vercel's actual
// execution model, not this ticker.
func buildOnce() {
	initOnce.Do(func() {
		cfg, err := config.LoadFromEnv()
		if err != nil {
			initErr = err
			return
		}
		// ServerlessMode is set for operator clarity in logs/metrics; the
		// actual "never start the local ticker" guarantee comes from this
		// file simply never calling StartAssetGenerationDispatchLoop, not
		// from branching on the flag — there is no code path here that
		// could start it by mistake.
		cfg.ServerlessMode = true

		logger := logging.New(os.Stdout, "info")

		mongoClient, err := platformmongo.Connect(cfg.MongoURI)
		if err != nil {
			initErr = err
			return
		}
		db := platformmongo.Database(mongoClient, cfg.MongoDatabase)

		services, err := composition.BuildServices(context.Background(), cfg, logger, db)
		if err != nil {
			initErr = err
			return
		}

		cachedHTTP = composition.NewHTTPHandler(cfg, logger, mongoClient, services)
	})
}

// Handler is Vercel's Go runtime entrypoint — the exact exported signature
// Vercel's @vercel/go builder requires. Every invocation on a warm process
// reuses the SAME cached handler/Mongo client buildOnce constructed; a cold
// start pays initialization cost once. An initialization failure is
// reported on every subsequent request on this process (never silently
// serving requests against a half-built service graph) until a fresh cold
// start gets a chance to succeed.
func Handler(w http.ResponseWriter, r *http.Request) {
	buildOnce()
	if initErr != nil {
		http.Error(w, "service initialization failed", http.StatusInternalServerError)
		return
	}
	cachedHTTP.ServeHTTP(w, r)
}
