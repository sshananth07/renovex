package supplieroffers

import (
	"context"
	"errors"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/shananth/renovation-platform/backend/internal/foundation/money"
)

const (
	collectionSupplierOfferDrafts = "supplier_offer_drafts"

	indexNameUniqueUnfinishedSupplierOfferDraft = "uq_supplier_offer_drafts_company_chain_unfinished"
	indexNameSupplierOfferDraftReconciliation   = "ix_supplier_offer_drafts_company_chain_submission_operation"
)

// unfinishedDraftStatuses is the single definition of "occupies the slot",
// shared by the partial unique index and every unfinished-draft query. Keeping
// one source prevents a query and the index from drifting apart, which would
// silently reopen the recipient-replacement race the barrier exists to close.
func unfinishedDraftStatuses() bson.A {
	return bson.A{DraftActive, DraftSubmitting, DraftRecipientReplacementClaimed}
}

// MongoOfferDraftRepository persists mutable drafts while the partial unique
// index enforces the single unfinished aggregate for each Offer Chain.
type MongoOfferDraftRepository struct {
	collection *mongo.Collection
}

func NewMongoOfferDraftRepository(db *mongo.Database) *MongoOfferDraftRepository {
	return &MongoOfferDraftRepository{
		collection: db.Collection(collectionSupplierOfferDrafts),
	}
}

func (repository *MongoOfferDraftRepository) EnsureIndexes(ctx context.Context) error {
	_, err := repository.collection.Indexes().CreateMany(ctx, []mongo.IndexModel{{
		Keys: bson.D{
			{Key: "companyId", Value: 1},
			{Key: "offerChainId", Value: 1},
		},
		Options: options.Index().
			SetName(indexNameUniqueUnfinishedSupplierOfferDraft).
			SetUnique(true).
			// A submission claim is frozen but unfinished. Keeping it in this
			// filter prevents a new active draft from bypassing recovery.
			//
			// A recipient-replacement claim is likewise unfinished: it is the
			// durable barrier (§5.3A) that must keep holding this slot until
			// the authoritative invitation replacement is confirmed. Because
			// replacement and ordinary draft creation contend on THIS index,
			// MongoDB picks one winner atomically — no check-then-insert race.
			SetPartialFilterExpression(bson.M{
				"status": bson.M{
					"$in": unfinishedDraftStatuses(),
				},
			}),
	}, {Keys: bson.D{
		{Key: "companyId", Value: 1}, {Key: "offerChainId", Value: 1},
		{Key: "submissionOperationId", Value: 1},
	}, Options: options.Index().SetName(indexNameSupplierOfferDraftReconciliation)}})
	return err
}

type supplierOfferDraftDocument struct {
	ID                           string                           `bson:"_id"`
	CompanyID                    string                           `bson:"companyId"`
	OfferChainID                 string                           `bson:"offerChainId"`
	InvitationID                 string                           `bson:"invitationId"`
	IssuedRFQVersionID           string                           `bson:"issuedRFQVersionId"`
	RecipientIdentity            string                           `bson:"recipientIdentity"`
	SourceOfferVersionID         *string                          `bson:"sourceOfferVersionId,omitempty"`
	Currency                     string                           `bson:"currency"`
	Status                       DraftStatus                      `bson:"status"`
	Lines                        []supplierOfferDraftLineDocument `bson:"lines"`
	Tax                          SupplierOfferTax                 `bson:"tax"`
	OfferTaxReviewRequired       bool                             `bson:"offerTaxReviewRequired"`
	ChargeGroups                 []SupplierChargeGroupDraft       `bson:"chargeGroups"`
	DeliveryCharge               *DeliveryCharge                  `bson:"deliveryCharge,omitempty"`
	DeliveryChargeReviewRequired bool                             `bson:"deliveryChargeReviewRequired"`
	OfferValidUntil              *time.Time                       `bson:"offerValidUntil,omitempty"`
	SupplierNotes                string                           `bson:"supplierNotes,omitempty"`
	SubmissionOperationID        string                           `bson:"submissionOperationId,omitempty"`
	SubmissionBaseRevision       int64                            `bson:"submissionBaseRevision,omitempty"`
	SubmissionFingerprint        string                           `bson:"submissionFingerprint,omitempty"`
	SubmissionRecipientIdentity  string                           `bson:"submissionRecipientIdentity,omitempty"`
	SubmissionInvitationID       string                           `bson:"submissionInvitationId,omitempty"`
	SubmissionRFQVersionID       string                           `bson:"submissionRFQVersionId,omitempty"`
	SubmissionAccessGeneration   int64                            `bson:"submissionAccessGeneration,omitempty"`
	CandidateOfferVersionID      string                           `bson:"candidateOfferVersionId,omitempty"`
	CandidateVersionNumber       int                              `bson:"candidateVersionNumber,omitempty"`
	SubmissionClaimedAt          *time.Time                       `bson:"submissionClaimedAt,omitempty"`

	// Recipient-replacement claim and archival bookkeeping (§5.3A). Recovery
	// reads these back to prove which operation owns a claim and whether the
	// authoritative invitation replacement still needs completing.
	DraftPurpose                 DraftPurpose        `bson:"draftPurpose,omitempty"`
	ReplacementOperationID       string              `bson:"replacementOperationId,omitempty"`
	PreviousRecipientIdentity    string              `bson:"previousRecipientIdentity,omitempty"`
	CandidateRecipientIdentity   string              `bson:"candidateRecipientIdentity,omitempty"`
	ClaimedAt                    *time.Time          `bson:"claimedAt,omitempty"`
	ArchivedReason               DraftArchivedReason `bson:"archivedReason,omitempty"`
	ArchivedByOperationID        string              `bson:"archivedByOperationId,omitempty"`
	ReplacementRecipientIdentity string              `bson:"replacementRecipientIdentity,omitempty"`

	Revision      int64      `bson:"revision"`
	CreatedAt     time.Time  `bson:"createdAt"`
	UpdatedAt     time.Time  `bson:"updatedAt"`
	ArchivedAt    *time.Time `bson:"archivedAt,omitempty"`
	SchemaVersion int        `bson:"schemaVersion"`
}

