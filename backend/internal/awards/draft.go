package awards

import (
	"strings"
	"time"
	"unicode/utf8"
)

// Provisional award drafts and their decision chain (§8C).
//
// A provisional selection claims nothing: it does not touch offer eligibility
// and therefore does not block withdrawal. A Supplier may withdraw an offer the
// contractor has provisionally selected; finalisation then fails and the
// contractor re-decides. That is deliberate — reserving a Supplier's offer
// because someone opened a draft would let internal deliberation freeze an
// external party's commercial position.

const (
	AwardDecisionChainSchemaVersion = 1
	AwardDraftSchemaVersion         = 1

	MaxUnawardedNoteLength = 500
)

// AwardDecision is the per-line discriminator. There is no third state: a line
// is bought from exactly one Supplier or deliberately not bought.
type AwardDecision string

const (
	AwardDecisionSelected  AwardDecision = "selected"
	AwardDecisionUnawarded AwardDecision = "unawarded"
)

// UnawardedReason is bounded so "we deliberately did not buy this" stays
// auditable later — retender_required in particular must be findable.
type UnawardedReason string

const (
	UnawardedNoAcceptableOffer UnawardedReason = "no_acceptable_offer"
	UnawardedPurchaseDeferred  UnawardedReason = "purchase_deferred"
	UnawardedScopeCancelled    UnawardedReason = "scope_cancelled"
	UnawardedRetenderRequired  UnawardedReason = "retender_required"
	UnawardedOther             UnawardedReason = "other"
)

// ValidateUnawardedReason enforces the bounded set and the note rule.
//
// `other` requires text, and every other reason forbids it. Allowing both would
// let a caller smuggle an explanation past the bounded set, making the reason
// field unreliable for exactly the auditing it exists to support.
func ValidateUnawardedReason(reason UnawardedReason, note string) error {
	trimmed := strings.TrimSpace(note)

	switch reason {
	case UnawardedNoAcceptableOffer, UnawardedPurchaseDeferred,
		UnawardedScopeCancelled, UnawardedRetenderRequired:
		if trimmed != "" {
			return ErrInvalidUnawardedReason
		}
		return nil
	case UnawardedOther:
		if trimmed == "" ||
			utf8.RuneCountInString(trimmed) > MaxUnawardedNoteLength {
			return ErrInvalidUnawardedReason
		}
		return nil
	default:
		return ErrInvalidUnawardedReason
	}
}

// FinalisationState is the Award chain's durable publication state.
//
// It is required by D2: because the chain pointer may lag an authoritative
// revision insert, a durable state is the only thing preventing a second
// operation from reading a stale pointer and publishing a duplicate revision
// number.
type FinalisationState string

const (
	FinalisationDraft      FinalisationState = "draft"
	FinalisationFinalising FinalisationState = "finalising"
	FinalisationPublished  FinalisationState = "published"
)

type AwardDecisionChain struct {
	ID                     string
	CompanyID              string
	RFQChainID             string
	IssuedRFQVersionID     string
	CurrentAwardRevisionID *string
	LatestRevisionNumber   int
	FinalisationState      FinalisationState
	FinalisingOperationID  string
	FinalisingRevisionID   string
	Revision               int64
	CreatedAt              time.Time
	UpdatedAt              time.Time
	SchemaVersion          int
}

// AllowsDraftMutation reports whether the chain permits a draft edit. Mutation
// is refused the moment finalisation begins, so a contractor cannot change the
// decisions an in-flight publication is already calculating from.
func (chain AwardDecisionChain) AllowsDraftMutation() bool {
	return chain.FinalisationState == FinalisationDraft
}

// Validate enforces state-shape invariants independently of repository CAS
// filters, so malformed claim data can never become recoverable state.
func (chain AwardDecisionChain) Validate() error {
	if strings.TrimSpace(chain.ID) == "" ||
		strings.TrimSpace(chain.CompanyID) == "" ||
		strings.TrimSpace(chain.RFQChainID) == "" ||
		strings.TrimSpace(chain.IssuedRFQVersionID) == "" ||
		chain.Revision < 1 {
		return ErrInvalidAwardChain
	}

	switch chain.FinalisationState {
	case FinalisationDraft:
		if chain.FinalisingOperationID != "" || chain.FinalisingRevisionID != "" {
			return ErrInvalidAwardChain
		}
	case FinalisationFinalising:
		// Without both, a retry cannot tell its own in-flight publication apart
		// from another operation's, and D2's adopt-or-insert resolution breaks.
		if strings.TrimSpace(chain.FinalisingOperationID) == "" ||
			strings.TrimSpace(chain.FinalisingRevisionID) == "" {
			return ErrInvalidAwardChain
		}
	case FinalisationPublished:
		// A published state with no pointer would make "the current award"
		// unresolvable, which is the one thing a published chain must answer.
		if chain.CurrentAwardRevisionID == nil ||
			strings.TrimSpace(*chain.CurrentAwardRevisionID) == "" ||
			chain.LatestRevisionNumber < 1 {
			return ErrInvalidAwardChain
		}
	default:
		return ErrInvalidAwardChain
	}
	return nil
}

