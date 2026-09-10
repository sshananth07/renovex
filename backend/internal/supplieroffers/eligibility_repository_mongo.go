package supplieroffers

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.mongodb.org/mongo-driver/v2/mongo/readconcern"
	"go.mongodb.org/mongo-driver/v2/mongo/writeconcern"
)

const (
	collectionSupplierOfferEligibilities = "supplier_offer_eligibilities"

	indexNameUniqueSupplierOfferEligibility = "uq_supplier_offer_eligibilities_company_version"
)

// MongoOfferEligibilityRepository owns the mutable withdrawal-versus-award
// serialization gate. It is intentionally separate from immutable versions.
type MongoOfferEligibilityRepository struct {
	client      *mongo.Client
	collection  *mongo.Collection
	chains      *mongo.Collection
	transaction withdrawalTransactionHooks
}

// withdrawalTransactionHooks are deterministic test boundaries around the
// actual transactional writes. Production never configures them; they exist so
// real-Mongo tests can force abort and ordering without sleeps.
type withdrawalTransactionHooks struct {
	afterChainFence func(context.Context) error
	afterCommit     func(SupplierOfferEligibility) error
}

func NewMongoOfferEligibilityRepository(
	db *mongo.Database,
) *MongoOfferEligibilityRepository {
	return &MongoOfferEligibilityRepository{
		client: db.Client(), collection: db.Collection(collectionSupplierOfferEligibilities),
		chains: db.Collection(collectionSupplierOfferChains),
	}
}

// LatestWithdrawalClaimInput is generated once before entering MongoDB's
// retryable transaction callback. Repeated callback execution therefore
// reproduces the same claim identity, reason, and timestamp.
type LatestWithdrawalClaimInput struct {
	CompanyID        string
	OfferChainID     string
	OfferVersionID   string
	OperationID      string
	ClaimID          string
	WithdrawalReason string
	ClaimedAt        time.Time
}