type supplierOfferDraftLineDocument struct {
	ID                       string                         `bson:"id"`
	RFQLineID                string                         `bson:"rfqLineId"`
	ResponseStatus           OfferLineResponseStatus        `bson:"responseStatus"`
	QuotedQuantity           *supplierOfferQuantityDocument `bson:"quotedQuantity,omitempty"`
	UnitPriceExcludingTax    *money.Money                   `bson:"unitPriceExcludingTax,omitempty"`
	LineSubtotalExcludingTax *money.Money                   `bson:"lineSubtotalExcludingTax,omitempty"`
	Brand                    string                         `bson:"brand,omitempty"`
	SKU                      string                         `bson:"sku,omitempty"`
	ProductDescription       string                         `bson:"productDescription,omitempty"`
	LeadTime                 string                         `bson:"leadTime,omitempty"`
	SupplierLineNotes        string                         `bson:"supplierLineNotes,omitempty"`
	CommercialExceptions     string                         `bson:"commercialExceptions,omitempty"`
	LineTax                  *QuotedLineTax                 `bson:"lineTax,omitempty"`
	ReviewRequired           bool                           `bson:"reviewRequired"`
	ConfirmationRequired     bool                           `bson:"confirmationRequired"`
	CopiedFromOfferVersionID string                         `bson:"copiedFromOfferVersionId,omitempty"`
	CopiedFromOfferLineID    string                         `bson:"copiedFromOfferLineId,omitempty"`
	CopiedAt                 *time.Time                     `bson:"copiedAt,omitempty"`
}

func supplierOfferDraftLinesToDocuments(
	lines []SupplierOfferDraftLine,
) []supplierOfferDraftLineDocument {
	documents := make([]supplierOfferDraftLineDocument, 0, len(lines))
	for _, line := range lines {
		document := supplierOfferDraftLineDocument{
			ID:                       line.ID,
			RFQLineID:                line.RFQLineID,
			ResponseStatus:           line.ResponseStatus,
			UnitPriceExcludingTax:    line.UnitPriceExcludingTax,
			LineSubtotalExcludingTax: line.LineSubtotalExcludingTax,
			Brand:                    line.Brand,
			SKU:                      line.SKU,
			ProductDescription:       line.ProductDescription,
			LeadTime:                 line.LeadTime,
			SupplierLineNotes:        line.SupplierLineNotes,
			CommercialExceptions:     line.CommercialExceptions,
			LineTax:                  line.LineTax,
			ReviewRequired:           line.ReviewRequired,
			ConfirmationRequired:     line.ConfirmationRequired,
			CopiedFromOfferVersionID: line.CopiedFromOfferVersionID,
			CopiedFromOfferLineID:    line.CopiedFromOfferLineID,
			CopiedAt:                 line.CopiedAt,
		}
		if line.QuotedQuantity != nil {
			quantityDocument := supplierOfferQuantityToDocument(*line.QuotedQuantity)
			document.QuotedQuantity = &quantityDocument
		}
		documents = append(documents, document)
	}
	return documents
}

func supplierOfferDraftLinesFromDocuments(
	documents []supplierOfferDraftLineDocument,
) ([]SupplierOfferDraftLine, error) {
	lines := make([]SupplierOfferDraftLine, 0, len(documents))
	for _, document := range documents {
		line := SupplierOfferDraftLine{
			ID:                       document.ID,
			RFQLineID:                document.RFQLineID,
			ResponseStatus:           document.ResponseStatus,
			UnitPriceExcludingTax:    document.UnitPriceExcludingTax,
			LineSubtotalExcludingTax: document.LineSubtotalExcludingTax,
			Brand:                    document.Brand,
			SKU:                      document.SKU,
			ProductDescription:       document.ProductDescription,
			LeadTime:                 document.LeadTime,
			SupplierLineNotes:        document.SupplierLineNotes,
			CommercialExceptions:     document.CommercialExceptions,
			LineTax:                  document.LineTax,
			ReviewRequired:           document.ReviewRequired,
			ConfirmationRequired:     document.ConfirmationRequired,
			CopiedFromOfferVersionID: document.CopiedFromOfferVersionID,
			CopiedFromOfferLineID:    document.CopiedFromOfferLineID,
			CopiedAt:                 document.CopiedAt,
		}
		if document.QuotedQuantity != nil {
			quotedQuantity, err := supplierOfferQuantityFromDocument(*document.QuotedQuantity)
			if err != nil {
				return nil, err
			}
			line.QuotedQuantity = &quotedQuantity
		}
		lines = append(lines, line)
	}
	return lines, nil
}

