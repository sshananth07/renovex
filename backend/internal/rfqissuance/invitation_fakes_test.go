package rfqissuance_test

import (
	"context"
	"encoding/base64"
	"strconv"
	"sync"
	"time"

	"github.com/shananth/renovation-platform/backend/internal/platform/mail"
	"github.com/shananth/renovation-platform/backend/internal/rfqissuance"
)

// In-memory collaborators for the invitation service tests.
//
// The uniqueness, rotation-atomicity and generation-guard properties are proven
// against real MongoDB in invitation_repository_mongo_test.go. What these prove
// is the service's ORCHESTRATION: what is derived, what is persisted, what is
// emailed, and in which order.

// testKey mirrors the keyring test fixture: deterministic and correctly sized.
func testKey(v int) []byte {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(v*31 + i)
	}
	return key
}

func encodedTestKey(v int) string {
	return base64.StdEncoding.EncodeToString(testKey(v))
}

type fakeInvitationStore struct {
	mu     sync.Mutex
	stored map[string]*rfqissuance.SupplierInvitation
	nextID int

	reactivations map[string]fakeReactivationRecord

	// recipientReplacements keeps replacement idempotency metadata OUT of
	// SupplierInvitation, mirroring the repository which stores it as a private
	// document field rather than a contractor-facing DTO field.
	recipientReplacements map[string]string

	// failAfterCreate makes every post-insert mutation fail, simulating a crash
	// or infrastructure failure immediately after the invitation row lands. A
	// correct creation flow is unaffected by it, because it performs exactly
	// one write.
	failAfterCreate bool

	// assignedID records whether the STORE had to mint the ID. The service must
	// mint it instead: the ID is part of the HMAC derivation input, so a
	// repository-assigned ID would make the secret underivable before the
	// insert and force a second write.
	assignedID bool

	// failSecretRotation makes ONLY the secret-rotation write fail. It targets
	// the crash window in a two-write recipient replacement: the snapshot has
	// landed but the link has not been invalidated yet. An atomic replacement
	// has no such window to expose.
	failSecretRotation bool

	// failAdvance makes the bulk advance fail, simulating an interruption
	// between creating a new version and moving invitations onto it.
	failAdvance bool
}

type fakeReactivationRecord struct {
	expectedRevision int64
	expiresAt        time.Time
	result           rfqissuance.SupplierInvitation
}

func newFakeInvitationStore() *fakeInvitationStore {
	return &fakeInvitationStore{
		stored:        map[string]*rfqissuance.SupplierInvitation{},
		reactivations: map[string]fakeReactivationRecord{},
	}
}

func (f *fakeInvitationStore) CreateInvitation(_ context.Context,
	invitation rfqissuance.SupplierInvitation) (rfqissuance.SupplierInvitation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	for _, existing := range f.stored {
		if existing.CompanyID == invitation.CompanyID &&
			existing.RFQChainID == invitation.RFQChainID &&
			existing.SupplierID == invitation.SupplierID {
			return rfqissuance.SupplierInvitation{}, rfqissuance.ErrInvitationAlreadyExists
		}
	}

	// A real repository lets Mongo mint _id. This fake only does so when the
	// caller supplied none — and records that, because a service relying on it
	// cannot have derived the secret before inserting.
	if invitation.ID == "" {
		f.assignedID = true
		f.nextID++
		invitation.ID = "invitation-doc-" + strconv.Itoa(f.nextID)
	}
	copied := invitation
	f.stored[invitation.ID] = &copied
	return copied, nil
}

func (f *fakeInvitationStore) FindInvitation(_ context.Context,
	companyID, invitationID string) (rfqissuance.SupplierInvitation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	invitation, ok := f.stored[invitationID]
	if !ok || invitation.CompanyID != companyID {
		return rfqissuance.SupplierInvitation{}, rfqissuance.ErrInvitationNotFound
	}
	return *invitation, nil
}

func (f *fakeInvitationStore) FindInvitationByAccessSecretHash(_ context.Context,
	accessSecretHash string) (rfqissuance.SupplierInvitation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	for _, invitation := range f.stored {
		if invitation.AccessSecretHash == accessSecretHash {
			return *invitation, nil
		}
	}
	return rfqissuance.SupplierInvitation{}, rfqissuance.ErrInvitationNotFound
}

