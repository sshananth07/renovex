package spatial

import (
	"context"
	"io"
)

// fakeAssetGenerationProvider is a scriptable in-memory
// AssetGenerationProvider for service-level tests — mirrors this
// package's existing fake-repository convention. StartCalls/ResumeCalls
// let a test assert exactly how many times each phase was invoked, which
// is the whole point of the two-phase design (proving a resumed job never
// re-invokes StartShapeGeneration).
type fakeAssetGenerationProvider struct {
	StartCalls  int
	ResumeCalls int

	StartFunc  func(ctx context.Context, source AssetGenerationSource, seed int64) (ProviderGenerationRef, error)
	ResumeFunc func(ctx context.Context, ref ProviderGenerationRef) (ProviderGenerationResult, error)
}

func (f *fakeAssetGenerationProvider) StartShapeGeneration(ctx context.Context, source AssetGenerationSource, seed int64) (ProviderGenerationRef, error) {
	f.StartCalls++
	return f.StartFunc(ctx, source, seed)
}

func (f *fakeAssetGenerationProvider) ResumeShapeGeneration(ctx context.Context, ref ProviderGenerationRef) (ProviderGenerationResult, error) {
	f.ResumeCalls++
	return f.ResumeFunc(ctx, ref)
}

type readCloserFromBytes struct{ io.Reader }

func (readCloserFromBytes) Close() error { return nil }
