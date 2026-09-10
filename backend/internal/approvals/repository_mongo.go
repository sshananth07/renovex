package approvals

import (
	"context"
	"errors"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// indexNameUniqueApprovalsSubject enforces exactly one Approval aggregate per
// exact subject. This is the ONLY uniqueness constraint approvals carries —
// cross-version chain terminality is the coordinator's job, not an index
// here (design spec §13).
const indexNameUniqueApprovalsSubject = "uq_approvals_subject"

// MongoApprovalRepository owns the "approvals" collection exclusively.
type MongoApprovalRepository struct {
	collection *mongo.Collection
}

func NewMongoApprovalRepository(db *mongo.Database) *MongoApprovalRepository {
	return &MongoApprovalRepository{collection: db.Collection("approvals")}
}

// EnsureIndexes creates the UNIQUE per-subject index.
func (r *MongoApprovalRepository) EnsureIndexes(ctx context.Context) error {
	_, err := r.collection.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{
			{Key: "companyId", Value: 1}, {Key: "subjectType", Value: 1},
			{Key: "subjectId", Value: 1}},
			Options: options.Index().SetUnique(true).SetName(indexNameUniqueApprovalsSubject)},
	})
	return err
}

type approvalDoc struct {
	ID              bson.ObjectID `bson:"_id,omitempty"`
	CompanyID       string        `bson:"companyId"`
	SubjectType     string        `bson:"subjectType"`
	SubjectID       string        `bson:"subjectId"`
	SubjectGroupKey string        `bson:"subjectGroupKey"`
	ActorType       string        `bson:"actorType"`
	ActorName       string        `bson:"actorName,omitempty"`
	ActorEmail      string        `bson:"actorEmail,omitempty"`
	Status          string        `bson:"status"`
	Comment         string        `bson:"comment,omitempty"`
	AccessGrantID   string        `bson:"accessGrantId"`
	Revision        int64         `bson:"revision"`
	DecidedAt       time.Time     `bson:"decidedAt"`
	SchemaVersion   int           `bson:"schemaVersion"`
}

func toApprovalDoc(a Approval) approvalDoc {
	return approvalDoc{
		CompanyID: a.CompanyID, SubjectType: a.SubjectType,
		SubjectID: a.SubjectID, SubjectGroupKey: a.SubjectGroupKey,
		ActorType: a.ActorType, ActorName: a.ActorName, ActorEmail: a.ActorEmail,
		Status: string(a.Status), Comment: a.Comment, AccessGrantID: a.AccessGrantID,
		Revision: a.Revision, DecidedAt: a.DecidedAt, SchemaVersion: a.SchemaVersion,
	}
}

func fromApprovalDoc(d approvalDoc) Approval {
	return Approval{
		ID: d.ID.Hex(), CompanyID: d.CompanyID, SubjectType: d.SubjectType,
		SubjectID: d.SubjectID, SubjectGroupKey: d.SubjectGroupKey,
		ActorType: d.ActorType, ActorName: d.ActorName, ActorEmail: d.ActorEmail,
		Status: ApprovalStatus(d.Status), Comment: d.Comment, AccessGrantID: d.AccessGrantID,
		Revision: d.Revision, DecidedAt: d.DecidedAt, SchemaVersion: d.SchemaVersion,
	}
}

func classifyApprovalCreateError(err error) error {
	if !mongo.IsDuplicateKeyError(err) {
		return err
	}
	// The only unique index on this collection is uq_approvals_subject, so any
	// duplicate-key error here is a first-decision race the service resolves
	// by re-reading.
	return ErrApprovalAlreadyExists
}

func (r *MongoApprovalRepository) Create(ctx context.Context, a Approval) (Approval, error) {
	res, err := r.collection.InsertOne(ctx, toApprovalDoc(a))
	if err != nil {
		return Approval{}, classifyApprovalCreateError(err)
	}
	a.ID = res.InsertedID.(bson.ObjectID).Hex()
	return a, nil
}

func (r *MongoApprovalRepository) FindBySubject(ctx context.Context, companyID, subjectType, subjectID string) (Approval, error) {
	var doc approvalDoc
	err := r.collection.FindOne(ctx, bson.M{
		"companyId": companyID, "subjectType": subjectType, "subjectId": subjectID,
	}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return Approval{}, ErrApprovalNotFound
	}
	if err != nil {
		return Approval{}, err
	}
	return fromApprovalDoc(doc), nil
}

// DeleteAllForCompany permanently removes every Approval owned by
// companyID. Never errors when zero documents match. Development-tool use
// only.
func (r *MongoApprovalRepository) DeleteAllForCompany(ctx context.Context, companyID string) error {
	_, err := r.collection.DeleteMany(ctx, bson.M{"companyId": companyID})
	return err
}

func (r *MongoApprovalRepository) UpdateDecision(ctx context.Context, companyID, subjectType, subjectID string, expectedRevision int64, updated Approval) (Approval, error) {
	filter := bson.M{
		"companyId": companyID, "subjectType": subjectType, "subjectId": subjectID,
		"revision": expectedRevision,
	}
	update := bson.M{
		"$set": bson.M{
			"actorType": updated.ActorType, "actorName": updated.ActorName,
			"actorEmail": updated.ActorEmail, "status": string(updated.Status),
			"comment": updated.Comment, "accessGrantId": updated.AccessGrantID,
			"decidedAt": updated.DecidedAt,
		},
		"$inc": bson.M{"revision": 1},
	}

	var doc approvalDoc
	opts := options.FindOneAndUpdate().SetReturnDocument(options.After)
	err := r.collection.FindOneAndUpdate(ctx, filter, update, opts).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return Approval{}, ErrApprovalRevisionMismatch
	}
	if err != nil {
		return Approval{}, err
	}
	return fromApprovalDoc(doc), nil
}
