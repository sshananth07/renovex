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
	collectionAwardLineClaims = "award_line_claims"

	indexNameUniqueAwardLineClaim = "uq_award_line_claims_company_chain_lineage"
	indexNameAwardLineClaimByOp   = "ix_award_line_claims_company_operation"
)

// LineClaimState is the RFQ-chain-scoped lineage claim's lifecycle.
//
// `awarded` is TERMINAL. Once a matching immutable revision exists, release is
// prohibited by D2: releasing would permit revision 1 to award a lineage, tell
// the Supplier they won, and revision 2 to reassign it to a competitor (§8G).
type LineClaimState string

const (
	LineClaimClaimed LineClaimState = "claimed"
	LineClaimAwarded LineClaimState = "awarded"
)

// AwardLineClaim is the SECOND serialization point of Phase F (§8A.2).
//
//	Offer eligibility gate   scope CompanyID + OfferVersionID
//	                         settles withdrawal versus award for ONE offer
//	Award line claim         scope CompanyID + RFQChainID + StableLineageID
//	                         settles one Supplier per line ACROSS every version
//
// Conflating them is a design error: an award acquires BOTH.
type AwardLineClaim struct {
	ID                  string
	CompanyID           string
	RFQChainID          string
	StableLineageID     string
	State               LineClaimState
	AwardOperationID    string
	CandidateRevisionID string
	AwardRevisionID     string
	IssuedRFQVersionID  string
	Revision            int64
	ClaimedAt           time.Time
	AwardedAt           *time.Time
}

type MongoAwardLineClaimRepository struct {
	collection *mongo.Collection
}

func NewMongoAwardLineClaimRepository(
	db *mongo.Database,
) *MongoAwardLineClaimRepository {
	return &MongoAwardLineClaimRepository{
		collection: db.Collection(collectionAwardLineClaims),
	}
}

// EnsureIndexes creates the one index that makes "one Supplier per line, across
// every issued version" true.
//
// It is deliberately NOT scoped to an issued version: D4 gives each issued
// version its own Award chain, and the cross-version duplicate-award guard is
// exactly this chain-wide lineage key. CompanyID is included because lineage
// IDs may repeat in another tenant.
func (repository *MongoAwardLineClaimRepository) EnsureIndexes(
	ctx context.Context,
) error {
	_, err := repository.collection.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{
			Keys: bson.D{
				{Key: "companyId", Value: 1},
				{Key: "rfqChainId", Value: 1},
				{Key: "stableLineageId", Value: 1},
			},
			Options: options.Index().
				SetName(indexNameUniqueAwardLineClaim).
				SetUnique(true),
		},
		{
			// Supports "release ONLY my own claims" after a partial acquisition.
			Keys: bson.D{
				{Key: "companyId", Value: 1},
				{Key: "awardOperationId", Value: 1},
			},
			Options: options.Index().SetName(indexNameAwardLineClaimByOp),
		},
	})
	return err
}

type awardLineClaimDocument struct {
	ID                  bson.ObjectID `bson:"_id,omitempty"`
	CompanyID           string        `bson:"companyId"`
	RFQChainID          string        `bson:"rfqChainId"`
	StableLineageID     string        `bson:"stableLineageId"`
	State               string        `bson:"state"`
	AwardOperationID    string        `bson:"awardOperationId"`
	CandidateRevisionID string        `bson:"candidateRevisionId"`
	AwardRevisionID     string        `bson:"awardRevisionId,omitempty"`
	IssuedRFQVersionID  string        `bson:"issuedRFQVersionId"`
	Revision            int64         `bson:"revision"`
	ClaimedAt           time.Time     `bson:"claimedAt"`
	AwardedAt           *time.Time    `bson:"awardedAt,omitempty"`
}

func awardLineClaimFromDocument(document awardLineClaimDocument) AwardLineClaim {
	return AwardLineClaim{
		ID:                  document.ID.Hex(),
		CompanyID:           document.CompanyID,
		RFQChainID:          document.RFQChainID,
		StableLineageID:     document.StableLineageID,
		State:               LineClaimState(document.State),
		AwardOperationID:    document.AwardOperationID,
		CandidateRevisionID: document.CandidateRevisionID,
		AwardRevisionID:     document.AwardRevisionID,
		IssuedRFQVersionID:  document.IssuedRFQVersionID,
		Revision:            document.Revision,
		ClaimedAt:           document.ClaimedAt,
		AwardedAt:           document.AwardedAt,
	}
}

