package rfqissuance_test

import (
	"context"
	"strconv"
	"sync"

	"github.com/shananth/renovation-platform/backend/internal/rfqissuance"
)

// In-memory stores for the service-level issuance tests.
//
// These deliberately do NOT reimplement the concurrency guarantees — immutable
// unique indexes and one-winner conditional updates are proven
// against real MongoDB in the *_repository_mongo_test.go files. What these
// fakes prove is the service's ORCHESTRATION: the order of steps, what a
// refusal leaves behind, and how a retry resolves.

func chainKey(companyID, rfqChainID string) string { return companyID + "|" + rfqChainID }

type fakeChainStore struct {
	mu     sync.Mutex
	chains map[string]*rfqissuance.RFQIssuanceChain
	nextID int
}

func newFakeChainStore() *fakeChainStore {
	return &fakeChainStore{chains: map[string]*rfqissuance.RFQIssuanceChain{}}
}

func (f *fakeChainStore) EnsureChain(_ context.Context,
	companyID, rfqChainID string) (rfqissuance.RFQIssuanceChain, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	key := chainKey(companyID, rfqChainID)
	if existing, ok := f.chains[key]; ok {
		return *existing, nil
	}
	f.nextID++
	chain := &rfqissuance.RFQIssuanceChain{
		ID: "chain-doc-" + strconv.Itoa(f.nextID), CompanyID: companyID,
		RFQChainID: rfqChainID, SchemaVersion: rfqissuance.RFQIssuanceChainSchemaVersion,
	}
	f.chains[key] = chain
	return *chain, nil
}

func (f *fakeChainStore) AdvanceChain(_ context.Context, companyID, rfqChainID string,
	expectedRevision int64, versionNumber int, issuedVersionID string) (
	rfqissuance.RFQIssuanceChain, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	chain, ok := f.chains[chainKey(companyID, rfqChainID)]
	if !ok {
		return rfqissuance.RFQIssuanceChain{}, rfqissuance.ErrIssuanceChainNotFound
	}
	if chain.Revision != expectedRevision {
		return rfqissuance.RFQIssuanceChain{}, rfqissuance.ErrRevisionMismatch
	}
	id := issuedVersionID
	chain.CurrentIssuedVersionID = &id
	if versionNumber > chain.LatestIssuedVersion {
		chain.LatestIssuedVersion = versionNumber
	}
	chain.Revision++
	return *chain, nil
}

func (f *fakeChainStore) FindChain(_ context.Context,
	companyID, rfqChainID string) (rfqissuance.RFQIssuanceChain, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	chain, ok := f.chains[chainKey(companyID, rfqChainID)]
	if !ok {
		return rfqissuance.RFQIssuanceChain{}, rfqissuance.ErrIssuanceChainNotFound
	}
	return *chain, nil
}

type fakeDraftStore struct {
	mu     sync.Mutex
	drafts map[string]*rfqissuance.RFQAmendmentDraft
	nextID int
}

func newFakeDraftStore() *fakeDraftStore {
	return &fakeDraftStore{drafts: map[string]*rfqissuance.RFQAmendmentDraft{}}
}

func (f *fakeDraftStore) CreateDraft(_ context.Context,
	draft rfqissuance.RFQAmendmentDraft) (rfqissuance.RFQAmendmentDraft, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	key := chainKey(draft.CompanyID, draft.RFQChainID)
	if _, exists := f.drafts[key]; exists {
		return rfqissuance.RFQAmendmentDraft{}, rfqissuance.ErrAmendmentDraftAlreadyExists
	}
	f.nextID++
	draft.ID = "draft-doc-" + strconv.Itoa(f.nextID)
	stored := draft
	f.drafts[key] = &stored
	return stored, nil
}

func (f *fakeDraftStore) FindDraft(_ context.Context,
	companyID, rfqChainID string) (rfqissuance.RFQAmendmentDraft, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	draft, ok := f.drafts[chainKey(companyID, rfqChainID)]
	if !ok {
		return rfqissuance.RFQAmendmentDraft{}, rfqissuance.ErrAmendmentDraftNotFound
	}
	return *draft, nil
}

