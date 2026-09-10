package composition

import (
	"context"

	"github.com/shananth/renovation-platform/backend/internal/platform/hunyuan"
	"github.com/shananth/renovation-platform/backend/internal/spatial"
)

// hunyuanProviderAdapter adapts hunyuan.Client's concrete types to
// spatial.AssetGenerationProvider's interface — a thin translation layer
// so internal/spatial never imports internal/platform/hunyuan directly
// (consumer-defines-interface, matching every other RP4D/RP4E0 provider
// boundary in this codebase).
type hunyuanProviderAdapter struct {
	client *hunyuan.Client
}

func NewHunyuanProviderAdapter(client *hunyuan.Client) *hunyuanProviderAdapter {
	return &hunyuanProviderAdapter{client: client}
}

func (a *hunyuanProviderAdapter) StartShapeGeneration(ctx context.Context, source spatial.AssetGenerationSource, seed int64) (spatial.ProviderGenerationRef, error) {
	ref, err := a.client.StartShapeGeneration(ctx, hunyuan.Source{URL: source.URL}, seed)
	return spatial.ProviderGenerationRef{ID: ref.ID}, err
}

func (a *hunyuanProviderAdapter) ResumeShapeGeneration(ctx context.Context, ref spatial.ProviderGenerationRef) (spatial.ProviderGenerationResult, error) {
	result, err := a.client.ResumeShapeGeneration(ctx, hunyuan.Ref{ID: ref.ID})
	if err != nil {
		return spatial.ProviderGenerationResult{}, err
	}
	return spatial.ProviderGenerationResult{
		Status:         spatial.ProviderGenerationStatus(result.Status),
		Output:         result.Output,
		FailureMessage: result.FailureMessage,
	}, nil
}
