package composition_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/shananth/renovation-platform/backend/internal/materialrequirements"
	"github.com/shananth/renovation-platform/backend/internal/platform/composition"
	"github.com/shananth/renovation-platform/backend/internal/rfqs"
)

// E1 covers the M7 composition adapter: the ONE place permitted to import both
// rfqs and materialrequirements (design spec §1.4.1).
//
// Go interface satisfaction requires exact named return types.
// materialrequirements.Service returns materialrequirements-owned snapshot and
// claim types; rfqs declares its own. The adapter performs that conversion at
// the leaf of the dependency graph, and translates the sentinels, so neither
// module gains a dependency ADR 0002 forbids.

// The adapter must satisfy the whole consumer interface — SIX methods, not the
// five §1.3 originally declared. ReadClaimSnapshot was added as revision 4's
// amendment.
//
// Asserted at compile time by assignment: if a method is missing or its
// signature drifts, this file does not build.
func TestAdapterSatisfiesMaterialRequirementSource(t *testing.T) {
	var _ rfqs.MaterialRequirementSource = composition.NewMaterialRequirementSourceAdapter(nil)

	typ := reflect.TypeOf((*rfqs.MaterialRequirementSource)(nil)).Elem()
	if typ.NumMethod() != 6 {
		t.Fatalf("MaterialRequirementSource declares %d methods, want 6", typ.NumMethod())
	}
	for _, name := range []string{
		"ClaimForRFQ", "ReleaseClaim", "ReadClaim",
		"ListClaimsForRFQChain", "ClaimedRequirementIsReadyForRFQ", "ReadClaimSnapshot",
	} {
		if _, ok := typ.MethodByName(name); !ok {
			t.Errorf("MaterialRequirementSource is missing %s", name)
		}
	}
}

// --- Type conversion ---

