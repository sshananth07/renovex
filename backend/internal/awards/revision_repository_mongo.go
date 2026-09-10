package awards

import (
	"context"
	"errors"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/shananth/renovation-platform/backend/internal/foundation/money"
	"github.com/shananth/renovation-platform/backend/internal/foundation/quantity"
)

const (
	collectionAwardRevisions = "award_revisions"

	indexNameUniqueAwardRevisionNumber = "uq_award_revisions_company_chain_number"
	indexNameUniqueAwardRevisionOp     = "uq_award_revisions_company_operation"
)

type MongoAwardRevisionRepository struct {
	collection *mongo.Collection
}

func NewMongoAwardRevisionRepository(
	db *mongo.Database,
) *MongoAwardRevisionRepository {
	return &MongoAwardRevisionRepository{
		collection: db.Collection(collectionAwardRevisions),
	}
}

// EnsureIndexes creates the two safeguards of §8F.
//
// The NUMBER index is the final safeguard: even if two operations somehow
// reached insert, only one revision number can exist on a chain. The OPERATION
// index is what makes an unknown-outcome retry resolvable — it lets a caller
// find its own revision instead of allocating a second number.
func (repository *MongoAwardRevisionRepository) EnsureIndexes(
	ctx context.Context,
) error {
	_, err := repository.collection.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{
			Keys: bson.D{
				{Key: "companyId", Value: 1},
				{Key: "awardChainId", Value: 1},
				{Key: "revisionNumber", Value: 1},
			},
			Options: options.Index().
				SetName(indexNameUniqueAwardRevisionNumber).
				SetUnique(true),
		},
		{
			Keys: bson.D{
				{Key: "companyId", Value: 1},
				{Key: "finalisationOperationId", Value: 1},
			},
			Options: options.Index().
				SetName(indexNameUniqueAwardRevisionOp).
				SetUnique(true),
		},
	})
	return err
}

type awardedLineDocument struct {
	IssuedRFQLineID string `bson:"issuedRFQLineId"`
	StableLineageID string `bson:"stableLineageId"`
	MaterialName    string `bson:"materialName,omitempty"`
	QuantityValue   string `bson:"quantityValue,omitempty"`
	QuantityUnit    string `bson:"quantityUnit,omitempty"`

	SupplierID     string `bson:"supplierId"`
	SupplierName   string `bson:"supplierName,omitempty"`
	InvitationID   string `bson:"invitationId"`
	OfferVersionID string `bson:"offerVersionId"`
	OfferLineID    string `bson:"offerLineId"`

	UnitPriceExcludingTax money.Money `bson:"unitPriceExcludingTax"`
	LineSubtotal          money.Money `bson:"lineSubtotal"`
	LineTaxAmount         money.Money `bson:"lineTaxAmount"`
	Brand                 string      `bson:"brand,omitempty"`
	SKU                   string      `bson:"sku,omitempty"`
	ProductDescription    string      `bson:"productDescription,omitempty"`
	LeadTime              string      `bson:"leadTime,omitempty"`
}

type unawardedLineDocument struct {
	IssuedRFQLineID string `bson:"issuedRFQLineId"`
	StableLineageID string `bson:"stableLineageId"`
	MaterialName    string `bson:"materialName,omitempty"`
	Reason          string `bson:"reason"`
	Note            string `bson:"note,omitempty"`
}

type appliedChargeGroupDocument struct {
	GroupID       string      `bson:"groupId"`
	Name          string      `bson:"name,omitempty"`
	Triggered     bool        `bson:"triggered"`
	GroupSubtotal money.Money `bson:"groupSubtotal"`
	Amount        money.Money `bson:"amount"`
}

type supplierSummaryDocument struct {
	SupplierID     string `bson:"supplierId"`
	SupplierName   string `bson:"supplierName,omitempty"`
	InvitationID   string `bson:"invitationId"`
	OfferVersionID string `bson:"offerVersionId"`
	OfferChainID   string `bson:"offerChainId,omitempty"`

	AwardedLineIDs []string                     `bson:"awardedLineIds"`
	LineSubtotal   money.Money                  `bson:"lineSubtotal"`
	TaxTotal       money.Money                  `bson:"taxTotal"`
	ChargeGroups   []appliedChargeGroupDocument `bson:"chargeGroups,omitempty"`
	ChargeTotal    money.Money                  `bson:"chargeTotal"`
	DeliveryCharge money.Money                  `bson:"deliveryCharge"`
	SupplierTotal  money.Money                  `bson:"supplierTotal"`
}