func (f *fakeInvitationStore) FindInvitationForSupplier(_ context.Context,
	companyID, rfqChainID, supplierID string) (rfqissuance.SupplierInvitation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	for _, invitation := range f.stored {
		if invitation.CompanyID == companyID && invitation.RFQChainID == rfqChainID &&
			invitation.SupplierID == supplierID {
			return *invitation, nil
		}
	}
	return rfqissuance.SupplierInvitation{}, rfqissuance.ErrInvitationNotFound
}

func (f *fakeInvitationStore) ListInvitationsForChain(_ context.Context,
	companyID, rfqChainID string) ([]rfqissuance.SupplierInvitation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	var out []rfqissuance.SupplierInvitation
	for _, invitation := range f.stored {
		if invitation.CompanyID == companyID && invitation.RFQChainID == rfqChainID {
			out = append(out, *invitation)
		}
	}
	return out, nil
}

func (f *fakeInvitationStore) UpdateInvitation(_ context.Context,
	companyID, invitationID string, expectedRevision int64,
	updated rfqissuance.SupplierInvitation) (rfqissuance.SupplierInvitation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.failAfterCreate {
		return rfqissuance.SupplierInvitation{}, errPostCreateWriteRefused
	}

	stored, ok := f.stored[invitationID]
	if !ok || stored.CompanyID != companyID {
		return rfqissuance.SupplierInvitation{}, rfqissuance.ErrInvitationNotFound
	}
	if stored.Revision != expectedRevision {
		return rfqissuance.SupplierInvitation{}, rfqissuance.ErrRevisionMismatch
	}

	// Mirror the repository: an ordinary update never moves the secret fields.
	updated.ID = stored.ID
	updated.AccessSecretHash = stored.AccessSecretHash
	updated.AccessGeneration = stored.AccessGeneration
	updated.SecretKeyVersion = stored.SecretKeyVersion
	updated.Revision = stored.Revision + 1
	*stored = updated
	return *stored, nil
}

func (f *fakeInvitationStore) RotateSecret(_ context.Context,
	companyID, invitationID string, expectedRevision int64,
	newSecretHash string, keyVersion int) (rfqissuance.SupplierInvitation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.failAfterCreate || f.failSecretRotation {
		return rfqissuance.SupplierInvitation{}, errPostCreateWriteRefused
	}

	stored, ok := f.stored[invitationID]
	if !ok || stored.CompanyID != companyID {
		return rfqissuance.SupplierInvitation{}, rfqissuance.ErrInvitationNotFound
	}
	if stored.Revision != expectedRevision {
		return rfqissuance.SupplierInvitation{}, rfqissuance.ErrRevisionMismatch
	}

	stored.AccessSecretHash = newSecretHash
	stored.SecretKeyVersion = keyVersion
	stored.AccessGeneration++
	stored.Revision++
	return *stored, nil
}

// ReplaceRecipientAtomically mirrors the repository's single conditional write.
// Because the snapshot and rotation move together here, failSecretRotation can
// only reject the WHOLE operation — it cannot produce a half-applied state.
func (f *fakeInvitationStore) ReplaceRecipientAtomically(_ context.Context,
	input rfqissuance.ReplaceRecipientAtomicInput) (
	rfqissuance.SupplierInvitation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.failAfterCreate || f.failSecretRotation {
		return rfqissuance.SupplierInvitation{}, errPostCreateWriteRefused
	}

	stored, ok := f.stored[input.InvitationID]
	if !ok || stored.CompanyID != input.CompanyID {
		return rfqissuance.SupplierInvitation{}, rfqissuance.ErrInvitationNotFound
	}

	// A completed same-operation replacement converges without another
	// generation increment.
	if input.ReplacementOperationID != "" &&
		f.recipientReplacements[input.InvitationID] == input.ReplacementOperationID {
		return *stored, nil
	}
	if stored.Revision != input.ExpectedRevision ||
		stored.RecipientEmailNormalized != input.PreviousRecipientIdentity {
		return rfqissuance.SupplierInvitation{}, rfqissuance.ErrRevisionMismatch
	}

	stored.RecipientName = input.RecipientName
	stored.RecipientEmail = input.RecipientEmail
	stored.RecipientEmailNormalized = input.RecipientEmailNormalized
	stored.AccessSecretHash = input.NewSecretHash
	stored.SecretKeyVersion = input.SecretKeyVersion
	stored.AccessGeneration++
	stored.Revision++
	if f.recipientReplacements == nil {
		f.recipientReplacements = map[string]string{}
	}
	f.recipientReplacements[input.InvitationID] = input.ReplacementOperationID
	return *stored, nil
}

