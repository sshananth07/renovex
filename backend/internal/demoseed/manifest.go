package demoseed

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

// manifestFixedID is the single document this whole collection ever holds.
const manifestFixedID = "renovex-demo:v1"

const manifestSeedVersion = 1

// demoEmail/demoCompanyName are the demo tenant's positively-identifying
// constants. Declared HERE, in Task 3's file, rather than in Task 4's
// identity.go — manifest.go's AcquireLease stamps DemoEmail into the very
// first document it ever creates, so this constant must exist before Task 4
// is implemented, not after. Task 4's identity.go references these same
// constants — it does not redeclare them.
const (
	demoEmail       = "demo@renovex.local"
	demoCompanyName = "Renovex Demo Contractor Sdn Bhd"
)

type ManifestState string

const (
	ManifestStateProvisioning ManifestState = "provisioning"
	ManifestStateReady        ManifestState = "ready"
	ManifestStateResetting    ManifestState = "resetting"
)

// Manifest positively identifies the demo tenant this tool provisioned, so
// seed and reset never have to guess which Company/User pair is "the" demo
// tenant from name/email matching alone. LeaseOwner/LeaseExpiresAt make
// concurrent seed/reset invocations mutually exclusive (design spec §6.2) —
// a fixed document ID alone is not a lock.
type Manifest struct {
	ID             string
	SeedVersion    int
	DemoEmail      string
	State          ManifestState
	DemoUserID     string
	DemoCompanyID  string
	LeaseOwner     string
	LeaseExpiresAt time.Time
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type manifestDoc struct {
	ID             string        `bson:"_id"`
	SeedVersion    int           `bson:"seedVersion"`
	DemoEmail      string        `bson:"demoEmail"`
	State          ManifestState `bson:"state"`
	DemoUserID     string        `bson:"demoUserId,omitempty"`
	DemoCompanyID  string        `bson:"demoCompanyId,omitempty"`
	LeaseOwner     string        `bson:"leaseOwner,omitempty"`
	LeaseExpiresAt time.Time     `bson:"leaseExpiresAt,omitempty"`
	CreatedAt      time.Time     `bson:"createdAt"`
	UpdatedAt      time.Time     `bson:"updatedAt"`
}

func (d manifestDoc) toManifest() Manifest {
	return Manifest{
		ID: d.ID, SeedVersion: d.SeedVersion, DemoEmail: d.DemoEmail,
		State: d.State, DemoUserID: d.DemoUserID, DemoCompanyID: d.DemoCompanyID,
		LeaseOwner: d.LeaseOwner, LeaseExpiresAt: d.LeaseExpiresAt,
		CreatedAt: d.CreatedAt, UpdatedAt: d.UpdatedAt,
	}
}

// ErrManifestInvalidTransition is returned when a CAS transition's
// precondition on the current state does not hold.
var ErrManifestInvalidTransition = errors.New("demoseed: manifest is not in the expected state for this transition")

// ErrManifestLeaseHeld is returned when AcquireLease finds a live lease
// belonging to a different owner — the concurrency guard the review
// required. The error message includes enough detail for an operator to
// understand another process is running, not that something is broken.
var ErrManifestLeaseHeld = errors.New("demoseed: another demoseed process currently holds the manifest lease")

// ErrManifestNotLeaseOwner is returned when a state-mutating call presents
// an ownerToken that does not match the manifest's current LeaseOwner —
// e.g. a process whose lease was reclaimed as stale trying to keep working.
var ErrManifestNotLeaseOwner = errors.New("demoseed: caller does not hold the manifest lease")

// ManifestStore owns the demo_seed_manifest collection exclusively. No other
// type in this codebase reads or writes it directly.
type ManifestStore struct {
	collection *mongo.Collection
}

// NewManifestStore constructs a ManifestStore against db's
// "demo_seed_manifest" collection.
func NewManifestStore(db *mongo.Database) *ManifestStore {
	return &ManifestStore{collection: db.Collection("demo_seed_manifest")}
}

// EnsureIndexes is a no-op beyond MongoDB's implicit _id index — the fixed
// single-document _id already gives this collection everything it needs.
func (s *ManifestStore) EnsureIndexes(ctx context.Context) error {
	return nil
}

// AcquireLease is the ONLY way any caller begins working with the
// manifest. Creates the document (state=provisioning, leased by
// ownerToken) if none exists. If one exists, takes over the lease only
// when no lease is currently held or the held lease has expired — a CAS
// conditioned on the OLD lease fields, so two simultaneously-recovering
// processes cannot both believe they reclaimed a stale lease. Refuses
// (ErrManifestLeaseHeld) when a live lease belongs to someone else.
func (s *ManifestStore) AcquireLease(ctx context.Context, ownerToken string, leaseDuration time.Duration) (Manifest, error) {
	now := time.Now().UTC()
	expiresAt := now.Add(leaseDuration)

	doc := manifestDoc{
		ID: manifestFixedID, SeedVersion: manifestSeedVersion,
		DemoEmail: demoEmail, State: ManifestStateProvisioning,
		LeaseOwner: ownerToken, LeaseExpiresAt: expiresAt,
		CreatedAt: now, UpdatedAt: now,
	}
	if _, err := s.collection.InsertOne(ctx, doc); err == nil {
		return doc.toManifest(), nil
	} else if !mongo.IsDuplicateKeyError(err) {
		return Manifest{}, err
	}

	// A document already exists — try to take over the lease, but ONLY if
	// it is currently unheld or expired. This filter is the entire
	// concurrency guarantee: it is evaluated atomically by MongoDB against
	// whatever the document's CURRENT lease state is at update time, not
	// whatever this process last read.
	res, err := s.collection.UpdateOne(ctx,
		bson.M{
			"_id": manifestFixedID,
			"$or": []bson.M{
				{"leaseOwner": ""},
				{"leaseOwner": bson.M{"$exists": false}},
				{"leaseExpiresAt": bson.M{"$lt": now}},
			},
		},
		bson.M{"$set": bson.M{
			"leaseOwner": ownerToken, "leaseExpiresAt": expiresAt, "updatedAt": now,
		}},
	)
	if err != nil {
		return Manifest{}, err
	}
	if res.MatchedCount == 1 {
		manifest, found, getErr := s.Get(ctx)
		if getErr != nil || !found {
			return Manifest{}, getErr
		}
		return manifest, nil
	}

	// No match: a live lease is held by someone else. Surface who/until
	// when, for a useful operator-facing error.
	existing, found, getErr := s.Get(ctx)
	if getErr != nil {
		return Manifest{}, getErr
	}
	if !found {
		return Manifest{}, fmt.Errorf("demoseed: manifest lease acquisition raced but no document was found afterward")
	}
	return Manifest{}, fmt.Errorf("%w: held by %q until %s", ErrManifestLeaseHeld, existing.LeaseOwner, existing.LeaseExpiresAt.Format(time.RFC3339))
}

// RenewLease extends LeaseExpiresAt for a caller that still owns the
// lease. Call periodically during a long-running seed/reset so a
// slow-but-alive run is never mistaken for a crashed one.
func (s *ManifestStore) RenewLease(ctx context.Context, ownerToken string, leaseDuration time.Duration) error {
	now := time.Now().UTC()
	res, err := s.collection.UpdateOne(ctx,
		bson.M{"_id": manifestFixedID, "leaseOwner": ownerToken},
		bson.M{"$set": bson.M{"leaseExpiresAt": now.Add(leaseDuration), "updatedAt": now}},
	)
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return ErrManifestNotLeaseOwner
	}
	return nil
}