type AwardLineClaimInput struct {
	CompanyID           string
	RFQChainID          string
	StableLineageID     string
	IssuedRFQVersionID  string
	AwardOperationID    string
	CandidateRevisionID string
}

// DeleteAllForCompany permanently removes every AwardLineClaim owned by
// companyID. Never errors when zero documents match. Development-tool use
// only.
func (repository *MongoAwardLineClaimRepository) DeleteAllForCompany(ctx context.Context, companyID string) error {
	_, err := repository.collection.DeleteMany(ctx, bson.M{"companyId": companyID})
	return err
}

// ClaimLineage acquires the chain-wide claim for one RFQ line lineage.
//
// It contends on the named unique index rather than checking existence then
// inserting: a check-then-insert sequence has a window in which two
// finalisations both see the lineage free and both proceed.
//
// The two rejection kinds are deliberately distinct. An `awarded` lineage is
// terminal — no retry can ever make it available — while a `claimed` one is a
// live competitor whose finalisation may yet fail.
func (repository *MongoAwardLineClaimRepository) ClaimLineage(
	ctx context.Context,
	input AwardLineClaimInput,
) (AwardLineClaim, error) {
	if strings.TrimSpace(input.CompanyID) == "" ||
		strings.TrimSpace(input.RFQChainID) == "" ||
		strings.TrimSpace(input.StableLineageID) == "" ||
		strings.TrimSpace(input.AwardOperationID) == "" ||
		strings.TrimSpace(input.CandidateRevisionID) == "" {
		return AwardLineClaim{}, ErrInvalidAwardChain
	}

	now := time.Now().UTC()
	document := awardLineClaimDocument{
		CompanyID:           input.CompanyID,
		RFQChainID:          input.RFQChainID,
		StableLineageID:     input.StableLineageID,
		State:               string(LineClaimClaimed),
		AwardOperationID:    input.AwardOperationID,
		CandidateRevisionID: input.CandidateRevisionID,
		IssuedRFQVersionID:  input.IssuedRFQVersionID,
		Revision:            1,
		ClaimedAt:           now,
	}

	result, err := repository.collection.InsertOne(ctx, document)
	if err == nil {
		document.ID = result.InsertedID.(bson.ObjectID)
		return awardLineClaimFromDocument(document), nil
	}
	if !mongo.IsDuplicateKeyError(err) {
		return AwardLineClaim{}, err
	}

	// Classify the collision against the claim that actually holds the lineage.
	current, found, findErr := repository.FindClaimByLineage(
		ctx, input.CompanyID, input.RFQChainID, input.StableLineageID)
	if findErr != nil {
		return AwardLineClaim{}, findErr
	}
	if !found {
		// The holder disappeared between the insert and this read, which means
		// a concurrent release. Report a retryable conflict rather than
		// inventing a claim.
		return AwardLineClaim{}, ErrRFQLineAwardConflict
	}
	// A same-operation retry converges: a timeout does not prove the claim
	// failed, and failing here would strand a recoverable finalisation.
	if current.AwardOperationID == input.AwardOperationID &&
		current.State == LineClaimClaimed {
		return current, nil
	}
	if current.State == LineClaimAwarded {
		return AwardLineClaim{}, ErrRFQLineAlreadyAwarded
	}
	return AwardLineClaim{}, ErrRFQLineAwardConflict
}

func (repository *MongoAwardLineClaimRepository) FindClaim(
	ctx context.Context,
	companyID, claimID string,
) (AwardLineClaim, bool, error) {
	objectID, err := bson.ObjectIDFromHex(claimID)
	if err != nil {
		return AwardLineClaim{}, false, nil
	}

	var document awardLineClaimDocument
	err = repository.collection.FindOne(ctx, bson.M{
		"_id":       objectID,
		"companyId": companyID,
	}).Decode(&document)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return AwardLineClaim{}, false, nil
	}
	if err != nil {
		return AwardLineClaim{}, false, err
	}
	return awardLineClaimFromDocument(document), true, nil
}

