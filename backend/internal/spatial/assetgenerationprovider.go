package spatial

import (
	"context"
	"io"
)

// AssetGenerationSource is a resolved, provider-ready reference to a
// tenant-authorized source image — never a raw io.Reader the Go backend
// proxies, and never an arbitrary caller-supplied URL (RP4E0 spec §5/§6).
type AssetGenerationSource struct {
	URL string
}

type ProviderGenerationStatus string

const (
	ProviderGenerationPending      ProviderGenerationStatus = "pending"
	ProviderGenerationCompleted    ProviderGenerationStatus = "completed"
	ProviderGenerationQuotaBlocked ProviderGenerationStatus = "quota_blocked"
	ProviderGenerationRejected     ProviderGenerationStatus = "rejected"
)

type ProviderGenerationRef struct {
	ID string // Gradio's event_id, or the equivalent for a future provider
}

type ProviderGenerationResult struct {
	Status         ProviderGenerationStatus
	Output         io.ReadCloser // set only when Status == ProviderGenerationCompleted; caller closes
	FailureMessage string
}

// AssetGenerationProvider is the provider-neutral boundary — the ONLY
// place spatial ever calls out to an external 3D generation service
// (RP4E0 spec §5). Two-phase to match Gradio's real HTTP contract
// (POST returns an event_id immediately; the result is retrieved via a
// separate resumable call) — this is what lets a worker crash and resume
// without ever re-invoking the provider once StartShapeGeneration has
// already been acknowledged.
type AssetGenerationProvider interface {
	// StartShapeGeneration submits a new generation request and returns
	// as soon as the provider ACKs receipt — it does NOT wait for the
	// result. The caller MUST durably persist ref.ID before doing
	// anything else; never call this twice for the same logical job.
	StartShapeGeneration(ctx context.Context, source AssetGenerationSource, seed int64) (ProviderGenerationRef, error)

	// ResumeShapeGeneration polls/reconnects to an ALREADY-STARTED
	// generation. Safe to call repeatedly and from a different process
	// than the one that called StartShapeGeneration.
	ResumeShapeGeneration(ctx context.Context, ref ProviderGenerationRef) (ProviderGenerationResult, error)
}
