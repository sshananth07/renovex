package rfqissuance_test

import (
	"context"
	"errors"
	"testing"

	"github.com/shananth/renovation-platform/backend/internal/rfqissuance"
)

type failAdvanceChainStore struct {
	*rfqissuance.MongoIssuanceChainRepository
	err error
}

func (s failAdvanceChainStore) AdvanceChain(
	context.Context, string, string, int64, int, string,
) (rfqissuance.RFQIssuanceChain, error) {
	return rfqissuance.RFQIssuanceChain{}, s.err
}

// Reconciliation completes only a known pointer gap: an immutable version
// already exists, but the process stopped before the issuance chain pointed at
// it. It never fabricates the version or its commercial content.
func TestReconcileIssuanceChainAdvancesToAnExistingOrphanVersion(t *testing.T) {
	db := setupDB(t)
	versions := newIssuedVersionRepo(t, db)
	chains := newChainRepo(t, db)
	ctx := context.Background()

	chain, err := chains.EnsureChain(ctx, "company-1", "chain-1")
	if err != nil {
		t.Fatalf("ensure chain: %v", err)
	}
	orphan, err := versions.CreateVersion(ctx, issuableVersion(t, 1, "op-interrupted"))
	if err != nil {
		t.Fatalf("create orphan immutable version: %v", err)
	}
	if chain.CurrentIssuedVersionID != nil {
		t.Fatal("test setup already has a chain pointer; the interrupted state was not created")
	}

	audit := &recordingIssuanceAudit{}
	svc := rfqissuance.NewService(
		versions,
		rfqissuance.WithIssuanceChains(chains),
		rfqissuance.WithAuditRecorder(audit),
	)
	reconciled, err := svc.ReconcileIssuanceChain(
		ctx, "company-1", "user-1", "chain-1")
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	if reconciled.CurrentIssuedVersionID == nil ||
		*reconciled.CurrentIssuedVersionID != orphan.ID {
		t.Errorf("CurrentIssuedVersionID = %v, want orphan %s",
			reconciled.CurrentIssuedVersionID, orphan.ID)
	}
	if reconciled.LatestIssuedVersion != 1 {
		t.Errorf("LatestIssuedVersion = %d, want 1", reconciled.LatestIssuedVersion)
	}

	// Re-running an aligned reconciliation is a no-op, not a second event.
	if _, err := svc.ReconcileIssuanceChain(
		ctx, "company-1", "user-1", "chain-1"); err != nil {
		t.Fatalf("idempotent reconcile: %v", err)
	}
	if len(audit.chainReconciled) != 1 {
		t.Fatalf("recorded %d reconciliation events, want exactly 1",
			len(audit.chainReconciled))
	}
	call := audit.chainReconciled[0]
	if call.companyID != "company-1" ||
		call.projectID != orphan.ProjectID ||
		call.actorUserID != "user-1" ||
		call.rfqChainID != "chain-1" ||
		call.rfqNumber != orphan.RFQNumber ||
		call.versionID != orphan.ID ||
		call.versionNumber != 1 {
		t.Errorf("audit call = %+v, want repaired version identity", call)
	}
}

// An operation-ID retry is the natural recovery path after a caller received
// an uncertain result. It must complete the pointer before returning the
// immutable version it found.
func TestInitialIssuanceRetryReconcilesItsOrphanVersion(t *testing.T) {
	db := setupDB(t)
	versions := newIssuedVersionRepo(t, db)
	chains := newChainRepo(t, db)
	ctx := context.Background()
	source := concurrentReadyRFQSource{snapshot: readySnapshot(futureDeadline())}

	interrupted := errors.New("simulated pointer-write interruption")
	firstAttempt := rfqissuance.NewService(
		versions,
		rfqissuance.WithReadyRFQSource(source),
		rfqissuance.WithIssuanceChains(failAdvanceChainStore{
			MongoIssuanceChainRepository: chains,
			err:                          interrupted,
		}),
	)
	issued, err := firstAttempt.IssueVersion(
		ctx, "company-1", "user-1", issueInput("op-interrupted"))
	if err != nil {
		t.Fatalf("the immutable version is authoritative despite pointer failure: %v", err)
	}

	gap, err := chains.FindChain(ctx, "company-1", "chain-1")
	if err != nil {
		t.Fatalf("find interrupted chain: %v", err)
	}
	if gap.CurrentIssuedVersionID != nil {
		t.Fatal("simulated interruption did not leave the expected pointer gap")
	}

	retry := rfqissuance.NewService(
		versions,
		rfqissuance.WithReadyRFQSource(source),
		rfqissuance.WithIssuanceChains(chains),
	)
	recovered, err := retry.IssueVersion(
		ctx, "company-1", "user-1", issueInput("op-interrupted"))
	if err != nil {
		t.Fatalf("idempotent retry: %v", err)
	}
	if recovered.ID != issued.ID {
		t.Errorf("recovered version %s, want original %s", recovered.ID, issued.ID)
	}

	reconciled, err := chains.FindChain(ctx, "company-1", "chain-1")
	if err != nil {
		t.Fatalf("find reconciled chain: %v", err)
	}
	if reconciled.CurrentIssuedVersionID == nil ||
		*reconciled.CurrentIssuedVersionID != issued.ID {
		t.Errorf("CurrentIssuedVersionID = %v, want recovered version %s",
			reconciled.CurrentIssuedVersionID, issued.ID)
	}
}