// ClaimLatestEligibilityForWithdrawal is the narrow G5 transaction exception.
// It atomically fences the chain's latest-version fact and claims the separate
// eligibility gate. Immutable history and every external side effect remain
// outside this transaction and complete forward from the durable claim.
func (repository *MongoOfferEligibilityRepository) ClaimLatestEligibilityForWithdrawal(
	ctx context.Context,
	input LatestWithdrawalClaimInput,
) (SupplierOfferEligibility, error) {
	chainID, err := bson.ObjectIDFromHex(input.OfferChainID)
	if err != nil {
		return SupplierOfferEligibility{}, ErrOfferChainNotFound
	}
	if strings.TrimSpace(input.OperationID) == "" || strings.TrimSpace(input.ClaimID) == "" ||
		strings.TrimSpace(input.WithdrawalReason) == "" || input.ClaimedAt.IsZero() {
		return SupplierOfferEligibility{}, ErrInvalidOfferEligibility
	}

	session, err := repository.client.StartSession()
	if err != nil {
		return SupplierOfferEligibility{}, err
	}
	defer session.EndSession(ctx)

	result, err := session.WithTransaction(ctx, func(transactionContext context.Context) (any, error) {
		var chain supplierOfferChainDocument
		if findErr := repository.chains.FindOne(transactionContext, bson.M{
			"_id": chainID, "companyId": input.CompanyID,
		}).Decode(&chain); findErr != nil {
			if errors.Is(findErr, mongo.ErrNoDocuments) {
				return nil, ErrOfferChainNotFound
			}
			return nil, findErr
		}

		var eligibility supplierOfferEligibilityDocument
		if findErr := repository.collection.FindOne(transactionContext, bson.M{
			"companyId": input.CompanyID, "offerVersionId": input.OfferVersionID,
		}).Decode(&eligibility); findErr != nil {
			if errors.Is(findErr, mongo.ErrNoDocuments) {
				return nil, ErrInvalidOfferEligibility
			}
			return nil, findErr
		}

		latestID := ""
		if chain.LatestSubmittedID != nil {
			latestID = *chain.LatestSubmittedID
		}
		decision := EvaluateOfferVersionState(OfferVersionStateFacts{
			OfferVersionID: input.OfferVersionID, LatestSubmittedVersionID: latestID,
			EligibilityState: eligibility.State,
		})
		if !decision.CanWithdraw {
			return nil, ErrOfferEligibilityConflict
		}

		// This write deliberately leaves LatestSubmittedID unchanged. Incrementing
		// the concurrency revision makes a submission using a stale chain snapshot
		// conflict with this transaction and retry from the authoritative V1/V2 fact.
		fence, fenceErr := repository.chains.UpdateOne(transactionContext, bson.M{
			"_id": chainID, "companyId": input.CompanyID,
			"latestSubmittedId": input.OfferVersionID, "revision": chain.Revision,
		}, bson.M{
			"$inc": bson.M{"revision": 1, "withdrawalFence": 1},
			"$set": bson.M{"updatedAt": input.ClaimedAt},
		})
		if fenceErr != nil {
			return nil, fenceErr
		}
		if fence.ModifiedCount != 1 {
			return nil, ErrOfferEligibilityConflict
		}
		if repository.transaction.afterChainFence != nil {
			if hookErr := repository.transaction.afterChainFence(transactionContext); hookErr != nil {
				return nil, hookErr
			}
		}

		var claimed supplierOfferEligibilityDocument
		claimErr := repository.collection.FindOneAndUpdate(transactionContext, bson.M{
			"companyId": input.CompanyID, "offerVersionId": input.OfferVersionID,
			"state": EligibilityEligible, "revision": eligibility.Revision,
		}, bson.M{"$set": bson.M{
			"state":       EligibilityWithdrawalClaimed,
			"claimType":   EligibilityClaimWithdrawal,
			"operationId": input.OperationID, "claimId": input.ClaimID,
			"withdrawalReason": strings.TrimSpace(input.WithdrawalReason),
			"claimedAt":        input.ClaimedAt, "revision": eligibility.Revision + 1,
		}}, options.FindOneAndUpdate().SetReturnDocument(options.After)).Decode(&claimed)
		if claimErr != nil {
			if errors.Is(claimErr, mongo.ErrNoDocuments) {
				return nil, ErrOfferEligibilityConflict
			}
			return nil, claimErr
		}
		return supplierOfferEligibilityFromDocument(claimed), nil
	}, options.Transaction().
		SetReadConcern(readconcern.Snapshot()).
		SetWriteConcern(writeconcern.Majority()))
	if err != nil {
		return SupplierOfferEligibility{}, classifyWithdrawalTransactionError(err)
	}
	claimed, ok := result.(SupplierOfferEligibility)
	if !ok {
		return SupplierOfferEligibility{}, ErrInvalidOfferEligibility
	}
	if repository.transaction.afterCommit != nil {
		if hookErr := repository.transaction.afterCommit(claimed); hookErr != nil {
			return SupplierOfferEligibility{}, hookErr
		}
	}
	return claimed, nil
}

// VerifyWithdrawalTransactionSupport fails startup clearly when MongoDB is a
// standalone deployment. The read-only transaction avoids mutating business
// data while still requiring the server topology needed by the G5 boundary.
func (repository *MongoOfferEligibilityRepository) VerifyWithdrawalTransactionSupport(
	ctx context.Context,
) error {
	session, err := repository.client.StartSession()
	if err != nil {
		return err
	}
	defer session.EndSession(ctx)
	_, err = session.WithTransaction(ctx, func(transactionContext context.Context) (any, error) {
		_, countErr := repository.chains.CountDocuments(transactionContext, bson.M{},
			options.Count().SetLimit(1))
		return nil, countErr
	}, options.Transaction().
		SetReadConcern(readconcern.Snapshot()).
		SetWriteConcern(writeconcern.Majority()))
	return classifyWithdrawalTransactionError(err)
}

func classifyWithdrawalTransactionError(err error) error {
	if err == nil {
		return nil
	}
	message := strings.ToLower(err.Error())
	if strings.Contains(message, "transaction numbers are only allowed") ||
		strings.Contains(message, "replica set") && strings.Contains(message, "transaction") {
		return fmt.Errorf("%w: %v", ErrOfferTransactionsUnavailable, err)
	}
	return err
}

func (repository *MongoOfferEligibilityRepository) EnsureIndexes(
	ctx context.Context,
) error {
	_, err := repository.collection.Indexes().CreateMany(ctx, []mongo.IndexModel{{
		Keys: bson.D{
			{Key: "companyId", Value: 1},
			{Key: "offerVersionId", Value: 1},
		},
		Options: options.Index().
			SetName(indexNameUniqueSupplierOfferEligibility).
			SetUnique(true),
	}, {Keys: bson.D{
		{Key: "companyId", Value: 1}, {Key: "offerChainId", Value: 1},
		{Key: "operationId", Value: 1},
	}, Options: options.Index().SetName("ix_supplier_offer_eligibility_company_chain_operation")}})
	return err
}

