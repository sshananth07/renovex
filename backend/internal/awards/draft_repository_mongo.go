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
	collectionAwardDrafts = "award_drafts"

	indexNameUniqueOpenAwardDraft = "uq_award_drafts_company_chain_open"
)

// MongoAwardDraftRepository owns provisional award drafts. A draft claims
// nothing and has no externally visible effect, so its only hard guarantee is
// structural: at most one OPEN draft per Award chain.
type MongoAwardDraftRepository struct {
	collection *mongo.Collection
}

func NewMongoAwardDraftRepository(db *mongo.Database) *MongoAwardDraftRepository {
	return &MongoAwardDraftRepository{
		collection: db.Collection(collectionAwardDrafts),
	}
}

// EnsureIndexes creates the PARTIAL unique index over open drafts only.
//
// It must be partial: archived drafts accumulate as the frozen inputs to
// published revisions, and a full unique index would make the second
// finalisation on a chain impossible.
func (repository *MongoAwardDraftRepository) EnsureIndexes(ctx context.Context) error {
	_, err := repository.collection.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys: bson.D{
			{Key: "companyId", Value: 1},
			{Key: "awardChainId", Value: 1},
		},
		Options: options.Index().
			SetName(indexNameUniqueOpenAwardDraft).
			SetUnique(true).
			SetPartialFilterExpression(bson.M{
				"status": string(AwardDraftOpen),
			}),
	})
	return err
}

type awardLineDecisionDocument struct {
	IssuedRFQLineID string `bson:"issuedRFQLineId"`
	StableLineageID string `bson:"stableLineageId"`
	Decision        string `bson:"decision"`
	OfferVersionID  string `bson:"offerVersionId,omitempty"`
	OfferLineID     string `bson:"offerLineId,omitempty"`
	UnawardedReason string `bson:"unawardedReason,omitempty"`
	UnawardedNote   string `bson:"unawardedNote,omitempty"`
}

type awardDraftDocument struct {
	ID                 bson.ObjectID               `bson:"_id,omitempty"`
	CompanyID          string                      `bson:"companyId"`
	AwardChainID       string                      `bson:"awardChainId"`
	IssuedRFQVersionID string                      `bson:"issuedRFQVersionId"`
	Status             string                      `bson:"status"`
	LineDecisions      []awardLineDecisionDocument `bson:"lineDecisions"`
	Revision           int64                       `bson:"revision"`
	CreatedByUserID    string                      `bson:"createdByUserId"`
	CreatedAt          time.Time                   `bson:"createdAt"`
	UpdatedAt          time.Time                   `bson:"updatedAt"`
	SchemaVersion      int                         `bson:"schemaVersion"`
}

func decisionDocuments(
	decisions []AwardLineDecisionDraft,
) []awardLineDecisionDocument {
	documents := make([]awardLineDecisionDocument, 0, len(decisions))
	for _, decision := range decisions {
		documents = append(documents, awardLineDecisionDocument{
			IssuedRFQLineID: decision.IssuedRFQLineID,
			StableLineageID: decision.StableLineageID,
			Decision:        string(decision.Decision),
			OfferVersionID:  decision.OfferVersionID,
			OfferLineID:     decision.OfferLineID,
			UnawardedReason: string(decision.UnawardedReason),
			UnawardedNote:   decision.UnawardedNote,
		})
	}
	return documents
}