type AwardDraftStatus string

const (
	AwardDraftOpen     AwardDraftStatus = "open"
	AwardDraftArchived AwardDraftStatus = "archived"
)

type AwardLineDecisionDraft struct {
	IssuedRFQLineID string
	StableLineageID string
	Decision        AwardDecision
	OfferVersionID  string
	OfferLineID     string
	UnawardedReason UnawardedReason
	UnawardedNote   string
}

// Validate enforces the discriminated shape. The two decisions carry disjoint
// fields so a revision can never be ambiguous about whether a line was bought.
func (decision AwardLineDecisionDraft) Validate() error {
	if strings.TrimSpace(decision.IssuedRFQLineID) == "" ||
		strings.TrimSpace(decision.StableLineageID) == "" {
		return ErrInvalidAwardDecision
	}

	switch decision.Decision {
	case AwardDecisionSelected:
		// Without both references F3 has nothing authoritative to calculate
		// from, and the award would name a Supplier with no quoted figure.
		if strings.TrimSpace(decision.OfferVersionID) == "" ||
			strings.TrimSpace(decision.OfferLineID) == "" {
			return ErrInvalidAwardDecision
		}
		if decision.UnawardedReason != "" || decision.UnawardedNote != "" {
			return ErrInvalidAwardDecision
		}
		return nil
	case AwardDecisionUnawarded:
		// A line the contractor declined to buy has no Supplier; recording one
		// would make the revision ambiguous.
		if decision.OfferVersionID != "" || decision.OfferLineID != "" {
			return ErrInvalidAwardDecision
		}
		return ValidateUnawardedReason(decision.UnawardedReason, decision.UnawardedNote)
	default:
		return ErrInvalidAwardDecision
	}
}

type AwardDraft struct {
	ID                 string
	CompanyID          string
	AwardChainID       string
	IssuedRFQVersionID string
	Status             AwardDraftStatus
	LineDecisions      []AwardLineDecisionDraft
	Revision           int64
	CreatedByUserID    string
	CreatedAt          time.Time
	UpdatedAt          time.Time
	SchemaVersion      int
}

// AllowsMutation reports whether this draft may still be edited. Only an open
// draft mutates; an archived one is the frozen input to a published revision.
func (draft AwardDraft) AllowsMutation() bool {
	return draft.Status == AwardDraftOpen
}

// Validate checks draft shape WITHOUT requiring completeness. An incomplete
// draft is the normal state while a contractor works through the lines
// incrementally (§8C); completeness belongs to ValidateComplete.
func (draft AwardDraft) Validate() error {
	if strings.TrimSpace(draft.ID) == "" ||
		strings.TrimSpace(draft.CompanyID) == "" ||
		strings.TrimSpace(draft.AwardChainID) == "" ||
		strings.TrimSpace(draft.IssuedRFQVersionID) == "" ||
		draft.Revision < 1 {
		return ErrInvalidAwardDraft
	}
	switch draft.Status {
	case AwardDraftOpen, AwardDraftArchived:
	default:
		return ErrInvalidAwardDraft
	}

	seen := make(map[string]bool, len(draft.LineDecisions))
	for _, decision := range draft.LineDecisions {
		if err := decision.Validate(); err != nil {
			return err
		}
		// A duplicate would let one lineage be both awarded and unawarded, and
		// a single lineage claim cannot resolve that contradiction.
		if seen[decision.IssuedRFQLineID] {
			return ErrInvalidAwardDraft
		}
		seen[decision.IssuedRFQLineID] = true
	}
	return nil
}

// ValidateComplete is the finalisation-time rule: every issued RFQ line appears
// exactly once, and no decision names a line the issued version does not have.
func (draft AwardDraft) ValidateComplete(issuedLines []IssuedRFQLineSnapshot) error {
	if err := draft.Validate(); err != nil {
		return err
	}

	decided := make(map[string]bool, len(draft.LineDecisions))
	for _, decision := range draft.LineDecisions {
		decided[decision.IssuedRFQLineID] = true
	}

	issued := make(map[string]bool, len(issuedLines))
	for _, line := range issuedLines {
		issued[line.ID] = true
		if !decided[line.ID] {
			return ErrAwardDraftIncomplete
		}
	}
	for _, decision := range draft.LineDecisions {
		// Awarding a line the Supplier was never asked to quote would produce
		// a commitment with no authoritative source.
		if !issued[decision.IssuedRFQLineID] {
			return ErrAwardDraftIncomplete
		}
	}
	return nil
}
