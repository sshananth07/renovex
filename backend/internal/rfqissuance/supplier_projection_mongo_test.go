package rfqissuance_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/shananth/renovation-platform/backend/internal/platform/secrets"
	"github.com/shananth/renovation-platform/backend/internal/rfqissuance"
)

type gatedIssuedVersionStore struct {
	delegate rfqissuance.IssuedVersionStore
	reached  chan struct{}
	release  chan struct{}
	once     sync.Once
}

func (store *gatedIssuedVersionStore) HasIssuedVersion(
	ctx context.Context, companyID, chainID string,
) (bool, error) {
	return store.delegate.HasIssuedVersion(ctx, companyID, chainID)
}

func (store *gatedIssuedVersionStore) CreateVersion(
	ctx context.Context, version rfqissuance.IssuedRFQVersion,
) (rfqissuance.IssuedRFQVersion, error) {
	return store.delegate.CreateVersion(ctx, version)
}

func (store *gatedIssuedVersionStore) FindVersion(
	ctx context.Context, companyID, versionID string,
) (rfqissuance.IssuedRFQVersion, error) {
	store.once.Do(func() {
		close(store.reached)
		<-store.release
	})
	return store.delegate.FindVersion(ctx, companyID, versionID)
}

func (store *gatedIssuedVersionStore) ListVersions(
	ctx context.Context, companyID, chainID string,
) ([]rfqissuance.IssuedRFQVersion, error) {
	return store.delegate.ListVersions(ctx, companyID, chainID)
}

func (store *gatedIssuedVersionStore) FindByOperationID(
	ctx context.Context, companyID, operationID string,
) (rfqissuance.IssuedRFQVersion, bool, error) {
	return store.delegate.FindByOperationID(ctx, companyID, operationID)
}

type gatedSupplierRFQAuthorizer struct {
	access  rfqissuance.AuthorizedSupplierRFQAccess
	reached chan struct{}
	release chan struct{}
}

func (authorizer *gatedSupplierRFQAuthorizer) AuthorizeSupplierRFQRead(
	context.Context, rfqissuance.SupplierRFQReadAuthorization,
) (rfqissuance.AuthorizedSupplierRFQAccess, error) {
	close(authorizer.reached)
	<-authorizer.release
	return authorizer.access, nil
}

func createProjectionVersion(t *testing.T,
	repo *rfqissuance.MongoIssuedRFQVersionRepository,
	companyID, chainID string, number int,
) rfqissuance.IssuedRFQVersion {
	t.Helper()
	now := time.Now().UTC()
	deadline := now.Add(30 * 24 * time.Hour)
	version, err := rfqissuance.NewIssuedVersion(rfqissuance.NewIssuedVersionInput{
		CompanyID: companyID, ProjectID: "project-1", RFQChainID: chainID,
		RFQNumber: fmt.Sprintf("RFQ-%s", chainID), VersionNumber: number,
		Currency: "MYR", Title: "Supplier projection RFQ",
		ResponseDeadline: &deadline, IssuedAt: now.Add(time.Duration(number) * time.Minute),
		IssuanceOperationID: fmt.Sprintf("projection-%s-%d", chainID, number),
	})
	if err != nil {
		t.Fatalf("build version: %v", err)
	}
	created, err := repo.CreateVersion(context.Background(), version)
	if err != nil {
		t.Fatalf("create version: %v", err)
	}
	return created
}

func createActiveProjectionInvitation(t *testing.T,
	repo *rfqissuance.MongoInvitationRepository,
	companyID, chainID, supplierID, versionID string,
) rfqissuance.SupplierInvitation {
	t.Helper()
	invitation := invitationFixture(t, companyID, chainID, supplierID)
	invitation.CurrentIssuedRFQVersionID = versionID
	invitation.Status = rfqissuance.InvitationStatusActive
	created, err := repo.CreateInvitation(context.Background(), invitation)
	if err != nil {
		t.Fatalf("create invitation: %v", err)
	}
	return created
}

func projectionAuthorizer(invitation rfqissuance.SupplierInvitation) *fakeSupplierRFQAuthorizer {
	return &fakeSupplierRFQAuthorizer{access: rfqissuance.AuthorizedSupplierRFQAccess{
		CompanyID: invitation.CompanyID, SupplierID: invitation.SupplierID,
		InvitationID: invitation.ID, AccessGeneration: invitation.AccessGeneration,
	}}
}

