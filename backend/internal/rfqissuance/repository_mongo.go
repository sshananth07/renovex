package rfqissuance

import (
	"context"
	"errors"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/shananth/renovation-platform/backend/internal/foundation/procurementlimits"
	"github.com/shananth/renovation-platform/backend/internal/foundation/quantity"
)

const (
	collectionIssuedRFQVersions = "issued_rfq_versions"

	indexNameIssuedVersionsCompanyChain = "idx_issued_rfq_versions_company_chain"
	indexNameUniqueIssuedVersion        = "uq_issued_rfq_versions_company_chain_version"
	indexNameUniqueIssuanceOperation    = "uq_issued_rfq_versions_company_operation"
)

// MongoIssuedRFQVersionRepository is the MongoDB-backed store of immutable
// issued RFQ versions.
//
// Documents here are never updated in place. A correction is a NEW version, so
// this repository deliberately exposes no update method (design spec §3.2).
type MongoIssuedRFQVersionRepository struct {
	collection *mongo.Collection
}

// NewMongoIssuedRFQVersionRepository constructs the repository over db's
// issued_rfq_versions collection.
func NewMongoIssuedRFQVersionRepository(db *mongo.Database) *MongoIssuedRFQVersionRepository {
	return &MongoIssuedRFQVersionRepository{
		collection: db.Collection(collectionIssuedRFQVersions),
	}
}

// EnsureIndexes creates the §11.2 indexes. It is idempotent: the composition
// root calls it on every boot.
func (r *MongoIssuedRFQVersionRepository) EnsureIndexes(ctx context.Context) error {
	_, err := r.collection.Indexes().CreateMany(ctx, []mongo.IndexModel{
		// Serves the issuance-status lookup and version listing without a scan.
		{Keys: bson.D{{Key: "companyId", Value: 1}, {Key: "rfqChainId", Value: 1}},
			Options: options.Index().SetName(indexNameIssuedVersionsCompanyChain)},
		// The immutability invariant: one immutable version per chain per
		// number. This is what makes a concurrent double-issue impossible
		// rather than merely unlikely — the loser gets a duplicate-key error
		// instead of silently creating a second version 2.
		{Keys: bson.D{{Key: "companyId", Value: 1}, {Key: "rfqChainId", Value: 1},
			{Key: "versionNumber", Value: 1}},
			Options: options.Index().SetUnique(true).SetName(indexNameUniqueIssuedVersion)},
		// Operation-ID idempotency (§10.1). PARTIAL so versions created without
		// an operation ID (reconciliation repairs, fixtures) do not all collide
		// on a single null key — a plain unique index would permit exactly one
		// such document per company.
		{Keys: bson.D{{Key: "companyId", Value: 1}, {Key: "issuanceOperationId", Value: 1}},
			Options: options.Index().SetUnique(true).
				SetPartialFilterExpression(bson.M{
					"issuanceOperationId": bson.M{"$type": "string"},
				}).
				SetName(indexNameUniqueIssuanceOperation)},
	})
	return err
}

// --- documents ---

type issuedLineDoc struct {
	ID                          string     `bson:"id"`
	LineageID                   string     `bson:"lineageId"`
	SourceM7RFQLineID           *string    `bson:"sourceM7RfqLineId,omitempty"`
	SourceMaterialRequirementID *string    `bson:"sourceMaterialRequirementId,omitempty"`
	MaterialID                  string     `bson:"materialId"`
	MaterialName                string     `bson:"materialName"`
	Specification               string     `bson:"specification,omitempty"`
	QuantityValue               string     `bson:"quantityValue"`
	QuantityUnit                string     `bson:"quantityUnit"`
	RequiredByDate              *time.Time `bson:"requiredByDate,omitempty"`
	ProcurementNotes            string     `bson:"procurementNotes,omitempty"`
	SortOrder                   int        `bson:"sortOrder"`
}