func (repository *MongoAwardLineClaimRepository) FindClaimByLineage(
	ctx context.Context,
	companyID, rfqChainID, stableLineageID string,
) (AwardLineClaim, bool, error) {
	var document awardLineClaimDocument
	err := repository.collection.FindOne(ctx, bson.M{
		"companyId":       companyID,
		"rfqChainId":      rfqChainID,
		"stableLineageId": stableLineageID,
	}).Decode(&document)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return AwardLineClaim{}, false, nil
	}
	if err != nil {
		return AwardLineClaim{}, false, err
	}
	return awardLineClaimFromDocument(document), true, nil
}

// ListClaimsForOperation returns every claim an operation holds.
//
// This is what makes "release ONLY your own claims, in reverse acquisition
// order" implementable after a partial acquisition (§8E).
func (repository *MongoAwardLineClaimRepository) ListClaimsForOperation(
	ctx context.Context,
	companyID, awardOperationID string,
) ([]AwardLineClaim, error) {
	cursor, err := repository.collection.Find(ctx, bson.M{
		"companyId":        companyID,
		"awardOperationId": awardOperationID,
	}, options.Find().SetSort(bson.D{{Key: "stableLineageId", Value: 1}}))
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var claims []AwardLineClaim
	for cursor.Next(ctx) {
		var document awardLineClaimDocument
		if err := cursor.Decode(&document); err != nil {
			return nil, err
		}
		claims = append(claims, awardLineClaimFromDocument(document))
	}
	return claims, cursor.Err()
}

// MarkLineageAwarded performs claimed -> awarded, the terminal transition.
//
// The owning operation is in the FILTER: another operation completing someone
// else's claim would attribute an award to the wrong decision. It is idempotent
// for its own operation because post-publication work is recoverable and may
// run more than once (D2).
func (repository *MongoAwardLineClaimRepository) MarkLineageAwarded(
	ctx context.Context,
	companyID, claimID, awardOperationID, awardRevisionID string,
	expectedRevision int64,
) error {
	objectID, err := bson.ObjectIDFromHex(claimID)
	if err != nil {
		return ErrAwardChainNotFound
	}

	now := time.Now().UTC()
	result, err := repository.collection.UpdateOne(
		ctx,
		bson.M{
			"_id":              objectID,
			"companyId":        companyID,
			"state":            string(LineClaimClaimed),
			"awardOperationId": awardOperationID,
			"revision":         expectedRevision,
		},
		bson.M{
			"$set": bson.M{
				"state":           string(LineClaimAwarded),
				"awardRevisionId": awardRevisionID,
				"awardedAt":       now,
				"revision":        expectedRevision + 1,
			},
		},
	)
	if err != nil {
		return err
	}
	if result.MatchedCount == 1 {
		return nil
	}

	// Already awarded by this operation for this revision is the idempotent
	// path, not a failure: recovery must be able to run twice.
	current, found, findErr := repository.FindClaim(ctx, companyID, claimID)
	if findErr != nil {
		return findErr
	}
	if found && current.State == LineClaimAwarded &&
		current.AwardOperationID == awardOperationID &&
		current.AwardRevisionID == awardRevisionID {
		return nil
	}
	return ErrRFQLineAwardConflict
}

// ReleaseLineageClaim deletes an in-flight claim held by this exact operation.
//
// It is deliberately narrow. State, company and operation are all in the
// filter, so:
//
//   - an AWARDED claim is never released, by anyone, ever — that is D2, and it
//     is what stops a competitor being awarded a line a Supplier was already
//     told they won;
//   - a foreign operation can never release another's claim.
//
// The caller must additionally verify no matching Award Revision exists before
// calling this (§8E). That check lives in the finalisation service, not here
// and not in Phase E, which has no award knowledge.
func (repository *MongoAwardLineClaimRepository) ReleaseLineageClaim(
	ctx context.Context,
	companyID, claimID, awardOperationID string,
	expectedRevision int64,
) error {
	objectID, err := bson.ObjectIDFromHex(claimID)
	if err != nil {
		return ErrAwardChainNotFound
	}

	result, err := repository.collection.DeleteOne(ctx, bson.M{
		"_id":              objectID,
		"companyId":        companyID,
		"state":            string(LineClaimClaimed),
		"awardOperationId": awardOperationID,
		"revision":         expectedRevision,
	})
	if err != nil {
		return err
	}
	if result.DeletedCount == 0 {
		return ErrRFQLineAwardConflict
	}
	return nil
}
