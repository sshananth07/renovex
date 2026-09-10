package rfqissuance

import "errors"

// Module-owned sentinels (design spec §14).
//
// The handler maps these to HTTP status codes. Every sentinel is owned by this
// module: a caller matches only on these, so an error escaping from a
// dependency falls through to 500 rather than being laundered into a
// client-facing meaning it does not have.
var (
	ErrInputLimitExceeded  = errors.New("rfqissuance: input exceeds an approved limit")
	ErrInvalidBusinessDate = errors.New("rfqissuance: business date is outside the approved horizon")
	// ErrResponseDeadlineRequired reports a ready M7 RFQ with no
	// ResponseDeadline (§4.1A). M7 permits such an RFQ to be marked ready; the
	// external issuance boundary is where a concrete Supplier response window
	// becomes commercially operative, and the platform must not invent one.
	// Maps to 422.
	ErrResponseDeadlineRequired = errors.New("rfqissuance: response deadline is required to issue")

	// ErrMaterialIDRequired reports an M8-native amendment line with no
	// Material. Without an originating Material Requirement, the Material is
	// the only thing tying the line to the catalogue. Maps to 422.
	ErrMaterialIDRequired = errors.New("rfqissuance: material id is required on an M8-native line")

	// ErrInvalidQuantity reports a malformed, non-positive, or unitless RFQ
	// line quantity. Exact decimal parsing remains authoritative; floats never
	// cross this boundary. Maps to 422.
	ErrInvalidQuantity = errors.New("rfqissuance: invalid rfq line quantity")

	// ErrAmendmentLineNotFound reports a client-supplied line ID that is not in
	// the current draft. Treating it as a new line would let the caller invent
	// server-owned lineage and provenance. Maps to 422.
	ErrAmendmentLineNotFound = errors.New("rfqissuance: amendment line not found")

	// ErrDuplicateAmendmentLine reports the same existing draft line more than
	// once in a full replacement. One server-owned identity cannot represent
	// two resulting commercial lines. Maps to 422.
	ErrDuplicateAmendmentLine = errors.New("rfqissuance: duplicate amendment line")

	// ErrIssuanceChainNotFound reports a missing chain OR one belonging to
	// another tenant. The two collapse deliberately: a foreign caller must not
	// learn that the chain exists. Maps to 404.
	ErrIssuanceChainNotFound = errors.New("rfqissuance: issuance chain not found")

	// ErrRevisionMismatch reports a conditional update whose expected revision
	// no longer matches — a concurrent issuance won. Maps to 409.
	ErrRevisionMismatch = errors.New("rfqissuance: revision mismatch")

	// ErrIssuedVersionNotFound reports a missing version OR one belonging to
	// another tenant, collapsed so a foreign caller learns nothing. Maps to 404.
	ErrIssuedVersionNotFound = errors.New("rfqissuance: issued rfq version not found")

	// ErrVersionAlreadyExists reports that this chain already has a version
	// with this number — a concurrent issuance won the race. Maps to 409.
	//
	// Deliberately distinct from ErrOperationAlreadyUsed: this means "someone
	// else got there first", which is a different instruction to the client
	// than "you already did this".
	ErrVersionAlreadyExists = errors.New("rfqissuance: issued version already exists")

	// ErrOperationAlreadyUsed reports an idempotency key already consumed by
	// another logical issuance. The caller should resolve its prior result
	// rather than retrying blindly. Maps to 409.
	ErrOperationAlreadyUsed = errors.New("rfqissuance: issuance operation id already used")
	// ErrOfferWorkspaceConflict reports that the Supplier's offer workspace
	// could not be claimed for recipient replacement — a submitting draft or a
	// competing replacement already owns it (§5.3A).
	ErrOfferWorkspaceConflict = errors.New(
		"rfqissuance: supplier offer workspace could not be claimed")
	// ErrRecipientReplacementPending reports that the offer-side claim is held
	// but the authoritative invitation replacement was not confirmed. The claim
	// is deliberately NOT released: an infrastructure error does not prove the
	// write failed, and releasing it could restore access to a replaced
	// recipient. The same operation ID retries to converge.
	ErrRecipientReplacementPending = errors.New(
		"rfqissuance: recipient replacement pending confirmation")

	// ErrRFQNotReady reports an M7 RFQ that is a draft, absent, or outside this
	// tenant. The three collapse deliberately: a foreign caller must not learn
	// which of them is true. Maps to 409.
	ErrRFQNotReady = errors.New("rfqissuance: rfq is not ready to issue")

	// ErrCurrencyRequired reports issuance with no currency. The contractor
	// sets one immutable currency at issuance and the platform has no basis to
	// choose it. Maps to 422.
	ErrCurrencyRequired = errors.New("rfqissuance: currency is required to issue")

	// ErrInvalidCurrency reports a non-empty currency outside the Phase 1
	// commercial boundary. RFQ chains are MYR-only for Phase 1; accepting a
	// second currency would imply conversion and comparison rules that are
	// explicitly out of scope. Maps to 422.
	ErrInvalidCurrency = errors.New("rfqissuance: unsupported currency")

	// ErrOperationIDRequired reports issuance with no idempotency key. Without
	// one, a retry after an uncertain response is indistinguishable from a
	// deliberate second issuance. Maps to 422.
	ErrOperationIDRequired = errors.New("rfqissuance: issuance operation id is required")

	// ErrIssuanceNotConfigured reports a Service constructed without the
	// collaborators issuance needs. This is a composition-root wiring fault,
	// never a client error. Maps to 503.
	ErrIssuanceNotConfigured = errors.New("rfqissuance: issuance capabilities are not configured")

	// ErrAmendmentDraftAlreadyExists reports a second draft on one chain. Only
	// one may exist, so two contractors cannot prepare divergent versions of
	// the same next version. Maps to 409.
	ErrAmendmentDraftAlreadyExists = errors.New("rfqissuance: an amendment draft already exists")

	// ErrAmendmentDraftNotFound reports a missing draft OR one belonging to
	// another tenant. Maps to 404.
	ErrAmendmentDraftNotFound = errors.New("rfqissuance: amendment draft not found")

	// ErrStaleBaseVersion reports a draft whose base is no longer the chain's
	// current version — another amendment was issued while this one was being
	// prepared, so these edits may contradict it (§4.2). Maps to 409.
	ErrStaleBaseVersion = errors.New("rfqissuance: amendment draft base version is stale")

	// --- Invitations (§5) ---

	// ErrInvalidRecipientEmail reports a missing, malformed or over-long
	// recipient address. The normalised form is the Supplier identity key, so a
	// value that cannot be normalised has no usable identity. Maps to 422.
	ErrInvalidRecipientEmail = errors.New("rfqissuance: invalid recipient email")

	// ErrRecipientNameRequired reports a blank recipient name. Maps to 422.
	ErrRecipientNameRequired = errors.New("rfqissuance: recipient name is required")

	// ErrInvitationExpiryNotInFuture reports an expiry at or before now, which
	// would create an invitation that never opens. Maps to 422.
	ErrInvitationExpiryNotInFuture = errors.New("rfqissuance: invitation expiry must be in the future")

	// ErrInvitationAlreadyExists reports a second invitation for one
	// Company + RFQChain + Supplier. The invitation is STABLE: the existing one
	// advances rather than being replaced. Maps to 409.
	ErrInvitationAlreadyExists = errors.New("rfqissuance: an invitation already exists for this supplier")

	// ErrAccessSecretHashCollision reports that two stable invitations resolved
	// to the same global public credential. With 256-bit HMAC-derived tokens,
	// this is a fail-closed configuration or derivation defect, never a normal
	// duplicate-invitation conflict and never safe to retry with changed input.
	// Maps to 500.
	ErrAccessSecretHashCollision = errors.New("rfqissuance: invitation access-secret hash collision")

	// ErrInvitationNotFound reports a missing invitation OR one belonging to
	// another tenant, collapsed so a foreign caller learns nothing. Maps to 404.
	ErrInvitationNotFound = errors.New("rfqissuance: invitation not found")

	// ErrInvalidInvitationID reports a caller-supplied invitation ID that is not
	// a valid identifier. This is a programming fault rather than a client
	// error: the service mints the ID itself. Maps to 500.
	ErrInvalidInvitationID = errors.New("rfqissuance: invalid invitation id")

	// ErrSupplierNotInvitable reports a Supplier that is absent, belongs to
	// another company, or is archived. The three collapse deliberately: a
	// caller probing supplier IDs must not learn which applies. Maps to 422.
	ErrSupplierNotInvitable = errors.New("rfqissuance: supplier cannot be invited")

	// ErrInvitationRevoked reports an action on a revoked invitation.
	// Revocation is terminal: resending never reactivates it (§5.4). Maps to 409.
	ErrInvitationRevoked = errors.New("rfqissuance: invitation is revoked")

	// ErrInvitationAlreadyActive prevents reactivation from acting as an
	// accidental rotate-secret operation. Maps to 409.
	ErrInvitationAlreadyActive = errors.New("rfqissuance: invitation is already active")

	// ErrInvitationNotReactivatable reports a draft or other unexpired state
	// that is neither explicitly revoked nor effectively expired. Maps to 409.
	ErrInvitationNotReactivatable = errors.New("rfqissuance: invitation cannot be reactivated")

	// --- Delivery (§3.5, §1A.2) ---

	// ErrDeliveryOperationAlreadyUsed reports an idempotency key already
	// consumed by another logical send. Maps to 409.
	ErrDeliveryOperationAlreadyUsed = errors.New("rfqissuance: delivery operation id already used")

	// ErrDeliveryAttemptNotFound reports a missing attempt or one outside this
	// tenant. Maps to 404.
	ErrDeliveryAttemptNotFound = errors.New("rfqissuance: delivery attempt not found")

	// ErrMailDeliveryFailed reports that the mail send failed AFTER the
	// delivery intent was persisted. The invitation and the failed attempt both
	// survive, so the contractor can retry (§1A.2). Maps to 503.
	ErrMailDeliveryFailed = errors.New("rfqissuance: invitation email could not be delivered")

	// ErrInvitationsNotConfigured reports a Service constructed without the
	// invitation collaborators. A composition-root wiring fault. Maps to 503.
	ErrInvitationsNotConfigured = errors.New("rfqissuance: invitation capabilities are not configured")

	// ErrInvitationSecretUnavailable reports that the current link cannot be
	// re-derived — its key version is no longer configured, or the stored hash
	// was written under different key material.
	//
	// It FAILS CLOSED rather than minting a fresh link: returning a token that
	// cannot verify would hand the contractor a link they would send and only
	// discover was broken through the Supplier (§6.1A). Maps to 503.
	ErrInvitationSecretUnavailable = errors.New("rfqissuance: invitation secret cannot be derived")

	// ErrSupplierRFQAccessInvalid deliberately covers a missing, foreign,
	// expired, revoked, or stale-generation Supplier boundary. Callers map all
	// of those cases to the same non-disclosing 404 response.
	ErrSupplierRFQAccessInvalid = errors.New("rfqissuance: supplier rfq access invalid")
)
