package materialrequirements_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"

	mr "github.com/shananth/renovation-platform/backend/internal/materialrequirements"
)

// B7a proves the claim invariants against a REAL MongoDB under genuine
// concurrency. The fake repository cannot establish these: it is single
// threaded, and its "atomic" filter is ordinary Go code that happens to run
// without interleaving. Only a real conditional update shows that exactly one
// of N racing claims wins (design spec §7.1, §7.2).

// The one-active-chain invariant: N goroutines race for one requirement and
// exactly one wins.
func TestMongoClaimForRFQIsExclusiveUnderConcurrency(t *testing.T) {
	db := setupDB(t)
	repo := newRepo(t, db)
	ctx := context.Background()

	created, err := repo.Create(ctx, claimableRequirement(t, "company_a", "project_1"))
	if err != nil {
		t.Fatal(err)
	}

	const racers = 8
	var wg sync.WaitGroup
	var mu sync.Mutex
	var winners []string
	losers := 0
	var otherErrs []error

	wg.Add(racers)
	for i := 0; i < racers; i++ {
		go func(i int) {
			defer wg.Done()
			chainID := fmt.Sprintf("chain_%d", i)
			lineID := fmt.Sprintf("line_%d", i)

			_, err := repo.ClaimForRFQ(ctx, "company_a", "project_1", created.ID,
				created.Revision, chainID, "RFQ-000124", lineID)

			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				winners = append(winners, chainID)
			case errors.Is(err, mr.ErrMaterialRequirementAlreadyClaimed),
				errors.Is(err, mr.ErrRevisionMismatch):
				// Both are legitimate losses: the winner advances the revision
				// AND sets the claim, so a loser may observe either state
				// depending on when its re-read lands.
				losers++
			default:
				otherErrs = append(otherErrs, err)
			}
		}(i)
	}
	wg.Wait()

	if len(otherErrs) != 0 {
		t.Fatalf("unexpected errors from racing claims: %v", otherErrs)
	}
	if len(winners) != 1 {
		t.Fatalf("%d claims succeeded, want exactly 1 — the conditional update IS the "+
			"serialization point (design spec §7.1)", len(winners))
	}
	if losers != racers-1 {
		t.Errorf("%d losers, want %d", losers, racers-1)
	}

	stored, err := repo.FindByID(ctx, "company_a", created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.ActiveRFQChainID == nil || *stored.ActiveRFQChainID != winners[0] {
		t.Fatalf("ActiveRFQChainID = %v, want the winner %q", stored.ActiveRFQChainID, winners[0])
	}
	// chain_N and line_N are written together, so a torn write would pair a
	// chain with another racer's line.
	wantLine := "line_" + strings.TrimPrefix(winners[0], "chain_")
	if stored.ActiveRFQLineID == nil || *stored.ActiveRFQLineID != wantLine {
		t.Errorf("ActiveRFQLineID = %v, want %q — chain and line must be set atomically",
			stored.ActiveRFQLineID, wantLine)
	}
	// Exactly ONE increment: every loser must have written nothing at all.
	if stored.Revision != created.Revision+1 {
		t.Errorf("Revision = %d, want exactly one increment (%d) — a loser wrote",
			stored.Revision, created.Revision+1)
	}
}