func (f *fakeDraftStore) UpdateDraft(_ context.Context, companyID, rfqChainID string,
	expectedRevision int64, updated rfqissuance.RFQAmendmentDraft) (
	rfqissuance.RFQAmendmentDraft, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	stored, ok := f.drafts[chainKey(companyID, rfqChainID)]
	if !ok {
		return rfqissuance.RFQAmendmentDraft{}, rfqissuance.ErrAmendmentDraftNotFound
	}
	if stored.Revision != expectedRevision {
		return rfqissuance.RFQAmendmentDraft{}, rfqissuance.ErrRevisionMismatch
	}
	updated.ID = stored.ID
	updated.Revision = stored.Revision + 1
	*stored = updated
	return *stored, nil
}

func (f *fakeDraftStore) DeleteDraft(_ context.Context, companyID, rfqChainID string,
	expectedRevision int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	key := chainKey(companyID, rfqChainID)
	stored, ok := f.drafts[key]
	if !ok {
		return rfqissuance.ErrAmendmentDraftNotFound
	}
	if stored.Revision != expectedRevision {
		return rfqissuance.ErrRevisionMismatch
	}
	delete(f.drafts, key)
	return nil
}

type fakeVersionStore struct {
	mu      sync.Mutex
	created []rfqissuance.IssuedRFQVersion
	nextID  int
}

func newFakeVersionStore() *fakeVersionStore {
	return &fakeVersionStore{}
}

func (f *fakeVersionStore) HasIssuedVersion(_ context.Context,
	companyID, rfqChainID string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	for _, v := range f.created {
		if v.CompanyID == companyID && v.RFQChainID == rfqChainID {
			return true, nil
		}
	}
	return false, nil
}

func (f *fakeVersionStore) CreateVersion(_ context.Context,
	version rfqissuance.IssuedRFQVersion) (rfqissuance.IssuedRFQVersion, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	for _, v := range f.created {
		if v.CompanyID == version.CompanyID && v.RFQChainID == version.RFQChainID &&
			v.VersionNumber == version.VersionNumber {
			return rfqissuance.IssuedRFQVersion{}, rfqissuance.ErrVersionAlreadyExists
		}
		if version.IssuanceOperationID != "" && v.CompanyID == version.CompanyID &&
			v.IssuanceOperationID == version.IssuanceOperationID {
			return rfqissuance.IssuedRFQVersion{}, rfqissuance.ErrOperationAlreadyUsed
		}
	}

	f.nextID++
	version.ID = "version-doc-" + strconv.Itoa(f.nextID)
	f.created = append(f.created, version)
	return version, nil
}

func (f *fakeVersionStore) FindVersion(_ context.Context,
	companyID, versionID string) (rfqissuance.IssuedRFQVersion, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	for _, v := range f.created {
		if v.CompanyID == companyID && v.ID == versionID {
			return v, nil
		}
	}
	return rfqissuance.IssuedRFQVersion{}, rfqissuance.ErrIssuedVersionNotFound
}

func (f *fakeVersionStore) ListVersions(_ context.Context,
	companyID, rfqChainID string) ([]rfqissuance.IssuedRFQVersion, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	var versions []rfqissuance.IssuedRFQVersion
	for _, version := range f.created {
		if version.CompanyID == companyID && version.RFQChainID == rfqChainID {
			versions = append(versions, version)
		}
	}
	return versions, nil
}

func (f *fakeVersionStore) FindByOperationID(_ context.Context,
	companyID, operationID string) (rfqissuance.IssuedRFQVersion, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	for _, v := range f.created {
		if v.CompanyID == companyID && v.IssuanceOperationID == operationID {
			return v, true, nil
		}
	}
	return rfqissuance.IssuedRFQVersion{}, false, nil
}
