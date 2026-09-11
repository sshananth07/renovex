package demoseed_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/mongodb"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/shananth/renovation-platform/backend/internal/demoseed"
)

func setupMongoDB(t *testing.T) *mongo.Database {
	if testing.Short() {
		t.Skip("integration test: requires Docker/testcontainers; run without -short")
	}
	t.Helper()
	ctx := context.Background()

	container, err := mongodb.Run(ctx, "mongo:7")
	if err != nil {
		t.Fatalf("failed to start mongodb container: %v", err)
	}
	t.Cleanup(func() {
		if err := testcontainers.TerminateContainer(container); err != nil {
			t.Logf("failed to terminate container: %v", err)
		}
	})

	uri, err := container.ConnectionString(ctx)
	if err != nil {
		t.Fatalf("failed to get connection string: %v", err)
	}
	client, err := mongo.Connect(options.Client().ApplyURI(uri))
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	t.Cleanup(func() { _ = client.Disconnect(ctx) })

	return client.Database(fmt.Sprintf("demoseed_test_%d", time.Now().UnixNano()))
}

const testLeaseDuration = 5 * time.Minute

func TestManifestStore_AcquireLease_FirstCallCreates(t *testing.T) {
	ctx := context.Background()
	db := setupMongoDB(t)
	store := demoseed.NewManifestStore(db)
	_ = store.EnsureIndexes(ctx)

	manifest, err := store.AcquireLease(ctx, "process-a", testLeaseDuration)
	if err != nil {
		t.Fatalf("AcquireLease: %v", err)
	}
	if manifest.State != demoseed.ManifestStateProvisioning {
		t.Fatalf("state = %q, want provisioning", manifest.State)
	}
	if manifest.LeaseOwner != "process-a" {
		t.Fatalf("LeaseOwner = %q, want process-a", manifest.LeaseOwner)
	}
	if manifest.DemoUserID != "" || manifest.DemoCompanyID != "" {
		t.Fatalf("expected no stored IDs yet, got user=%q company=%q", manifest.DemoUserID, manifest.DemoCompanyID)
	}
}

func TestManifestStore_AcquireLease_ConcurrentLiveProcessRefuses(t *testing.T) {
	ctx := context.Background()
	db := setupMongoDB(t)
	store := demoseed.NewManifestStore(db)
	_ = store.EnsureIndexes(ctx)

	if _, err := store.AcquireLease(ctx, "process-a", testLeaseDuration); err != nil {
		t.Fatalf("process-a AcquireLease: %v", err)
	}

	// process-b attempts to acquire the SAME manifest while process-a's
	// lease is still live (testLeaseDuration is 5 minutes — nowhere near
	// expired). This is the exact concurrency scenario the review flagged:
	// two simultaneous `demoseed seed` invocations must not both proceed.
	_, err := store.AcquireLease(ctx, "process-b", testLeaseDuration)
	if err == nil {
		t.Fatalf("expected process-b to be refused while process-a holds a live lease")
	}
}

func TestManifestStore_AcquireLease_StaleLeaseIsReclaimable(t *testing.T) {
	ctx := context.Background()
	db := setupMongoDB(t)
	store := demoseed.NewManifestStore(db)
	_ = store.EnsureIndexes(ctx)

	// process-a acquires a lease that expires almost immediately, simulating
	// a crash: the lease is never released, but its expiry passes.
	if _, err := store.AcquireLease(ctx, "process-a", 1*time.Millisecond); err != nil {
		t.Fatalf("process-a AcquireLease: %v", err)
	}
	time.Sleep(10 * time.Millisecond)

	// process-b must now be able to reclaim the stale lease.
	manifest, err := store.AcquireLease(ctx, "process-b", testLeaseDuration)
	if err != nil {
		t.Fatalf("expected process-b to reclaim a stale (expired) lease, got: %v", err)
	}
	if manifest.LeaseOwner != "process-b" {
		t.Fatalf("LeaseOwner = %q, want process-b after reclaiming", manifest.LeaseOwner)
	}
}

func TestManifestStore_LeaseOwnerGating_WrongOwnerRefused(t *testing.T) {
	ctx := context.Background()
	db := setupMongoDB(t)
	store := demoseed.NewManifestStore(db)
	_ = store.EnsureIndexes(ctx)

	if _, err := store.AcquireLease(ctx, "process-a", testLeaseDuration); err != nil {
		t.Fatalf("AcquireLease: %v", err)
	}

	// A caller presenting the WRONG owner token (e.g. a reclaimed-lease
	// straggler) must be refused by every state-mutating method, not just
	// AcquireLease.
	if err := store.RecordIdentity(ctx, "process-b", "user-1", "company-1"); err == nil {
		t.Fatalf("expected RecordIdentity to refuse a non-owning caller")
	}
	if err := store.MarkReady(ctx, "process-b"); err == nil {
		t.Fatalf("expected MarkReady to refuse a non-owning caller")
	}
}

