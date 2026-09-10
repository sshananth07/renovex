package supplieroffers

import (
	"context"
	"errors"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/shananth/renovation-platform/backend/internal/foundation/money"
	"github.com/shananth/renovation-platform/backend/internal/foundation/procurementlimits"
)

const (
	collectionSupplierOfferVersions = "supplier_offer_versions"

	indexNameUniqueSupplierOfferVersionNumber = "uq_supplier_offer_versions_company_chain_number"
	indexNameUniqueSupplierOfferSubmission    = "uq_supplier_offer_versions_company_submission_operation"

	indexNameSupplierOfferVersionsByIssuedVersion = "ix_supplier_offer_versions_company_issued_version"
)

// MongoOfferVersionRepository only inserts immutable versions. It exposes no
// generic update method, keeping later recovery paths additive and verifiable.
type MongoOfferVersionRepository struct {
	collection *mongo.Collection
}

func NewMongoOfferVersionRepository(db *mongo.Database) *MongoOfferVersionRepository {
	return &MongoOfferVersionRepository{
		collection: db.Collection(collectionSupplierOfferVersions),
	}
}

func (repository *MongoOfferVersionRepository) EnsureIndexes(ctx context.Context) error {
	_, err := repository.collection.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{
			Keys: bson.D{
				{Key: "companyId", Value: 1},
				{Key: "offerChainId", Value: 1},
				{Key: "versionNumber", Value: 1},
			},
			Options: options.Index().
				SetName(indexNameUniqueSupplierOfferVersionNumber).
				SetUnique(true),
		},
		{
			Keys: bson.D{
				{Key: "companyId", Value: 1},
				{Key: "submissionOperationId", Value: 1},
			},
			// Operation IDs are idempotency identities within a tenant. A
			// reused operation may recover its exact result, never create an
			// unrelated immutable commercial record.
			Options: options.Index().
				SetName(indexNameUniqueSupplierOfferSubmission).
				SetUnique(true),
		},
		{
			// Supports the M8 award-integration read: every immutable version
			// answering one issued RFQ version, in a deterministic order.
			Keys: bson.D{
				{Key: "companyId", Value: 1},
				{Key: "issuedRFQVersionId", Value: 1},
				{Key: "offerChainId", Value: 1},
				{Key: "versionNumber", Value: 1},
			},
			Options: options.Index().
				SetName(indexNameSupplierOfferVersionsByIssuedVersion),
		},
	})
	return err
}

// DeleteAllForCompany permanently removes every SupplierOfferVersion owned by
// companyID. Never errors when zero documents match. Development-tool use
// only.
func (repository *MongoOfferVersionRepository) DeleteAllForCompany(ctx context.Context, companyID string) error {
	_, err := repository.collection.DeleteMany(ctx, bson.M{"companyId": companyID})
	return err
}

// ListVersionsForIssuedRFQVersion returns every immutable Offer Version
// answering one issued RFQ version, for the M8 award-integration surface.
//
// It returns FACTS, not policy. Phase F decides which version is the latest
// eligible one, how historical versions are labelled, who participates in
// outcomes, and how withdrawn, expired and superseded versions are treated.
// This module must not calculate comparison or outcome semantics (§8B, §8H).
//
// Ordering is chain then version number, so a comparison projection renders
// identically on every read rather than depending on storage order.
func (repository *MongoOfferVersionRepository) ListVersionsForIssuedRFQVersion(
	ctx context.Context,
	companyID string,
	issuedRFQVersionID string,
) ([]SupplierOfferVersion, error) {
	cursor, err := repository.collection.Find(ctx,
		bson.M{
			"companyId":          companyID,
			"issuedRFQVersionId": issuedRFQVersionID,
		},
		options.Find().SetSort(bson.D{
			{Key: "offerChainId", Value: 1},
			{Key: "versionNumber", Value: 1},
		}),
	)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	versions := []SupplierOfferVersion{}
	for cursor.Next(ctx) {
		var document supplierOfferVersionDocument
		if err := cursor.Decode(&document); err != nil {
			return nil, err
		}
		// A version that cannot be decoded is an error, never a silent
		// omission: dropping it would hide a Supplier's quote from comparison.
		version, err := supplierOfferVersionFromDocument(document)
		if err != nil {
			return nil, err
		}
		versions = append(versions, version)
	}
	return versions, cursor.Err()
}