func supplierOfferDraftToDocument(draft SupplierOfferDraft) supplierOfferDraftDocument {
	return supplierOfferDraftDocument{
		ID:                           draft.ID,
		CompanyID:                    draft.CompanyID,
		OfferChainID:                 draft.OfferChainID,
		InvitationID:                 draft.InvitationID,
		IssuedRFQVersionID:           draft.IssuedRFQVersionID,
		RecipientIdentity:            draft.RecipientIdentity,
		SourceOfferVersionID:         draft.SourceOfferVersionID,
		Currency:                     draft.Currency,
		Status:                       draft.Status,
		Lines:                        supplierOfferDraftLinesToDocuments(draft.Lines),
		Tax:                          draft.Tax,
		OfferTaxReviewRequired:       draft.OfferTaxReviewRequired,
		ChargeGroups:                 draft.ChargeGroups,
		DeliveryCharge:               draft.DeliveryCharge,
		DeliveryChargeReviewRequired: draft.DeliveryChargeReviewRequired,
		OfferValidUntil:              draft.OfferValidUntil,
		SupplierNotes:                draft.SupplierNotes,
		SubmissionOperationID:        draft.SubmissionOperationID,
		SubmissionBaseRevision:       draft.SubmissionBaseRevision,
		SubmissionFingerprint:        draft.SubmissionFingerprint,
		SubmissionRecipientIdentity:  draft.SubmissionRecipientIdentity,
		SubmissionInvitationID:       draft.SubmissionInvitationID,
		SubmissionRFQVersionID:       draft.SubmissionRFQVersionID,
		SubmissionAccessGeneration:   draft.SubmissionAccessGeneration,
		CandidateOfferVersionID:      draft.CandidateOfferVersionID,
		CandidateVersionNumber:       draft.CandidateVersionNumber,
		SubmissionClaimedAt:          draft.SubmissionClaimedAt,
		DraftPurpose:                 draft.DraftPurpose,
		ReplacementOperationID:       draft.ReplacementOperationID,
		PreviousRecipientIdentity:    draft.PreviousRecipientIdentity,
		CandidateRecipientIdentity:   draft.CandidateRecipientIdentity,
		ClaimedAt:                    draft.ClaimedAt,
		ArchivedReason:               draft.ArchivedReason,
		ArchivedByOperationID:        draft.ArchivedByOperationID,
		ReplacementRecipientIdentity: draft.ReplacementRecipientIdentity,
		Revision:                     draft.Revision,
		CreatedAt:                    draft.CreatedAt,
		UpdatedAt:                    draft.UpdatedAt,
		ArchivedAt:                   draft.ArchivedAt,
		SchemaVersion:                draft.SchemaVersion,
	}
}

func supplierOfferDraftFromDocument(
	document supplierOfferDraftDocument,
) (SupplierOfferDraft, error) {
	lines, err := supplierOfferDraftLinesFromDocuments(document.Lines)
	if err != nil {
		return SupplierOfferDraft{}, err
	}
	return SupplierOfferDraft{
		ID:                           document.ID,
		CompanyID:                    document.CompanyID,
		OfferChainID:                 document.OfferChainID,
		InvitationID:                 document.InvitationID,
		IssuedRFQVersionID:           document.IssuedRFQVersionID,
		RecipientIdentity:            document.RecipientIdentity,
		SourceOfferVersionID:         document.SourceOfferVersionID,
		Currency:                     document.Currency,
		Status:                       document.Status,
		Lines:                        lines,
		Tax:                          document.Tax,
		OfferTaxReviewRequired:       document.OfferTaxReviewRequired,
		ChargeGroups:                 document.ChargeGroups,
		DeliveryCharge:               document.DeliveryCharge,
		DeliveryChargeReviewRequired: document.DeliveryChargeReviewRequired,
		OfferValidUntil:              document.OfferValidUntil,
		SupplierNotes:                document.SupplierNotes,
		SubmissionOperationID:        document.SubmissionOperationID,
		SubmissionBaseRevision:       document.SubmissionBaseRevision,
		SubmissionFingerprint:        document.SubmissionFingerprint,
		SubmissionRecipientIdentity:  document.SubmissionRecipientIdentity,
		SubmissionInvitationID:       document.SubmissionInvitationID,
		SubmissionRFQVersionID:       document.SubmissionRFQVersionID,
		SubmissionAccessGeneration:   document.SubmissionAccessGeneration,
		CandidateOfferVersionID:      document.CandidateOfferVersionID,
		CandidateVersionNumber:       document.CandidateVersionNumber,
		SubmissionClaimedAt:          document.SubmissionClaimedAt,
		DraftPurpose:                 document.DraftPurpose,
		ReplacementOperationID:       document.ReplacementOperationID,
		PreviousRecipientIdentity:    document.PreviousRecipientIdentity,
		CandidateRecipientIdentity:   document.CandidateRecipientIdentity,
		ClaimedAt:                    document.ClaimedAt,
		ArchivedReason:               document.ArchivedReason,
		ArchivedByOperationID:        document.ArchivedByOperationID,
		ReplacementRecipientIdentity: document.ReplacementRecipientIdentity,
		Revision:                     document.Revision,
		CreatedAt:                    document.CreatedAt,
		UpdatedAt:                    document.UpdatedAt,
		ArchivedAt:                   document.ArchivedAt,
		SchemaVersion:                document.SchemaVersion,
	}, nil
}

