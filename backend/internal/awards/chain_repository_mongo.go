package awards

import (
	"context"
	"errors"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

const (
	collectionAwardDecisionChains = "award_decision_chains"

	indexNameUniqueAwardChain = "uq_award_chains_company_issued_version"
)

// MongoAwardChainRepository owns the Award decision chain: one per Issued RFQ
// Version (D4), carrying the durable finalisation state that prevents a stale
// pointer from admitting a duplicate revision number (D2).
type MongoAwardChainRepository struct {
	collection *mongo.Collection
}

func NewMongoAwardChainRepository(db *mongo.Database) *MongoAwardChainRepository {
	return &MongoAwardChainRepository{
		collection: db.Collection(collectionAwardDecisionChains),
	}
}

// EnsureIndexes makes the tenant-scoped issued-version identity unique.
//
// CompanyID is part of the key because identifier values may repeat in another
// company; IssuedRFQVersionID rather than RFQChainID because D4 gives every
// issued version its own independent chain.
func (repository *MongoAwardChainRepository) EnsureIndexes(ctx context.Context) error {
	_, err := repository.collection.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys: bson.D{
			{Key: "companyId", Value: 1},
			{Key: "issuedRFQVersionId", Value: 1},
		},
		Options: options.Index().
			SetName(indexNameUniqueAwardChain).
			SetUnique(true),
	})
	return err
}

type awardDecisionChainDocument struct {
	ID                     bson.ObjectID `bson:"_id,omitempty"`
	CompanyID              string        `bson:"companyId"`
	RFQChainID             string        `bson:"rfqChainId"`
	IssuedRFQVersionID     string        `bson:"issuedRFQVersionId"`
	CurrentAwardRevisionID *string       `bson:"currentAwardRevisionId,omitempty"`
	LatestRevisionNumber   int           `bson:"latestRevisionNumber"`
	FinalisationState      string        `bson:"finalisationState"`
	FinalisingOperationID  string        `bson:"finalisingOperationId,omitempty"`
	FinalisingRevisionID   string        `bson:"finalisingRevisionId,omitempty"`
	Revision               int64         `bson:"revision"`
	CreatedAt              time.Time     `bson:"createdAt"`
	UpdatedAt              time.Time     `bson:"updatedAt"`
	SchemaVersion          int           `bson:"schemaVersion"`
}

func awardChainFromDocument(document awardDecisionChainDocument) AwardDecisionChain {
	return AwardDecisionChain{
		ID:                     document.ID.Hex(),
		CompanyID:              document.CompanyID,
		RFQChainID:             document.RFQChainID,
		IssuedRFQVersionID:     document.IssuedRFQVersionID,
		CurrentAwardRevisionID: document.CurrentAwardRevisionID,
		LatestRevisionNumber:   document.LatestRevisionNumber,
		FinalisationState:      FinalisationState(document.FinalisationState),
		FinalisingOperationID:  document.FinalisingOperationID,
		FinalisingRevisionID:   document.FinalisingRevisionID,
		Revision:               document.Revision,
		CreatedAt:              document.CreatedAt,
		UpdatedAt:              document.UpdatedAt,
		SchemaVersion:          document.SchemaVersion,
	}
}

// DeleteAllForCompany permanently removes every AwardDecisionChain owned by
// companyID. Never errors when zero documents match. Development-tool use
// only.
func (repository *MongoAwardChainRepository) DeleteAllForCompany(ctx context.Context, companyID string) error {
	_, err := repository.collection.DeleteMany(ctx, bson.M{"companyId": companyID})
	return err
}

// EnsureAwardChain uses one upsert rather than a create-then-read sequence, so
// concurrent first access converges on the document the unique index protects
// instead of creating competing chains.
func (repository *MongoAwardChainRepository) EnsureAwardChain(
	ctx context.Context,
	companyID, rfqChainID, issuedRFQVersionID string,
) (AwardDecisionChain, error) {
	if strings.TrimSpace(companyID) == "" ||
		strings.TrimSpace(rfqChainID) == "" ||
		strings.TrimSpace(issuedRFQVersionID) == "" {
		return AwardDecisionChain{}, ErrInvalidAwardChain
	}

	now := time.Now().UTC()
	var document awardDecisionChainDocument
	err := repository.collection.FindOneAndUpdate(
		ctx,
		bson.M{
			"companyId":          companyID,
			"issuedRFQVersionId": issuedRFQVersionID,
		},
		bson.M{
			"$setOnInsert": bson.M{
				"companyId":            companyID,
				"rfqChainId":           rfqChainID,
				"issuedRFQVersionId":   issuedRFQVersionID,
				"latestRevisionNumber": 0,
				"finalisationState":    string(FinalisationDraft),
				"revision":             int64(1),
				"createdAt":            now,
				"updatedAt":            now,
				"schemaVersion":        AwardDecisionChainSchemaVersion,
			},
		},
		options.FindOneAndUpdate().
			SetUpsert(true).
			SetReturnDocument(options.After),
	).Decode(&document)
	if err != nil {
		return AwardDecisionChain{}, err
	}
	return awardChainFromDocument(document), nil
}