type issuedVersionDoc struct {
	ID            bson.ObjectID `bson:"_id,omitempty"`
	CompanyID     string        `bson:"companyId"`
	ProjectID     string        `bson:"projectId"`
	RFQChainID    string        `bson:"rfqChainId"`
	RFQNumber     string        `bson:"rfqNumber"`
	VersionNumber int           `bson:"versionNumber"`
	Currency      string        `bson:"currency"`

	Title                string     `bson:"title,omitempty"`
	DeliveryAddress      string     `bson:"deliveryAddress,omitempty"`
	RequiredByDate       *time.Time `bson:"requiredByDate,omitempty"`
	ResponseDeadline     time.Time  `bson:"responseDeadline"`
	SupplierInstructions string     `bson:"supplierInstructions,omitempty"`

	Lines []issuedLineDoc `bson:"lines,omitempty"`

	SourceM7RFQRevision int64  `bson:"sourceM7RfqRevision"`
	SourceFingerprint   string `bson:"sourceFingerprint,omitempty"`
	// omitempty matters: it keeps an absent operation ID out of the partial
	// unique index rather than storing "".
	IssuanceOperationID string `bson:"issuanceOperationId,omitempty"`

	IssuedByUserID string    `bson:"issuedByUserId,omitempty"`
	IssuedAt       time.Time `bson:"issuedAt"`
	SchemaVersion  int       `bson:"schemaVersion"`
}

func toIssuedLineDocs(lines []IssuedRFQLine) []issuedLineDoc {
	docs := make([]issuedLineDoc, 0, len(lines))
	for _, l := range lines {
		docs = append(docs, issuedLineDoc{
			ID: l.ID, LineageID: l.LineageID,
			SourceM7RFQLineID:           l.SourceM7RFQLineID,
			SourceMaterialRequirementID: l.SourceMaterialRequirementID,
			MaterialID:                  l.MaterialID,
			MaterialName:                l.MaterialName,
			Specification:               l.Specification,
			// Canonical decimal STRING, never a float (ADR 0001).
			QuantityValue:    l.Quantity.Value.String(),
			QuantityUnit:     l.Quantity.Unit,
			RequiredByDate:   l.RequiredByDate,
			ProcurementNotes: l.ProcurementNotes,
			SortOrder:        l.SortOrder,
		})
	}
	return docs
}

func toIssuedVersionDoc(v IssuedRFQVersion) issuedVersionDoc {
	return issuedVersionDoc{
		CompanyID: v.CompanyID, ProjectID: v.ProjectID, RFQChainID: v.RFQChainID,
		RFQNumber: v.RFQNumber, VersionNumber: v.VersionNumber, Currency: v.Currency,
		Title: v.Title, DeliveryAddress: v.DeliveryAddress,
		RequiredByDate: v.RequiredByDate, ResponseDeadline: v.ResponseDeadline,
		SupplierInstructions: v.SupplierInstructions,
		Lines:                toIssuedLineDocs(v.Lines),
		SourceM7RFQRevision:  v.SourceM7RFQRevision,
		SourceFingerprint:    v.SourceFingerprint,
		IssuanceOperationID:  v.IssuanceOperationID,
		IssuedByUserID:       v.IssuedByUserID,
		IssuedAt:             v.IssuedAt,
		SchemaVersion:        v.SchemaVersion,
	}
}