// ListVersionsForChain returns one Supplier's immutable history newest first.
// Company and chain are both in the database predicate; callers never filter a
// cross-tenant result in memory.
func (repository *MongoOfferVersionRepository) ListVersionsForChain(
	ctx context.Context,
	companyID string,
	offerChainID string,
) ([]SupplierOfferVersion, error) {
	cursor, err := repository.collection.Find(ctx, bson.M{
		"companyId": companyID, "offerChainId": offerChainID,
	}, options.Find().SetSort(bson.D{{Key: "versionNumber", Value: -1}}))
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	versions := make([]SupplierOfferVersion, 0)
	for cursor.Next(ctx) {
		var document supplierOfferVersionDocument
		if err := cursor.Decode(&document); err != nil {
			return nil, err
		}
		version, err := supplierOfferVersionFromDocument(document)
		if err != nil {
			return nil, err
		}
		versions = append(versions, version)
	}
	return versions, cursor.Err()
}

type supplierOfferVersionDocument struct {
	ID                    string                      `bson:"_id"`
	CompanyID             string                      `bson:"companyId"`
	OfferChainID          string                      `bson:"offerChainId"`
	InvitationID          string                      `bson:"invitationId"`
	IssuedRFQVersionID    string                      `bson:"issuedRFQVersionId"`
	VersionNumber         int                         `bson:"versionNumber"`
	Currency              string                      `bson:"currency"`
	RecipientIdentity     string                      `bson:"recipientIdentity"`
	SourceDraftID         string                      `bson:"sourceDraftId"`
	SourceDraftRevision   int64                       `bson:"sourceDraftRevision"`
	SubmissionOperationID string                      `bson:"submissionOperationId"`
	SubmissionFingerprint string                      `bson:"submissionFingerprint"`
	Lines                 []supplierOfferLineDocument `bson:"lines"`
	Tax                   SupplierOfferTax            `bson:"tax"`
	ChargeGroups          []ConditionalChargeGroup    `bson:"chargeGroups"`
	DeliveryCharge        *DeliveryCharge             `bson:"deliveryCharge,omitempty"`
	QuotedLineSubtotal    money.Money                 `bson:"quotedLineSubtotal"`
	QuotedTaxTotal        money.Money                 `bson:"quotedTaxTotal"`
	FullOfferChargeTotal  money.Money                 `bson:"fullOfferChargeTotal"`
	DeliveryChargeTotal   money.Money                 `bson:"deliveryChargeTotal"`
	GrandTotal            money.Money                 `bson:"grandTotal"`
	OfferValidUntil       time.Time                   `bson:"offerValidUntil"`
	SupplierNotes         string                      `bson:"supplierNotes,omitempty"`
	SubmittedAt           time.Time                   `bson:"submittedAt"`
	SchemaVersion         int                         `bson:"schemaVersion"`
}

type supplierOfferLineDocument struct {
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
	LineTaxAmount            money.Money                    `bson:"lineTaxAmount"`
	CopiedFromOfferVersionID string                         `bson:"copiedFromOfferVersionId,omitempty"`
	CopiedFromOfferLineID    string                         `bson:"copiedFromOfferLineId,omitempty"`
	CopiedAt                 *time.Time                     `bson:"copiedAt,omitempty"`
}