// FindChain resolves a chain within one tenant. A foreign company's chain is
// simply absent, never visible-but-refused.
func (repository *MongoAwardChainRepository) FindChain(
	ctx context.Context,
	companyID, chainID string,
) (AwardDecisionChain, bool, error) {
	objectID, err := bson.ObjectIDFromHex(chainID)
	if err != nil {
		// A malformed ID cannot name any document, so it is not-found rather
		// than an error the caller could use to probe the store.
		return AwardDecisionChain{}, false, nil
	}

	var document awardDecisionChainDocument
	err = repository.collection.FindOne(ctx, bson.M{
		"_id":       objectID,
		"companyId": companyID,
	}).Decode(&document)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return AwardDecisionChain{}, false, nil
	}
	if err != nil {
		return AwardDecisionChain{}, false, err
	}
	return awardChainFromDocument(document), true, nil
}

// FindChainByIssuedVersion resolves the chain owning an issued RFQ version.
func (repository *MongoAwardChainRepository) FindChainByIssuedVersion(
	ctx context.Context,
	companyID, issuedRFQVersionID string,
) (AwardDecisionChain, bool, error) {
	var document awardDecisionChainDocument
	err := repository.collection.FindOne(ctx, bson.M{
		"companyId":          companyID,
		"issuedRFQVersionId": issuedRFQVersionID,
	}).Decode(&document)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return AwardDecisionChain{}, false, nil
	}
	if err != nil {
		return AwardDecisionChain{}, false, err
	}
	return awardChainFromDocument(document), true, nil
}

type FinalisationClaimInput struct {
	CompanyID           string
	AwardChainID        string
	ExpectedRevision    int64
	OperationID         string
	CandidateRevisionID string
}

// ClaimFinalisation performs draft -> finalising, or published -> finalising
// for a correction (§8G).
//
// State is part of the FILTER, so the second of two concurrent claims finds no
// match and loses cleanly rather than overwriting the winner. This is the
// cheapest place to reject a second finalisation: it happens before either
// operation touches a shared line claim or offer gate (§8E step 1).
//
// `published` is an accepted starting state because a correction supersedes a
// settled award. A chain already `finalising` is excluded, so an in-flight
// publication can never be disturbed by a concurrent correction.
func (repository *MongoAwardChainRepository) ClaimFinalisation(
	ctx context.Context,
	input FinalisationClaimInput,
) (AwardDecisionChain, error) {
	if strings.TrimSpace(input.OperationID) == "" ||
		strings.TrimSpace(input.CandidateRevisionID) == "" {
		return AwardDecisionChain{}, ErrInvalidAwardChain
	}
	objectID, err := bson.ObjectIDFromHex(input.AwardChainID)
	if err != nil {
		return AwardDecisionChain{}, ErrAwardChainNotFound
	}

	now := time.Now().UTC()
	var document awardDecisionChainDocument
	err = repository.collection.FindOneAndUpdate(
		ctx,
		bson.M{
			"_id":       objectID,
			"companyId": input.CompanyID,
			"finalisationState": bson.M{"$in": []string{
				string(FinalisationDraft),
				string(FinalisationPublished),
			}},
			"revision": input.ExpectedRevision,
		},
		bson.M{
			"$set": bson.M{
				"finalisationState":     string(FinalisationFinalising),
				"finalisingOperationId": input.OperationID,
				"finalisingRevisionId":  input.CandidateRevisionID,
				"updatedAt":             now,
				"revision":              input.ExpectedRevision + 1,
			},
		},
		options.FindOneAndUpdate().SetReturnDocument(options.After),
	).Decode(&document)
	if err == nil {
		return awardChainFromDocument(document), nil
	}
	if !errors.Is(err, mongo.ErrNoDocuments) {
		return AwardDecisionChain{}, err
	}

	// Classify the miss. A timeout does not prove the claim failed, so a
	// same-operation retry must converge on its own claim without another
	// increment and without re-numbering the candidate revision (D2).
	current, found, findErr := repository.FindChain(
		ctx, input.CompanyID, input.AwardChainID)
	if findErr != nil {
		return AwardDecisionChain{}, findErr
	}
	if !found {
		return AwardDecisionChain{}, ErrAwardChainNotFound
	}
	if current.FinalisationState == FinalisationFinalising &&
		current.FinalisingOperationID == input.OperationID &&
		current.FinalisingRevisionID == input.CandidateRevisionID {
		return current, nil
	}
	return AwardDecisionChain{}, ErrAwardRevisionConflict
}

