// Command api is the entrypoint for the Renovation Project Intelligence
// Platform backend: loads configuration, connects to MongoDB, wires the
// HTTP router, and serves the API.
package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/joho/godotenv"

	"github.com/shananth/renovation-platform/backend/internal/platform/composition"
	"github.com/shananth/renovation-platform/backend/internal/platform/config"
	"github.com/shananth/renovation-platform/backend/internal/platform/logging"
	platformmongo "github.com/shananth/renovation-platform/backend/internal/platform/mongo"
)

func main() {
	// Loading a .env file is best-effort; in production config comes from
	// real environment variables and no .env file is expected to exist. Try
	// the process's working directory first, then fall back to the repo
	// root, since the documented workflow is `cd backend && go run
	// ./cmd/api` while .env lives at the repo root.
	if err := godotenv.Load(); err != nil {
		_ = godotenv.Load("../.env")
	}

	cfg, err := config.LoadFromEnv()
	if err != nil {
		panic(err)
	}

	logger := logging.New(os.Stdout, "info")
	logger.Info().Str("env", cfg.AppEnv).Msg("starting api")

	mongoClient, err := platformmongo.Connect(cfg.MongoURI)
	if err != nil {
		logger.Fatal().Err(err).Msg("failed to construct mongodb client")
	}
	defer func() {
		if err := platformmongo.Disconnect(context.Background(), mongoClient); err != nil {
			logger.Error().Err(err).Msg("failed to disconnect mongodb client")
		}
	}()

	db := platformmongo.Database(mongoClient, cfg.MongoDatabase)

	services, err := composition.BuildServices(context.Background(), cfg, logger, db)
	if err != nil {
		logger.Fatal().Err(err).Msg("failed to build service graph")
	}

	// RP4E0: local/dev asset-generation dispatch hint — see
	// composition.StartAssetGenerationDispatchLoop's own doc comment for
	// why this is explicitly NOT sufficient for a production Vercel
	// deployment. Only started when the Hugging Face config is present;
	// harmless no-op churn otherwise (ProcessOneAssetGenerationJob
	// returns ErrAssetGenerationNotConfigured immediately when unwired).
	dispatchCtx, cancelDispatch := context.WithCancel(context.Background())
	defer cancelDispatch()
	if cfg.HuggingFaceSpaceURL != "" {
		composition.StartAssetGenerationDispatchLoop(dispatchCtx, services.Spatial, "api-dispatch-worker", 5*time.Second)
	}

	// --- HTTP ---
	// NewHTTPHandler is the ONE router-construction code path this binary
	// shares with the Vercel serverless entrypoint (api/index.go) — every
	// route registration lives there now, not duplicated here.
	handler := composition.NewHTTPHandler(cfg, logger, mongoClient, services)

	server := &http.Server{
		Addr:              ":" + cfg.HTTPPort,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		logger.Info().Str("port", cfg.HTTPPort).Msg("listening")
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Fatal().Err(err).Msg("server failed")
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	logger.Info().Msg("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Error().Err(err).Msg("graceful shutdown failed")
	}
}