func (repository *MongoOfferDraftRepository) InsertDraft(
	ctx context.Context,
	draft SupplierOfferDraft,
) error {
	_, err := repository.collection.InsertOne(ctx, supplierOfferDraftToDocument(draft))
	return err
}

// FindDraft always includes CompanyID in the query. Returning found=false for
// both missing and foreign documents preserves the tenant-safe not-found
// boundary expected by the Supplier HTTP layer.
func (repository *MongoOfferDraftRepository) FindDraft(
	ctx context.Context,
	companyID string,
	draftID string,
) (SupplierOfferDraft, bool, error) {
	var document supplierOfferDraftDocument
	err := repository.collection.FindOne(ctx, bson.M{
		"_id":       draftID,
		"companyId": companyID,
	}).Decode(&document)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return SupplierOfferDraft{}, false, nil
	}
	if err != nil {
		return SupplierOfferDraft{}, false, err
	}
	draft, err := supplierOfferDraftFromDocument(document)
	if err != nil {
		return SupplierOfferDraft{}, false, err
	}
	return draft, true, nil
}

// FindSubmissionByOperation is the privileged recovery locator. Both tenant
// and chain remain in Mongo's predicate; reconciliation never scans another
// Supplier's workspace and filters it in memory.
func (repository *MongoOfferDraftRepository) FindSubmissionByOperation(
	ctx context.Context, companyID, offerChainID, operationID string,
) (SupplierOfferDraft, bool, error) {
	var document supplierOfferDraftDocument
	err := repository.collection.FindOne(ctx, bson.M{
		"companyId": companyID, "offerChainId": offerChainID,
		"submissionOperationId": operationID,
	}).Decode(&document)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return SupplierOfferDraft{}, false, nil
	}
	if err != nil {
		return SupplierOfferDraft{}, false, err
	}
	draft, err := supplierOfferDraftFromDocument(document)
	return draft, err == nil, err
}

func (repository *MongoOfferDraftRepository) FindLiveSubmissionForChain(
	ctx context.Context, companyID, offerChainID string,
) (SupplierOfferDraft, bool, error) {
	var document supplierOfferDraftDocument
	err := repository.collection.FindOne(ctx, bson.M{
		"companyId": companyID, "offerChainId": offerChainID,
		"status": DraftSubmitting,
	}).Decode(&document)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return SupplierOfferDraft{}, false, nil
	}
	if err != nil {
		return SupplierOfferDraft{}, false, err
	}
	draft, err := supplierOfferDraftFromDocument(document)
	return draft, err == nil, err
}

func (repository *MongoOfferDraftRepository) findUnfinishedDraft(
	ctx context.Context,
	companyID string,
	offerChainID string,
) (SupplierOfferDraft, bool, error) {
	var document supplierOfferDraftDocument
	err := repository.collection.FindOne(ctx, bson.M{
		"companyId":    companyID,
		"offerChainId": offerChainID,
		"status": bson.M{
			"$in": unfinishedDraftStatuses(),
		},
	}).Decode(&document)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return SupplierOfferDraft{}, false, nil
	}
	if err != nil {
		return SupplierOfferDraft{}, false, err
	}
	draft, err := supplierOfferDraftFromDocument(document)
	if err != nil {
		return SupplierOfferDraft{}, false, err
	}
	return draft, true, nil
}

// DeleteAllForCompany permanently removes every SupplierOfferDraft owned by
// companyID. Never errors when zero documents match. Development-tool use
// only.
func (repository *MongoOfferDraftRepository) DeleteAllForCompany(ctx context.Context, companyID string) error {
	_, err := repository.collection.DeleteMany(ctx, bson.M{"companyId": companyID})
	return err
}

// EnsureUnfinishedDraft atomically creates the first active draft or returns
// the existing active/submitting aggregate. Every caller reads back MongoDB's
// winner, so a losing candidate ID can never escape into audit or later writes.
func (repository *MongoOfferDraftRepository) EnsureUnfinishedDraft(
	ctx context.Context,
	candidate SupplierOfferDraft,
) (SupplierOfferDraft, bool, error) {
	result, err := repository.collection.UpdateOne(
		ctx,
		bson.M{
			"companyId":    candidate.CompanyID,
			"offerChainId": candidate.OfferChainID,
			"status": bson.M{
				"$in": unfinishedDraftStatuses(),
			},
		},
		bson.M{
			"$setOnInsert": supplierOfferDraftToDocument(candidate),
		},
		options.UpdateOne().SetUpsert(true),
	)
	if err != nil && !mongo.IsDuplicateKeyError(err) {
		return SupplierOfferDraft{}, false, err
	}

	// A duplicate key is the expected loser path when concurrent upserts both
	// initially observe no match. The unique index has already selected the
	// winner, so recovery is a read—not a retry with altered identity.
	persisted, found, findErr := repository.findUnfinishedDraft(
		ctx,
		candidate.CompanyID,
		candidate.OfferChainID,
	)
	if findErr != nil {
		return SupplierOfferDraft{}, false, findErr
	}
	if !found {
		return SupplierOfferDraft{}, false, ErrOfferDraftNotFound
	}
	return persisted, err == nil && result.UpsertedCount == 1, nil
}

