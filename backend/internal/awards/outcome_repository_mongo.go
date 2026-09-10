package awards

import (
	"context"
	"errors"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/shananth/renovation-platform/backend/internal/foundation/money"
	"github.com/shananth/renovation-platform/backend/internal/foundation/quantity"
)

const (
	collectionAwardOutcomes = "award_outcomes"

	indexNameUniqueAwardOutcome     = "uq_award_outcomes_company_revision_supplier"
	indexNameAwardOutcomeBySupplier = "ix_award_outcomes_company_supplier_invitation"
)

type MongoAwardOutcomeRepository struct {
	collection *mongo.Collection
}

func NewMongoAwardOutcomeRepository(
	db *mongo.Database,
) *MongoAwardOutcomeRepository {
	return &MongoAwardOutcomeRepository{
		collection: db.Collection(collectionAwardOutcomes),
	}
}

// EnsureIndexes enforces one outcome per Supplier + Award Revision.
//
// The revision is part of the key rather than the chain: a correction generates
// NEW outcomes and never edits one already sent, so two revisions legitimately
// hold an outcome for the same Supplier.
func (repository *MongoAwardOutcomeRepository) EnsureIndexes(
	ctx context.Context,
) error {
	_, err := repository.collection.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{
			Keys: bson.D{
				{Key: "companyId", Value: 1},
				{Key: "awardRevisionId", Value: 1},
				{Key: "supplierId", Value: 1},
			},
			Options: options.Index().
				SetName(indexNameUniqueAwardOutcome).
				SetUnique(true),
		},
		{
			// Supports the Phase D Supplier read, which resolves by
			// Supplier + Invitation rather than by outcome ID alone (D3).
			Keys: bson.D{
				{Key: "companyId", Value: 1},
				{Key: "supplierId", Value: 1},
				{Key: "invitationId", Value: 1},
			},
			Options: options.Index().SetName(indexNameAwardOutcomeBySupplier),
		},
	})
	return err
}

type outcomeAwardedLineDocument struct {
	IssuedRFQLineID string      `bson:"issuedRFQLineId"`
	MaterialName    string      `bson:"materialName,omitempty"`
	QuantityValue   string      `bson:"quantityValue,omitempty"`
	QuantityUnit    string      `bson:"quantityUnit,omitempty"`
	UnitPrice       money.Money `bson:"unitPrice"`
	LineSubtotal    money.Money `bson:"lineSubtotal"`
	LineTaxAmount   money.Money `bson:"lineTaxAmount"`
	Brand           string      `bson:"brand,omitempty"`
	SKU             string      `bson:"sku,omitempty"`
	LeadTime        string      `bson:"leadTime,omitempty"`
}

type outcomeProjectionDocument struct {
	Result    string `bson:"result"`
	RFQNumber string `bson:"rfqNumber,omitempty"`
	RFQTitle  string `bson:"rfqTitle,omitempty"`

	AwardedLines   []outcomeAwardedLineDocument `bson:"awardedLines,omitempty"`
	LineSubtotal   money.Money                  `bson:"lineSubtotal"`
	TaxTotal       money.Money                  `bson:"taxTotal"`
	ChargeTotal    money.Money                  `bson:"chargeTotal"`
	DeliveryCharge money.Money                  `bson:"deliveryCharge"`
	AwardTotal     money.Money                  `bson:"awardTotal"`

	ContractorMessage string `bson:"contractorMessage,omitempty"`
	NextSteps         string `bson:"nextSteps,omitempty"`
}

type awardOutcomeDocument struct {
	ID                 bson.ObjectID `bson:"_id,omitempty"`
	CompanyID          string        `bson:"companyId"`
	AwardChainID       string        `bson:"awardChainId"`
	AwardRevisionID    string        `bson:"awardRevisionId"`
	RFQChainID         string        `bson:"rfqChainId"`
	IssuedRFQVersionID string        `bson:"issuedRFQVersionId"`
	SupplierID         string        `bson:"supplierId"`
	InvitationID       string        `bson:"invitationId"`
	Result             string        `bson:"result"`

	Projection    outcomeProjectionDocument `bson:"projection"`
	CreatedAt     time.Time                 `bson:"createdAt"`
	SchemaVersion int                       `bson:"schemaVersion"`
}

