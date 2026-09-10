package spatial

import (
	"context"
	"errors"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// Explicit index names — every index carries one (design pattern established
// in internal/access/repository_mongo.go).
const (
	indexNameUniqueSpatialRoomDraftEditsOperation = "uq_spatial_room_draft_edits_company_operation"
	indexNameSpatialRoomDraftEditsByDraft         = "idx_spatial_room_draft_edits_company_draft"
)

// MongoRoomDraftEditRepository owns the "spatial_room_draft_edits"
// collection exclusively, and additionally implements RoomDraftEditApplier
// against the "spatial_room_drafts" collection RoomDraftRepository owns —
// a single Mongo transaction spans both, matching
// supplieroffers/eligibility_repository_mongo.go's established
// session.WithTransaction precedent for exactly this "two collections, one
// atomic outcome" requirement (plan §RP4B: "ensure RoomDraft CAS
// persistence and record creation cannot produce an unrecorded successful
// edit").
type MongoRoomDraftEditRepository struct {
	client           *mongo.Client
	editsCollection  *mongo.Collection
	draftsCollection *mongo.Collection
}

// NewMongoRoomDraftEditRepository takes db (not just a collection) because
// ApplyAndRecord's transaction spans two collections in the same database —
// db.Client() is how supplieroffers' equivalent repository obtains its
// *mongo.Client without changing composition.BuildServices' signature.
func NewMongoRoomDraftEditRepository(db *mongo.Database) *MongoRoomDraftEditRepository {
	return &MongoRoomDraftEditRepository{
		client:           db.Client(),
		editsCollection:  db.Collection("spatial_room_draft_edits"),
		draftsCollection: db.Collection("spatial_room_drafts"),
	}
}

func (r *MongoRoomDraftEditRepository) EnsureIndexes(ctx context.Context) error {
	_, err := r.editsCollection.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "companyId", Value: 1}, {Key: "operationId", Value: 1}},
			Options: options.Index().SetUnique(true).SetName(indexNameUniqueSpatialRoomDraftEditsOperation)},
		{Keys: bson.D{{Key: "companyId", Value: 1}, {Key: "roomDraftId", Value: 1}, {Key: "createdAt", Value: -1}},
			Options: options.Index().SetName(indexNameSpatialRoomDraftEditsByDraft)},
	})
	return err
}

type spatialRoomDraftEditDoc struct {
	ID                   bson.ObjectID `bson:"_id,omitempty"`
	CompanyID            string        `bson:"companyId"`
	RoomDraftID          string        `bson:"roomDraftId"`
	OperationID          string        `bson:"operationId"`
	OperationFingerprint string        `bson:"operationFingerprint"`
	OperationKind        string        `bson:"operationKind"`
	OperationPayload     string        `bson:"operationPayload"`
	BaseRevision         int64         `bson:"baseRevision"`
	ResultingRevision    int64         `bson:"resultingRevision"`
	ActorUserID          string        `bson:"actorUserId,omitempty"`
	CreatedAt            time.Time     `bson:"createdAt"`
	SchemaVersion        int           `bson:"schemaVersion"`
}

func toRoomDraftEditDoc(rec RoomDraftEditRecord) spatialRoomDraftEditDoc {
	doc := spatialRoomDraftEditDoc{
		CompanyID: rec.CompanyID, RoomDraftID: rec.RoomDraftID,
		OperationID: rec.OperationID, OperationFingerprint: rec.OperationFingerprint,
		OperationKind: rec.OperationKind, OperationPayload: rec.OperationPayload,
		BaseRevision: rec.BaseRevision, ResultingRevision: rec.ResultingRevision,
		ActorUserID: rec.ActorUserID, CreatedAt: rec.CreatedAt, SchemaVersion: rec.SchemaVersion,
	}
	if rec.ID != "" {
		objID, _ := bson.ObjectIDFromHex(rec.ID)
		doc.ID = objID
	}
	return doc
}