// RecipientReplacementClaimInput carries the exact identities a replacement
// claim must record. Both recipient identities are required so recovery can
// prove which replacement owns the claim before completing or aborting it.
type RecipientReplacementClaimInput struct {
	CompanyID                  string
	OfferChainID               string
	InvitationID               string
	IssuedRFQVersionID         string
	PreviousRecipientIdentity  string
	CandidateRecipientIdentity string
	ReplacementOperationID     string
	// BarrierDraftID is used only when no draft exists and a barrier-only row
	// must be created. An existing draft keeps its own identity.
	BarrierDraftID string
	ClaimedAt      time.Time
}

// ClaimRecipientReplacement acquires the durable replacement barrier (§5.3A).
//
// This is step one of claim -> authoritative invitation replacement -> archival.
// The claim keeps occupying the unfinished-draft uniqueness slot, so the
// previous recipient cannot edit, submit or open a new draft during the
// cross-collection window, and a crash leaves the slot held rather than freed.
//
// Two paths, one uniqueness rule:
//
//   - an existing active draft transitions active -> claimed, competing
//     directly with active -> submitting so exactly one of submission and
//     replacement wins;
//   - with no draft, a barrier-only row is INSERTED under the same named unique
//     index ordinary draft creation contends on. That is what makes the
//     "no draft" case a guarded race rather than an unprotected no-op.
func (repository *MongoOfferDraftRepository) ClaimRecipientReplacement(
	ctx context.Context,
	input RecipientReplacementClaimInput,
) (SupplierOfferDraft, error) {
	claimFields := bson.M{
		"status":                     DraftRecipientReplacementClaimed,
		"replacementOperationId":     input.ReplacementOperationID,
		"previousRecipientIdentity":  input.PreviousRecipientIdentity,
		"candidateRecipientIdentity": input.CandidateRecipientIdentity,
		"claimedAt":                  input.ClaimedAt,
		"updatedAt":                  input.ClaimedAt,
	}

	// Path 1: transition an existing active draft. Requiring the exact previous
	// recipient stops an operation working from a stale view of who the
	// recipient is from clobbering another replacement.
	var document supplierOfferDraftDocument
	err := repository.collection.FindOneAndUpdate(
		ctx,
		bson.M{
			"companyId":         input.CompanyID,
			"offerChainId":      input.OfferChainID,
			"status":            DraftActive,
			"recipientIdentity": input.PreviousRecipientIdentity,
		},
		bson.M{
			"$set": claimFields,
			"$inc": bson.M{"revision": 1},
		},
		options.FindOneAndUpdate().SetReturnDocument(options.After),
	).Decode(&document)
	if err == nil {
		return supplierOfferDraftFromDocument(document)
	}
	if !errors.Is(err, mongo.ErrNoDocuments) {
		return SupplierOfferDraft{}, err
	}

	// Path 2: no active draft matched. Insert the barrier under the shared
	// unique index. A concurrent draft creation either loses to this insert or
	// wins and makes it fail with a duplicate key — never both.
	barrier := SupplierOfferDraft{
		ID:                         input.BarrierDraftID,
		CompanyID:                  input.CompanyID,
		OfferChainID:               input.OfferChainID,
		InvitationID:               input.InvitationID,
		IssuedRFQVersionID:         input.IssuedRFQVersionID,
		RecipientIdentity:          input.PreviousRecipientIdentity,
		Status:                     DraftRecipientReplacementClaimed,
		DraftPurpose:               DraftPurposeRecipientReplacementBarrier,
		ReplacementOperationID:     input.ReplacementOperationID,
		PreviousRecipientIdentity:  input.PreviousRecipientIdentity,
		CandidateRecipientIdentity: input.CandidateRecipientIdentity,
		ClaimedAt:                  &input.ClaimedAt,
		Revision:                   1,
		CreatedAt:                  input.ClaimedAt,
		UpdatedAt:                  input.ClaimedAt,
		SchemaVersion:              SupplierOfferDraftSchemaVersion,
	}
	if validationErr := barrier.ValidateBarrierInvariants(); validationErr != nil {
		return SupplierOfferDraft{}, validationErr
	}

	_, insertErr := repository.collection.InsertOne(
		ctx, supplierOfferDraftToDocument(barrier))
	if insertErr == nil {
		return barrier, nil
	}
	if !mongo.IsDuplicateKeyError(insertErr) {
		return SupplierOfferDraft{}, insertErr
	}

	// Someone else holds the slot. Resume only OUR own claim; a concurrently
	// created commercial draft is never silently converted into this claim,
	// because that would let both concurrent operations report success.
	existing, found, findErr := repository.findUnfinishedDraft(
		ctx, input.CompanyID, input.OfferChainID)
	if findErr != nil {
		return SupplierOfferDraft{}, findErr
	}
	if !found {
		return SupplierOfferDraft{}, ErrOfferDraftConflict
	}
	if existing.Status == DraftRecipientReplacementClaimed &&
		existing.ReplacementOperationID == input.ReplacementOperationID &&
		existing.PreviousRecipientIdentity == input.PreviousRecipientIdentity &&
		existing.CandidateRecipientIdentity == input.CandidateRecipientIdentity {
		return existing, nil
	}
	return SupplierOfferDraft{}, ErrOfferDraftConflict
}