type supplierOfferEligibilityDocument struct {
	ID               string               `bson:"_id"`
	CompanyID        string               `bson:"companyId"`
	OfferChainID     string               `bson:"offerChainId"`
	OfferVersionID   string               `bson:"offerVersionId"`
	State            EligibilityState     `bson:"state"`
	ClaimType        EligibilityClaimType `bson:"claimType,omitempty"`
	OperationID      string               `bson:"operationId,omitempty"`
	ClaimID          string               `bson:"claimId,omitempty"`
	WithdrawalReason string               `bson:"withdrawalReason,omitempty"`
	Revision         int64                `bson:"revision"`
	ClaimedAt        *time.Time           `bson:"claimedAt,omitempty"`
	CompletedAt      *time.Time           `bson:"completedAt,omitempty"`
}

func supplierOfferEligibilityToDocument(
	eligibility SupplierOfferEligibility,
) supplierOfferEligibilityDocument {
	return supplierOfferEligibilityDocument{
		ID:               eligibility.ID,
		CompanyID:        eligibility.CompanyID,
		OfferChainID:     eligibility.OfferChainID,
		OfferVersionID:   eligibility.OfferVersionID,
		State:            eligibility.State,
		ClaimType:        eligibility.ClaimType,
		OperationID:      eligibility.OperationID,
		ClaimID:          eligibility.ClaimID,
		WithdrawalReason: eligibility.WithdrawalReason,
		Revision:         eligibility.Revision,
		ClaimedAt:        eligibility.ClaimedAt,
		CompletedAt:      eligibility.CompletedAt,
	}
}

func supplierOfferEligibilityFromDocument(
	document supplierOfferEligibilityDocument,
) SupplierOfferEligibility {
	return SupplierOfferEligibility{
		ID:               document.ID,
		CompanyID:        document.CompanyID,
		OfferChainID:     document.OfferChainID,
		OfferVersionID:   document.OfferVersionID,
		State:            document.State,
		ClaimType:        document.ClaimType,
		OperationID:      document.OperationID,
		ClaimID:          document.ClaimID,
		WithdrawalReason: document.WithdrawalReason,
		Revision:         document.Revision,
		ClaimedAt:        document.ClaimedAt,
		CompletedAt:      document.CompletedAt,
	}
}

// DeleteAllForCompany permanently removes every SupplierOfferEligibility
// owned by companyID. Never errors when zero documents match.
// Development-tool use only.
func (repository *MongoOfferEligibilityRepository) DeleteAllForCompany(ctx context.Context, companyID string) error {
	_, err := repository.collection.DeleteMany(ctx, bson.M{"companyId": companyID})
	return err
}

func (repository *MongoOfferEligibilityRepository) InsertEligibility(
	ctx context.Context,
	eligibility SupplierOfferEligibility,
) error {
	// Reject a malformed recoverable state before it reaches the collection;
	// repository CAS filters cannot repair missing claim identity later.
	if err := eligibility.Validate(); err != nil {
		return err
	}
	_, err := repository.collection.InsertOne(
		ctx,
		supplierOfferEligibilityToDocument(eligibility),
	)
	return err
}

func (repository *MongoOfferEligibilityRepository) FindEligibility(
	ctx context.Context,
	companyID string,
	offerVersionID string,
) (SupplierOfferEligibility, bool, error) {
	var document supplierOfferEligibilityDocument
	err := repository.collection.FindOne(ctx, bson.M{
		"companyId":      companyID,
		"offerVersionId": offerVersionID,
	}).Decode(&document)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return SupplierOfferEligibility{}, false, nil
	}
	if err != nil {
		return SupplierOfferEligibility{}, false, err
	}
	return supplierOfferEligibilityFromDocument(document), true, nil
}

func (repository *MongoOfferEligibilityRepository) FindEligibilityByOperation(
	ctx context.Context, companyID, offerChainID, operationID string,
) (SupplierOfferEligibility, bool, error) {
	var document supplierOfferEligibilityDocument
	err := repository.collection.FindOne(ctx, bson.M{
		"companyId": companyID, "offerChainId": offerChainID,
		"operationId": operationID,
	}).Decode(&document)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return SupplierOfferEligibility{}, false, nil
	}
	if err != nil {
		return SupplierOfferEligibility{}, false, err
	}
	return supplierOfferEligibilityFromDocument(document), true, nil
}

