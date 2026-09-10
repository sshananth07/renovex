package supplieroffers_test

import (
	"context"
	"testing"

	"go.mongodb.org/mongo-driver/v2/mongo"

	"github.com/shananth/renovation-platform/backend/internal/supplieroffers"
)

func newOfferChainRepository(
	t *testing.T,
	db *mongo.Database,
) *supplieroffers.MongoOfferChainRepository {
	t.Helper()

	repository := supplieroffers.NewMongoOfferChainRepository(db)
	if err := repository.EnsureIndexes(context.Background()); err != nil {
		t.Fatalf("ensuring offer-chain indexes: %v", err)
	}
	return repository
}

func TestEnsureOfferChainConvergesOnOneIdentity(t *testing.T) {
	db := setupDB(t)
	repository := newOfferChainRepository(t, db)
	ctx := context.Background()

	first, err := repository.EnsureOfferChain(
		ctx,
		"company-1",
		"invitation-1",
		"issued-rfq-version-1",
	)
	if err != nil {
		t.Fatalf("ensuring first offer chain: %v", err)
	}
	second, err := repository.EnsureOfferChain(
		ctx,
		"company-1",
		"invitation-1",
		"issued-rfq-version-1",
	)
	if err != nil {
		t.Fatalf("ensuring the same offer chain again: %v", err)
	}

	if first.ID == "" {
		t.Fatal("persisted offer chain has no ID")
	}
	if second.ID != first.ID {
		t.Fatalf(
			"same Company + Invitation + Issued RFQ Version produced two chains: %q and %q",
			first.ID,
			second.ID,
		)
	}
	if first.LatestSubmittedVersion != 0 || first.LatestSubmittedID != nil {
		t.Fatalf(
			"new chain submission projection = version %d, ID %v; want no submitted version",
			first.LatestSubmittedVersion,
			first.LatestSubmittedID,
		)
	}
	if first.Revision != 0 {
		t.Fatalf("new chain revision = %d, want 0", first.Revision)
	}
}

func TestEnsureOfferChainUsesEveryIdentityKey(t *testing.T) {
	db := setupDB(t)
	repository := newOfferChainRepository(t, db)
	ctx := context.Background()

	base, err := repository.EnsureOfferChain(
		ctx,
		"company-1",
		"invitation-1",
		"issued-rfq-version-1",
	)
	if err != nil {
		t.Fatalf("ensuring base chain: %v", err)
	}

	cases := []struct {
		name            string
		companyID       string
		invitationID    string
		issuedVersionID string
	}{
		{
			name:            "company",
			companyID:       "company-2",
			invitationID:    "invitation-1",
			issuedVersionID: "issued-rfq-version-1",
		},
		{
			name:            "invitation",
			companyID:       "company-1",
			invitationID:    "invitation-2",
			issuedVersionID: "issued-rfq-version-1",
		},
		{
			name:            "issued RFQ version",
			companyID:       "company-1",
			invitationID:    "invitation-1",
			issuedVersionID: "issued-rfq-version-2",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			other, ensureErr := repository.EnsureOfferChain(
				ctx,
				testCase.companyID,
				testCase.invitationID,
				testCase.issuedVersionID,
			)
			if ensureErr != nil {
				t.Fatalf("ensuring distinct chain: %v", ensureErr)
			}
			if other.ID == base.ID {
				t.Fatalf("changing %s reused the base offer chain", testCase.name)
			}
		})
	}
}

// ListChainsForInvitation is the read the M8.1 copy-forward source resolver
// uses to search backward across a Supplier's earlier issued RFQ versions of
// the SAME invitation. It must return every chain for that invitation and
// company, regardless of which issued RFQ version it belongs to, and must
// never leak another company's or another invitation's chain.
func TestListChainsForInvitationReturnsEveryChainAcrossIssuedVersions(t *testing.T) {
	db := setupDB(t)
	repository := newOfferChainRepository(t, db)
	ctx := context.Background()

	v1, err := repository.EnsureOfferChain(ctx, "company-1", "invitation-1", "issued-rfq-version-1")
	if err != nil {
		t.Fatalf("ensuring v1 chain: %v", err)
	}
	v2, err := repository.EnsureOfferChain(ctx, "company-1", "invitation-1", "issued-rfq-version-2")
	if err != nil {
		t.Fatalf("ensuring v2 chain: %v", err)
	}
	// A different invitation must never appear in the result.
	if _, err := repository.EnsureOfferChain(
		ctx, "company-1", "invitation-OTHER", "issued-rfq-version-1"); err != nil {
		t.Fatalf("ensuring other-invitation chain: %v", err)
	}
	// A different company must never appear in the result, even with the
	// exact same invitation and issued-version identifiers.
	if _, err := repository.EnsureOfferChain(
		ctx, "company-OTHER", "invitation-1", "issued-rfq-version-1"); err != nil {
		t.Fatalf("ensuring other-company chain: %v", err)
	}

	chains, err := repository.ListChainsForInvitation(ctx, "company-1", "invitation-1")
	if err != nil {
		t.Fatalf("ListChainsForInvitation: %v", err)
	}
	if len(chains) != 2 {
		t.Fatalf("chains = %d, want exactly the 2 company-1/invitation-1 chains", len(chains))
	}
	found := map[string]bool{}
	for _, chain := range chains {
		found[chain.ID] = true
		if chain.CompanyID != "company-1" || chain.InvitationID != "invitation-1" {
			t.Fatalf("chain leaked foreign scope: %#v", chain)
		}
	}
	if !found[v1.ID] || !found[v2.ID] {
		t.Fatalf("chains = %#v, want both v1 (%s) and v2 (%s)", chains, v1.ID, v2.ID)
	}
}