// ReleaseLease clears the lease fields on clean completion. Never errors
// if the lease was already cleared or reclaimed by someone else —
// releasing a lease you no longer hold is a safe no-op.
func (s *ManifestStore) ReleaseLease(ctx context.Context, ownerToken string) error {
	_, err := s.collection.UpdateOne(ctx,
		bson.M{"_id": manifestFixedID, "leaseOwner": ownerToken},
		bson.M{"$set": bson.M{"leaseOwner": "", "leaseExpiresAt": time.Time{}, "updatedAt": time.Now().UTC()}},
	)
	return err
}

// RecordIdentity stores the demo User/Company IDs once Register has
// succeeded. Only valid while state=provisioning AND ownerToken holds the
// current lease.
func (s *ManifestStore) RecordIdentity(ctx context.Context, ownerToken, demoUserID, demoCompanyID string) error {
	res, err := s.collection.UpdateOne(ctx,
		bson.M{"_id": manifestFixedID, "state": string(ManifestStateProvisioning), "leaseOwner": ownerToken},
		bson.M{"$set": bson.M{
			"demoUserId": demoUserID, "demoCompanyId": demoCompanyID,
			"updatedAt": time.Now().UTC(),
		}},
	)
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return ErrManifestInvalidTransition
	}
	return nil
}

// MarkReady transitions provisioning -> ready. Fails if the manifest is not
// currently in state=provisioning, or ownerToken does not hold the lease.
func (s *ManifestStore) MarkReady(ctx context.Context, ownerToken string) error {
	res, err := s.collection.UpdateOne(ctx,
		bson.M{"_id": manifestFixedID, "state": string(ManifestStateProvisioning), "leaseOwner": ownerToken},
		bson.M{"$set": bson.M{"state": string(ManifestStateReady), "updatedAt": time.Now().UTC()}},
	)
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return ErrManifestInvalidTransition
	}
	return nil
}

// Get returns the manifest document, or found=false if none exists.
// Lease-independent — safe to call without holding the lease.
func (s *ManifestStore) Get(ctx context.Context) (Manifest, bool, error) {
	var doc manifestDoc
	err := s.collection.FindOne(ctx, bson.M{"_id": manifestFixedID}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return Manifest{}, false, nil
	}
	if err != nil {
		return Manifest{}, false, err
	}
	return doc.toManifest(), true, nil
}