func outcomeToDocument(outcome AwardOutcome) awardOutcomeDocument {
	lines := make([]outcomeAwardedLineDocument, 0,
		len(outcome.Projection.AwardedLines))
	for _, line := range outcome.Projection.AwardedLines {
		document := outcomeAwardedLineDocument{
			IssuedRFQLineID: line.IssuedRFQLineID,
			MaterialName:    line.MaterialName,
			UnitPrice:       line.UnitPrice,
			LineSubtotal:    line.LineSubtotal,
			LineTaxAmount:   line.LineTaxAmount,
			Brand:           line.Brand,
			SKU:             line.SKU,
			LeadTime:        line.LeadTime,
		}
		// The exact decimal string, never a float: what a Supplier was told
		// must not change because it round-tripped through the database.
		if line.Quantity.Unit != "" {
			document.QuantityValue = line.Quantity.Value.String()
			document.QuantityUnit = line.Quantity.Unit
		}
		lines = append(lines, document)
	}

	return awardOutcomeDocument{
		CompanyID:          outcome.CompanyID,
		AwardChainID:       outcome.AwardChainID,
		AwardRevisionID:    outcome.AwardRevisionID,
		RFQChainID:         outcome.RFQChainID,
		IssuedRFQVersionID: outcome.IssuedRFQVersionID,
		SupplierID:         outcome.SupplierID,
		InvitationID:       outcome.InvitationID,
		Result:             string(outcome.Result),
		Projection: outcomeProjectionDocument{
			Result:            string(outcome.Projection.Result),
			RFQNumber:         outcome.Projection.RFQNumber,
			RFQTitle:          outcome.Projection.RFQTitle,
			AwardedLines:      lines,
			LineSubtotal:      outcome.Projection.LineSubtotal,
			TaxTotal:          outcome.Projection.TaxTotal,
			ChargeTotal:       outcome.Projection.ChargeTotal,
			DeliveryCharge:    outcome.Projection.DeliveryCharge,
			AwardTotal:        outcome.Projection.AwardTotal,
			ContractorMessage: outcome.Projection.ContractorMessage,
			NextSteps:         outcome.Projection.NextSteps,
		},
		CreatedAt:     outcome.CreatedAt,
		SchemaVersion: AwardOutcomeSchemaVersion,
	}
}

func outcomeFromDocument(document awardOutcomeDocument) AwardOutcome {
	lines := make([]OutcomeAwardedLine, 0, len(document.Projection.AwardedLines))
	for _, line := range document.Projection.AwardedLines {
		value := OutcomeAwardedLine{
			IssuedRFQLineID: line.IssuedRFQLineID,
			MaterialName:    line.MaterialName,
			UnitPrice:       line.UnitPrice,
			LineSubtotal:    line.LineSubtotal,
			LineTaxAmount:   line.LineTaxAmount,
			Brand:           line.Brand,
			SKU:             line.SKU,
			LeadTime:        line.LeadTime,
		}
		if line.QuantityValue != "" {
			if parsed, err := quantity.New(
				line.QuantityValue, line.QuantityUnit); err == nil {
				value.Quantity = parsed
			}
		}
		lines = append(lines, value)
	}

	return AwardOutcome{
		ID:                 document.ID.Hex(),
		CompanyID:          document.CompanyID,
		AwardChainID:       document.AwardChainID,
		AwardRevisionID:    document.AwardRevisionID,
		RFQChainID:         document.RFQChainID,
		IssuedRFQVersionID: document.IssuedRFQVersionID,
		SupplierID:         document.SupplierID,
		InvitationID:       document.InvitationID,
		Result:             OutcomeResult(document.Result),
		Projection: OutcomeProjection{
			Result:            OutcomeResult(document.Projection.Result),
			RFQNumber:         document.Projection.RFQNumber,
			RFQTitle:          document.Projection.RFQTitle,
			AwardedLines:      lines,
			LineSubtotal:      document.Projection.LineSubtotal,
			TaxTotal:          document.Projection.TaxTotal,
			ChargeTotal:       document.Projection.ChargeTotal,
			DeliveryCharge:    document.Projection.DeliveryCharge,
			AwardTotal:        document.Projection.AwardTotal,
			ContractorMessage: document.Projection.ContractorMessage,
			NextSteps:         document.Projection.NextSteps,
		},
		CreatedAt:     document.CreatedAt,
		SchemaVersion: document.SchemaVersion,
	}
}

// DeleteAllForCompany permanently removes every AwardOutcome owned by
// companyID. Never errors when zero documents match. Development-tool use
// only.
func (repository *MongoAwardOutcomeRepository) DeleteAllForCompany(ctx context.Context, companyID string) error {
	_, err := repository.collection.DeleteMany(ctx, bson.M{"companyId": companyID})
	return err
}

