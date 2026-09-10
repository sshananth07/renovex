package composition

import (
	"context"
	"time"

	"github.com/shananth/renovation-platform/backend/internal/spatial"
)

// StartAssetGenerationDispatchLoop is a LOCAL/DEV wake-up hint only — it
// periodically calls ProcessOneAssetGenerationJob so a submitted job
// doesn't wait indefinitely for a real dispatch signal. Mongo's
// ClaimNext query remains the only correctness boundary; losing this
// loop (process restart, no loop running) only delays processing, never
// breaks it. NOT sufficient for a production Vercel deployment — a
// durable production wake-up mechanism (e.g. Vercel Queues or an
// external cron/worker) is explicitly deferred to a future slice, never
// built or assumed working here.
func StartAssetGenerationDispatchLoop(ctx context.Context, svc *spatial.Service, workerID string, pollInterval time.Duration) {
	go func() {
		ticker := time.NewTicker(pollInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				_, _ = svc.ProcessOneAssetGenerationJob(ctx, workerID)
			}
		}
	}()
}