// Claim then release, racing: the requirement must end in a consistent state,
// never claimed by one chain while another believes it holds the claim.
func TestMongoClaimAndReleaseRaceLeavesConsistentState(t *testing.T) {
	db := setupDB(t)
	repo := newRepo(t, db)
	ctx := context.Background()

	created, err := repo.Create(ctx, claimableRequirement(t, "company_a", "project_1"))
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := repo.ClaimForRFQ(ctx, "company_a", "project_1", created.ID,
		created.Revision, "chain_1", "RFQ-000124", "line_1")
	if err != nil {
		t.Fatal(err)
	}

	// One goroutine releases the real claim; others attempt to claim it for
	// themselves at the pre-release revision.
	var wg sync.WaitGroup
	var mu sync.Mutex
	releaseErr := error(nil)
	claimWins := 0

	wg.Add(1)
	go func() {
		defer wg.Done()
		_, err := repo.ReleaseClaim(ctx, "company_a", created.ID, claimed.Revision,
			"chain_1", "line_1")
		mu.Lock()
		releaseErr = err
		mu.Unlock()
	}()

	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			// Deliberately the STALE revision: these must all lose.
			if _, err := repo.ClaimForRFQ(ctx, "company_a", "project_1", created.ID,
				created.Revision, fmt.Sprintf("chain_x%d", i), "RFQ-000199",
				fmt.Sprintf("line_x%d", i)); err == nil {
				mu.Lock()
				claimWins++
				mu.Unlock()
			}
		}(i)
	}
	wg.Wait()

	if releaseErr != nil {
		t.Fatalf("the legitimate release failed: %v", releaseErr)
	}
	if claimWins != 0 {
		t.Errorf("%d stale-revision claims succeeded, want 0", claimWins)
	}

	stored, err := repo.FindByID(ctx, "company_a", created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.IsClaimed() {
		t.Errorf("requirement is still claimed by %v after a successful release",
			stored.ActiveRFQChainID)
	}
	if stored.ActiveRFQNumber != nil || stored.ActiveRFQLineID != nil || stored.RFQClaimedAt != nil {
		t.Errorf("release left claim residue: %+v", stored)
	}
}

// A release naming the wrong line must not clear the claim (design spec §7.4).
func TestMongoReleaseClaimRequiresExactChainAndLine(t *testing.T) {
	db := setupDB(t)
	repo := newRepo(t, db)
	ctx := context.Background()

	created, err := repo.Create(ctx, claimableRequirement(t, "company_a", "project_1"))
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := repo.ClaimForRFQ(ctx, "company_a", "project_1", created.ID,
		created.Revision, "chain_1", "RFQ-000124", "line_1")
	if err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct{ name, chain, line string }{
		{"wrong line", "chain_1", "line_zzz"},
		{"wrong chain", "chain_zzz", "line_1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := repo.ReleaseClaim(ctx, "company_a", created.ID, claimed.Revision,
				tc.chain, tc.line); !errors.Is(err, mr.ErrRevisionMismatch) {
				t.Fatalf("error = %v, want ErrRevisionMismatch", err)
			}
			stillClaimed, err := repo.FindByID(ctx, "company_a", created.ID)
			if err != nil {
				t.Fatal(err)
			}
			if !stillClaimed.IsClaimed() {
				t.Fatalf("a %s release cleared the claim", tc.name)
			}
		})
	}

	released, err := repo.ReleaseClaim(ctx, "company_a", created.ID, claimed.Revision,
		"chain_1", "line_1")
	if err != nil {
		t.Fatalf("the exact release failed: %v", err)
	}
	if released.ActiveRFQChainID != nil || released.ActiveRFQNumber != nil ||
		released.ActiveRFQLineID != nil || released.RFQClaimedAt != nil {
		t.Errorf("release left claim residue: %+v", released)
	}
}

// projectId sits INSIDE the atomic claim filter, so a requirement in another
// project of the same company matches zero documents (design spec §7.2).
func TestMongoClaimForRFQEnforcesProjectInsideTheFilter(t *testing.T) {
	db := setupDB(t)
	repo := newRepo(t, db)
	ctx := context.Background()

	created, err := repo.Create(ctx, claimableRequirement(t, "company_a", "project_1"))
	if err != nil {
		t.Fatal(err)
	}

	if _, err := repo.ClaimForRFQ(ctx, "company_a", "project_2", created.ID,
		created.Revision, "chain_1", "RFQ-000124", "line_1"); !errors.Is(err,
		mr.ErrMaterialRequirementNotFound) {
		t.Fatalf("error = %v, want ErrMaterialRequirementNotFound", err)
	}

	stored, err := repo.FindByID(ctx, "company_a", created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.IsClaimed() {
		t.Error("a cross-project claim created a claim — contamination must be structural")
	}
}

// The §8.5 predicate lives in the FILTER, not in a prior read, so a concurrent
// edit cannot race past it.
func TestMongoClaimForRFQEnforcesEligibilityInsideTheFilter(t *testing.T) {
	db := setupDB(t)
	repo := newRepo(t, db)
	ctx := context.Background()

	cases := []struct {
		name   string
		mutate func(*mr.MaterialRequirement)
	}{
		{"draft", func(r *mr.MaterialRequirement) { r.Status = mr.RequirementStatusDraft }},
		{"change_detected", func(r *mr.MaterialRequirement) {
			r.SourceSyncState = mr.SourceSyncStateChangeDetected
		}},
		{"unacknowledged unit mismatch", func(r *mr.MaterialRequirement) {
			r.UnitMismatch = true
			r.UnitMismatchAcknowledged = false
		}},
		{"archived", func(r *mr.MaterialRequirement) { r.Status = mr.RequirementStatusArchived }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := claimableRequirement(t, "company_a", "project_1")
			tc.mutate(&req)
			created, err := repo.Create(ctx, req)
			if err != nil {
				t.Fatal(err)
			}

			if _, err := repo.ClaimForRFQ(ctx, "company_a", "project_1", created.ID,
				created.Revision, "chain_1", "RFQ-000124", "line_1"); !errors.Is(err,
				mr.ErrRequirementNotEligibleForRFQ) {
				t.Fatalf("error = %v, want ErrRequirementNotEligibleForRFQ", err)
			}
			stored, err := repo.FindByID(ctx, "company_a", created.ID)
			if err != nil {
				t.Fatal(err)
			}
			if stored.IsClaimed() {
				t.Errorf("%s was claimed despite failing the eligibility predicate", tc.name)
			}
		})
	}
}

