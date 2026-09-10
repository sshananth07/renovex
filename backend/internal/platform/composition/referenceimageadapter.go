package composition

import (
	"context"
	"encoding/base64"
	"errors"

	platformai "github.com/shananth/renovation-platform/backend/internal/platform/ai"
	"github.com/shananth/renovation-platform/backend/internal/spatial"
)

// referenceImageAdapter adapts platformai.ReferenceImageClient's transport
// DTOs to spatial.ReferenceImageGenerator's interface — the ONLY place
// spatial's primitive ReferenceImageGenerationRequest is translated to the
// platform/ai wire contract, matching spatialReasoningAdapter's exact "thin
// translation layer so internal/spatial never imports the platform package
// directly" precedent.
type referenceImageAdapter struct {
	client *platformai.ReferenceImageClient
}

func NewReferenceImageAdapter(client *platformai.ReferenceImageClient) *referenceImageAdapter {
	return &referenceImageAdapter{client: client}
}

func (a *referenceImageAdapter) GenerateReference(ctx context.Context, req spatial.ReferenceImageGenerationRequest) (spatial.ReferenceImageGenerationResult, error) {
	resp, err := a.client.GenerateReference(ctx, toReferenceImageRequest(req))
	if err != nil {
		return spatial.ReferenceImageGenerationResult{}, mapPlatformReferenceImageError(err)
	}

	imageBytes, err := base64.StdEncoding.DecodeString(resp.ImageBase64)
	if err != nil {
		return spatial.ReferenceImageGenerationResult{}, spatial.ErrDesignReasoningInvalidOutput
	}

	return spatial.ReferenceImageGenerationResult{
		ImageBytes: imageBytes, ContentType: resp.ContentType,
		Provider: resp.Provider, Model: resp.Model, ProviderRequestID: resp.ProviderRequestID,
		Seed: resp.Seed, PromptVersion: resp.PromptVersion,
	}, nil
}

// mapPlatformReferenceImageError reuses spatial's existing design-reasoning
// sentinels — the caller (Gate 2's attempt worker) classifies reference-
// generation failures with the same ambiguous-vs-definite distinction
// CreateDesignTurn's classifyAndFinishReasonerError already implements, so
// no parallel sentinel set is introduced for a second provider call site.
func mapPlatformReferenceImageError(err error) error {
	switch {
	case errors.Is(err, platformai.ErrProviderRateLimited):
		return spatial.ErrDesignReasoningProviderRejected
	case errors.Is(err, platformai.ErrProviderUnavailable), errors.Is(err, platformai.ErrServiceUnavailable):
		return spatial.ErrDesignReasoningProviderUnavailable
	case errors.Is(err, platformai.ErrProviderTimeout):
		return spatial.ErrDesignReasoningTimeout
	case errors.Is(err, platformai.ErrInvalidProviderResponse), errors.Is(err, platformai.ErrInvalidServiceResponse):
		return spatial.ErrDesignReasoningInvalidOutput
	case errors.Is(err, platformai.ErrInvalidRequest):
		return spatial.ErrDesignReasoningInvalidOutput
	default:
		return spatial.ErrDesignReasoningProviderUnavailable
	}
}

func toReferenceImageRequest(req spatial.ReferenceImageGenerationRequest) platformai.ReferenceImageRequest {
	var dimensions *platformai.ReferenceTargetDimensions
	if req.Target.Dimensions != nil {
		dimensions = &platformai.ReferenceTargetDimensions{
			Width: req.Target.Dimensions.X, Height: req.Target.Dimensions.Y, Depth: req.Target.Dimensions.Z,
		}
	}

	var appearance *platformai.ReferenceMaterialAppearance
	if req.Appearance != nil {
		appearance = &platformai.ReferenceMaterialAppearance{
			BaseColor: req.Appearance.BaseColor, MaterialFamily: string(req.Appearance.MaterialFamily),
			Roughness: string(req.Appearance.Roughness), Metallic: req.Appearance.Metallic,
		}
	}

	return platformai.ReferenceImageRequest{
		SchemaVersion: "1", DesignSessionID: req.DesignSessionID, TurnID: req.TurnID, PlanFingerprint: req.PlanFingerprint,
		Target: platformai.ReferenceTarget{
			Kind: string(req.Target.Kind), ID: req.Target.ID, Category: req.Target.Category, DimensionsMeters: dimensions,
		},
		AssetGenerationSpec: platformai.ReferenceAssetGenerationSpec{
			Category: req.Geometry.Category, ShapeDescription: req.Geometry.ShapeDescription,
			PreserveCanonicalDimensions: req.Geometry.PreserveCanonicalDimensions,
		},
		MaterialAppearance: appearance,
		RenderBrief: platformai.ReferenceRenderBrief{
			View: "three_quarter_front", Isolated: true, FullObjectVisible: true, Background: "plain_warm_white",
			NoText: true, NoPeople: true, NoRoom: true,
		},
		PromptVersion: req.PromptVersion, Seed: req.Seed,
	}
}