func fromRoomDraftEditDoc(doc spatialRoomDraftEditDoc) RoomDraftEditRecord {
	return RoomDraftEditRecord{
		ID: doc.ID.Hex(), CompanyID: doc.CompanyID, RoomDraftID: doc.RoomDraftID,
		OperationID: doc.OperationID, OperationFingerprint: doc.OperationFingerprint,
		OperationKind: doc.OperationKind, OperationPayload: doc.OperationPayload,
		BaseRevision: doc.BaseRevision, ResultingRevision: doc.ResultingRevision,
		ActorUserID: doc.ActorUserID, CreatedAt: doc.CreatedAt, SchemaVersion: doc.SchemaVersion,
	}
}

func (r *MongoRoomDraftEditRepository) FindByOperationID(ctx context.Context, companyID, operationID string) (RoomDraftEditRecord, error) {
	var doc spatialRoomDraftEditDoc
	err := r.editsCollection.FindOne(ctx, bson.M{"companyId": companyID, "operationId": operationID}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return RoomDraftEditRecord{}, ErrRoomDraftEditRecordNotFound
	}
	if err != nil {
		return RoomDraftEditRecord{}, err
	}
	return fromRoomDraftEditDoc(doc), nil
}

func (r *MongoRoomDraftEditRepository) ListByRoomDraft(ctx context.Context, companyID, roomDraftID string) ([]RoomDraftEditRecord, error) {
	cursor, err := r.editsCollection.Find(ctx,
		bson.M{"companyId": companyID, "roomDraftId": roomDraftID},
		options.Find().SetSort(bson.D{{Key: "createdAt", Value: -1}}))
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var docs []spatialRoomDraftEditDoc
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, err
	}
	out := make([]RoomDraftEditRecord, 0, len(docs))
	for _, doc := range docs {
		out = append(out, fromRoomDraftEditDoc(doc))
	}
	return out, nil
}

// errOperationAlreadyRecordedConflict is a sentinel used ONLY to unwind
// out of the WithTransaction callback when the in-transaction idempotency
// check (see ApplyAndRecord's doc comment) finds a conflicting existing
// record — distinct from the exported ErrOperationIDConflict so the
// callback's early-return path is unambiguous from any other error
// WithTransaction might see, and translated back to ErrOperationIDConflict
// once outside the transaction.
var errOperationAlreadyRecordedConflict = errors.New("spatial: internal — operation id conflict inside transaction")