func supplierOfferLinesToDocuments(
	lines []SupplierOfferLine,
) []supplierOfferLineDocument {
	documents := make([]supplierOfferLineDocument, 0, len(lines))
	for _, line := range lines {
		document := supplierOfferLineDocument{
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
			LineTaxAmount:            line.LineTaxAmount,
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

func supplierOfferLinesFromDocuments(
	documents []supplierOfferLineDocument,
) ([]SupplierOfferLine, error) {
	lines := make([]SupplierOfferLine, 0, len(documents))
	for _, document := range documents {
		line := SupplierOfferLine{
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
			LineTaxAmount:            document.LineTaxAmount,
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

func supplierOfferVersionToDocument(
	version SupplierOfferVersion,
) supplierOfferVersionDocument {
	return supplierOfferVersionDocument{
		ID:                    version.ID,
		CompanyID:             version.CompanyID,
		OfferChainID:          version.OfferChainID,
		InvitationID:          version.InvitationID,
		IssuedRFQVersionID:    version.IssuedRFQVersionID,
		VersionNumber:         version.VersionNumber,
		Currency:              version.Currency,
		RecipientIdentity:     version.RecipientIdentity,
		SourceDraftID:         version.SourceDraftID,
		SourceDraftRevision:   version.SourceDraftRevision,
		SubmissionOperationID: version.SubmissionOperationID,
		SubmissionFingerprint: version.SubmissionFingerprint,
		Lines:                 supplierOfferLinesToDocuments(version.Lines),
		Tax:                   version.Tax,
		ChargeGroups:          version.ChargeGroups,
		DeliveryCharge:        version.DeliveryCharge,
		QuotedLineSubtotal:    version.QuotedLineSubtotal,
		QuotedTaxTotal:        version.QuotedTaxTotal,
		FullOfferChargeTotal:  version.FullOfferChargeTotal,
		DeliveryChargeTotal:   version.DeliveryChargeTotal,
		GrandTotal:            version.GrandTotal,
		OfferValidUntil:       version.OfferValidUntil,
		SupplierNotes:         version.SupplierNotes,
		SubmittedAt:           version.SubmittedAt,
		SchemaVersion:         version.SchemaVersion,
	}
}

func supplierOfferVersionFromDocument(
	document supplierOfferVersionDocument,
) (SupplierOfferVersion, error) {
	lines, err := supplierOfferLinesFromDocuments(document.Lines)
	if err != nil {
		return SupplierOfferVersion{}, err
	}
	return SupplierOfferVersion{
		ID:                    document.ID,
		CompanyID:             document.CompanyID,
		OfferChainID:          document.OfferChainID,
		InvitationID:          document.InvitationID,
		IssuedRFQVersionID:    document.IssuedRFQVersionID,
		VersionNumber:         document.VersionNumber,
		Currency:              document.Currency,
		RecipientIdentity:     document.RecipientIdentity,
		SourceDraftID:         document.SourceDraftID,
		SourceDraftRevision:   document.SourceDraftRevision,
		SubmissionOperationID: document.SubmissionOperationID,
		SubmissionFingerprint: document.SubmissionFingerprint,
		Lines:                 lines,
		Tax:                   document.Tax,
		ChargeGroups:          document.ChargeGroups,
		DeliveryCharge:        document.DeliveryCharge,
		QuotedLineSubtotal:    document.QuotedLineSubtotal,
		QuotedTaxTotal:        document.QuotedTaxTotal,
		FullOfferChargeTotal:  document.FullOfferChargeTotal,
		DeliveryChargeTotal:   document.DeliveryChargeTotal,
		GrandTotal:            document.GrandTotal,
		OfferValidUntil:       document.OfferValidUntil,
		SupplierNotes:         document.SupplierNotes,
		SubmittedAt:           document.SubmittedAt,
		SchemaVersion:         document.SchemaVersion,
	}, nil
}

func (repository *MongoOfferVersionRepository) InsertVersion(
	ctx context.Context,
	version SupplierOfferVersion,
) error {
	if err := validateOfferVersionPersistence(version); err != nil {
		return err
	}
	// The candidate version ID is MongoDB's _id. Any collision is therefore a
	// global duplicate-key failure and is never silently reassigned.
	_, err := repository.collection.InsertOne(
		ctx,
		supplierOfferVersionToDocument(version),
	)
	return err
}

func validateOfferVersionPersistence(version SupplierOfferVersion) error {
	if procurementlimits.ValidateCount(len(version.Lines),
		procurementlimits.MaxLines) != nil ||
		procurementlimits.ValidateCount(len(version.ChargeGroups),
			procurementlimits.MaxChargeGroups) != nil {
		return ErrInputLimitExceeded
	}
	return nil
}

// FindVersion is tenant-scoped even though the candidate ID is globally
// unique. Public callers must not be able to use an ID as a cross-company
// existence oracle.
func (repository *MongoOfferVersionRepository) FindVersion(
	ctx context.Context,
	companyID string,
	versionID string,
) (SupplierOfferVersion, bool, error) {
	var document supplierOfferVersionDocument
	err := repository.collection.FindOne(ctx, bson.M{
		"_id":       versionID,
		"companyId": companyID,
	}).Decode(&document)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return SupplierOfferVersion{}, false, nil
	}
	if err != nil {
		return SupplierOfferVersion{}, false, err
	}
	version, err := supplierOfferVersionFromDocument(document)
	if err != nil {
		return SupplierOfferVersion{}, false, err
	}
	return version, true, nil
}