// ClaimForRFQ converts the materialrequirements snapshot into the rfqs-owned
// one, field for field.
func TestAdapterConvertsClaimSnapshot(t *testing.T) {
	svc, req := seedClaimedRequirement(t)
	adapter := composition.NewMaterialRequirementSourceAdapter(svc)
	ctx := context.Background()

	snap, err := adapter.ReadClaimSnapshot(ctx, "company_a", req.ID, "chain_1", "line_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The returned type must be the RFQS-owned one, or rfqs would need to
	// import materialrequirements.
	if reflect.TypeOf(snap) != reflect.TypeOf(rfqs.ClaimSnapshot{}) {
		t.Fatalf("ReadClaimSnapshot returned %T, want rfqs.ClaimSnapshot", snap)
	}

	if snap.RequirementID != req.ID {
		t.Errorf("RequirementID = %q, want %q", snap.RequirementID, req.ID)
	}
	if snap.MaterialID != "material_1" || snap.MaterialName != "Portland Cement" {
		t.Errorf("material identity lost in conversion: %+v", snap)
	}
	if snap.Specification != "OPC 50kg" {
		t.Errorf("Specification = %q", snap.Specification)
	}
	if snap.QuantityValue != "100" || snap.QuantityUnit != "bag" {
		t.Errorf("quantity = %s %s, want 100 bag", snap.QuantityValue, snap.QuantityUnit)
	}
	if snap.ProcurementNotes != "deliver to site gate" {
		t.Errorf("ProcurementNotes = %q", snap.ProcurementNotes)
	}
}

// The conversion is the privacy boundary in code: rfqs.ClaimSnapshot has no
// InternalNotes field, so the contractor-only note has nowhere to land.
func TestAdapterCannotCarryInternalNotesAcross(t *testing.T) {
	typ := reflect.TypeOf(rfqs.ClaimSnapshot{})
	if _, ok := typ.FieldByName("InternalNotes"); ok {
		t.Error("rfqs.ClaimSnapshot has an InternalNotes field; the adapter could then " +
			"carry a contractor-only note into a supplier-visible line")
	}
}

func TestAdapterConvertsClaimForRFQSnapshot(t *testing.T) {
	svc, req := seedEligibleRequirement(t)
	adapter := composition.NewMaterialRequirementSourceAdapter(svc)
	ctx := context.Background()

	snap, err := adapter.ClaimForRFQ(ctx, "company_a", "project_1", req.ID, req.Revision,
		"chain_1", "RFQ-000124", "line_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reflect.TypeOf(snap) != reflect.TypeOf(rfqs.ClaimSnapshot{}) {
		t.Fatalf("ClaimForRFQ returned %T, want rfqs.ClaimSnapshot", snap)
	}
	if snap.RequirementID != req.ID || snap.Revision != req.Revision+1 {
		t.Errorf("snapshot = %+v, want the post-claim revision", snap)
	}
}

// ListClaimsForRFQChain converts each claim into the rfqs-owned type.
func TestAdapterConvertsRequirementClaims(t *testing.T) {
	svc, req := seedClaimedRequirement(t)
	adapter := composition.NewMaterialRequirementSourceAdapter(svc)

	claims, err := adapter.ListClaimsForRFQChain(context.Background(), "company_a", "chain_1")
	if err != nil {
		t.Fatal(err)
	}
	if len(claims) != 1 {
		t.Fatalf("got %d claims, want 1", len(claims))
	}
	if reflect.TypeOf(claims[0]) != reflect.TypeOf(rfqs.RFQRequirementClaim{}) {
		t.Fatalf("got %T, want rfqs.RFQRequirementClaim", claims[0])
	}
	if claims[0].RequirementID != req.ID || claims[0].RFQChainID != "chain_1" ||
		claims[0].LineID != "line_1" || claims[0].RFQNumber != "RFQ-000124" {
		t.Errorf("claim conversion lost a field: %+v", claims[0])
	}
	if claims[0].Revision == 0 {
		t.Error("Revision must be carried across; rfqs releases under its guard")
	}
}

// An empty result converts to an empty slice, never a nil the caller must
// special-case.
func TestAdapterConvertsAnEmptyClaimList(t *testing.T) {
	svc, _ := seedEligibleRequirement(t)
	adapter := composition.NewMaterialRequirementSourceAdapter(svc)

	claims, err := adapter.ListClaimsForRFQChain(context.Background(), "company_a", "chain_zzz")
	if err != nil {
		t.Fatal(err)
	}
	if claims == nil {
		t.Error("an empty result must be an empty slice, not nil")
	}
	if len(claims) != 0 {
		t.Errorf("got %d claims, want 0", len(claims))
	}
}

// --- Sentinel translation ---
//
// rfqs declares its OWN sentinels and never imports materialrequirements', so
// the adapter must translate every one that can cross the boundary.

func TestAdapterTranslatesClaimSentinels(t *testing.T) {
	svc, req := seedEligibleRequirement(t)
	adapter := composition.NewMaterialRequirementSourceAdapter(svc)
	ctx := context.Background()

	t.Run("not found", func(t *testing.T) {
		_, err := adapter.ClaimForRFQ(ctx, "company_a", "project_1", "mr_zzz", 0,
			"chain_1", "RFQ-000124", "line_1")
		assertRFQSSentinel(t, err, rfqs.ErrMaterialRequirementNotFound)
	})

	t.Run("wrong project", func(t *testing.T) {
		_, err := adapter.ClaimForRFQ(ctx, "company_a", "project_2", req.ID, req.Revision,
			"chain_1", "RFQ-000124", "line_1")
		assertRFQSSentinel(t, err, rfqs.ErrMaterialRequirementNotFound)
	})

	t.Run("already claimed", func(t *testing.T) {
		claimed, _ := seedClaimedRequirement(t)
		other := composition.NewMaterialRequirementSourceAdapter(claimed)
		reqs, err := claimed.ListClaimsForRFQChain(ctx, "company_a", "chain_1")
		if err != nil || len(reqs) == 0 {
			t.Fatalf("setup: %v", err)
		}
		_, err = other.ClaimForRFQ(ctx, "company_a", "project_1", reqs[0].RequirementID,
			reqs[0].Revision, "chain_2", "RFQ-000125", "line_2")
		assertRFQSSentinel(t, err, rfqs.ErrMaterialRequirementAlreadyClaimed)
	})

	t.Run("not eligible", func(t *testing.T) {
		draft, d := seedDraftRequirement(t)
		a := composition.NewMaterialRequirementSourceAdapter(draft)
		_, err := a.ClaimForRFQ(ctx, "company_a", "project_1", d.ID, d.Revision,
			"chain_1", "RFQ-000124", "line_1")
		assertRFQSSentinel(t, err, rfqs.ErrRFQLineNotEligible)
	})

	t.Run("stale revision", func(t *testing.T) {
		_, err := adapter.ClaimForRFQ(ctx, "company_a", "project_1", req.ID, req.Revision+99,
			"chain_1", "RFQ-000124", "line_1")
		assertRFQSSentinel(t, err, rfqs.ErrRevisionMismatch)
	})
}

// ReadClaimSnapshot's translation, including EVERY exact-claim mismatch case.
//
// materialrequirements returns ErrMaterialRequirementNotFound for a missing
// requirement AND for each claim-term mismatch — deliberately, so the caller
// cannot learn the state of a claim it does not hold. All of them must arrive
// as the rfqs sentinel (design spec §1.3, revision 4).
func TestAdapterTranslatesReadClaimSnapshotSentinels(t *testing.T) {
	svc, req := seedClaimedRequirement(t)
	adapter := composition.NewMaterialRequirementSourceAdapter(svc)
	ctx := context.Background()

	cases := []struct {
		name                     string
		companyID, requirementID string
		chainID, lineID          string
	}{
		{"missing requirement", "company_a", "mr_zzz", "chain_1", "line_1"},
		{"wrong chain", "company_a", req.ID, "chain_zzz", "line_1"},
		{"wrong line", "company_a", req.ID, "chain_1", "line_zzz"},
		{"both wrong", "company_a", req.ID, "chain_zzz", "line_zzz"},
		{"foreign tenant", "company_b", req.ID, "chain_1", "line_1"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := adapter.ReadClaimSnapshot(ctx, tc.companyID, tc.requirementID,
				tc.chainID, tc.lineID)
			assertRFQSSentinel(t, err, rfqs.ErrMaterialRequirementNotFound)
		})
	}
}