// DraftSubmissionClaimInput freezes an active draft as a submission claim.
//
// Every field below is frozen at claim time so completion and recovery build
// the immutable version from claimed content only, never from a draft that
// might have been edited afterwards.
type DraftSubmissionClaimInput struct {
	CompanyID             string
	DraftID               string
	ExpectedRevision      int64
	SubmissionOperationID string
	// SubmissionBaseRevision is the revision whose CONTENT was fingerprinted,
	// not the incremented revision this claim creates.
	SubmissionBaseRevision      int64
	SubmissionFingerprint       string
	SubmissionRecipientIdentity string
	SubmissionInvitationID      string
	SubmissionRFQVersionID      string
	SubmissionAccessGeneration  int64
	// Candidate identity is reserved permanently for this operation. Retries
	// and reconciliation never allocate a new ID or number.
	CandidateOfferVersionID string
	CandidateVersionNumber  int
	ClaimedAt               time.Time
}

// ClaimDraftForSubmission performs active -> submitting.
//
// This CAS competes directly with the recipient-replacement claim on the same
// document (§5.3A): both require status=active, so exactly one wins and
// whichever wins determines the outcome. A submission that wins first is never
// invalidated or erased by a later replacement; a replacement that wins first
// makes submission and editing fail closed.
//
// E7 extends this with candidate-version reservation and recovery. The claim
// transition itself lives here because E4 owns the active-draft lifecycle.
func (repository *MongoOfferDraftRepository) ClaimDraftForSubmission(
	ctx context.Context,
	input DraftSubmissionClaimInput,
) (SupplierOfferDraft, error) {
	var document supplierOfferDraftDocument
	err := repository.collection.FindOneAndUpdate(
		ctx,
		bson.M{
			"_id":       input.DraftID,
			"companyId": input.CompanyID,
			"status":    DraftActive,
			"revision":  input.ExpectedRevision,
		},
		bson.M{
			"$set": bson.M{
				"status":                      DraftSubmitting,
				"submissionOperationId":       input.SubmissionOperationID,
				"submissionBaseRevision":      input.SubmissionBaseRevision,
				"submissionFingerprint":       input.SubmissionFingerprint,
				"submissionRecipientIdentity": input.SubmissionRecipientIdentity,
				"submissionInvitationId":      input.SubmissionInvitationID,
				"submissionRFQVersionId":      input.SubmissionRFQVersionID,
				"submissionAccessGeneration":  input.SubmissionAccessGeneration,
				"candidateOfferVersionId":     input.CandidateOfferVersionID,
				"candidateVersionNumber":      input.CandidateVersionNumber,
				"submissionClaimedAt":         input.ClaimedAt,
				"updatedAt":                   input.ClaimedAt,
				"revision":                    input.ExpectedRevision + 1,
			},
		},
		options.FindOneAndUpdate().SetReturnDocument(options.After),
	).Decode(&document)
	if errors.Is(err, mongo.ErrNoDocuments) {
		_, found, findErr := repository.FindDraft(ctx, input.CompanyID, input.DraftID)
		if findErr != nil {
			return SupplierOfferDraft{}, findErr
		}
		if !found {
			return SupplierOfferDraft{}, ErrOfferDraftNotFound
		}
		return SupplierOfferDraft{}, ErrOfferDraftConflict
	}
	if err != nil {
		return SupplierOfferDraft{}, err
	}
	return supplierOfferDraftFromDocument(document)
}

// ArchiveSubmittedDraft closes the submission lifecycle.
//
// It runs only after the chain points at the immutable version, and the
// operation ID is part of the filter so one submission can never archive
// another's claim. An already-archived draft is an idempotent no-op because
// recovery re-drives this final step.
func (repository *MongoOfferDraftRepository) ArchiveSubmittedDraft(
	ctx context.Context,
	companyID string,
	draftID string,
	submissionOperationID string,
	archivedAt time.Time,
) error {
	result, err := repository.collection.UpdateOne(
		ctx,
		bson.M{
			"_id":                   draftID,
			"companyId":             companyID,
			"status":                DraftSubmitting,
			"submissionOperationId": submissionOperationID,
		},
		bson.M{
			"$set": bson.M{
				"status":     DraftArchived,
				"archivedAt": archivedAt,
				"updatedAt":  archivedAt,
			},
			"$inc": bson.M{"revision": 1},
		},
	)
	if err != nil {
		return err
	}
	if result.MatchedCount == 1 {
		return nil
	}

	// Classify the miss so an already-archived draft converges.
	existing, found, findErr := repository.FindDraft(ctx, companyID, draftID)
	if findErr != nil {
		return findErr
	}
	if !found {
		return ErrOfferDraftNotFound
	}
	if existing.Status == DraftArchived &&
		existing.SubmissionOperationID == submissionOperationID {
		return nil
	}
	return ErrOfferDraftConflict
}

