package spatial

import (
	"context"
	"errors"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

type spatialAssetGenerationSourceDoc struct {
	Kind            string `bson:"kind"`
	ArtifactID      string `bson:"artifactId,omitempty"`
	DesignAttemptID string `bson:"designAttemptId,omitempty"`
	ObjectKey       string `bson:"objectKey,omitempty"`
}

func toAssetGenerationSourceDoc(src AssetGenerationSourceRef) *spatialAssetGenerationSourceDoc {
	return &spatialAssetGenerationSourceDoc{
		Kind: string(src.Kind), ArtifactID: src.ArtifactID,
		DesignAttemptID: src.DesignAttemptID, ObjectKey: src.ObjectKey,
	}
}

// fromAssetGenerationSourceDoc is the backward-compatibility read path: a
// pre-RP4E2 document has no `source` sub-document at all, only the legacy
// top-level sourceArtifactId — that decodes as {Kind: capture_artifact,
// ArtifactID: legacyArtifactID}. A document written after this change
// always has `source` populated and legacyArtifactID is ignored.
func fromAssetGenerationSourceDoc(doc *spatialAssetGenerationSourceDoc, legacyArtifactID string) AssetGenerationSourceRef {
	if doc == nil {
		return AssetGenerationSourceRef{Kind: AssetGenerationSourceKindCaptureArtifact, ArtifactID: legacyArtifactID}
	}
	return AssetGenerationSourceRef{
		Kind: AssetGenerationSourceKind(doc.Kind), ArtifactID: doc.ArtifactID,
		DesignAttemptID: doc.DesignAttemptID, ObjectKey: doc.ObjectKey,
	}
}

type spatialAssetGenerationJobDoc struct {
	ID        bson.ObjectID `bson:"_id,omitempty"`
	CompanyID string        `bson:"companyId"`
	ProjectID string        `bson:"projectId,omitempty"`

	ClientRequestID    string `bson:"clientRequestId"`
	RequestFingerprint string `bson:"requestFingerprint"`

	// SourceArtifactID is the pre-RP4E2 field name, kept unchanged on the
	// wire for backward compatibility — an existing document with no
	// `source` sub-document decodes as {Kind: capture_artifact, ArtifactID:
	// this field}. New documents always populate Source instead and leave
	// this legacy field empty.
	SourceArtifactID string                           `bson:"sourceArtifactId,omitempty"`
	Source           *spatialAssetGenerationSourceDoc `bson:"source,omitempty"`
	Seed             int64                            `bson:"seed"`

	ProviderStartedAt *time.Time `bson:"providerStartedAt,omitempty"`
	ProviderRequestID *string    `bson:"providerRequestId,omitempty"`

	GeneratedObjectKey string `bson:"generatedObjectKey,omitempty"`
	GeneratedChecksum  string `bson:"generatedChecksum,omitempty"`
	GeneratedByteCount int64  `bson:"generatedByteCount,omitempty"`

	ResultAssetID string `bson:"resultAssetId,omitempty"`
	ResultVersion int    `bson:"resultVersion,omitempty"`

	CreatedByUserID string    `bson:"createdByUserId"`
	CreatedAt       time.Time `bson:"createdAt"`

	Status      string    `bson:"status"`
	Attempt     int       `bson:"attempt"`
	MaxAttempts int       `bson:"maxAttempts"`
	AvailableAt time.Time `bson:"availableAt"`

	LeaseOwner     string     `bson:"leaseOwner,omitempty"`
	LeaseExpiresAt *time.Time `bson:"leaseExpiresAt,omitempty"`

	StartedAt   *time.Time `bson:"startedAt,omitempty"`
	CompletedAt *time.Time `bson:"completedAt,omitempty"`

	FailureCode       string     `bson:"failureCode,omitempty"`
	FailureMessage    string     `bson:"failureMessage,omitempty"`
	CancelRequestedAt *time.Time `bson:"cancelRequestedAt,omitempty"`
}

func toAssetGenerationJobDoc(j SpatialAssetGenerationJob) spatialAssetGenerationJobDoc {
	doc := spatialAssetGenerationJobDoc{
		CompanyID: j.CompanyID, ProjectID: j.ProjectID,
		ClientRequestID: j.ClientRequestID, RequestFingerprint: j.RequestFingerprint,
		Source: toAssetGenerationSourceDoc(j.Source), Seed: j.Seed,
		ProviderStartedAt: j.ProviderStartedAt, ProviderRequestID: j.ProviderRequestID,
		GeneratedObjectKey: j.GeneratedObjectKey, GeneratedChecksum: j.GeneratedChecksum,
		GeneratedByteCount: j.GeneratedByteCount,
		ResultAssetID:      j.ResultAssetID, ResultVersion: j.ResultVersion,
		CreatedByUserID: j.CreatedByUserID, CreatedAt: j.CreatedAt,
		Status: string(j.Execution.Status), Attempt: j.Execution.Attempt,
		MaxAttempts: j.Execution.MaxAttempts, AvailableAt: j.Execution.AvailableAt,
		LeaseOwner: j.Execution.LeaseOwner, LeaseExpiresAt: j.Execution.LeaseExpiresAt,
		StartedAt: j.Execution.StartedAt, CompletedAt: j.Execution.CompletedAt,
		FailureCode: j.Execution.FailureCode, FailureMessage: j.Execution.FailureMessage,
		CancelRequestedAt: j.Execution.CancelRequestedAt,
	}
	if j.ID != "" {
		if oid, err := bson.ObjectIDFromHex(j.ID); err == nil {
			doc.ID = oid
		}
	}
	return doc
}

func fromAssetGenerationJobDoc(doc spatialAssetGenerationJobDoc) SpatialAssetGenerationJob {
	return SpatialAssetGenerationJob{
		ID: doc.ID.Hex(), CompanyID: doc.CompanyID, ProjectID: doc.ProjectID,
		ClientRequestID: doc.ClientRequestID, RequestFingerprint: doc.RequestFingerprint,
		Source: fromAssetGenerationSourceDoc(doc.Source, doc.SourceArtifactID), Seed: doc.Seed,
		ProviderStartedAt: doc.ProviderStartedAt, ProviderRequestID: doc.ProviderRequestID,
		GeneratedObjectKey: doc.GeneratedObjectKey, GeneratedChecksum: doc.GeneratedChecksum,
		GeneratedByteCount: doc.GeneratedByteCount,
		ResultAssetID:      doc.ResultAssetID, ResultVersion: doc.ResultVersion,
		CreatedByUserID: doc.CreatedByUserID, CreatedAt: doc.CreatedAt,
		Execution: JobExecution{
			Status: JobExecutionStatus(doc.Status), Attempt: doc.Attempt,
			MaxAttempts: doc.MaxAttempts, AvailableAt: doc.AvailableAt,
			LeaseOwner: doc.LeaseOwner, LeaseExpiresAt: doc.LeaseExpiresAt,
			StartedAt: doc.StartedAt, CompletedAt: doc.CompletedAt,
			FailureCode: doc.FailureCode, FailureMessage: doc.FailureMessage,
			CancelRequestedAt: doc.CancelRequestedAt,
		},
	}
}

// MongoAssetGenerationJobRepository owns the "spatial_asset_generation_jobs"
// collection exclusively (RP4E0).
type MongoAssetGenerationJobRepository struct {
	collection *mongo.Collection
}

func NewMongoAssetGenerationJobRepository(db *mongo.Database) *MongoAssetGenerationJobRepository {
	return &MongoAssetGenerationJobRepository{collection: db.Collection("spatial_asset_generation_jobs")}
}

func (r *MongoAssetGenerationJobRepository) EnsureIndexes(ctx context.Context) error {
	_, err := r.collection.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "companyId", Value: 1}, {Key: "clientRequestId", Value: 1}},
			Options: options.Index().SetUnique(true).SetName(indexNameUniqueSpatialAssetGenerationJobsClientRequestID)},
		{Keys: bson.D{{Key: "status", Value: 1}, {Key: "availableAt", Value: 1}},
			Options: options.Index().SetName(indexNameSpatialAssetGenerationJobsStatusAvailableAt)},
	})
	return err
}

