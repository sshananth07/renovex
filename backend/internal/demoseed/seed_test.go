package demoseed_test

import (
	"context"
	"errors"
	"testing"

	"github.com/shananth/renovation-platform/backend/internal/demoseed"
	"github.com/shananth/renovation-platform/backend/internal/identity"
)

func TestSeed_LeaseIsReleasedOnFailure(t *testing.T) {
	// If Seed fails partway through, the lease must still be released so a
	// subsequent invocation is not blocked waiting out the full lease
	// duration for no reason.
	ctx := context.Background()
	db := setupMongoDB(t)

	brokenAuth := &fakeAuth{registerErr: errors.New("simulated registration failure")}
	deps := demoseed.SeedDependencies{Auth: brokenAuth, Users: &fakeUserLookup{byEmail: map[string]identity.User{}}}
	_, err := demoseed.SeedWithDependencies(ctx, db, deps, "test-only-password")
	if err == nil {
		t.Fatalf("expected Seed to propagate a Register failure")
	}

	manifests := demoseed.NewManifestStore(db)
	manifest, found, getErr := manifests.Get(ctx)
	if getErr != nil || !found {
		t.Fatalf("expected a manifest to exist even after a failed seed: found=%v err=%v", found, getErr)
	}
	if manifest.LeaseOwner != "" {
		t.Fatalf("expected the lease to be released after a failed seed, got owner=%q", manifest.LeaseOwner)
	}
}

func TestSeed_ConcurrentInvocation_SecondRefuses(t *testing.T) {
	ctx := context.Background()
	db := setupMongoDB(t)
	manifests := demoseed.NewManifestStore(db)
	_ = manifests.EnsureIndexes(ctx)

	// Simulate a first Seed invocation that is still "in flight" by
	// directly acquiring the lease under a different owner token and never
	// releasing it — exactly what a genuinely concurrent second process
	// would observe.
	if _, err := manifests.AcquireLease(ctx, "other-process-owner-token", testLeaseDuration); err != nil {
		t.Fatalf("simulate a concurrent process's lease: %v", err)
	}

	deps := demoseed.SeedDependencies{Auth: &fakeAuth{}, Users: &fakeUserLookup{byEmail: map[string]identity.User{}}}
	_, err := demoseed.SeedWithDependencies(ctx, db, deps, "test-only-password")
	if err == nil {
		t.Fatalf("expected Seed to refuse while another process holds the lease")
	}
}
