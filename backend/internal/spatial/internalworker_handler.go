package spatial

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
)

// RegisterInternalWorkerHandlers registers the RP4E2/RP4E0 bounded-worker
// routes OUTSIDE the public OpenAPI group (huma.Operation.Hidden — never
// appears in the generated schema/docs) and outside the JWT-authenticated
// group entirely: these are called only by Gate 4's Vercel Queue consumer
// (or, locally, StartAssetGenerationDispatchLoop-style polling), never by a
// contractor's browser session. Authentication is a single static
// shared-secret header (X-Spatial-Worker-Token), matching
// SPATIAL_WORKER_TOKEN's own doc comment ("guards Queue-consumer-or-similar
// callers, never a contractor bearer token") — deliberately NOT the JWT
// middleware, which assumes an authenticated human principal that does not
// exist for this caller.
//
// workerToken empty means these routes are not registered at all — the
// caller (composition root) only calls this when SPATIAL_WORKER_TOKEN is
// actually configured, matching every other optional-capability's "absent
// config == feature unavailable" convention in this codebase.
func RegisterInternalWorkerHandlers(api huma.API, svc *Service, workerToken string) {
	huma.Register(api, huma.Operation{
		OperationID: "spatial-internal-design-generation-process-one",
		Method:      http.MethodPost,
		Path:        "/internal/spatial/design-generation/process-one",
		Summary:     "Bounded worker step: advance one design generation attempt's reference phase (RP4E2 Gate 2)",
		Hidden:      true,
	}, func(ctx context.Context, input *internalWorkerInput) (*internalWorkerOutput, error) {
		if err := verifyWorkerToken(input.WorkerToken, workerToken); err != nil {
			return nil, err
		}
		processed, err := svc.ProcessOneDesignGenerationAttempt(ctx, "queue-worker")
		if err != nil {
			return nil, mapDesignGenerationError(err)
		}
		return internalWorkerOutputFor(processed), nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "spatial-internal-asset-generation-process-one",
		Method:      http.MethodPost,
		Path:        "/internal/spatial/asset-generation/process-one",
		Summary:     "Bounded worker step: advance one existing RP4E0 asset generation job (RP4E0/RP4E2 Gate 2)",
		Hidden:      true,
	}, func(ctx context.Context, input *internalWorkerInput) (*internalWorkerOutput, error) {
		if err := verifyWorkerToken(input.WorkerToken, workerToken); err != nil {
			return nil, err
		}
		processed, err := svc.ProcessOneAssetGenerationJob(ctx, "queue-worker")
		if err != nil {
			return nil, mapSpatialError(err)
		}
		return internalWorkerOutputFor(processed), nil
	})
}

// verifyWorkerToken does a constant-time-irrelevant plain comparison — this
// is a single shared secret known only to the Web project's enqueue bridge
// and this API, not a per-caller credential, so there is no timing-attack
// surface worth defending (matching the plan's own "company-safe attempt/
// job ID or an empty wake hint; no authoritative state" trust model for
// this internal boundary).
func verifyWorkerToken(provided, expected string) error {
	if expected == "" || provided != expected {
		return huma.Error401Unauthorized("invalid or missing worker token")
	}
	return nil
}

type internalWorkerInput struct {
	WorkerToken string `header:"X-Spatial-Worker-Token"`
}

// internalWorkerOutput is the plan's exact documented control-flow shape:
// "{processed, terminal, nextWakeDelaySeconds}". terminal/nextWakeDelaySeconds
// are necessarily conservative here — ProcessOneDesignGenerationAttempt/
// ProcessOneAssetGenerationJob report only "was something claimed and
// advanced," not the resulting status, so this never claims false
// certainty: processed=true always asks for one more wake (the next call
// naturally finds nothing claimable and reports processed=false once the
// underlying work has genuinely reached a terminal state), which is
// strictly safe — an extra idle wake costs nothing under the Mongo
// claim-safety rules that are the actual correctness boundary — never
// unsafe in the other direction (silently dropping a wake a still-pending
// job needed).
type internalWorkerOutput struct {
	Body struct {
		Processed            bool `json:"processed"`
		Terminal             bool `json:"terminal"`
		NextWakeDelaySeconds int  `json:"nextWakeDelaySeconds,omitempty"`
	}
}

const internalWorkerFollowUpDelaySeconds = 5

func internalWorkerOutputFor(processed bool) *internalWorkerOutput {
	out := &internalWorkerOutput{}
	out.Body.Processed = processed
	out.Body.Terminal = !processed
	if processed {
		out.Body.NextWakeDelaySeconds = internalWorkerFollowUpDelaySeconds
	}
	return out
}