// BeginResetting transitions ready -> resetting (lease-owner-gated) and
// returns the manifest's stored anchor IDs, which every subsequent delete
// step in a reset uses instead of re-deriving identity.
//
// If the manifest is ALREADY in state=resetting under the SAME ownerToken,
// this is a safe resume: the stored document is returned unchanged rather
// than erroring, so a retry within the same invocation (or a genuinely
// resumed run after AcquireLease reclaimed a stale lease under the same
// caller's new token — see Task 16) picks up exactly where it left off.
//
// If the manifest is in state=provisioning, this refuses
// (ErrManifestInvalidTransition) — an interrupted seed is never something
// reset should act on.
func (s *ManifestStore) BeginResetting(ctx context.Context, ownerToken string) (Manifest, error) {
	res, err := s.collection.UpdateOne(ctx,
		bson.M{"_id": manifestFixedID, "state": string(ManifestStateReady), "leaseOwner": ownerToken},
		bson.M{"$set": bson.M{"state": string(ManifestStateResetting), "updatedAt": time.Now().UTC()}},
	)
	if err != nil {
		return Manifest{}, err
	}
	if res.MatchedCount == 1 {
		manifest, found, getErr := s.Get(ctx)
		if getErr != nil || !found {
			return Manifest{}, getErr
		}
		return manifest, nil
	}

	// No match on state=ready — check whether it's already resetting
	// under this SAME owner (safe resume) or genuinely the wrong state.
	existing, found, getErr := s.Get(ctx)
	if getErr != nil {
		return Manifest{}, getErr
	}
	if !found || existing.State != ManifestStateResetting || existing.LeaseOwner != ownerToken {
		return Manifest{}, ErrManifestInvalidTransition
	}
	return existing, nil
}

// Delete removes the manifest document. Lease-owner-gated. Called last in
// a reset, after every other deletion in that attempt has completed.
func (s *ManifestStore) Delete(ctx context.Context, ownerToken string) error {
	res, err := s.collection.DeleteOne(ctx, bson.M{"_id": manifestFixedID, "leaseOwner": ownerToken})
	if err != nil {
		return err
	}
	if res.DeletedCount == 0 {
		return ErrManifestNotLeaseOwner
	}
	return nil
}

// leaseHeartbeatInterval is deliberately well inside leaseDuration (Task
// 15 sets leaseDuration to 15 minutes) so a live process renews long before
// its lease could be mistaken for stale, while a genuinely stuck/crashed
// process still stops renewing almost immediately.
const leaseHeartbeatInterval = 2 * time.Minute

// StartLeaseHeartbeat renews ownerToken's lease every leaseHeartbeatInterval
// (2 minutes) until ctx is cancelled. It returns a cancel function the
// caller MUST call (via defer) to stop the background goroutine, and a
// lost-ownership channel that is closed the moment ANY renewal attempt
// fails — whether because the lease was confirmed reclaimed
// (ErrManifestNotLeaseOwner) or because RenewLease returned some other
// error (e.g. a transient Mongo failure). Both are treated as fatal to the
// current run: a renewal failure means this process can no longer prove it
// still holds the lease, so continuing to write domain data would risk
// operating without exclusive ownership regardless of the failure's exact
// cause — treating an unconfirmed renewal as safe-to-continue would let the
// heartbeat goroutine exit silently while Seed/Reset kept writing, believing
// the lease was still being renewed when in fact nothing was renewing it.
func (s *ManifestStore) StartLeaseHeartbeat(ctx context.Context, ownerToken string, leaseDuration time.Duration) (stop func(), lostOwnership <-chan struct{}) {
	return s.StartLeaseHeartbeatWithInterval(ctx, ownerToken, leaseDuration, leaseHeartbeatInterval)
}

// StartLeaseHeartbeatWithInterval is StartLeaseHeartbeat with an explicit
// renewal interval — production code always uses StartLeaseHeartbeat;
// tests use this directly with a short interval so heartbeat behavior can
// be proven without waiting out the real 2-minute cadence.
func (s *ManifestStore) StartLeaseHeartbeatWithInterval(ctx context.Context, ownerToken string, leaseDuration, interval time.Duration) (stop func(), lostOwnership <-chan struct{}) {
	heartbeatCtx, cancel := context.WithCancel(ctx)
	lost := make(chan struct{})

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-heartbeatCtx.Done():
				return
			case <-ticker.C:
				// ANY renewal failure is fatal to the run — not just a
				// confirmed ErrManifestNotLeaseOwner. This process cannot
				// distinguish "the lease was genuinely reclaimed" from
				// "Mongo hiccuped and I couldn't confirm renewal" without
				// querying again, and treating the latter as safe-to-continue
				// is exactly the silent-stop bug this closes.
				if err := s.RenewLease(heartbeatCtx, ownerToken, leaseDuration); err != nil {
					close(lost)
					return
				}
			}
		}
	}()

	return cancel, lost
}