// ApplyAndRecord runs the CAS-guarded RoomDraft update and the
// RoomDraftEditRecord insert inside one Mongo multi-document transaction —
// either both land or neither does.
//
// IMPORTANT: session.WithTransaction may SILENTLY RE-RUN this callback
// end-to-end if the driver classifies an error as a
// TransientTransactionError (which a duplicate-key violation on a unique
// index, encountered inside a transaction on a replica set, commonly is) —
// this is documented driver behavior, not a bug to work around via manual
// transaction management. The callback is therefore written to be
// IDEMPOTENT UNDER REPLAY: it checks FindByOperationID INSIDE the
// transaction, as the very first thing, before touching the RoomDraft at
// all. If the record already exists (either because a concurrent
// transaction committed it first, or because the driver is re-running this
// exact callback after an earlier attempt's insert actually succeeded but
// surfaced as transient), this returns that record's result directly with
// zero mutation — safe to run any number of times. Only when no record
// exists yet does it proceed to CAS-update the draft and insert the new
// record. Matches AwardRevision.InsertRevision's precedent in spirit (an
// unknown-outcome retry resolves by operation ID) while additionally
// respecting WithTransaction's specific replay contract, which a
// post-transaction duplicate-key catch does NOT reliably observe (proven
// by two initially-failing tests during implementation — see
// ROOMPLAN_HARDWARE_HANDOFF.md's RP4B ledger entry).
//
// The exported unique index on (companyId, operationId) remains the
// authoritative final race guard for two genuinely concurrent FIRST
// attempts that both pass the in-transaction existence check under their
// own snapshots — one of their inserts still wins the index, and the
// loser's transaction is retried by the driver, at which point THIS SAME
// idempotent-replay logic (not a separate code path) resolves it: the
// retried callback re-checks FindByOperationID, now sees the winner's
// committed record, and adopts it.
func (r *MongoRoomDraftEditRepository) ApplyAndRecord(ctx context.Context, companyID, roomDraftID string, updatedDraft RoomDraft, expectedRevision int64, record RoomDraftEditRecord) (RoomDraft, RoomDraftEditRecord, bool, error) {
	objID, err := bson.ObjectIDFromHex(roomDraftID)
	if err != nil {
		return RoomDraft{}, RoomDraftEditRecord{}, false, ErrRoomDraftNotFound
	}

	session, err := r.client.StartSession()
	if err != nil {
		return RoomDraft{}, RoomDraftEditRecord{}, false, err
	}
	defer session.EndSession(ctx)

	type txResult struct {
		draft    RoomDraft
		record   RoomDraftEditRecord
		replayed bool
	}

	result, err := session.WithTransaction(ctx, func(transactionContext context.Context) (any, error) {
		// Idempotency check FIRST, inside the transaction, before any
		// RoomDraft mutation — see the doc comment above for why this
		// ordering (not a post-transaction duplicate-key catch) is what
		// makes this callback safe under WithTransaction's silent-replay
		// behavior.
		var existingEditDoc spatialRoomDraftEditDoc
		findErr := r.editsCollection.FindOne(transactionContext, bson.M{"companyId": companyID, "operationId": record.OperationID}).Decode(&existingEditDoc)
		if findErr == nil {
			existing := fromRoomDraftEditDoc(existingEditDoc)
			if existing.RoomDraftID != record.RoomDraftID || existing.OperationFingerprint != record.OperationFingerprint {
				return nil, errOperationAlreadyRecordedConflict
			}
			var draftDoc spatialRoomDraftDoc
			if draftErr := r.draftsCollection.FindOne(transactionContext, bson.M{"_id": objID, "companyId": companyID}).Decode(&draftDoc); draftErr != nil {
				if errors.Is(draftErr, mongo.ErrNoDocuments) {
					return nil, ErrRoomDraftNotFound
				}
				return nil, draftErr
			}
			return txResult{draft: fromRoomDraftDoc(draftDoc), record: existing, replayed: true}, nil
		}
		if !errors.Is(findErr, mongo.ErrNoDocuments) {
			return nil, findErr
		}

		draftFilter := bson.M{"_id": objID, "companyId": companyID, "revision": expectedRevision}
		draftUpdate := bson.M{
			"$set": bson.M{
				"walls": updatedDraft.Walls, "openings": updatedDraft.Openings, "objects": updatedDraft.Objects,
				"fixtures": updatedDraft.Fixtures, "servicePoints": updatedDraft.ServicePoints, "constraints": updatedDraft.Constraints,
				"originalBaseline": updatedDraft.OriginalBaseline, "updatedAt": time.Now(),
			},
			"$inc": bson.M{"revision": 1},
		}
		opts := options.FindOneAndUpdate().SetReturnDocument(options.After)
		var draftDoc spatialRoomDraftDoc
		if updateErr := r.draftsCollection.FindOneAndUpdate(transactionContext, draftFilter, draftUpdate, opts).Decode(&draftDoc); updateErr != nil {
			if errors.Is(updateErr, mongo.ErrNoDocuments) {
				return nil, ErrRoomDraftRevisionMismatch
			}
			return nil, updateErr
		}

		editDoc := toRoomDraftEditDoc(record)
		insertResult, insertErr := r.editsCollection.InsertOne(transactionContext, editDoc)
		if insertErr != nil {
			// A concurrent transaction's own insert may win the unique
			// index right here; WithTransaction will classify this as
			// transient and silently re-run the whole callback, which will
			// then take the existence-check branch above and adopt the
			// winner's record. No special handling needed at this call
			// site — returning the raw error is exactly right.
			return nil, insertErr
		}
		editDoc.ID = insertResult.InsertedID.(bson.ObjectID)

		return txResult{draft: fromRoomDraftDoc(draftDoc), record: fromRoomDraftEditDoc(editDoc), replayed: false}, nil
	})

	if err != nil {
		if errors.Is(err, errOperationAlreadyRecordedConflict) {
			return RoomDraft{}, RoomDraftEditRecord{}, false, ErrOperationIDConflict
		}
		return RoomDraft{}, RoomDraftEditRecord{}, false, err
	}
	tx := result.(txResult)
	return tx.draft, tx.record, tx.replayed, nil
}