func (f *fakeInvitationStore) ReactivateInvitation(_ context.Context,
	companyID, invitationID string, command rfqissuance.ReactivateInvitationCommand) (
	rfqissuance.SupplierInvitation, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.failAfterCreate {
		return rfqissuance.SupplierInvitation{}, false, errPostCreateWriteRefused
	}

	operationKey := companyID + "|" + invitationID + "|" + command.OperationID
	if prior, ok := f.reactivations[operationKey]; ok {
		if prior.expectedRevision != command.ExpectedRevision ||
			!prior.expiresAt.Equal(command.ExpiresAt) {
			return rfqissuance.SupplierInvitation{}, false,
				rfqissuance.ErrOperationAlreadyUsed
		}
		return prior.result, false, nil
	}

	stored, ok := f.stored[invitationID]
	if !ok || stored.CompanyID != companyID {
		return rfqissuance.SupplierInvitation{}, false,
			rfqissuance.ErrInvitationNotFound
	}
	if stored.Revision != command.ExpectedRevision {
		return rfqissuance.SupplierInvitation{}, false,
			rfqissuance.ErrRevisionMismatch
	}

	eligible := stored.RevokedAt != nil ||
		stored.Status == rfqissuance.InvitationStatusRevoked ||
		stored.Status == rfqissuance.InvitationStatusExpired ||
		!stored.ExpiresAt.After(command.Now)
	if !eligible {
		if stored.Status == rfqissuance.InvitationStatusActive &&
			command.Now.Before(stored.ExpiresAt) {
			return rfqissuance.SupplierInvitation{}, false,
				rfqissuance.ErrInvitationAlreadyActive
		}
		return rfqissuance.SupplierInvitation{}, false,
			rfqissuance.ErrInvitationNotReactivatable
	}

	stored.Status = rfqissuance.InvitationStatusActive
	stored.ExpiresAt = command.ExpiresAt
	stored.RevokedAt = nil
	stored.CurrentIssuedRFQVersionID = command.CurrentIssuedRFQVersionID
	stored.AccessSecretHash = command.NewAccessSecretHash
	stored.SecretKeyVersion = command.SecretKeyVersion
	stored.AccessGeneration++
	stored.Revision++
	stored.UpdatedAt = command.Now

	result := *stored
	f.reactivations[operationKey] = fakeReactivationRecord{
		expectedRevision: command.ExpectedRevision,
		expiresAt:        command.ExpiresAt,
		result:           result,
	}
	return result, true, nil
}

func (f *fakeInvitationStore) RecordViewed(_ context.Context,
	companyID, invitationID string, accessGeneration int64, viewedAt time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.failAfterCreate {
		return errPostCreateWriteRefused
	}

	stored, ok := f.stored[invitationID]
	if !ok || stored.CompanyID != companyID ||
		stored.AccessGeneration != accessGeneration {
		return rfqissuance.ErrInvitationNotFound
	}
	if stored.FirstViewedAt == nil || viewedAt.Before(*stored.FirstViewedAt) {
		v := viewedAt
		stored.FirstViewedAt = &v
	}
	if stored.LastViewedAt == nil || viewedAt.After(*stored.LastViewedAt) {
		v := viewedAt
		stored.LastViewedAt = &v
	}
	return nil
}

func (f *fakeInvitationStore) AdvanceInvitationsToVersion(_ context.Context,
	companyID, rfqChainID, issuedVersionID string) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.failAdvance {
		return 0, errPostCreateWriteRefused
	}

	var advanced int64
	for _, invitation := range f.stored {
		if invitation.CompanyID == companyID && invitation.RFQChainID == rfqChainID &&
			invitation.RevokedAt == nil &&
			invitation.Status != rfqissuance.InvitationStatusRevoked {
			invitation.CurrentIssuedRFQVersionID = issuedVersionID
			advanced++
		}
	}
	return advanced, nil
}