func (r *MongoAssetGenerationJobRepository) Create(ctx context.Context, j SpatialAssetGenerationJob) (SpatialAssetGenerationJob, error) {
	doc := toAssetGenerationJobDoc(j)
	res, err := r.collection.InsertOne(ctx, doc)
	if err == nil {
		j.ID = res.InsertedID.(bson.ObjectID).Hex()
		return j, nil
	}
	if !mongo.IsDuplicateKeyError(err) {
		return SpatialAssetGenerationJob{}, err
	}
	existing, findErr := r.FindByClientRequestID(ctx, j.CompanyID, j.ClientRequestID)
	if findErr != nil {
		return SpatialAssetGenerationJob{}, findErr
	}
	if existing.RequestFingerprint == j.RequestFingerprint {
		return existing, nil
	}
	return SpatialAssetGenerationJob{}, ErrAssetGenerationRequestFingerprintConflict
}

func (r *MongoAssetGenerationJobRepository) FindByID(ctx context.Context, companyID, id string) (SpatialAssetGenerationJob, error) {
	oid, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return SpatialAssetGenerationJob{}, ErrAssetGenerationJobNotFound
	}
	var doc spatialAssetGenerationJobDoc
	err = r.collection.FindOne(ctx, bson.M{"_id": oid, "companyId": companyID}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return SpatialAssetGenerationJob{}, ErrAssetGenerationJobNotFound
	}
	if err != nil {
		return SpatialAssetGenerationJob{}, err
	}
	return fromAssetGenerationJobDoc(doc), nil
}