type PublishRevisionInput struct {
	CompanyID        string
	AwardChainID     string
	ExpectedRevision int64
	OperationID      string
	AwardRevisionID  string
	RevisionNumber   int
}

// PublishRevision performs finalising -> published and points the chain at the
// authoritative revision.
//
// This is recoverable post-publication work (D2 step 6): the revision already
// exists, so this transition may run more than once and is idempotent for its
// own operation. It never rolls the chain backward.
func (repository *MongoAwardChainRepository) PublishRevision(
	ctx context.Context,
	input PublishRevisionInput,
) (AwardDecisionChain, error) {
	objectID, err := bson.ObjectIDFromHex(input.AwardChainID)
	if err != nil {
		return AwardDecisionChain{}, ErrAwardChainNotFound
	}

	now := time.Now().UTC()
	var document awardDecisionChainDocument
	err = repository.collection.FindOneAndUpdate(
		ctx,
		bson.M{
			"_id":                   objectID,
			"companyId":             input.CompanyID,
			"finalisationState":     string(FinalisationFinalising),
			"finalisingOperationId": input.OperationID,
			"revision":              input.ExpectedRevision,
		},
		bson.M{
			"$set": bson.M{
				"finalisationState":      string(FinalisationPublished),
				"currentAwardRevisionId": input.AwardRevisionID,
				"latestRevisionNumber":   input.RevisionNumber,
				"updatedAt":              now,
				"revision":               input.ExpectedRevision + 1,
			},
			// The in-flight claim fields have served their purpose; leaving
			// them would make a published chain look to any reader as though
			// it were still finalising.
			"$unset": bson.M{
				"finalisingOperationId": "",
				"finalisingRevisionId":  "",
			},
		},
		options.FindOneAndUpdate().SetReturnDocument(options.After),
	).Decode(&document)
	if err == nil {
		return awardChainFromDocument(document), nil
	}
	if !errors.Is(err, mongo.ErrNoDocuments) {
		return AwardDecisionChain{}, err
	}

	// Already advanced to this exact revision is the idempotent path, not a
	// failure: recovery must be able to run twice.
	current, found, findErr := repository.FindChain(
		ctx, input.CompanyID, input.AwardChainID)
	if findErr != nil {
		return AwardDecisionChain{}, findErr
	}
	if found && current.FinalisationState == FinalisationPublished &&
		current.CurrentAwardRevisionID != nil &&
		*current.CurrentAwardRevisionID == input.AwardRevisionID {
		return current, nil
	}
	return AwardDecisionChain{}, ErrAwardRevisionConflict
}

// AbandonFinalisation reverts a chain after a finalisation that published
// NOTHING.
//
// The caller must have verified that no matching Award Revision exists (D2).
// Once a revision exists the chain may never be rolled back — the correct
// action is always to complete forward.
//
// It restores the state the chain's own history implies, NOT unconditionally
// `draft`: a failed CORRECTION starts from a chain that already holds a
// published award, and forcing it to `draft` would orphan that award's pointer
// and make the settled award unresolvable.
func (repository *MongoAwardChainRepository) AbandonFinalisation(
	ctx context.Context,
	companyID, chainID, operationID string,
	expectedRevision int64,
) error {
	objectID, err := bson.ObjectIDFromHex(chainID)
	if err != nil {
		return ErrAwardChainNotFound
	}

	current, found, err := repository.FindChain(ctx, companyID, chainID)
	if err != nil {
		return err
	}
	if !found {
		return ErrAwardChainNotFound
	}
	restored := FinalisationDraft
	if current.CurrentAwardRevisionID != nil &&
		strings.TrimSpace(*current.CurrentAwardRevisionID) != "" {
		restored = FinalisationPublished
	}

	now := time.Now().UTC()
	result, err := repository.collection.UpdateOne(
		ctx,
		bson.M{
			"_id":       objectID,
			"companyId": companyID,
			// Only the OWNING operation may abandon its own finalisation, and
			// only while the chain is still finalising.
			"finalisationState":     string(FinalisationFinalising),
			"finalisingOperationId": operationID,
			"revision":              expectedRevision,
		},
		bson.M{
			"$set": bson.M{
				"finalisationState": string(restored),
				"updatedAt":         now,
				"revision":          expectedRevision + 1,
			},
			"$unset": bson.M{
				"finalisingOperationId": "",
				"finalisingRevisionId":  "",
			},
		},
	)
	if err != nil {
		return err
	}
	if result.MatchedCount == 0 {
		return ErrAwardRevisionConflict
	}
	return nil
}
