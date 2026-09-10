package rfqissuance

import "context"

// ListIssuedVersions returns the immutable history for one tenant-scoped RFQ
// chain in ascending version order.
func (s *Service) ListIssuedVersions(ctx context.Context,
	companyID, rfqChainID string) ([]IssuedRFQVersion, error) {

	if s.chains == nil {
		return nil, ErrIssuanceNotConfigured
	}
	// An empty repository result cannot distinguish "known chain with no
	// versions" from "real chain in another tenant". Resolve the tenant-scoped
	// chain first so a foreign identifier remains the same 404 as an absent one.
	if _, err := s.chains.FindChain(ctx, companyID, rfqChainID); err != nil {
		return nil, err
	}
	return s.versions.ListVersions(ctx, companyID, rfqChainID)
}

// ReconcileIssuanceChain completes a bounded interrupted transition:
//
//	immutable version exists -> chain pointer did not advance
//
// The immutable version is the authority. Reconciliation may point the chain
// only at the highest existing version returned by the tenant-and-chain-scoped
// store. It never creates content, reuses a number, or rewinds a pointer.
//
// A chain with no version, or an already aligned chain, is an idempotent no-op:
// there is no known transition to complete.
func (s *Service) ReconcileIssuanceChain(ctx context.Context,
	companyID, actorUserID, rfqChainID string) (RFQIssuanceChain, error) {

	if s.chains == nil {
		return RFQIssuanceChain{}, ErrIssuanceNotConfigured
	}
	chain, err := s.chains.FindChain(ctx, companyID, rfqChainID)
	if err != nil {
		return RFQIssuanceChain{}, err
	}
	versions, err := s.versions.ListVersions(ctx, companyID, rfqChainID)
	if err != nil {
		return RFQIssuanceChain{}, err
	}
	if len(versions) == 0 {
		return chain, nil
	}

	target := versions[len(versions)-1]
	if chain.CurrentIssuedVersionID != nil &&
		*chain.CurrentIssuedVersionID == target.ID &&
		chain.LatestIssuedVersion >= target.VersionNumber {
		return chain, nil
	}

	// AdvanceChain uses $max for LatestIssuedVersion, so a repair cannot move
	// the recorded number backwards if another transition has already advanced.
	reconciled, err := s.chains.AdvanceChain(ctx, companyID, rfqChainID, chain.Revision,
		target.VersionNumber, target.ID)
	if err != nil {
		return RFQIssuanceChain{}, err
	}
	_ = s.audit.RecordRFQIssuanceChainReconciled(
		ctx, companyID, target.ProjectID, actorUserID,
		target.RFQChainID, target.RFQNumber, target.ID, target.VersionNumber)
	return reconciled, nil
}