// FindUnfinishedDraftForInvitation resolves the workspace slot from the
// invitation identity rather than an Offer Chain ID, which is what the
// cross-module replacement capability carries.
func (repository *MongoOfferDraftRepository) FindUnfinishedDraftForInvitation(
	ctx context.Context,
	companyID string,
	invitationID string,
	issuedRFQVersionID string,
) (SupplierOfferDraft, bool, error) {
	var document supplierOfferDraftDocument
	err := repository.collection.FindOne(ctx, bson.M{
		"companyId":          companyID,
		"invitationId":       invitationID,
		"issuedRFQVersionId": issuedRFQVersionID,
		"status": bson.M{
			"$in": unfinishedDraftStatuses(),
		},
	}).Decode(&document)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return SupplierOfferDraft{}, false, nil
	}
	if err != nil {
		return SupplierOfferDraft{}, false, err
	}
	draft, err := supplierOfferDraftFromDocument(document)
	if err != nil {
		return SupplierOfferDraft{}, false, err
	}
	return draft, true, nil
}

// RecipientReplacementArchivalInput carries the provenance an archived draft
// preserves so replacement history is auditable.
type RecipientReplacementArchivalInput struct {
	CompanyID                    string
	InvitationID                 string
	IssuedRFQVersionID           string
	PreviousRecipientIdentity    string
	ReplacementRecipientIdentity string
	ReplacementOperationID       string
	ArchivedAt                   time.Time
}

// ArchiveClaimedRecipientReplacement completes the barrier lifecycle:
// recipient_replacement_claimed -> archived (§5.3A).
//
// The operation ID is part of the FILTER, so one replacement can never archive
// another's claim. Archiving releases the uniqueness slot, which is safe only
// here — after the authoritative invitation replacement is confirmed.
func (repository *MongoOfferDraftRepository) ArchiveClaimedRecipientReplacement(
	ctx context.Context,
	input RecipientReplacementArchivalInput,
) (SupplierOfferDraft, error) {
	var document supplierOfferDraftDocument
	err := repository.collection.FindOneAndUpdate(
		ctx,
		bson.M{
			"companyId":                 input.CompanyID,
			"invitationId":              input.InvitationID,
			"issuedRFQVersionId":        input.IssuedRFQVersionID,
			"status":                    DraftRecipientReplacementClaimed,
			"replacementOperationId":    input.ReplacementOperationID,
			"previousRecipientIdentity": input.PreviousRecipientIdentity,
		},
		bson.M{
			"$set": bson.M{
				"status":                       DraftArchived,
				"archivedReason":               DraftArchivedReasonRecipientReplacement,
				"archivedByOperationId":        input.ReplacementOperationID,
				"replacementRecipientIdentity": input.ReplacementRecipientIdentity,
				"archivedAt":                   input.ArchivedAt,
				"updatedAt":                    input.ArchivedAt,
			},
			"$inc": bson.M{"revision": 1},
		},
		options.FindOneAndUpdate().SetReturnDocument(options.After),
	).Decode(&document)
	if err == nil {
		return supplierOfferDraftFromDocument(document)
	}
	if !errors.Is(err, mongo.ErrNoDocuments) {
		return SupplierOfferDraft{}, err
	}
	return SupplierOfferDraft{}, ErrOfferDraftConflict
}

// ReleaseClaimedRecipientReplacement aborts a claim that provably never became
// an authoritative replacement.
//
// A barrier-only row is deleted, since it never represented Supplier work. An
// existing commercial draft returns to active with its content intact. Both are
// gated on the owning operation ID and the claim revision, so one operation can
// never release another's barrier.
func (repository *MongoOfferDraftRepository) ReleaseClaimedRecipientReplacement(
	ctx context.Context,
	input RecipientReplacementAbortInput,
) error {
	filter := bson.M{
		"companyId":                 input.CompanyID,
		"invitationId":              input.InvitationID,
		"issuedRFQVersionId":        input.IssuedRFQVersionID,
		"status":                    DraftRecipientReplacementClaimed,
		"replacementOperationId":    input.ReplacementOperationID,
		"previousRecipientIdentity": input.PreviousRecipientIdentity,
	}

	var document supplierOfferDraftDocument
	if err := repository.collection.FindOne(ctx, filter).Decode(&document); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return ErrOfferDraftConflict
		}
		return err
	}

	// The claim revision is part of the write filter so a concurrent change to
	// the claim invalidates this rollback rather than silently overwriting it.
	filter["revision"] = document.Revision

	if document.DraftPurpose == DraftPurposeRecipientReplacementBarrier {
		result, err := repository.collection.DeleteOne(ctx, filter)
		if err != nil {
			return err
		}
		if result.DeletedCount == 0 {
			return ErrOfferDraftConflict
		}
		return nil
	}

	result, err := repository.collection.UpdateOne(ctx, filter, bson.M{
		"$set": bson.M{
			"status":    DraftActive,
			"updatedAt": input.AbortedAt,
		},
		"$unset": bson.M{
			"replacementOperationId":     "",
			"previousRecipientIdentity":  "",
			"candidateRecipientIdentity": "",
			"claimedAt":                  "",
		},
		"$inc": bson.M{"revision": 1},
	})
	if err != nil {
		return err
	}
	if result.ModifiedCount == 0 {
		return ErrOfferDraftConflict
	}
	return nil
}