func TestManifestStore_FullLifecycle(t *testing.T) {
	ctx := context.Background()
	db := setupMongoDB(t)
	store := demoseed.NewManifestStore(db)
	_ = store.EnsureIndexes(ctx)

	if _, err := store.AcquireLease(ctx, "process-a", testLeaseDuration); err != nil {
		t.Fatalf("AcquireLease: %v", err)
	}
	if err := store.RecordIdentity(ctx, "process-a", "user-1", "company-1"); err != nil {
		t.Fatalf("RecordIdentity: %v", err)
	}
	if err := store.MarkReady(ctx, "process-a"); err != nil {
		t.Fatalf("MarkReady: %v", err)
	}
	if err := store.ReleaseLease(ctx, "process-a"); err != nil {
		t.Fatalf("ReleaseLease: %v", err)
	}

	got, found, err := store.Get(ctx)
	if err != nil || !found {
		t.Fatalf("Get: found=%v err=%v", found, err)
	}
	if got.State != demoseed.ManifestStateReady || got.DemoUserID != "user-1" || got.DemoCompanyID != "company-1" {
		t.Fatalf("unexpected manifest after MarkReady: %+v", got)
	}
	if got.LeaseOwner != "" {
		t.Fatalf("expected the lease to be cleared after ReleaseLease, got owner=%q", got.LeaseOwner)
	}

	// A NEW process (process-b) can now freely acquire the lease for
	// resetting, since process-a released it. BeginResetting is
	// lease-owner-gated (its Mongo filter requires leaseOwner==ownerToken),
	// so process-b must acquire the lease FIRST — skipping this call is
	// exactly the gap a code review found in an earlier draft of this test:
	// it called BeginResetting for an owner that never held the lease, which
	// the real implementation would reject with ErrManifestInvalidTransition.
	if _, err := store.AcquireLease(ctx, "process-b-after-lease", testLeaseDuration); err != nil {
		t.Fatalf("process-b AcquireLease: %v", err)
	}
	resetting, err := store.BeginResetting(ctx, "process-b-after-lease")
	if err != nil {
		t.Fatalf("BeginResetting: %v", err)
	}
	if resetting.DemoUserID != "user-1" || resetting.DemoCompanyID != "company-1" {
		t.Fatalf("BeginResetting must return the stored anchor IDs, got %+v", resetting)
	}

	// The SAME owner resuming (e.g. a retry within the same reset
	// invocation) on an already-resetting manifest is a safe no-op.
	resumed, err := store.BeginResetting(ctx, "process-b-after-lease")
	if err != nil {
		t.Fatalf("BeginResetting resumed by the SAME owner should not error: %v", err)
	}
	if resumed.DemoUserID != "user-1" {
		t.Fatalf("resumed BeginResetting must still return the stored anchor IDs")
	}

	if err := store.Delete(ctx, "process-b-after-lease"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, found, err := store.Get(ctx); err != nil || found {
		t.Fatalf("expected no manifest after Delete, found=%v err=%v", found, err)
	}
}

func TestManifestStore_MarkReady_RefusesFromWrongState(t *testing.T) {
	ctx := context.Background()
	db := setupMongoDB(t)
	store := demoseed.NewManifestStore(db)
	_ = store.EnsureIndexes(ctx)

	if _, err := store.AcquireLease(ctx, "process-a", testLeaseDuration); err != nil {
		t.Fatalf("AcquireLease: %v", err)
	}
	if err := store.MarkReady(ctx, "process-a"); err != nil {
		t.Fatalf("first MarkReady: %v", err)
	}
	if err := store.MarkReady(ctx, "process-a"); err == nil {
		t.Fatalf("expected MarkReady to fail when state is already ready")
	}
}

func TestManifestStore_BeginResetting_RefusesFromProvisioning(t *testing.T) {
	ctx := context.Background()
	db := setupMongoDB(t)
	store := demoseed.NewManifestStore(db)
	_ = store.EnsureIndexes(ctx)

	if _, err := store.AcquireLease(ctx, "process-a", testLeaseDuration); err != nil {
		t.Fatalf("AcquireLease: %v", err)
	}
	// state is "provisioning", not "ready" — BeginResetting must refuse per
	// spec §10's terminal-case table.
	if _, err := store.BeginResetting(ctx, "process-a"); err == nil {
		t.Fatalf("expected BeginResetting to refuse from state=provisioning")
	}
}

func TestManifestStore_StartLeaseHeartbeat_KeepsLeaseAliveAndSignalsLoss(t *testing.T) {
	ctx := context.Background()
	db := setupMongoDB(t)
	store := demoseed.NewManifestStore(db)
	_ = store.EnsureIndexes(ctx)

	// A short lease that would expire almost immediately WITHOUT renewal.
	shortLease := 50 * time.Millisecond
	if _, err := store.AcquireLease(ctx, "process-a", shortLease); err != nil {
		t.Fatalf("AcquireLease: %v", err)
	}

	heartbeatCtx, cancel := context.WithCancel(ctx)
	stop, lost := store.StartLeaseHeartbeatWithInterval(heartbeatCtx, "process-a", shortLease, 10*time.Millisecond)
	defer stop()

	// Wait well past the original lease's expiry — the heartbeat must have
	// renewed it, so a competing process must still be refused.
	time.Sleep(150 * time.Millisecond)
	if _, err := store.AcquireLease(ctx, "process-b", shortLease); err == nil {
		t.Fatalf("expected process-b to be refused — the heartbeat should have kept process-a's lease alive")
	}

	select {
	case <-lost:
		t.Fatalf("did not expect lostOwnership to fire while the heartbeat is still renewing successfully")
	default:
	}

	// Stop the heartbeat and let the lease actually expire, simulating a
	// stuck process; a new process reclaims it out from under the old
	// owner token, and the (still-running, if we hadn't stopped it) old
	// heartbeat would observe ErrManifestNotLeaseOwner on its next tick.
	cancel()
	time.Sleep(shortLease + 20*time.Millisecond)
	if _, err := store.AcquireLease(ctx, "process-b", shortLease); err != nil {
		t.Fatalf("expected process-b to reclaim the now-expired, no-longer-heartbeaten lease: %v", err)
	}
}