// EnsureOutcome inserts an outcome, adopting an existing one for the same
// Supplier + revision.
//
// Generation is recoverable post-publication work (F5 step 9), so it must be
// safe to run twice: recovery regenerates MISSING outcomes without duplicating
// existing ones. A Supplier told twice would read it as two awards.
func (repository *MongoAwardOutcomeRepository) EnsureOutcome(
	ctx context.Context,
	candidate AwardOutcome,
) (AwardOutcome, bool, error) {
	if strings.TrimSpace(candidate.CompanyID) == "" ||
		strings.TrimSpace(candidate.AwardRevisionID) == "" ||
		strings.TrimSpace(candidate.SupplierID) == "" ||
		strings.TrimSpace(candidate.InvitationID) == "" {
		return AwardOutcome{}, false, ErrInvalidAwardOutcome
	}
	if candidate.CreatedAt.IsZero() {
		candidate.CreatedAt = time.Now().UTC()
	}

	document := outcomeToDocument(candidate)
	result, err := repository.collection.InsertOne(ctx, document)
	if err == nil {
		document.ID = result.InsertedID.(bson.ObjectID)
		return outcomeFromDocument(document), true, nil
	}
	if !mongo.IsDuplicateKeyError(err) {
		return AwardOutcome{}, false, err
	}

	// A concurrent generator won. Adopt its outcome rather than failing: both
	// callers wanted the same frozen projection for the same revision.
	existing, found, findErr := repository.findOutcomeForRevision(
		ctx, candidate.CompanyID, candidate.AwardRevisionID, candidate.SupplierID)
	if findErr != nil {
		return AwardOutcome{}, false, findErr
	}
	if !found {
		return AwardOutcome{}, false, ErrAwardOutcomeNotFound
	}
	return existing, false, nil
}

func (repository *MongoAwardOutcomeRepository) findOutcomeForRevision(
	ctx context.Context,
	companyID, awardRevisionID, supplierID string,
) (AwardOutcome, bool, error) {
	var document awardOutcomeDocument
	err := repository.collection.FindOne(ctx, bson.M{
		"companyId":       companyID,
		"awardRevisionId": awardRevisionID,
		"supplierId":      supplierID,
	}).Decode(&document)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return AwardOutcome{}, false, nil
	}
	if err != nil {
		return AwardOutcome{}, false, err
	}
	return outcomeFromDocument(document), true, nil
}

// FindOutcome resolves an outcome for the CONTRACTOR, scoped by tenant.
func (repository *MongoAwardOutcomeRepository) FindOutcome(
	ctx context.Context,
	companyID, outcomeID string,
) (AwardOutcome, bool, error) {
	objectID, err := bson.ObjectIDFromHex(outcomeID)
	if err != nil {
		return AwardOutcome{}, false, nil
	}

	var document awardOutcomeDocument
	err = repository.collection.FindOne(ctx, bson.M{
		"_id":       objectID,
		"companyId": companyID,
	}).Decode(&document)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return AwardOutcome{}, false, nil
	}
	if err != nil {
		return AwardOutcome{}, false, err
	}
	return outcomeFromDocument(document), true, nil
}

// FindOutcomeForSupplier resolves an outcome for a SUPPLIER.
//
// The scope is Supplier AND Invitation (D3), both in the filter: another
// Supplier's outcome, or the right Supplier under a different invitation, is
// simply absent — the same non-disclosing not-found Phase D uses throughout.
func (repository *MongoAwardOutcomeRepository) FindOutcomeForSupplier(
	ctx context.Context,
	companyID, outcomeID, supplierID, invitationID string,
) (AwardOutcome, bool, error) {
	objectID, err := bson.ObjectIDFromHex(outcomeID)
	if err != nil {
		return AwardOutcome{}, false, nil
	}

	var document awardOutcomeDocument
	err = repository.collection.FindOne(ctx, bson.M{
		"_id":          objectID,
		"companyId":    companyID,
		"supplierId":   supplierID,
		"invitationId": invitationID,
	}).Decode(&document)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return AwardOutcome{}, false, nil
	}
	if err != nil {
		return AwardOutcome{}, false, err
	}
	return outcomeFromDocument(document), true, nil
}

// ListOutcomes returns every outcome generated for one revision.
func (repository *MongoAwardOutcomeRepository) ListOutcomes(
	ctx context.Context,
	companyID, awardRevisionID string,
) ([]AwardOutcome, error) {
	cursor, err := repository.collection.Find(ctx,
		bson.M{"companyId": companyID, "awardRevisionId": awardRevisionID},
		options.Find().SetSort(bson.D{{Key: "supplierId", Value: 1}}),
	)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	outcomes := []AwardOutcome{}
	for cursor.Next(ctx) {
		var document awardOutcomeDocument
		if err := cursor.Decode(&document); err != nil {
			return nil, err
		}
		outcomes = append(outcomes, outcomeFromDocument(document))
	}
	return outcomes, cursor.Err()
}