func (r *MongoAssetGenerationJobRepository) FindByClientRequestID(ctx context.Context, companyID, clientRequestID string) (SpatialAssetGenerationJob, error) {
	var doc spatialAssetGenerationJobDoc
	err := r.collection.FindOne(ctx, bson.M{"companyId": companyID, "clientRequestId": clientRequestID}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return SpatialAssetGenerationJob{}, ErrAssetGenerationJobNotFound
	}
	if err != nil {
		return SpatialAssetGenerationJob{}, err
	}
	return fromAssetGenerationJobDoc(doc), nil
}

// ClaimNext's filter implements SpatialAssetGenerationJob.isSafeToClaim's
// EXACT logic (assetgenerationjob.go) as a Mongo query — see that
// function's doc comment. Any change to one MUST be mirrored in the
// other; TestMongoAssetGenerationJobRepository_ClaimNext_ExcludesDangerousState
// is the parity proof.
func (r *MongoAssetGenerationJobRepository) ClaimNext(ctx context.Context, workerID string, leaseTTL time.Duration, now time.Time) (SpatialAssetGenerationJob, error) {
	newExpiresAt := now.Add(leaseTTL)
	filter := bson.M{
		"status":      bson.M{"$in": []string{string(JobExecutionPending), string(JobExecutionProcessing)}},
		"availableAt": bson.M{"$lte": now},
		"$or": []bson.M{
			{"leaseExpiresAt": bson.M{"$exists": false}},
			{"leaseExpiresAt": nil},
			{"leaseExpiresAt": bson.M{"$lte": now}},
		},
		"$and": []bson.M{
			{"$or": []bson.M{
				{"providerStartedAt": bson.M{"$exists": false}},
				{"providerStartedAt": nil},
				{"providerRequestId": bson.M{"$exists": true, "$ne": nil}},
				{"generatedObjectKey": bson.M{"$exists": true, "$ne": ""}},
			}},
		},
	}
	update := bson.M{"$set": bson.M{
		"status":         string(JobExecutionProcessing),
		"leaseOwner":     workerID,
		"leaseExpiresAt": newExpiresAt,
	}}
	var doc spatialAssetGenerationJobDoc
	err := r.collection.FindOneAndUpdate(ctx, filter, update,
		options.FindOneAndUpdate().SetReturnDocument(options.After),
	).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return SpatialAssetGenerationJob{}, ErrAssetGenerationJobNotClaimable
	}
	if err != nil {
		return SpatialAssetGenerationJob{}, err
	}
	return fromAssetGenerationJobDoc(doc), nil
}

func (r *MongoAssetGenerationJobRepository) RenewLease(ctx context.Context, id, workerID string, newExpiresAt time.Time) error {
	return r.setFieldFenced(ctx, id, workerID, bson.M{"leaseExpiresAt": newExpiresAt})
}

func (r *MongoAssetGenerationJobRepository) SetProviderStarted(ctx context.Context, id, workerID string, startedAt time.Time) error {
	return r.setFieldFenced(ctx, id, workerID, bson.M{"providerStartedAt": startedAt})
}