// fromIssuedLineDocs decodes persisted lines. Shared by the immutable-version
// and amendment-draft repositories so the two can never drift on how a line
// round-trips — particularly the nil-vs-empty provenance distinction.
func fromIssuedLineDocs(docs []issuedLineDoc) ([]IssuedRFQLine, error) {
	lines := make([]IssuedRFQLine, 0, len(docs))
	for _, l := range docs {
		qty, err := quantity.New(l.QuantityValue, l.QuantityUnit)
		if err != nil {
			return nil, err
		}
		lines = append(lines, IssuedRFQLine{
			ID: l.ID, LineageID: l.LineageID,
			SourceM7RFQLineID:           l.SourceM7RFQLineID,
			SourceMaterialRequirementID: l.SourceMaterialRequirementID,
			MaterialID:                  l.MaterialID,
			MaterialName:                l.MaterialName,
			Specification:               l.Specification,
			Quantity:                    qty,
			RequiredByDate:              l.RequiredByDate,
			ProcurementNotes:            l.ProcurementNotes,
			SortOrder:                   l.SortOrder,
		})
	}
	return lines, nil
}

func fromIssuedVersionDoc(doc issuedVersionDoc) (IssuedRFQVersion, error) {
	lines, err := fromIssuedLineDocs(doc.Lines)
	if err != nil {
		return IssuedRFQVersion{}, err
	}

	return IssuedRFQVersion{
		ID: doc.ID.Hex(), CompanyID: doc.CompanyID, ProjectID: doc.ProjectID,
		RFQChainID: doc.RFQChainID, RFQNumber: doc.RFQNumber,
		VersionNumber: doc.VersionNumber, Currency: doc.Currency,
		Title: doc.Title, DeliveryAddress: doc.DeliveryAddress,
		RequiredByDate: doc.RequiredByDate, ResponseDeadline: doc.ResponseDeadline,
		SupplierInstructions: doc.SupplierInstructions,
		Lines:                lines,
		SourceM7RFQRevision:  doc.SourceM7RFQRevision,
		SourceFingerprint:    doc.SourceFingerprint,
		IssuanceOperationID:  doc.IssuanceOperationID,
		IssuedByUserID:       doc.IssuedByUserID,
		IssuedAt:             doc.IssuedAt,
		SchemaVersion:        doc.SchemaVersion,
	}, nil
}

// classifyCreateVersionError translates a duplicate key into the named sentinel
// by matching the EXPLICIT index name.
//
// The two collisions mean different things and must not be conflated: a version
// -number collision is a concurrent issuance that lost, while an operation-ID
// collision is the SAME caller retrying. Returning the wrong one would tell a
// retrying client it lost a race it never entered.
//
// An unrecognised duplicate passes through unchanged rather than being
// laundered into a domain meaning this function cannot justify.
func classifyCreateVersionError(err error) error {
	if !mongo.IsDuplicateKeyError(err) {
		return err
	}
	var writeException mongo.WriteException
	if errors.As(err, &writeException) {
		for _, we := range writeException.WriteErrors {
			switch {
			case strings.Contains(we.Message, indexNameUniqueIssuedVersion):
				return ErrVersionAlreadyExists
			case strings.Contains(we.Message, indexNameUniqueIssuanceOperation):
				return ErrOperationAlreadyUsed
			}
		}
	}
	return err
}

// CreateVersion persists one immutable version.
//
// There is no update counterpart by design: a correction is a NEW version
// (design spec §3.2).
// DeleteAllForCompany permanently removes every IssuedRFQVersion owned by
// companyID. Never errors when zero documents match. Development-tool use
// only.
func (r *MongoIssuedRFQVersionRepository) DeleteAllForCompany(ctx context.Context, companyID string) error {
	_, err := r.collection.DeleteMany(ctx, bson.M{"companyId": companyID})
	return err
}

func (r *MongoIssuedRFQVersionRepository) CreateVersion(ctx context.Context,
	version IssuedRFQVersion) (IssuedRFQVersion, error) {
	if err := validateIssuedVersionPersistence(version); err != nil {
		return IssuedRFQVersion{}, err
	}

	res, err := r.collection.InsertOne(ctx, toIssuedVersionDoc(version))
	if err != nil {
		return IssuedRFQVersion{}, classifyCreateVersionError(err)
	}
	version.ID = res.InsertedID.(bson.ObjectID).Hex()
	return version, nil
}