// An acknowledged unit mismatch DOES permit a claim, proving the filter
// implements §8.5's disjunction rather than simply requiring no mismatch.
func TestMongoClaimForRFQAllowsAcknowledgedUnitMismatch(t *testing.T) {
	db := setupDB(t)
	repo := newRepo(t, db)
	ctx := context.Background()

	req := claimableRequirement(t, "company_a", "project_1")
	req.UnitMismatch = true
	req.UnitMismatchAcknowledged = true
	created, err := repo.Create(ctx, req)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := repo.ClaimForRFQ(ctx, "company_a", "project_1", created.ID,
		created.Revision, "chain_1", "RFQ-000124", "line_1"); err != nil {
		t.Fatalf("an acknowledged mismatch must not block a claim: %v", err)
	}
}

// ListClaimsForRFQChain makes an orphaned claim discoverable, and must never
// cross a tenant boundary even when two companies use the same chain id
// (design spec §7.5).
func TestMongoListClaimsForRFQChainIsTenantScoped(t *testing.T) {
	db := setupDB(t)
	repo := newRepo(t, db)
	ctx := context.Background()

	for _, companyID := range []string{"company_a", "company_b"} {
		created, err := repo.Create(ctx, claimableRequirement(t, companyID, "project_1"))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := repo.ClaimForRFQ(ctx, companyID, "project_1", created.ID,
			created.Revision, "chain_shared", "RFQ-000124", "line_1"); err != nil {
			t.Fatal(err)
		}
	}

	claims, err := repo.ListClaimsForRFQChain(ctx, "company_a", "chain_shared")
	if err != nil {
		t.Fatal(err)
	}
	if len(claims) != 1 {
		t.Fatalf("got %d claims, want 1 — a shared chain id must never cross tenants", len(claims))
	}
	if claims[0].CompanyID != "company_a" {
		t.Errorf("claim belongs to %q, want company_a", claims[0].CompanyID)
	}
}