type awardRevisionDocument struct {
	ID                      bson.ObjectID `bson:"_id,omitempty"`
	CompanyID               string        `bson:"companyId"`
	AwardChainID            string        `bson:"awardChainId"`
	RFQChainID              string        `bson:"rfqChainId"`
	IssuedRFQVersionID      string        `bson:"issuedRFQVersionId"`
	RevisionNumber          int           `bson:"revisionNumber"`
	FinalisationOperationID string        `bson:"finalisationOperationId"`
	SelectionFingerprint    string        `bson:"selectionFingerprint"`
	ChangeReason            string        `bson:"changeReason,omitempty"`

	AwardedLines      []awardedLineDocument     `bson:"awardedLines"`
	UnawardedLines    []unawardedLineDocument   `bson:"unawardedLines,omitempty"`
	SupplierSummaries []supplierSummaryDocument `bson:"supplierSummaries"`
	GrandAwardTotal   money.Money               `bson:"grandAwardTotal"`

	SupersedesRevisionID *string   `bson:"supersedesRevisionId,omitempty"`
	FinalisedByUserID    string    `bson:"finalisedByUserId"`
	FinalisedAt          time.Time `bson:"finalisedAt"`
	SchemaVersion        int       `bson:"schemaVersion"`
}

func awardRevisionToDocument(revision AwardRevision) awardRevisionDocument {
	awarded := make([]awardedLineDocument, 0, len(revision.AwardedLines))
	for _, line := range revision.AwardedLines {
		document := awardedLineDocument{
			IssuedRFQLineID:       line.IssuedRFQLineID,
			StableLineageID:       line.StableLineageID,
			MaterialName:          line.MaterialName,
			SupplierID:            line.SupplierID,
			SupplierName:          line.SupplierName,
			InvitationID:          line.InvitationID,
			OfferVersionID:        line.OfferVersionID,
			OfferLineID:           line.OfferLineID,
			UnitPriceExcludingTax: line.UnitPriceExcludingTax,
			LineSubtotal:          line.LineSubtotal,
			LineTaxAmount:         line.LineTaxAmount,
			Brand:                 line.Brand,
			SKU:                   line.SKU,
			ProductDescription:    line.ProductDescription,
			LeadTime:              line.LeadTime,
		}
		// Quantity persists as its exact decimal string: a float round-trip
		// would silently alter an authoritative quantity (ADR 0001).
		if !line.Quantity.Value.IsZero() || line.Quantity.Unit != "" {
			document.QuantityValue = line.Quantity.Value.String()
			document.QuantityUnit = line.Quantity.Unit
		}
		awarded = append(awarded, document)
	}

	unawarded := make([]unawardedLineDocument, 0, len(revision.UnawardedLines))
	for _, line := range revision.UnawardedLines {
		unawarded = append(unawarded, unawardedLineDocument{
			IssuedRFQLineID: line.IssuedRFQLineID,
			StableLineageID: line.StableLineageID,
			MaterialName:    line.MaterialName,
			Reason:          string(line.Reason),
			Note:            line.Note,
		})
	}

	summaries := make([]supplierSummaryDocument, 0, len(revision.SupplierSummaries))
	for _, summary := range revision.SupplierSummaries {
		groups := make([]appliedChargeGroupDocument, 0, len(summary.ChargeGroups))
		for _, group := range summary.ChargeGroups {
			groups = append(groups, appliedChargeGroupDocument{
				GroupID:       group.GroupID,
				Name:          group.Name,
				Triggered:     group.Triggered,
				GroupSubtotal: group.GroupSubtotal,
				Amount:        group.Amount,
			})
		}
		summaries = append(summaries, supplierSummaryDocument{
			SupplierID:     summary.SupplierID,
			SupplierName:   summary.SupplierName,
			InvitationID:   summary.InvitationID,
			OfferVersionID: summary.OfferVersionID,
			OfferChainID:   summary.OfferChainID,
			AwardedLineIDs: summary.AwardedLineIDs,
			LineSubtotal:   summary.LineSubtotal,
			TaxTotal:       summary.TaxTotal,
			ChargeGroups:   groups,
			ChargeTotal:    summary.ChargeTotal,
			DeliveryCharge: summary.DeliveryCharge,
			SupplierTotal:  summary.SupplierTotal,
		})
	}

	return awardRevisionDocument{
		CompanyID:               revision.CompanyID,
		AwardChainID:            revision.AwardChainID,
		RFQChainID:              revision.RFQChainID,
		IssuedRFQVersionID:      revision.IssuedRFQVersionID,
		RevisionNumber:          revision.RevisionNumber,
		FinalisationOperationID: revision.FinalisationOperationID,
		SelectionFingerprint:    revision.SelectionFingerprint,
		ChangeReason:            revision.ChangeReason,
		AwardedLines:            awarded,
		UnawardedLines:          unawarded,
		SupplierSummaries:       summaries,
		GrandAwardTotal:         revision.GrandAwardTotal,
		SupersedesRevisionID:    revision.SupersedesRevisionID,
		FinalisedByUserID:       revision.FinalisedByUserID,
		FinalisedAt:             revision.FinalisedAt,
		SchemaVersion:           AwardRevisionSchemaVersion,
	}
}