// RecipientReplacementAbortInput identifies the exact claim to release.
type RecipientReplacementAbortInput struct {
	CompanyID                 string
	InvitationID              string
	IssuedRFQVersionID        string
	PreviousRecipientIdentity string
	ReplacementOperationID    string
	AbortedAt                 time.Time
}

// ReplaceActiveCommercialState is the common CAS boundary for draft edits.
// Status is part of the database filter, not merely a service pre-check, so a
// concurrent submission claim or recipient-replacement archive always wins
// against a stale edit rather than being overwritten.
func (repository *MongoOfferDraftRepository) ReplaceActiveCommercialState(
	ctx context.Context,
	companyID string,
	draftID string,
	expectedRevision int64,
	state SupplierOfferDraftCommercialState,
	updatedAt time.Time,
) (SupplierOfferDraft, error) {
	opts := options.FindOneAndUpdate().SetReturnDocument(options.After)

	var document supplierOfferDraftDocument
	err := repository.collection.FindOneAndUpdate(
		ctx,
		bson.M{
			"_id":       draftID,
			"companyId": companyID,
			"status":    DraftActive,
			"revision":  expectedRevision,
		},
		bson.M{
			"$set": bson.M{
				"lines":                        supplierOfferDraftLinesToDocuments(state.Lines),
				"tax":                          state.Tax,
				"offerTaxReviewRequired":       state.OfferTaxReviewRequired,
				"chargeGroups":                 state.ChargeGroups,
				"deliveryCharge":               state.DeliveryCharge,
				"deliveryChargeReviewRequired": state.DeliveryChargeReviewRequired,
				"offerValidUntil":              state.OfferValidUntil,
				"supplierNotes":                state.SupplierNotes,
				"sourceOfferVersionId":         state.SourceOfferVersionID,
				"updatedAt":                    updatedAt,
				"revision":                     expectedRevision + 1,
			},
		},
		opts,
	).Decode(&document)
	if errors.Is(err, mongo.ErrNoDocuments) {
		_, found, findErr := repository.FindDraft(ctx, companyID, draftID)
		if findErr != nil {
			return SupplierOfferDraft{}, findErr
		}
		if !found {
			return SupplierOfferDraft{}, ErrOfferDraftNotFound
		}
		return SupplierOfferDraft{}, ErrOfferDraftConflict
	}
	if err != nil {
		return SupplierOfferDraft{}, err
	}
	return supplierOfferDraftFromDocument(document)
}

func supplierOfferCommercialStateFromDraft(
	draft SupplierOfferDraft,
) SupplierOfferDraftCommercialState {
	return SupplierOfferDraftCommercialState{
		Lines:                        draft.Lines,
		Tax:                          draft.Tax,
		OfferTaxReviewRequired:       draft.OfferTaxReviewRequired,
		ChargeGroups:                 draft.ChargeGroups,
		DeliveryCharge:               draft.DeliveryCharge,
		DeliveryChargeReviewRequired: draft.DeliveryChargeReviewRequired,
		OfferValidUntil:              draft.OfferValidUntil,
		SupplierNotes:                draft.SupplierNotes,
		SourceOfferVersionID:         draft.SourceOfferVersionID,
	}
}

// AcknowledgeActiveDraftLine requires the exact copied response type currently
// stored on the line. This prevents an acknowledgement prepared for an old
// quote from silently confirming a concurrently changed decline, or vice versa.
func (repository *MongoOfferDraftRepository) AcknowledgeActiveDraftLine(
	ctx context.Context,
	companyID string,
	draftID string,
	expectedRevision int64,
	draftLineID string,
	expectedResponse OfferLineResponseStatus,
	updatedAt time.Time,
) (SupplierOfferDraft, error) {
	draft, found, err := repository.FindDraft(ctx, companyID, draftID)
	if err != nil {
		return SupplierOfferDraft{}, err
	}
	if !found {
		return SupplierOfferDraft{}, ErrOfferDraftNotFound
	}
	if draft.Status != DraftActive || draft.Revision != expectedRevision {
		return SupplierOfferDraft{}, ErrOfferDraftConflict
	}

	state := supplierOfferCommercialStateFromDraft(draft)
	matched := false
	for lineIndex := range state.Lines {
		line := &state.Lines[lineIndex]
		if line.ID != draftLineID {
			continue
		}
		if line.ResponseStatus != expectedResponse {
			return SupplierOfferDraft{}, ErrOfferDraftConflict
		}

		switch expectedResponse {
		case OfferLineQuoted:
			if !line.ReviewRequired {
				return SupplierOfferDraft{}, ErrOfferDraftConflict
			}
			line.ReviewRequired = false
		case OfferLineNoBid, OfferLineUnavailable:
			if !line.ConfirmationRequired {
				return SupplierOfferDraft{}, ErrOfferDraftConflict
			}
			line.ConfirmationRequired = false
		default:
			return SupplierOfferDraft{}, ErrOfferDraftConflict
		}
		matched = true
		break
	}
	if !matched {
		return SupplierOfferDraft{}, ErrOfferDraftConflict
	}

	// ReplaceActiveCommercialState repeats both the status and revision checks
	// in MongoDB. The preliminary read is never treated as authorization.
	return repository.ReplaceActiveCommercialState(
		ctx,
		companyID,
		draftID,
		expectedRevision,
		state,
		updatedAt,
	)
}