// These tests intentionally use real Mongo repositories. The tenant filters,
// immutable ordering, and conditional generation/chain writes are database
// behavior; replacing them with maps would only test the fake itself.
func TestSupplierRFQProjectionRealMongoIsolationAndRaces(t *testing.T) {
	db := setupDB(t)
	versions := newIssuedVersionRepo(t, db)
	invitations := newInvitationRepo(t, db)

	t.Run("tenant chain membership and pagination remain isolated", func(t *testing.T) {
		v1 := createProjectionVersion(t, versions, "company-1", "chain-page", 1)
		v2 := createProjectionVersion(t, versions, "company-1", "chain-page", 2)
		foreign := createProjectionVersion(t, versions, "company-2", "chain-page", 1)
		otherChain := createProjectionVersion(t, versions, "company-1", "chain-other", 1)
		invitation := createActiveProjectionInvitation(t, invitations,
			"company-1", "chain-page", "supplier-page", v2.ID)
		service := rfqissuance.NewService(versions, rfqissuance.WithInvitations(invitations),
			rfqissuance.WithSupplierRFQAccessAuthorizer(projectionAuthorizer(invitation)))
		read := rfqissuance.SupplierRFQReadInput{
			SessionToken: "session-token", InvitationID: invitation.ID,
			AccessedAt: time.Now().UTC(),
		}

		first, err := service.ListSupplierRFQVersions(context.Background(),
			rfqissuance.SupplierRFQHistoryInput{SupplierRFQReadInput: read, PageSize: 1})
		if err != nil || len(first.Versions) != 1 || first.Versions[0].ID != v2.ID ||
			first.NextCursor == nil {
			t.Fatalf("first page = %+v, %v", first, err)
		}
		second, err := service.ListSupplierRFQVersions(context.Background(),
			rfqissuance.SupplierRFQHistoryInput{SupplierRFQReadInput: read,
				PageSize: 1, Cursor: *first.NextCursor})
		if err != nil || len(second.Versions) != 1 || second.Versions[0].ID != v1.ID {
			t.Fatalf("second page = %+v, %v", second, err)
		}
		for _, forbidden := range []string{foreign.ID, otherChain.ID} {
			_, err = service.GetSupplierRFQVersion(context.Background(),
				rfqissuance.SupplierRFQVersionInput{SupplierRFQReadInput: read,
					VersionID: forbidden})
			if !errors.Is(err, rfqissuance.ErrSupplierRFQAccessInvalid) {
				t.Errorf("detail %s error = %v", forbidden, err)
			}
		}
	})

	t.Run("foreign expired and revoked invitations are non-disclosing", func(t *testing.T) {
		version := createProjectionVersion(t, versions, "company-access", "chain-access", 1)
		invitation := createActiveProjectionInvitation(t, invitations,
			"company-access", "chain-access", "supplier-access", version.ID)
		read := rfqissuance.SupplierRFQReadInput{
			SessionToken: "session-token", InvitationID: invitation.ID,
			AccessedAt: time.Now().UTC(),
		}

		foreignAuth := projectionAuthorizer(invitation)
		foreignAuth.access.CompanyID = "company-foreign"
		foreignService := rfqissuance.NewService(versions,
			rfqissuance.WithInvitations(invitations),
			rfqissuance.WithSupplierRFQAccessAuthorizer(foreignAuth))
		if _, err := foreignService.GetSupplierInvitationRFQ(context.Background(), read); !errors.Is(err, rfqissuance.ErrSupplierRFQAccessInvalid) {
			t.Errorf("foreign error = %v", err)
		}

		expired := invitation
		expired.ExpiresAt = read.AccessedAt.Add(-time.Minute)
		expired, err := invitations.UpdateInvitation(context.Background(), invitation.CompanyID,
			invitation.ID, invitation.Revision, expired)
		if err != nil {
			t.Fatalf("expire invitation: %v", err)
		}
		service := rfqissuance.NewService(versions, rfqissuance.WithInvitations(invitations),
			rfqissuance.WithSupplierRFQAccessAuthorizer(projectionAuthorizer(expired)))
		if _, err := service.GetSupplierInvitationRFQ(context.Background(), read); !errors.Is(err, rfqissuance.ErrSupplierRFQAccessInvalid) {
			t.Errorf("expired error = %v", err)
		}

		revoked := expired
		revoked.ExpiresAt = read.AccessedAt.Add(time.Hour)
		revokedAt := read.AccessedAt.Add(-time.Second)
		revoked.RevokedAt = &revokedAt
		revoked.Status = rfqissuance.InvitationStatusRevoked
		revoked, err = invitations.UpdateInvitation(context.Background(), invitation.CompanyID,
			invitation.ID, expired.Revision, revoked)
		if err != nil {
			t.Fatalf("revoke invitation: %v", err)
		}
		service = rfqissuance.NewService(versions, rfqissuance.WithInvitations(invitations),
			rfqissuance.WithSupplierRFQAccessAuthorizer(projectionAuthorizer(revoked)))
		if _, err := service.GetSupplierInvitationRFQ(context.Background(), read); !errors.Is(err, rfqissuance.ErrSupplierRFQAccessInvalid) {
			t.Errorf("revoked error = %v", err)
		}
	})

	t.Run("generation rotates after authorization and before owner recheck", func(t *testing.T) {
		version := createProjectionVersion(t, versions, "company-gen", "chain-gen", 1)
		invitation := createActiveProjectionInvitation(t, invitations,
			"company-gen", "chain-gen", "supplier-gen", version.ID)
		authorizer := &gatedSupplierRFQAuthorizer{
			access:  projectionAuthorizer(invitation).access,
			reached: make(chan struct{}), release: make(chan struct{}),
		}
		service := rfqissuance.NewService(versions, rfqissuance.WithInvitations(invitations),
			rfqissuance.WithSupplierRFQAccessAuthorizer(authorizer))
		result := make(chan error, 1)
		go func() {
			_, err := service.GetSupplierInvitationRFQ(context.Background(),
				rfqissuance.SupplierRFQReadInput{
					SessionToken: "session-token", InvitationID: invitation.ID,
					AccessedAt: time.Now().UTC(),
				})
			result <- err
		}()
		<-authorizer.reached
		_, err := invitations.RotateSecret(context.Background(), invitation.CompanyID,
			invitation.ID, invitation.Revision,
			secrets.HashInvitationSecret("replacement-secret"), 1)
		if err != nil {
			t.Fatalf("rotate generation: %v", err)
		}
		close(authorizer.release)
		if err := <-result; !errors.Is(err, rfqissuance.ErrSupplierRFQAccessInvalid) {
			t.Fatalf("race error = %v, want invalid access", err)
		}
	})

	t.Run("chain advances after invitation snapshot without leaking newer version", func(t *testing.T) {
		v1 := createProjectionVersion(t, versions, "company-race", "chain-race", 1)
		v2 := createProjectionVersion(t, versions, "company-race", "chain-race", 2)
		invitation := createActiveProjectionInvitation(t, invitations,
			"company-race", "chain-race", "supplier-race", v1.ID)
		gated := &gatedIssuedVersionStore{delegate: versions,
			reached: make(chan struct{}), release: make(chan struct{})}
		service := rfqissuance.NewService(gated, rfqissuance.WithInvitations(invitations),
			rfqissuance.WithSupplierRFQAccessAuthorizer(projectionAuthorizer(invitation)))
		type response struct {
			projection rfqissuance.SupplierInvitationRFQProjection
			err        error
		}
		result := make(chan response, 1)
		go func() {
			projection, err := service.GetSupplierInvitationRFQ(context.Background(),
				rfqissuance.SupplierRFQReadInput{
					SessionToken: "session-token", InvitationID: invitation.ID,
					AccessedAt: time.Now().UTC(),
				})
			result <- response{projection: projection, err: err}
		}()
		<-gated.reached
		if _, err := invitations.AdvanceInvitationsToVersion(context.Background(),
			invitation.CompanyID, invitation.RFQChainID, v2.ID); err != nil {
			t.Fatalf("advance invitation pointer: %v", err)
		}
		close(gated.release)
		got := <-result
		if got.err != nil || got.projection.CurrentRFQVersion.ID != v1.ID {
			t.Fatalf("projection = %+v, err = %v; want V1 snapshot", got.projection, got.err)
		}
	})
}

var _ rfqissuance.IssuedVersionStore = (*gatedIssuedVersionStore)(nil)