// An UNCLAIMED requirement likewise yields the rfqs sentinel.
func TestAdapterTranslatesReadClaimSnapshotOnAnUnclaimedRequirement(t *testing.T) {
	svc, req := seedEligibleRequirement(t)
	adapter := composition.NewMaterialRequirementSourceAdapter(svc)

	_, err := adapter.ReadClaimSnapshot(context.Background(), "company_a", req.ID,
		"chain_1", "line_1")
	assertRFQSSentinel(t, err, rfqs.ErrMaterialRequirementNotFound)
}

func TestAdapterTranslatesReleaseAndReadClaimSentinels(t *testing.T) {
	svc, req := seedClaimedRequirement(t)
	adapter := composition.NewMaterialRequirementSourceAdapter(svc)
	ctx := context.Background()

	t.Run("release with the wrong line", func(t *testing.T) {
		claims, err := svc.ListClaimsForRFQChain(ctx, "company_a", "chain_1")
		if err != nil {
			t.Fatal(err)
		}
		err = adapter.ReleaseClaim(ctx, "company_a", req.ID, claims[0].Revision,
			"chain_1", "line_zzz")
		assertRFQSSentinel(t, err, rfqs.ErrRevisionMismatch)
	})

	t.Run("read claim on a missing requirement", func(t *testing.T) {
		_, _, _, _, _, err := adapter.ReadClaim(ctx, "company_a", "mr_zzz")
		assertRFQSSentinel(t, err, rfqs.ErrMaterialRequirementNotFound)
	})
}

// A sentinel the adapter does not recognise must pass through unchanged rather
// than being translated into an unrelated rfqs error — an invented meaning is
// worse than an opaque one.
func TestAdapterPassesThroughAnUnknownError(t *testing.T) {
	sentinel := errors.New("boom: some infrastructure failure")
	svc := materialRequirementServiceWithFailingRepo(t, sentinel)
	adapter := composition.NewMaterialRequirementSourceAdapter(svc)

	_, err := adapter.ReadClaimSnapshot(context.Background(), "company_a", "mr_1",
		"chain_1", "line_1")
	if !errors.Is(err, sentinel) {
		t.Fatalf("error = %v, want the original error passed through unchanged", err)
	}
	// It must NOT have been laundered into an rfqs domain sentinel.
	for _, s := range []error{
		rfqs.ErrMaterialRequirementNotFound,
		rfqs.ErrMaterialRequirementAlreadyClaimed,
		rfqs.ErrRFQLineNotEligible,
		rfqs.ErrRevisionMismatch,
	} {
		if errors.Is(err, s) {
			t.Errorf("an unknown infrastructure error was translated into %v", s)
		}
	}
}

// The adapter must never leak a materialrequirements sentinel to rfqs: if one
// escaped, rfqs' handler would fall through to 500 instead of mapping it.
func TestAdapterNeverLeaksAMaterialRequirementsSentinel(t *testing.T) {
	svc, req := seedClaimedRequirement(t)
	adapter := composition.NewMaterialRequirementSourceAdapter(svc)
	ctx := context.Background()

	_, err := adapter.ReadClaimSnapshot(ctx, "company_a", req.ID, "chain_zzz", "line_1")
	if errors.Is(err, materialrequirements.ErrMaterialRequirementNotFound) {
		t.Error("a materialrequirements sentinel crossed the boundary; rfqs matches only " +
			"its OWN sentinels, so this would surface as a 500")
	}
	if !errors.Is(err, rfqs.ErrMaterialRequirementNotFound) {
		t.Errorf("error = %v, want the rfqs sentinel", err)
	}
}

func assertRFQSSentinel(t *testing.T, got, want error) {
	t.Helper()
	if !errors.Is(got, want) {
		t.Fatalf("error = %v, want %v", got, want)
	}
}