func awardDraftFromDocument(document awardDraftDocument) AwardDraft {
	decisions := make([]AwardLineDecisionDraft, 0, len(document.LineDecisions))
	for _, decision := range document.LineDecisions {
		decisions = append(decisions, AwardLineDecisionDraft{
			IssuedRFQLineID: decision.IssuedRFQLineID,
			StableLineageID: decision.StableLineageID,
			Decision:        AwardDecision(decision.Decision),
			OfferVersionID:  decision.OfferVersionID,
			OfferLineID:     decision.OfferLineID,
			UnawardedReason: UnawardedReason(decision.UnawardedReason),
			UnawardedNote:   decision.UnawardedNote,
		})
	}
	return AwardDraft{
		ID:                 document.ID.Hex(),
		CompanyID:          document.CompanyID,
		AwardChainID:       document.AwardChainID,
		IssuedRFQVersionID: document.IssuedRFQVersionID,
		Status:             AwardDraftStatus(document.Status),
		LineDecisions:      decisions,
		Revision:           document.Revision,
		CreatedByUserID:    document.CreatedByUserID,
		CreatedAt:          document.CreatedAt,
		UpdatedAt:          document.UpdatedAt,
		SchemaVersion:      document.SchemaVersion,
	}
}

// DeleteAllForCompany permanently removes every AwardDraft owned by
// companyID. Never errors when zero documents match. Development-tool use
// only.
func (repository *MongoAwardDraftRepository) DeleteAllForCompany(ctx context.Context, companyID string) error {
	_, err := repository.collection.DeleteMany(ctx, bson.M{"companyId": companyID})
	return err
}

// EnsureOpenDraft returns the chain's open draft, creating it if absent, and
// reports whether it created one.
//
// It contends on the named partial unique index rather than checking existence
// then inserting: a check-then-create sequence has a window in which two
// callers both see no draft and both insert.
//
// Adoption preserves the original author. Adopting is not a takeover: the
// person who opened the draft is the honest record of who started deciding.
func (repository *MongoAwardDraftRepository) EnsureOpenDraft(
	ctx context.Context,
	candidate AwardDraft,
) (AwardDraft, bool, error) {
	if strings.TrimSpace(candidate.CompanyID) == "" ||
		strings.TrimSpace(candidate.AwardChainID) == "" ||
		strings.TrimSpace(candidate.IssuedRFQVersionID) == "" {
		return AwardDraft{}, false, ErrInvalidAwardDraft
	}

	// Read before the upsert so "created" is a real observation rather than a
	// timestamp comparison, which would misreport under clock granularity.
	if existing, found, err := repository.FindOpenDraft(
		ctx, candidate.CompanyID, candidate.AwardChainID); err != nil {
		return AwardDraft{}, false, err
	} else if found {
		return existing, false, nil
	}

	now := time.Now().UTC()
	var document awardDraftDocument
	err := repository.collection.FindOneAndUpdate(
		ctx,
		bson.M{
			"companyId":    candidate.CompanyID,
			"awardChainId": candidate.AwardChainID,
			"status":       string(AwardDraftOpen),
		},
		bson.M{
			"$setOnInsert": bson.M{
				"companyId":          candidate.CompanyID,
				"awardChainId":       candidate.AwardChainID,
				"issuedRFQVersionId": candidate.IssuedRFQVersionID,
				"status":             string(AwardDraftOpen),
				"lineDecisions":      decisionDocuments(candidate.LineDecisions),
				"revision":           int64(1),
				"createdByUserId":    candidate.CreatedByUserID,
				"createdAt":          now,
				"updatedAt":          now,
				"schemaVersion":      AwardDraftSchemaVersion,
			},
		},
		options.FindOneAndUpdate().
			SetUpsert(true).
			SetReturnDocument(options.After),
	).Decode(&document)
	if err == nil {
		// The pre-read found nothing, so a returned revision of 1 is this
		// caller's own insert; anything higher means a concurrent writer's
		// draft was adopted between the read and the upsert.
		return awardDraftFromDocument(document), document.Revision == 1, nil
	}

	// A duplicate key means a concurrent caller won the insert. Reload and
	// adopt its draft rather than failing: both callers wanted the same thing.
	if mongo.IsDuplicateKeyError(err) {
		existing, found, findErr := repository.FindOpenDraft(
			ctx, candidate.CompanyID, candidate.AwardChainID)
		if findErr != nil {
			return AwardDraft{}, false, findErr
		}
		if !found {
			return AwardDraft{}, false, ErrAwardDraftConflict
		}
		return existing, false, nil
	}
	return AwardDraft{}, false, err
}