func awardRevisionFromDocument(document awardRevisionDocument) AwardRevision {
	awarded := make([]AwardedLine, 0, len(document.AwardedLines))
	for _, line := range document.AwardedLines {
		value := AwardedLine{
			IssuedRFQLineID:       line.IssuedRFQLineID,
			StableLineageID:       line.StableLineageID,
			MaterialName:          line.MaterialName,
			SupplierID:            line.SupplierID,
			SupplierName:          line.SupplierName,
			InvitationID:          line.InvitationID,
			OfferVersionID:        line.OfferVersionID,
			OfferLineID:           line.OfferLineID,
			UnitPriceExcludingTax: line.UnitPriceExcludingTax,
			LineSubtotal:          line.LineSubtotal,
			LineTaxAmount:         line.LineTaxAmount,
			Brand:                 line.Brand,
			SKU:                   line.SKU,
			ProductDescription:    line.ProductDescription,
			LeadTime:              line.LeadTime,
		}
		if line.QuantityValue != "" {
			if parsed, err := quantity.New(
				line.QuantityValue, line.QuantityUnit); err == nil {
				value.Quantity = parsed
			}
		}
		awarded = append(awarded, value)
	}

	unawarded := make([]UnawardedLine, 0, len(document.UnawardedLines))
	for _, line := range document.UnawardedLines {
		unawarded = append(unawarded, UnawardedLine{
			IssuedRFQLineID: line.IssuedRFQLineID,
			StableLineageID: line.StableLineageID,
			MaterialName:    line.MaterialName,
			Reason:          UnawardedReason(line.Reason),
			Note:            line.Note,
		})
	}

	summaries := make([]AwardSupplierSummary, 0, len(document.SupplierSummaries))
	for _, summary := range document.SupplierSummaries {
		groups := make([]AppliedChargeGroup, 0, len(summary.ChargeGroups))
		for _, group := range summary.ChargeGroups {
			groups = append(groups, AppliedChargeGroup{
				GroupID:       group.GroupID,
				Name:          group.Name,
				Triggered:     group.Triggered,
				GroupSubtotal: group.GroupSubtotal,
				Amount:        group.Amount,
			})
		}
		summaries = append(summaries, AwardSupplierSummary{
			SupplierID:     summary.SupplierID,
			SupplierName:   summary.SupplierName,
			InvitationID:   summary.InvitationID,
			OfferVersionID: summary.OfferVersionID,
			OfferChainID:   summary.OfferChainID,
			AwardedLineIDs: summary.AwardedLineIDs,
			LineSubtotal:   summary.LineSubtotal,
			TaxTotal:       summary.TaxTotal,
			ChargeGroups:   groups,
			ChargeTotal:    summary.ChargeTotal,
			DeliveryCharge: summary.DeliveryCharge,
			SupplierTotal:  summary.SupplierTotal,
		})
	}

	return AwardRevision{
		ID:                      document.ID.Hex(),
		CompanyID:               document.CompanyID,
		AwardChainID:            document.AwardChainID,
		RFQChainID:              document.RFQChainID,
		IssuedRFQVersionID:      document.IssuedRFQVersionID,
		RevisionNumber:          document.RevisionNumber,
		FinalisationOperationID: document.FinalisationOperationID,
		SelectionFingerprint:    document.SelectionFingerprint,
		ChangeReason:            document.ChangeReason,
		AwardedLines:            awarded,
		UnawardedLines:          unawarded,
		SupplierSummaries:       summaries,
		GrandAwardTotal:         document.GrandAwardTotal,
		SupersedesRevisionID:    document.SupersedesRevisionID,
		FinalisedByUserID:       document.FinalisedByUserID,
		FinalisedAt:             document.FinalisedAt,
		SchemaVersion:           document.SchemaVersion,
	}
}

// DeleteAllForCompany permanently removes every AwardRevision owned by
// companyID. Never errors when zero documents match. Development-tool use
// only.
func (repository *MongoAwardRevisionRepository) DeleteAllForCompany(ctx context.Context, companyID string) error {
	_, err := repository.collection.DeleteMany(ctx, bson.M{"companyId": companyID})
	return err
}