func (r *MongoAssetGenerationJobRepository) SetProviderRequestID(ctx context.Context, id, workerID, eventID string) error {
	return r.setFieldFenced(ctx, id, workerID, bson.M{"providerRequestId": eventID})
}

func (r *MongoAssetGenerationJobRepository) SetGenerated(ctx context.Context, id, workerID, objectKey, checksum string, byteCount int64) error {
	return r.setFieldFenced(ctx, id, workerID, bson.M{
		"generatedObjectKey": objectKey, "generatedChecksum": checksum, "generatedByteCount": byteCount,
	})
}

func (r *MongoAssetGenerationJobRepository) Complete(ctx context.Context, id, workerID, resultAssetID string, resultVersion int) error {
	now := time.Now()
	return r.setFieldFenced(ctx, id, workerID, bson.M{
		"status": string(JobExecutionCompleted), "resultAssetId": resultAssetID,
		"resultVersion": resultVersion, "completedAt": now,
	})
}

func (r *MongoAssetGenerationJobRepository) MarkFailed(ctx context.Context, id, workerID, failureCode, failureMessage string) error {
	return r.setFieldFenced(ctx, id, workerID, bson.M{
		"status": string(JobExecutionFailed), "failureCode": failureCode, "failureMessage": failureMessage,
	})
}

func (r *MongoAssetGenerationJobRepository) MarkNeedsAttention(ctx context.Context, id, workerID, failureCode, failureMessage string) error {
	return r.setFieldFenced(ctx, id, workerID, bson.M{
		"status": string(JobExecutionNeedsAttention), "failureCode": failureCode, "failureMessage": failureMessage,
	})
}

// RequeueForRetry is lease-fenced like every other mutation, ALWAYS
// clears the lease (a requeued job must be immediately claimable by
// anyone, not blocked behind a now-meaningless lease), and enforces
// MaxAttempts server-side: if incrementing Attempt would reach or exceed
// MaxAttempts, the job goes straight to terminal Status=failed instead
// of being requeued — a two-step read-then-write (fenced find, then
// fenced update) rather than a single $inc, since the cap decision needs
// to see the CURRENT Attempt/MaxAttempts before deciding which status to
// set.
func (r *MongoAssetGenerationJobRepository) RequeueForRetry(ctx context.Context, id, workerID string, availableAt time.Time) error {
	oid, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return ErrAssetGenerationJobNotFound
	}
	var current spatialAssetGenerationJobDoc
	err = r.collection.FindOne(ctx, bson.M{"_id": oid, "leaseOwner": workerID}).Decode(&current)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return ErrAssetGenerationJobNotClaimable
	}
	if err != nil {
		return err
	}

	nextAttempt := current.Attempt + 1
	var update bson.M
	if current.MaxAttempts > 0 && nextAttempt >= current.MaxAttempts {
		update = bson.M{
			"$set": bson.M{
				"status": string(JobExecutionFailed), "failureCode": "max_attempts_exhausted",
				"failureMessage": "asset generation job exhausted its retry budget",
			},
			"$inc":   bson.M{"attempt": 1},
			"$unset": bson.M{"leaseOwner": "", "leaseExpiresAt": ""},
		}
	} else {
		update = bson.M{
			"$set":   bson.M{"status": string(JobExecutionPending), "availableAt": availableAt},
			"$inc":   bson.M{"attempt": 1},
			"$unset": bson.M{"leaseOwner": "", "leaseExpiresAt": ""},
		}
	}
	res, err := r.collection.UpdateOne(ctx, bson.M{"_id": oid, "leaseOwner": workerID}, update)
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return ErrAssetGenerationJobNotClaimable
	}
	return nil
}

// setFieldFenced is the shared LEASE-FENCED update primitive every
// mutation method (except RequeueForRetry, which needs its own two-step
// MaxAttempts logic above) uses — the filter always includes
// leaseOwner=workerID, so a worker that lost its lease gets
// ErrAssetGenerationJobNotClaimable back, never a silent no-op success.
func (r *MongoAssetGenerationJobRepository) setFieldFenced(ctx context.Context, id, workerID string, fields bson.M) error {
	oid, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return ErrAssetGenerationJobNotFound
	}
	res, err := r.collection.UpdateOne(ctx,
		bson.M{"_id": oid, "leaseOwner": workerID},
		bson.M{"$set": fields},
	)
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return ErrAssetGenerationJobNotClaimable
	}
	return nil
}