// The repository repeats aggregate count validation so an internal caller
// cannot bypass the service constructor and persist work whose fan-out exceeds
// the approved bound. Reads remain unrestricted for grandfathered history.
func validateIssuedVersionPersistence(version IssuedRFQVersion) error {
	if err := procurementlimits.ValidateCount(len(version.Lines),
		procurementlimits.MaxLines); err != nil {
		return ErrInputLimitExceeded
	}
	return nil
}

// FindVersion reads one immutable version, tenant-scoped.
func (r *MongoIssuedRFQVersionRepository) FindVersion(ctx context.Context,
	companyID, versionID string) (IssuedRFQVersion, error) {

	objID, err := bson.ObjectIDFromHex(versionID)
	if err != nil {
		// A malformed id is reported as not-found so the handler maps it to 404
		// rather than 500.
		return IssuedRFQVersion{}, ErrIssuedVersionNotFound
	}

	var doc issuedVersionDoc
	err = r.collection.FindOne(ctx, bson.M{"_id": objID, "companyId": companyID}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return IssuedRFQVersion{}, ErrIssuedVersionNotFound
	}
	if err != nil {
		return IssuedRFQVersion{}, err
	}
	return fromIssuedVersionDoc(doc)
}

// ListVersions returns one tenant's immutable history for one RFQ chain in
// business-version order.
//
// Both scope keys are in the Mongo filter. Filtering after the read would make
// another tenant's versions cross the repository privacy boundary even if the
// service later happened to discard them.
func (r *MongoIssuedRFQVersionRepository) ListVersions(ctx context.Context,
	companyID, rfqChainID string) ([]IssuedRFQVersion, error) {

	cursor, err := r.collection.Find(ctx,
		bson.M{"companyId": companyID, "rfqChainId": rfqChainID},
		options.Find().SetSort(bson.D{{Key: "versionNumber", Value: 1}}),
	)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var docs []issuedVersionDoc
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, err
	}
	versions := make([]IssuedRFQVersion, 0, len(docs))
	for _, doc := range docs {
		version, err := fromIssuedVersionDoc(doc)
		if err != nil {
			return nil, err
		}
		versions = append(versions, version)
	}
	return versions, nil
}

// FindByOperationID resolves the version a previous attempt created under this
// idempotency key.
//
// This is what makes a retry RECOVER rather than merely fail: an interrupted
// caller re-presents its operation ID and receives the version it already
// created, instead of a conflict it cannot act on (design spec §10.1).
func (r *MongoIssuedRFQVersionRepository) FindByOperationID(ctx context.Context,
	companyID, operationID string) (IssuedRFQVersion, bool, error) {

	var doc issuedVersionDoc
	err := r.collection.FindOne(ctx, bson.M{
		"companyId": companyID, "issuanceOperationId": operationID,
	}).Decode(&doc)

	if errors.Is(err, mongo.ErrNoDocuments) {
		return IssuedRFQVersion{}, false, nil
	}
	if err != nil {
		return IssuedRFQVersion{}, false, err
	}
	version, err := fromIssuedVersionDoc(doc)
	if err != nil {
		return IssuedRFQVersion{}, false, err
	}
	return version, true, nil
}

// HasIssuedVersion reports whether companyID's chain has at least one immutable
// issued version.
//
// companyId is part of the FILTER, never a post-read check: another tenant's
// issued version must never answer this tenant's question. This is the fact
// rfqs consumes to decide whether an M7 RFQ may still be reopened.
func (r *MongoIssuedRFQVersionRepository) HasIssuedVersion(ctx context.Context,
	companyID, rfqChainID string) (bool, error) {

	err := r.collection.FindOne(ctx, bson.M{
		"companyId": companyID, "rfqChainId": rfqChainID,
	}, options.FindOne().SetProjection(bson.M{"_id": 1})).Err()

	if errors.Is(err, mongo.ErrNoDocuments) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}