// InsertRevision is the AUTHORITATIVE PUBLICATION POINT (D2).
//
// On a duplicate key it resolves the collision rather than failing blindly: a
// timeout does not prove the insert failed, so a same-operation retry carrying
// the same fingerprint ADOPTS the existing revision. It never allocates a new
// number and never generates a different revision.
//
// A retry whose fingerprint differs is a DIFFERENT award and conflicts: the
// published record of what was decided, and possibly already communicated to a
// Supplier, must never be silently overwritten.
func (repository *MongoAwardRevisionRepository) InsertRevision(
	ctx context.Context,
	candidate AwardRevision,
) (AwardRevision, error) {
	if err := candidate.Validate(); err != nil {
		return AwardRevision{}, err
	}

	document := awardRevisionToDocument(candidate)
	result, err := repository.collection.InsertOne(ctx, document)
	if err == nil {
		document.ID = result.InsertedID.(bson.ObjectID)
		return awardRevisionFromDocument(document), nil
	}
	if !mongo.IsDuplicateKeyError(err) {
		return AwardRevision{}, err
	}

	existing, found, findErr := repository.FindRevisionByOperation(
		ctx, candidate.CompanyID, candidate.FinalisationOperationID)
	if findErr != nil {
		return AwardRevision{}, findErr
	}
	if found &&
		existing.AwardChainID == candidate.AwardChainID &&
		existing.RevisionNumber == candidate.RevisionNumber &&
		existing.SelectionFingerprint == candidate.SelectionFingerprint {
		return existing, nil
	}
	return AwardRevision{}, ErrAwardRevisionConflict
}

func (repository *MongoAwardRevisionRepository) FindRevision(
	ctx context.Context,
	companyID, revisionID string,
) (AwardRevision, bool, error) {
	objectID, err := bson.ObjectIDFromHex(revisionID)
	if err != nil {
		return AwardRevision{}, false, nil
	}

	var document awardRevisionDocument
	err = repository.collection.FindOne(ctx, bson.M{
		"_id":       objectID,
		"companyId": companyID,
	}).Decode(&document)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return AwardRevision{}, false, nil
	}
	if err != nil {
		return AwardRevision{}, false, err
	}
	return awardRevisionFromDocument(document), true, nil
}

// FindRevisionByOperation is what makes an unknown-outcome retry resolvable
// without re-numbering (§8F).
func (repository *MongoAwardRevisionRepository) FindRevisionByOperation(
	ctx context.Context,
	companyID, finalisationOperationID string,
) (AwardRevision, bool, error) {
	var document awardRevisionDocument
	err := repository.collection.FindOne(ctx, bson.M{
		"companyId":               companyID,
		"finalisationOperationId": finalisationOperationID,
	}).Decode(&document)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return AwardRevision{}, false, nil
	}
	if err != nil {
		return AwardRevision{}, false, err
	}
	return awardRevisionFromDocument(document), true, nil
}

// FindLatestRevision resolves the current award directly from the immutable
// revisions.
//
// This is what a read during the crash window uses: it must recognise an
// inserted revision even when the chain pointer is still stale, so it must
// never report "no award exists" while an authoritative revision is present.
func (repository *MongoAwardRevisionRepository) FindLatestRevision(
	ctx context.Context,
	companyID, awardChainID string,
) (AwardRevision, bool, error) {
	var document awardRevisionDocument
	err := repository.collection.FindOne(ctx,
		bson.M{"companyId": companyID, "awardChainId": awardChainID},
		options.FindOne().SetSort(bson.D{{Key: "revisionNumber", Value: -1}}),
	).Decode(&document)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return AwardRevision{}, false, nil
	}
	if err != nil {
		return AwardRevision{}, false, err
	}
	return awardRevisionFromDocument(document), true, nil
}

// ListRevisions returns a chain's history, oldest first, so the numbering reads
// as the sequence of decisions it is.
func (repository *MongoAwardRevisionRepository) ListRevisions(
	ctx context.Context,
	companyID, awardChainID string,
) ([]AwardRevision, error) {
	cursor, err := repository.collection.Find(ctx,
		bson.M{"companyId": companyID, "awardChainId": awardChainID},
		options.Find().SetSort(bson.D{{Key: "revisionNumber", Value: 1}}),
	)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	revisions := []AwardRevision{}
	for cursor.Next(ctx) {
		var document awardRevisionDocument
		if err := cursor.Decode(&document); err != nil {
			return nil, err
		}
		revisions = append(revisions, awardRevisionFromDocument(document))
	}
	return revisions, cursor.Err()
}