// --- Persisted-document defence: the positive-quantity term ---
//
// parseQuantity rejects zero and negative at WRITE time, but service validation
// cannot protect against malformed legacy documents, direct database writes,
// migrations, or a repository defect. The atomic claim filter must enforce the
// invariant itself, so these tests insert documents Mongo-side, bypassing the
// service entirely.
//
// $convert uses explicit onError/onNull results, so a malformed value compares
// as zero — NOT claimable — instead of raising a query error that would turn one
// bad document into a 500 for an unrelated caller.
func TestMongoClaimRejectsNonPositivePersistedQuantities(t *testing.T) {
	db := setupDB(t)
	repo := newRepo(t, db)
	ctx := context.Background()

	cases := []struct {
		name  string
		value any
	}{
		{"zero", "0"},
		{"zero with decimals", "0.00"},
		{"negative", "-1"},
		{"negative decimal", "-0.5"},
		{"non-numeric", "invalid"},
		{"empty string", ""},
		{"explicit null", nil},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			id := insertRawClaimable(t, db, ctx, "company_a", "project_1", tc.value, true)

			_, err := repo.ClaimForRFQ(ctx, "company_a", "project_1", id, 0,
				"chain_1", "RFQ-000124", "line_1")
			if err == nil {
				t.Fatalf("a persisted quantity of %v was claimed; the filter must refuse it", tc.value)
			}
			// Whatever the classification, it must be a domain sentinel — never
			// a raw Mongo or decode error surfacing as 500.
			if !errors.Is(err, mr.ErrRequirementNotEligibleForRFQ) &&
				!errors.Is(err, mr.ErrMaterialRequirementNotFound) {
				t.Fatalf("error = %v, want a domain sentinel (no query error may escape)", err)
			}

			var doc struct {
				ActiveRFQChainID *string `bson:"activeRfqChainId"`
			}
			objID, decodeErr := bson.ObjectIDFromHex(id)
			if decodeErr != nil {
				t.Fatal(decodeErr)
			}
			if err := db.Collection("material_requirements").
				FindOne(ctx, bson.M{"_id": objID}).Decode(&doc); err != nil {
				t.Fatal(err)
			}
			if doc.ActiveRFQChainID != nil {
				t.Errorf("a claim was written despite a %s quantity", tc.name)
			}
		})
	}
}

