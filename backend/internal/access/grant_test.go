package access

import (
	"testing"
	"time"
)

func futureTime() time.Time { return time.Now().Add(24 * time.Hour) }
func pastTime() time.Time   { return time.Now().Add(-24 * time.Hour) }

func liveGrantAndCoordinator() (AccessGrant, AccessGroupState) {
	grantID := "grant_1"
	grant := AccessGrant{
		ID: grantID, CompanyID: "company_a", ProjectID: "project_1",
		ResourceType: ResourceTypeQuotation, ResourceID: "quotation_1",
		ResourceGroupKey: "quotation:QT-000001", QuotationNumber: "QT-000001",
		Status: AccessGrantStatusActive, ExpiresAt: futureTime(),
	}
	coordinator := AccessGroupState{
		CompanyID: "company_a", ResourceType: ResourceTypeQuotation,
		ResourceGroupKey:  "quotation:QT-000001",
		CurrentResourceID: "quotation_1", ActiveGrantID: &grantID,
	}
	return grant, coordinator
}

func TestGrantIsExternallyLiveHappyPath(t *testing.T) {
	grant, coordinator := liveGrantAndCoordinator()
	if !IsExternallyLive(grant, coordinator, time.Now()) {
		t.Fatal("expected a fully-consistent active grant to be externally live")
	}
}

func TestGrantIsNotLiveWhenRevoked(t *testing.T) {
	grant, coordinator := liveGrantAndCoordinator()
	grant.Status = AccessGrantStatusRevoked
	if IsExternallyLive(grant, coordinator, time.Now()) {
		t.Fatal("expected a revoked grant to be dead")
	}
}

func TestGrantIsNotLiveWhenExpired(t *testing.T) {
	grant, coordinator := liveGrantAndCoordinator()
	grant.ExpiresAt = pastTime()
	if IsExternallyLive(grant, coordinator, time.Now()) {
		t.Fatal("expected an expired grant to be dead")
	}
}

// The two coordinator-authority conditions (design spec §2.3 conditions
// 4-5): AccessGrant.Status alone must NEVER be sufficient.
func TestGrantIsNotLiveWhenCoordinatorPointsElsewhere(t *testing.T) {
	grant, coordinator := liveGrantAndCoordinator()
	other := "some_other_grant"
	coordinator.ActiveGrantID = &other
	if IsExternallyLive(grant, coordinator, time.Now()) {
		t.Fatal("expected a stored-active grant NOT referenced by the coordinator to be dead (orphan)")
	}
}

func TestGrantIsNotLiveWhenCoordinatorHasNoActiveGrant(t *testing.T) {
	grant, coordinator := liveGrantAndCoordinator()
	coordinator.ActiveGrantID = nil
	if IsExternallyLive(grant, coordinator, time.Now()) {
		t.Fatal("expected a grant to be dead when the coordinator has no active grant")
	}
}

func TestGrantIsNotLiveWhenCoordinatorMovedToNewerVersion(t *testing.T) {
	grant, coordinator := liveGrantAndCoordinator()
	coordinator.CurrentResourceID = "quotation_2" // superseded by a newer version
	if IsExternallyLive(grant, coordinator, time.Now()) {
		t.Fatal("expected a grant for a superseded version to be dead")
	}
}

func TestResourceGroupKeyForQuotation(t *testing.T) {
	got := ResourceGroupKeyForQuotation("QT-000124")
	if got != "quotation:QT-000124" {
		t.Fatalf("expected namespaced group key, got %q", got)
	}
}

func TestEffectiveStatus(t *testing.T) {
	grant, coordinator := liveGrantAndCoordinator()
	if got := EffectiveStatus(grant, coordinator, time.Now()); got != EffectiveStatusActive {
		t.Fatalf("expected active, got %q", got)
	}

	expired := grant
	expired.ExpiresAt = pastTime()
	if got := EffectiveStatus(expired, coordinator, time.Now()); got != EffectiveStatusExpired {
		t.Fatalf("expected expired, got %q", got)
	}

	manual := grant
	manual.Status = AccessGrantStatusRevoked
	manual.RevokedReason = RevokedReasonManual
	if got := EffectiveStatus(manual, coordinator, time.Now()); got != EffectiveStatusRevoked {
		t.Fatalf("expected revoked, got %q", got)
	}

	superseded := grant
	superseded.Status = AccessGrantStatusRevoked
	superseded.RevokedReason = RevokedReasonSuperseded
	if got := EffectiveStatus(superseded, coordinator, time.Now()); got != EffectiveStatusSuperseded {
		t.Fatalf("expected superseded, got %q", got)
	}

	rotated := grant
	rotated.Status = AccessGrantStatusRevoked
	rotated.RevokedReason = RevokedReasonRotated
	if got := EffectiveStatus(rotated, coordinator, time.Now()); got != EffectiveStatusRotated {
		t.Fatalf("expected rotated, got %q", got)
	}

	// A stored-active grant whose chain has moved to a DIFFERENT version
	// reads as superseded, even though its own Status still says active.
	supersededOrphan := grant
	supersededCoordinator := coordinator
	supersededCoordinator.CurrentResourceID = "quotation_2"
	if got := EffectiveStatus(supersededOrphan, supersededCoordinator, time.Now()); got != EffectiveStatusSuperseded {
		t.Fatalf("expected a stored-active grant on a superseded version to read as superseded, got %q", got)
	}

	// A stored-active grant on the SAME version, but no longer the
	// coordinator's active grant, reads as rotated — a replacement token was
	// issued for this very version, which is a different fact from the whole
	// version being superseded.
	rotatedOrphan := grant
	rotatedCoordinator := coordinator
	replacement := "grant_2"
	rotatedCoordinator.ActiveGrantID = &replacement
	if got := EffectiveStatus(rotatedOrphan, rotatedCoordinator, time.Now()); got != EffectiveStatusRotated {
		t.Fatalf("expected a stored-active grant replaced on the same version to read as rotated, got %q", got)
	}
}