func (repository *MongoOfferEligibilityRepository) FindLiveClaimForChain(
	ctx context.Context, companyID, offerChainID string,
) (SupplierOfferEligibility, bool, error) {
	var document supplierOfferEligibilityDocument
	err := repository.collection.FindOne(ctx, bson.M{
		"companyId": companyID, "offerChainId": offerChainID,
		"state": bson.M{"$in": bson.A{
			EligibilityWithdrawalClaimed, EligibilityAwardClaimed,
		}},
	}).Decode(&document)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return SupplierOfferEligibility{}, false, nil
	}
	if err != nil {
		return SupplierOfferEligibility{}, false, err
	}
	return supplierOfferEligibilityFromDocument(document), true, nil
}

// Eligibility claim lifecycle (spec §7).
//
// The eligibility gate is a SEPARATE mutable aggregate precisely so withdrawal
// and award never mutate a frozen Offer Version. Withdrawal and award compete
// on this one document, so exactly one can win: a Supplier cannot withdraw an
// offer the contractor is awarding, and an award cannot land on an offer
// already withdrawn.

// EligibilityClaimInput claims an eligible gate for withdrawal or award.
type EligibilityClaimInput struct {
	CompanyID        string
	OfferVersionID   string
	ExpectedRevision int64
	ClaimType        EligibilityClaimType
	OperationID      string
	ClaimID          string
	WithdrawalReason string
	ClaimedAt        time.Time
}

// EligibilityCompletionInput moves a claim to its terminal state.
type EligibilityCompletionInput struct {
	CompanyID        string
	OfferVersionID   string
	ExpectedRevision int64
	ClaimType        EligibilityClaimType
	OperationID      string
	CompletedAt      time.Time
}

// EligibilityReleaseInput is the ONLY compensating transition, and it exists
// solely for a failed multi-version award finalisation.
type EligibilityReleaseInput struct {
	CompanyID        string
	OfferVersionID   string
	ExpectedRevision int64
	OperationID      string
	ClaimID          string
	ReleasedAt       time.Time
}

// ClaimEligibility performs eligible -> withdrawal_claimed | award_claimed.
//
// State is part of the FILTER, so the second of two concurrent claims finds no
// match and loses cleanly rather than overwriting the winner.
func (repository *MongoOfferEligibilityRepository) ClaimEligibility(
	ctx context.Context,
	input EligibilityClaimInput,
) (SupplierOfferEligibility, error) {
	claimedState, err := claimedStateFor(input.ClaimType)
	if err != nil {
		return SupplierOfferEligibility{}, err
	}
	// A withdrawal must explain itself; an award must not carry a withdrawal
	// reason, so the two claim kinds can never impersonate one another.
	reason := strings.TrimSpace(input.WithdrawalReason)
	if input.ClaimType == EligibilityClaimWithdrawal {
		if reason == "" || len(reason) > MaxWithdrawalReasonLength {
			return SupplierOfferEligibility{}, ErrInvalidOfferEligibility
		}
	} else if reason != "" {
		return SupplierOfferEligibility{}, ErrInvalidOfferEligibility
	}
	if strings.TrimSpace(input.OperationID) == "" ||
		strings.TrimSpace(input.ClaimID) == "" {
		return SupplierOfferEligibility{}, ErrInvalidOfferEligibility
	}

	var document supplierOfferEligibilityDocument
	err = repository.collection.FindOneAndUpdate(
		ctx,
		bson.M{
			"companyId":      input.CompanyID,
			"offerVersionId": input.OfferVersionID,
			"state":          EligibilityEligible,
			"revision":       input.ExpectedRevision,
		},
		bson.M{
			"$set": bson.M{
				"state":            claimedState,
				"claimType":        input.ClaimType,
				"operationId":      input.OperationID,
				"claimId":          input.ClaimID,
				"withdrawalReason": reason,
				"claimedAt":        input.ClaimedAt,
				"revision":         input.ExpectedRevision + 1,
			},
		},
		options.FindOneAndUpdate().SetReturnDocument(options.After),
	).Decode(&document)
	if err == nil {
		return supplierOfferEligibilityFromDocument(document), nil
	}
	if !errors.Is(err, mongo.ErrNoDocuments) {
		return SupplierOfferEligibility{}, err
	}

	// Classify the miss so a same-operation retry converges without another
	// increment, while a genuinely competing claim still conflicts.
	current, found, findErr := repository.FindEligibility(
		ctx, input.CompanyID, input.OfferVersionID)
	if findErr != nil {
		return SupplierOfferEligibility{}, findErr
	}
	if !found {
		return SupplierOfferEligibility{}, ErrInvalidOfferEligibility
	}
	if current.ClaimType == input.ClaimType &&
		current.OperationID == input.OperationID &&
		current.ClaimID == input.ClaimID {
		return current, nil
	}
	return SupplierOfferEligibility{}, ErrOfferEligibilityConflict
}