type fakeDeliveryStore struct {
	mu     sync.Mutex
	stored []rfqissuance.InvitationDeliveryAttempt
	nextID int
}

func newFakeDeliveryStore() *fakeDeliveryStore {
	return &fakeDeliveryStore{}
}

func (f *fakeDeliveryStore) CreateAttempt(_ context.Context,
	attempt rfqissuance.InvitationDeliveryAttempt) (
	rfqissuance.InvitationDeliveryAttempt, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	for _, existing := range f.stored {
		if existing.CompanyID == attempt.CompanyID &&
			existing.InvitationID == attempt.InvitationID &&
			existing.DeliveryOperationID == attempt.DeliveryOperationID {
			return rfqissuance.InvitationDeliveryAttempt{},
				rfqissuance.ErrDeliveryOperationAlreadyUsed
		}
	}

	f.nextID++
	attempt.ID = "delivery-doc-" + strconv.Itoa(f.nextID)
	f.stored = append(f.stored, attempt)
	return attempt, nil
}

func (f *fakeDeliveryStore) UpdateAttemptStatus(_ context.Context,
	companyID, attemptID string, status rfqissuance.DeliveryStatus,
	failureCode rfqissuance.DeliveryFailureCode, sentAt *time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	for i := range f.stored {
		if f.stored[i].CompanyID == companyID && f.stored[i].ID == attemptID {
			f.stored[i].Status = status
			f.stored[i].FailureCode = failureCode
			f.stored[i].SentAt = sentAt
			return nil
		}
	}
	return rfqissuance.ErrDeliveryAttemptNotFound
}

func (f *fakeDeliveryStore) FindAttemptByOperationID(_ context.Context,
	companyID, invitationID, operationID string) (
	rfqissuance.InvitationDeliveryAttempt, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	for _, attempt := range f.stored {
		if attempt.CompanyID == companyID && attempt.InvitationID == invitationID &&
			attempt.DeliveryOperationID == operationID {
			return attempt, true, nil
		}
	}
	return rfqissuance.InvitationDeliveryAttempt{}, false, nil
}

func (f *fakeDeliveryStore) ListAttemptsForInvitation(_ context.Context,
	companyID, invitationID string) ([]rfqissuance.InvitationDeliveryAttempt, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	var out []rfqissuance.InvitationDeliveryAttempt
	for _, attempt := range f.stored {
		if attempt.CompanyID == companyID && attempt.InvitationID == invitationID {
			out = append(out, attempt)
		}
	}
	return out, nil
}

func (f *fakeDeliveryStore) ObsoletePendingAttemptsBeforeGeneration(
	_ context.Context, companyID, invitationID string, currentGeneration int64,
) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	var changed int64
	for i := range f.stored {
		attempt := &f.stored[i]
		if attempt.CompanyID != companyID || attempt.InvitationID != invitationID ||
			attempt.Status != rfqissuance.DeliveryStatusPending ||
			attempt.AccessGeneration >= currentGeneration {
			continue
		}
		attempt.Status = rfqissuance.DeliveryStatusObsolete
		attempt.FailureCode = ""
		attempt.SentAt = nil
		changed++
	}
	return changed, nil
}

// fakeSupplierLookup stands in for the composition adapter over
// suppliers.Service. The three refusal reasons collapse to one boolean because
// that is exactly what the capability exposes.
type fakeSupplierLookup struct {
	invitable    bool
	err          error
	gotCompanyID string
}

func (f *fakeSupplierLookup) SupplierIsInvitable(_ context.Context,
	companyID, supplierID string) (bool, error) {
	f.gotCompanyID = companyID
	return f.invitable, f.err
}

type fakeMailer struct {
	mu   sync.Mutex
	sent []mail.Message
	err  error
}

func (f *fakeMailer) Send(_ context.Context, msg mail.Message) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.err != nil {
		return f.err
	}
	f.sent = append(f.sent, msg)
	return nil
}

// errPostCreateWriteRefused simulates a crash or infrastructure failure that
// makes every write AFTER the invitation insert fail. A creation flow that
// needs a second write cannot survive it; one that inserts its complete initial
// secret state is unaffected.
var errPostCreateWriteRefused = &stubError{"post-create write refused"}