// FindOpenDraft resolves the chain's open draft within one tenant.
func (repository *MongoAwardDraftRepository) FindOpenDraft(
	ctx context.Context,
	companyID, awardChainID string,
) (AwardDraft, bool, error) {
	var document awardDraftDocument
	err := repository.collection.FindOne(ctx, bson.M{
		"companyId":    companyID,
		"awardChainId": awardChainID,
		"status":       string(AwardDraftOpen),
	}).Decode(&document)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return AwardDraft{}, false, nil
	}
	if err != nil {
		return AwardDraft{}, false, err
	}
	return awardDraftFromDocument(document), true, nil
}

// FindDraft resolves any draft by ID within one tenant, open or archived.
func (repository *MongoAwardDraftRepository) FindDraft(
	ctx context.Context,
	companyID, draftID string,
) (AwardDraft, bool, error) {
	objectID, err := bson.ObjectIDFromHex(draftID)
	if err != nil {
		return AwardDraft{}, false, nil
	}

	var document awardDraftDocument
	err = repository.collection.FindOne(ctx, bson.M{
		"_id":       objectID,
		"companyId": companyID,
	}).Decode(&document)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return AwardDraft{}, false, nil
	}
	if err != nil {
		return AwardDraft{}, false, err
	}
	return awardDraftFromDocument(document), true, nil
}

// ReplaceDecisions is the open-only, expected-revision CAS every edit uses.
//
// Status and revision are both in the FILTER, so a stale writer and a writer
// against an archived draft each find no match and lose cleanly. An award
// decision silently lost to a racing write is a commercial error, not a UI
// inconvenience.
func (repository *MongoAwardDraftRepository) ReplaceDecisions(
	ctx context.Context,
	companyID, draftID string,
	expectedRevision int64,
	decisions []AwardLineDecisionDraft,
) (AwardDraft, error) {
	objectID, err := bson.ObjectIDFromHex(draftID)
	if err != nil {
		return AwardDraft{}, ErrAwardDraftNotFound
	}

	now := time.Now().UTC()
	var document awardDraftDocument
	err = repository.collection.FindOneAndUpdate(
		ctx,
		bson.M{
			"_id":       objectID,
			"companyId": companyID,
			"status":    string(AwardDraftOpen),
			"revision":  expectedRevision,
		},
		bson.M{
			"$set": bson.M{
				"lineDecisions": decisionDocuments(decisions),
				"updatedAt":     now,
				"revision":      expectedRevision + 1,
			},
		},
		options.FindOneAndUpdate().SetReturnDocument(options.After),
	).Decode(&document)
	if err == nil {
		return awardDraftFromDocument(document), nil
	}
	if errors.Is(err, mongo.ErrNoDocuments) {
		return AwardDraft{}, ErrAwardDraftConflict
	}
	return AwardDraft{}, err
}

// ArchiveDraft performs open -> archived on an expected revision.
//
// Archiving frees the open-draft slot, which is how a failed finalisation is
// re-decided and how discard works.
func (repository *MongoAwardDraftRepository) ArchiveDraft(
	ctx context.Context,
	companyID, draftID string,
	expectedRevision int64,
) error {
	objectID, err := bson.ObjectIDFromHex(draftID)
	if err != nil {
		return ErrAwardDraftNotFound
	}

	now := time.Now().UTC()
	result, err := repository.collection.UpdateOne(
		ctx,
		bson.M{
			"_id":       objectID,
			"companyId": companyID,
			"status":    string(AwardDraftOpen),
			"revision":  expectedRevision,
		},
		bson.M{
			"$set": bson.M{
				"status":    string(AwardDraftArchived),
				"updatedAt": now,
				"revision":  expectedRevision + 1,
			},
		},
	)
	if err != nil {
		return err
	}
	if result.MatchedCount == 0 {
		return ErrAwardDraftConflict
	}
	return nil
}