// CompleteEligibilityClaim performs claimed -> withdrawn | awarded.
func (repository *MongoOfferEligibilityRepository) CompleteEligibilityClaim(
	ctx context.Context,
	input EligibilityCompletionInput,
) (SupplierOfferEligibility, error) {
	claimedState, err := claimedStateFor(input.ClaimType)
	if err != nil {
		return SupplierOfferEligibility{}, err
	}
	completedState := EligibilityWithdrawn
	if input.ClaimType == EligibilityClaimAward {
		completedState = EligibilityAwarded
	}

	var document supplierOfferEligibilityDocument
	err = repository.collection.FindOneAndUpdate(
		ctx,
		bson.M{
			"companyId":      input.CompanyID,
			"offerVersionId": input.OfferVersionID,
			"state":          claimedState,
			"revision":       input.ExpectedRevision,
			// Only the owning operation may complete its own claim.
			"claimType":   input.ClaimType,
			"operationId": input.OperationID,
		},
		bson.M{
			"$set": bson.M{
				"state":       completedState,
				"completedAt": input.CompletedAt,
				"revision":    input.ExpectedRevision + 1,
			},
		},
		options.FindOneAndUpdate().SetReturnDocument(options.After),
	).Decode(&document)
	if err == nil {
		return supplierOfferEligibilityFromDocument(document), nil
	}
	if !errors.Is(err, mongo.ErrNoDocuments) {
		return SupplierOfferEligibility{}, err
	}

	current, found, findErr := repository.FindEligibility(
		ctx, input.CompanyID, input.OfferVersionID)
	if findErr != nil {
		return SupplierOfferEligibility{}, findErr
	}
	// An already-completed claim owned by this operation is the idempotent path.
	if found && current.State == completedState &&
		current.OperationID == input.OperationID {
		return current, nil
	}
	return SupplierOfferEligibility{}, ErrOfferEligibilityConflict
}

// ReleaseAwardClaim performs award_claimed -> eligible.
//
// This is the only compensating transition in the aggregate, and it is
// deliberately narrow: ClaimType, operation ID and candidate claim ID are all
// in the filter. A withdrawal claim can never be released, because withdrawal
// is the Supplier's decision and must not be silently undone by the platform.
func (repository *MongoOfferEligibilityRepository) ReleaseAwardClaim(
	ctx context.Context,
	input EligibilityReleaseInput,
) (SupplierOfferEligibility, error) {
	var document supplierOfferEligibilityDocument
	err := repository.collection.FindOneAndUpdate(
		ctx,
		bson.M{
			"companyId":      input.CompanyID,
			"offerVersionId": input.OfferVersionID,
			"state":          EligibilityAwardClaimed,
			"revision":       input.ExpectedRevision,
			"claimType":      EligibilityClaimAward,
			"operationId":    input.OperationID,
			"claimId":        input.ClaimID,
		},
		bson.M{
			"$set": bson.M{
				"state":    EligibilityEligible,
				"revision": input.ExpectedRevision + 1,
			},
			"$unset": bson.M{
				"claimType":        "",
				"operationId":      "",
				"claimId":          "",
				"withdrawalReason": "",
				"claimedAt":        "",
				"completedAt":      "",
			},
		},
		options.FindOneAndUpdate().SetReturnDocument(options.After),
	).Decode(&document)
	if err == nil {
		return supplierOfferEligibilityFromDocument(document), nil
	}
	if !errors.Is(err, mongo.ErrNoDocuments) {
		return SupplierOfferEligibility{}, err
	}
	return SupplierOfferEligibility{}, ErrInvalidOfferEligibility
}

func claimedStateFor(claimType EligibilityClaimType) (EligibilityState, error) {
	switch claimType {
	case EligibilityClaimWithdrawal:
		return EligibilityWithdrawalClaimed, nil
	case EligibilityClaimAward:
		return EligibilityAwardClaimed, nil
	default:
		return "", ErrInvalidOfferEligibility
	}
}
