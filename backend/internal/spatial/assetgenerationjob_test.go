package spatial

import (
	"testing"
	"time"
)

func ptr(s string) *string           { return &s }
func ptrTime(t time.Time) *time.Time { return &t }

func TestSpatialAssetGenerationJob_IsSafeToClaim(t *testing.T) {
	now := time.Now()
	past := now.Add(-time.Minute)

	cases := []struct {
		name   string
		job    SpatialAssetGenerationJob
		expect bool
	}{
		{
			name:   "never started — safe (lease expired, no lease at all)",
			job:    SpatialAssetGenerationJob{Execution: JobExecution{Status: JobExecutionPending}},
			expect: true,
		},
		{
			name: "provider started, event id known — safe to resume",
			job: SpatialAssetGenerationJob{
				Execution:         JobExecution{Status: JobExecutionProcessing, LeaseExpiresAt: &past},
				ProviderStartedAt: &now,
				ProviderRequestID: ptr("evt_123"),
			},
			expect: true,
		},
		{
			name: "GLB already staged — safe to resume validation",
			job: SpatialAssetGenerationJob{
				Execution:          JobExecution{Status: JobExecutionProcessing, LeaseExpiresAt: &past},
				ProviderStartedAt:  &now,
				GeneratedObjectKey: "asset-generation/c1/j1/raw/abc.glb",
			},
			expect: true,
		},
		{
			name: "THE DANGEROUS STATE — provider started, no event id, no staged GLB — MUST NOT be claimable",
			job: SpatialAssetGenerationJob{
				Execution:         JobExecution{Status: JobExecutionProcessing, LeaseExpiresAt: &past},
				ProviderStartedAt: &now,
			},
			expect: false,
		},
		{
			name: "lease currently held by another worker — not claimable regardless of checkpoint state",
			job: SpatialAssetGenerationJob{
				Execution: JobExecution{Status: JobExecutionProcessing, LeaseOwner: "other-worker", LeaseExpiresAt: ptrTime(now.Add(time.Minute))},
			},
			expect: false,
		},
		{
			name:   "terminal status — not claimable",
			job:    SpatialAssetGenerationJob{Execution: JobExecution{Status: JobExecutionCompleted}},
			expect: false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.job.isSafeToClaim(now); got != c.expect {
				t.Errorf("isSafeToClaim() = %v, want %v", got, c.expect)
			}
		})
	}
}

func TestSpatialAssetGenerationJob_ClaimPhase(t *testing.T) {
	if (SpatialAssetGenerationJob{}).claimPhase() != ClaimPhaseStartProvider {
		t.Error("expected fresh job to be ClaimPhaseStartProvider")
	}
	withEventID := SpatialAssetGenerationJob{ProviderRequestID: ptr("evt_1")}
	if withEventID.claimPhase() != ClaimPhaseResumeProvider {
		t.Error("expected job with event id to be ClaimPhaseResumeProvider")
	}
	withStaged := SpatialAssetGenerationJob{ProviderRequestID: ptr("evt_1"), GeneratedObjectKey: "k"}
	if withStaged.claimPhase() != ClaimPhaseResumeValidation {
		t.Error("expected staged job to be ClaimPhaseResumeValidation regardless of event id presence")
	}
}
