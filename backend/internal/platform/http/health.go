package http

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"go.mongodb.org/mongo-driver/v2/mongo"

	platformmongo "github.com/shananth/renovation-platform/backend/internal/platform/mongo"
)

// readinessProbeTimeout bounds the readiness probe so a hung or unreachable
// deployment fails closed quickly instead of holding the request open.
const readinessProbeTimeout = 2 * time.Second

// notReady is the single external readiness failure. The cause — unreachable,
// unsupported topology, or no client — is deliberately indistinguishable:
// replica-set names, driver text and connection strings are operational
// diagnostics, not public information.
func notReady() error {
	return huma.Error503ServiceUnavailable("service_not_ready")
}

// HealthOutput is the response body for /health and /ready.
type HealthOutput struct {
	Body struct {
		Status string `json:"status" example:"ok" doc:"ok or unavailable"`
	}
}

// RegisterHealth registers /health (process liveness; never touches Mongo)
// and /ready (Mongo connectivity AND transaction capability) on api.
//
// mongoClient may be nil, in which case /ready always reports unavailable.
// databaseName names the database the read-only transaction probe runs
// against; it is only used to reach a probe collection, never to read business
// data.
func RegisterHealth(
	api huma.API, mongoClient *mongo.Client, databaseName string) {
	huma.Register(api, huma.Operation{
		OperationID: "get-health",
		Method:      http.MethodGet,
		Path:        "/health",
		Summary:     "Process liveness check",
		Tags:        []string{"System"},
	}, func(_ context.Context, _ *struct{}) (*HealthOutput, error) {
		resp := &HealthOutput{}
		resp.Body.Status = "ok"
		return resp, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "get-ready",
		Method:      http.MethodGet,
		Path:        "/ready",
		Summary: "Readiness check (MongoDB connectivity and transaction " +
			"capability)",
		Tags: []string{"System"},
	}, func(ctx context.Context, _ *struct{}) (*HealthOutput, error) {
		// Every unready cause returns ONE generic code. Distinguishing "down"
		// from "wrong topology" would tell an unauthenticated caller about the
		// deployment; the specific cause belongs in operational logs.
		probeCtx, cancel := context.WithTimeout(ctx, readinessProbeTimeout)
		defer cancel()

		if mongoClient == nil {
			return nil, notReady()
		}
		if err := platformmongo.Ping(probeCtx, mongoClient); err != nil {
			return nil, notReady()
		}
		// Connectivity alone is not readiness: a standalone deployment answers
		// pings while the withdrawal boundary cannot begin. The probe is a
		// read-only transaction, so readiness never mutates business data.
		if err := platformmongo.VerifyTransactionSupport(
			probeCtx, mongoClient, databaseName); err != nil {
			return nil, notReady()
		}

		resp := &HealthOutput{}
		resp.Body.Status = "ok"
		return resp, nil
	})
}