// A missing requiredQuantity.value field entirely — distinct from an explicit
// null, and the shape a partial migration would leave behind.
func TestMongoClaimRejectsMissingPersistedQuantityField(t *testing.T) {
	db := setupDB(t)
	repo := newRepo(t, db)
	ctx := context.Background()

	res, err := db.Collection("material_requirements").InsertOne(ctx, bson.M{
		"companyId": "company_a", "projectId": "project_1", "workItemId": "work_1",
		"materialId": "material_1", "materialName": "Portland Cement",
		// requiredQuantity carries a unit but NO value key at all.
		"requiredQuantity": bson.M{"unit": "bag"},
		"catalogUnit":      "bag",
		"unitMismatch":     false, "unitMismatchAcknowledged": false,
		"status": string(mr.RequirementStatusReviewed), "sourceType": string(mr.SourceTypeManual),
		"sourceSyncState": string(mr.SourceSyncStateClean),
		"revision":        int64(0), "schemaVersion": 1,
		"createdAt": time.Now(), "updatedAt": time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	id := res.InsertedID.(bson.ObjectID).Hex()

	if _, err := repo.ClaimForRFQ(ctx, "company_a", "project_1", id, 0,
		"chain_1", "RFQ-000124", "line_1"); err == nil {
		t.Fatal("a document with no requiredQuantity.value was claimed")
	} else if !errors.Is(err, mr.ErrRequirementNotEligibleForRFQ) &&
		!errors.Is(err, mr.ErrMaterialRequirementNotFound) {
		t.Fatalf("error = %v, want a domain sentinel", err)
	}
}

// A positive persisted quantity written the same raw way DOES claim, proving
// the tests above fail on the quantity term rather than on the raw-insert
// shape itself.
func TestMongoClaimAcceptsPositivePersistedQuantity(t *testing.T) {
	db := setupDB(t)
	repo := newRepo(t, db)
	ctx := context.Background()

	id := insertRawClaimable(t, db, ctx, "company_a", "project_1", "100", true)

	if _, err := repo.ClaimForRFQ(ctx, "company_a", "project_1", id, 0,
		"chain_1", "RFQ-000124", "line_1"); err != nil {
		t.Fatalf("a raw-inserted positive quantity must be claimable: %v", err)
	}
}

// The remaining eligibility terms are mirrored between IsRFQEligible() (Go) and
// eligibilityFilter() (BSON) — unavoidable persistence-layer duplication, since
// one runs in the application and the other inside MongoDB.
//
// This test pins their EQUIVALENCE: for each case it asserts that the in-memory
// predicate and the atomic filter reach the same verdict on the same persisted
// document. A change to one without the other fails here.
func TestMongoEligibilityFilterAgreesWithIsRFQEligible(t *testing.T) {
	db := setupDB(t)
	repo := newRepo(t, db)
	ctx := context.Background()

	cases := []struct {
		name         string
		mutate       func(*mr.MaterialRequirement)
		wantEligible bool
	}{
		{"fully eligible", func(*mr.MaterialRequirement) {}, true},
		{"draft", func(r *mr.MaterialRequirement) { r.Status = mr.RequirementStatusDraft }, false},
		{"archived", func(r *mr.MaterialRequirement) { r.Status = mr.RequirementStatusArchived }, false},
		{"split", func(r *mr.MaterialRequirement) { r.Status = mr.RequirementStatusSplit }, false},
		{"change_detected", func(r *mr.MaterialRequirement) {
			r.SourceSyncState = mr.SourceSyncStateChangeDetected
		}, false},
		{"source_removed", func(r *mr.MaterialRequirement) {
			r.SourceSyncState = mr.SourceSyncStateSourceRemoved
		}, false},
		{"unacknowledged unit mismatch", func(r *mr.MaterialRequirement) {
			r.UnitMismatch = true
			r.UnitMismatchAcknowledged = false
		}, false},
		{"acknowledged unit mismatch", func(r *mr.MaterialRequirement) {
			r.UnitMismatch = true
			r.UnitMismatchAcknowledged = true
		}, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := claimableRequirement(t, "company_a", "project_1")
			tc.mutate(&req)

			// The Go predicate's verdict on the same value.
			goVerdict := req.IsRFQEligible()
			if goVerdict != tc.wantEligible {
				t.Fatalf("IsRFQEligible() = %v, want %v — the case itself is mis-specified",
					goVerdict, tc.wantEligible)
			}

			created, err := repo.Create(ctx, req)
			if err != nil {
				t.Fatal(err)
			}

			// The BSON filter's verdict on the persisted document.
			_, claimErr := repo.ClaimForRFQ(ctx, "company_a", "project_1", created.ID,
				created.Revision, "chain_1", "RFQ-000124", "line_1")
			filterVerdict := claimErr == nil

			if filterVerdict != goVerdict {
				t.Errorf("eligibilityFilter() says eligible=%v but IsRFQEligible() says %v — "+
					"the mirrored predicates have drifted apart (claim error: %v)",
					filterVerdict, goVerdict, claimErr)
			}
		})
	}
}

// insertRawClaimable writes a requirement DIRECTLY to MongoDB, bypassing the
// service and its validation, so a test can persist a quantity the domain would
// never produce.
func insertRawClaimable(t *testing.T, db *mongo.Database, ctx context.Context,
	companyID, projectID string, quantityValue any, reviewed bool) string {
	t.Helper()

	status := mr.RequirementStatusDraft
	if reviewed {
		status = mr.RequirementStatusReviewed
	}
	res, err := db.Collection("material_requirements").InsertOne(ctx, bson.M{
		"companyId": companyID, "projectId": projectID, "workItemId": "work_1",
		"materialId": "material_1", "materialName": "Portland Cement",
		"requiredQuantity": bson.M{"value": quantityValue, "unit": "bag"},
		"catalogUnit":      "bag",
		"unitMismatch":     false, "unitMismatchAcknowledged": false,
		"status": string(status), "sourceType": string(mr.SourceTypeManual),
		"sourceSyncState": string(mr.SourceSyncStateClean),
		"revision":        int64(0), "schemaVersion": 1,
		"createdAt": time.Now(), "updatedAt": time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	return res.InsertedID.(bson.ObjectID).Hex()
}

// claimableRequirement builds an RFQ-eligible manual requirement: reviewed,
// positive quantity, matching units, clean sync state.
func claimableRequirement(t *testing.T, companyID, projectID string) mr.MaterialRequirement {
	t.Helper()
	workItemID := "work_1"
	now := time.Now().UTC().Truncate(time.Millisecond)
	return mr.MaterialRequirement{
		CompanyID: companyID, ProjectID: projectID, WorkItemID: &workItemID,
		MaterialID: "material_1", MaterialName: "Portland Cement",
		Specification:    "OPC 50kg",
		RequiredQuantity: qty(t, "100", "bag"),
		CatalogUnit:      "bag",
		ProcurementNotes: "deliver to site gate",
		InternalNotes:    "contractor-only",
		Status:           mr.RequirementStatusReviewed,
		SourceType:       mr.SourceTypeManual,
		SourceSyncState:  mr.SourceSyncStateClean,
		CreatedByUserID:  "user_1",
		CreatedAt:        now, UpdatedAt: now, SchemaVersion: 1,
	}
}
