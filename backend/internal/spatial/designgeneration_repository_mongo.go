package spatial

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// Explicit index names — every index carries one (established convention,
// internal/access/repository_mongo.go).
const (
	indexNameUniqueSpatialDesignGenerationAttemptsClientRequest = "uq_spatial_design_generation_attempts_company_session_client_request_id"
	indexNameUniqueSpatialDesignGenerationAttemptsNumber        = "uq_spatial_design_generation_attempts_company_turn_attempt_number"
	indexNameUniqueSpatialDesignGenerationAttemptsActiveSlot    = "uq_spatial_design_generation_attempts_company_turn_active_slot"
	indexNameSpatialDesignGenerationAttemptsBySession           = "idx_spatial_design_generation_attempts_company_session_created"
	indexNameSpatialDesignGenerationAttemptsByJob               = "idx_spatial_design_generation_attempts_company_job"

	indexNameUniqueSpatialDesignAcceptancesClientRequest = "uq_spatial_design_acceptances_company_session_client_request_id"
	indexNameUniqueSpatialDesignAcceptancesAttempt       = "uq_spatial_design_acceptances_company_attempt"
	indexNameSpatialDesignAcceptancesByRoomDraft         = "idx_spatial_design_acceptances_company_roomdraft_created"
)

// MongoDesignGenerationRepository owns the
// "spatial_design_generation_attempts" and "spatial_design_acceptances"
// collections, and implements DesignAcceptanceApplier against
// "spatial_room_drafts", "spatial_room_draft_edits", and
// "spatial_design_sessions" as well — ApplyAcceptance is the one operation
// that spans all five collections in a single Mongo transaction, matching
// MongoRoomDraftEditRepository.ApplyAndRecord's established "N collections,
// one atomic outcome" precedent, generalized from one edit record to a
// fixed-order batch plus the acceptance/attempt/session side effects only
// Use Design produces.
type MongoDesignGenerationRepository struct {
	client             *mongo.Client
	attemptsCollection *mongo.Collection
	acceptances        *mongo.Collection
	draftsCollection   *mongo.Collection
	editsCollection    *mongo.Collection
	sessionCollection  *mongo.Collection
}

func NewMongoDesignGenerationRepository(db *mongo.Database) *MongoDesignGenerationRepository {
	return &MongoDesignGenerationRepository{
		client:             db.Client(),
		attemptsCollection: db.Collection("spatial_design_generation_attempts"),
		acceptances:        db.Collection("spatial_design_acceptances"),
		draftsCollection:   db.Collection("spatial_room_drafts"),
		editsCollection:    db.Collection("spatial_room_draft_edits"),
		sessionCollection:  db.Collection("spatial_design_sessions"),
	}
}

func (r *MongoDesignGenerationRepository) EnsureIndexes(ctx context.Context) error {
	if _, err := r.attemptsCollection.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "companyId", Value: 1}, {Key: "sessionId", Value: 1}, {Key: "clientRequestId", Value: 1}},
			Options: options.Index().SetUnique(true).SetName(indexNameUniqueSpatialDesignGenerationAttemptsClientRequest)},
		{Keys: bson.D{{Key: "companyId", Value: 1}, {Key: "turnId", Value: 1}, {Key: "attemptNumber", Value: 1}},
			Options: options.Index().SetUnique(true).SetName(indexNameUniqueSpatialDesignGenerationAttemptsNumber)},
		{Keys: bson.D{{Key: "companyId", Value: 1}, {Key: "turnId", Value: 1}, {Key: "activeSlot", Value: 1}},
			Options: options.Index().SetUnique(true).SetName(indexNameUniqueSpatialDesignGenerationAttemptsActiveSlot).
				SetPartialFilterExpression(bson.M{"activeSlot": bson.M{"$exists": true}})},
		{Keys: bson.D{{Key: "companyId", Value: 1}, {Key: "sessionId", Value: 1}, {Key: "createdAt", Value: -1}},
			Options: options.Index().SetName(indexNameSpatialDesignGenerationAttemptsBySession)},
		{Keys: bson.D{{Key: "companyId", Value: 1}, {Key: "assetGenerationJobId", Value: 1}},
			Options: options.Index().SetName(indexNameSpatialDesignGenerationAttemptsByJob).SetSparse(true)},
	}); err != nil {
		return err
	}
	_, err := r.acceptances.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "companyId", Value: 1}, {Key: "sessionId", Value: 1}, {Key: "clientRequestId", Value: 1}},
			Options: options.Index().SetUnique(true).SetName(indexNameUniqueSpatialDesignAcceptancesClientRequest)},
		{Keys: bson.D{{Key: "companyId", Value: 1}, {Key: "attemptId", Value: 1}},
			Options: options.Index().SetUnique(true).SetName(indexNameUniqueSpatialDesignAcceptancesAttempt)},
		{Keys: bson.D{{Key: "companyId", Value: 1}, {Key: "roomDraftId", Value: 1}, {Key: "createdAt", Value: -1}},
			Options: options.Index().SetName(indexNameSpatialDesignAcceptancesByRoomDraft)},
	})
	return err
}

// --- BSON document shapes: DesignGenerationAttempt ---

type spatialDesignReferenceImageDoc struct {
	ObjectKey         string    `bson:"objectKey"`
	ChecksumSHA256    string    `bson:"checksumSha256"`
	ContentType       string    `bson:"contentType"`
	SizeBytes         int64     `bson:"sizeBytes"`
	Width             int       `bson:"width"`
	Height            int       `bson:"height"`
	Provider          string    `bson:"provider"`
	ProviderRequestID string    `bson:"providerRequestId,omitempty"`
	Model             string    `bson:"model"`
	PromptVersion     string    `bson:"promptVersion"`
	Seed              int64     `bson:"seed"`
	CreatedAt         time.Time `bson:"createdAt"`
}

func toDesignReferenceImageDoc(img DesignReferenceImage) spatialDesignReferenceImageDoc {
	return spatialDesignReferenceImageDoc{
		ObjectKey: img.ObjectKey, ChecksumSHA256: img.ChecksumSHA256, ContentType: img.ContentType,
		SizeBytes: img.SizeBytes, Width: img.Width, Height: img.Height,
		Provider: img.Provider, ProviderRequestID: img.ProviderRequestID, Model: img.Model,
		PromptVersion: img.PromptVersion, Seed: img.Seed, CreatedAt: img.CreatedAt,
	}
}

func fromDesignReferenceImageDoc(doc spatialDesignReferenceImageDoc) DesignReferenceImage {
	return DesignReferenceImage{
		ObjectKey: doc.ObjectKey, ChecksumSHA256: doc.ChecksumSHA256, ContentType: doc.ContentType,
		SizeBytes: doc.SizeBytes, Width: doc.Width, Height: doc.Height,
		Provider: doc.Provider, ProviderRequestID: doc.ProviderRequestID, Model: doc.Model,
		PromptVersion: doc.PromptVersion, Seed: doc.Seed, CreatedAt: doc.CreatedAt,
	}
}

type spatialVisualAppearanceDoc struct {
	BaseColor      string `bson:"baseColor"`
	MaterialFamily string `bson:"materialFamily"`
	Roughness      string `bson:"roughness"`
	Metallic       bool   `bson:"metallic"`
}

func toVisualAppearanceDoc(a VisualAppearance) spatialVisualAppearanceDoc {
	return spatialVisualAppearanceDoc{
		BaseColor: a.BaseColor, MaterialFamily: string(a.MaterialFamily),
		Roughness: string(a.Roughness), Metallic: a.Metallic,
	}
}

func fromVisualAppearanceDoc(doc spatialVisualAppearanceDoc) VisualAppearance {
	return VisualAppearance{
		BaseColor: doc.BaseColor, MaterialFamily: MaterialFamily(doc.MaterialFamily),
		Roughness: Roughness(doc.Roughness), Metallic: doc.Metallic,
	}
}

type spatialDesignConceptDoc struct {
	Target           spatialDesignTargetDoc      `bson:"target"`
	Transform        RoomLocalTransform          `bson:"transform"`
	Dimensions       *RoomLocalPoint             `bson:"dimensions,omitempty"`
	VisualAction     string                      `bson:"visualAction"`
	AppearanceAction string                      `bson:"appearanceAction"`
	Appearance       *spatialVisualAppearanceDoc `bson:"appearance,omitempty"`
	VisualAsset      *VisualAssetRef             `bson:"visualAsset,omitempty"`
}

func toDesignConceptDoc(c DesignConcept) spatialDesignConceptDoc {
	doc := spatialDesignConceptDoc{
		Target: toDesignTargetDoc(c.Target), Transform: c.Transform, Dimensions: c.Dimensions,
		VisualAction: string(c.VisualAction), AppearanceAction: string(c.AppearanceAction),
		VisualAsset: c.VisualAsset,
	}
	if c.Appearance != nil {
		appearanceDoc := toVisualAppearanceDoc(*c.Appearance)
		doc.Appearance = &appearanceDoc
	}
	return doc
}

func fromDesignConceptDoc(doc spatialDesignConceptDoc) DesignConcept {
	c := DesignConcept{
		Target: fromDesignTargetDoc(doc.Target), Transform: doc.Transform, Dimensions: doc.Dimensions,
		VisualAction: ConceptBindingAction(doc.VisualAction), AppearanceAction: ConceptAppearanceAction(doc.AppearanceAction),
		VisualAsset: doc.VisualAsset,
	}
	if doc.Appearance != nil {
		appearance := fromVisualAppearanceDoc(*doc.Appearance)
		c.Appearance = &appearance
	}
	return c
}

type spatialAuthorizedDesignTargetDoc struct {
	Kind             string             `bson:"kind"`
	ID               string             `bson:"id"`
	Category         string             `bson:"category"`
	Transform        RoomLocalTransform `bson:"transform"`
	Dimensions       *RoomLocalPoint    `bson:"dimensions,omitempty"`
	AttachedToWallID string             `bson:"attachedToWallId,omitempty"`
	VisualAssetBound bool               `bson:"visualAssetBound"`
}

func toAuthorizedDesignTargetDoc(t AuthorizedDesignTarget) spatialAuthorizedDesignTargetDoc {
	return spatialAuthorizedDesignTargetDoc{
		Kind: string(t.Kind), ID: t.ID, Category: t.Category, Transform: t.Transform,
		Dimensions: t.Dimensions, AttachedToWallID: t.AttachedToWallID, VisualAssetBound: t.VisualAssetBound,
	}
}

func fromAuthorizedDesignTargetDoc(doc spatialAuthorizedDesignTargetDoc) AuthorizedDesignTarget {
	return AuthorizedDesignTarget{
		Kind: DesignTargetKind(doc.Kind), ID: doc.ID, Category: doc.Category, Transform: doc.Transform,
		Dimensions: doc.Dimensions, AttachedToWallID: doc.AttachedToWallID, VisualAssetBound: doc.VisualAssetBound,
	}
}

type spatialDesignGenerationAttemptDoc struct {
	ID          bson.ObjectID `bson:"_id,omitempty"`
	CompanyID   string        `bson:"companyId"`
	ProjectID   string        `bson:"projectId"`
	SpaceID     string        `bson:"spaceId"`
	RoomDraftID string        `bson:"roomDraftId"`

	SessionID       string `bson:"sessionId"`
	TurnID          string `bson:"turnId"`
	PlanFingerprint string `bson:"planFingerprint"`
	AttemptNumber   int64  `bson:"attemptNumber"`

	ClientRequestID    string `bson:"clientRequestId"`
	RequestFingerprint string `bson:"requestFingerprint"`

	BasedOnRoomDraftRevision int64 `bson:"basedOnRoomDraftRevision"`

	Kind       string `bson:"kind"`
	Status     string `bson:"status"`
	ActiveSlot string `bson:"activeSlot,omitempty"`

	TargetSnapshot spatialAuthorizedDesignTargetDoc `bson:"targetSnapshot"`

	ReferenceProviderStartedAt *time.Time                      `bson:"referenceProviderStartedAt,omitempty"`
	ReferenceImage             *spatialDesignReferenceImageDoc `bson:"referenceImage,omitempty"`

	AssetGenerationJobID string `bson:"assetGenerationJobId,omitempty"`

	Candidate spatialDesignConceptDoc `bson:"candidate"`

	SafeFailureCode string `bson:"safeFailureCode,omitempty"`

	CancelClientRequestID    string `bson:"cancelClientRequestId,omitempty"`
	CancelRequestFingerprint string `bson:"cancelRequestFingerprint,omitempty"`

	CreatedByUserID string    `bson:"createdByUserId"`
	CreatedAt       time.Time `bson:"createdAt"`
	UpdatedAt       time.Time `bson:"updatedAt"`

	CompletedAt *time.Time `bson:"completedAt,omitempty"`
	AbandonedAt *time.Time `bson:"abandonedAt,omitempty"`
	AcceptedAt  *time.Time `bson:"acceptedAt,omitempty"`

	SchemaVersion int `bson:"schemaVersion"`
}

func toDesignGenerationAttemptDoc(a DesignGenerationAttempt) spatialDesignGenerationAttemptDoc {
	doc := spatialDesignGenerationAttemptDoc{
		CompanyID: a.CompanyID, ProjectID: a.ProjectID, SpaceID: a.SpaceID, RoomDraftID: a.RoomDraftID,
		SessionID: a.SessionID, TurnID: a.TurnID, PlanFingerprint: a.PlanFingerprint, AttemptNumber: a.AttemptNumber,
		ClientRequestID: a.ClientRequestID, RequestFingerprint: a.RequestFingerprint,
		BasedOnRoomDraftRevision: a.BasedOnRoomDraftRevision,
		Kind:                     string(a.Kind), Status: string(a.Status), ActiveSlot: a.ActiveSlot,
		TargetSnapshot:             toAuthorizedDesignTargetDoc(a.TargetSnapshot),
		ReferenceProviderStartedAt: a.ReferenceProviderStartedAt,
		AssetGenerationJobID:       a.AssetGenerationJobID,
		Candidate:                  toDesignConceptDoc(a.Candidate),
		SafeFailureCode:            a.SafeFailureCode,
		CancelClientRequestID:      a.CancelClientRequestID, CancelRequestFingerprint: a.CancelRequestFingerprint,
		CreatedByUserID: a.CreatedByUserID, CreatedAt: a.CreatedAt, UpdatedAt: a.UpdatedAt,
		CompletedAt: a.CompletedAt, AbandonedAt: a.AbandonedAt, AcceptedAt: a.AcceptedAt,
		SchemaVersion: a.SchemaVersion,
	}
	if a.ID != "" {
		objID, _ := bson.ObjectIDFromHex(a.ID)
		doc.ID = objID
	}
	if a.ReferenceImage != nil {
		imgDoc := toDesignReferenceImageDoc(*a.ReferenceImage)
		doc.ReferenceImage = &imgDoc
	}
	return doc
}

func fromDesignGenerationAttemptDoc(doc spatialDesignGenerationAttemptDoc) DesignGenerationAttempt {
	a := DesignGenerationAttempt{
		ID: doc.ID.Hex(), CompanyID: doc.CompanyID, ProjectID: doc.ProjectID, SpaceID: doc.SpaceID, RoomDraftID: doc.RoomDraftID,
		SessionID: doc.SessionID, TurnID: doc.TurnID, PlanFingerprint: doc.PlanFingerprint, AttemptNumber: doc.AttemptNumber,
		ClientRequestID: doc.ClientRequestID, RequestFingerprint: doc.RequestFingerprint,
		BasedOnRoomDraftRevision: doc.BasedOnRoomDraftRevision,
		Kind:                     DesignGenerationKind(doc.Kind), Status: DesignGenerationStatus(doc.Status), ActiveSlot: doc.ActiveSlot,
		TargetSnapshot:             fromAuthorizedDesignTargetDoc(doc.TargetSnapshot),
		ReferenceProviderStartedAt: doc.ReferenceProviderStartedAt,
		AssetGenerationJobID:       doc.AssetGenerationJobID,
		Candidate:                  fromDesignConceptDoc(doc.Candidate),
		SafeFailureCode:            doc.SafeFailureCode,
		CancelClientRequestID:      doc.CancelClientRequestID, CancelRequestFingerprint: doc.CancelRequestFingerprint,
		CreatedByUserID: doc.CreatedByUserID, CreatedAt: doc.CreatedAt, UpdatedAt: doc.UpdatedAt,
		CompletedAt: doc.CompletedAt, AbandonedAt: doc.AbandonedAt, AcceptedAt: doc.AcceptedAt,
		SchemaVersion: doc.SchemaVersion,
	}
	if doc.ReferenceImage != nil {
		img := fromDesignReferenceImageDoc(*doc.ReferenceImage)
		a.ReferenceImage = &img
	}
	return a
}

// --- BSON document shape: DesignAcceptance ---

type spatialDesignAcceptanceDoc struct {
	ID        bson.ObjectID `bson:"_id,omitempty"`
	CompanyID string        `bson:"companyId"`

	SessionID       string `bson:"sessionId"`
	TurnID          string `bson:"turnId"`
	AttemptID       string `bson:"attemptId"`
	PlanFingerprint string `bson:"planFingerprint"`

	ClientRequestID    string `bson:"clientRequestId"`
	RequestFingerprint string `bson:"requestFingerprint"`

	RoomDraftID                string `bson:"roomDraftId"`
	BaseRoomDraftRevision      int64  `bson:"baseRoomDraftRevision"`
	ResultingRoomDraftRevision int64  `bson:"resultingRoomDraftRevision"`

	AppliedOperationIDs []string `bson:"appliedOperationIds"`

	AcceptedDesign spatialWorkingDesignDoc `bson:"acceptedDesign"`

	CreatedByUserID string    `bson:"createdByUserId"`
	CreatedAt       time.Time `bson:"createdAt"`

	SchemaVersion int `bson:"schemaVersion"`
}

func toDesignAcceptanceDoc(a DesignAcceptance) spatialDesignAcceptanceDoc {
	doc := spatialDesignAcceptanceDoc{
		CompanyID: a.CompanyID, SessionID: a.SessionID, TurnID: a.TurnID, AttemptID: a.AttemptID,
		PlanFingerprint: a.PlanFingerprint, ClientRequestID: a.ClientRequestID, RequestFingerprint: a.RequestFingerprint,
		RoomDraftID: a.RoomDraftID, BaseRoomDraftRevision: a.BaseRoomDraftRevision, ResultingRoomDraftRevision: a.ResultingRoomDraftRevision,
		AppliedOperationIDs: a.AppliedOperationIDs, AcceptedDesign: toWorkingDesignDoc(a.AcceptedDesign),
		CreatedByUserID: a.CreatedByUserID, CreatedAt: a.CreatedAt, SchemaVersion: a.SchemaVersion,
	}
	if a.ID != "" {
		objID, _ := bson.ObjectIDFromHex(a.ID)
		doc.ID = objID
	}
	return doc
}

func fromDesignAcceptanceDoc(doc spatialDesignAcceptanceDoc) DesignAcceptance {
	return DesignAcceptance{
		ID: doc.ID.Hex(), CompanyID: doc.CompanyID, SessionID: doc.SessionID, TurnID: doc.TurnID, AttemptID: doc.AttemptID,
		PlanFingerprint: doc.PlanFingerprint, ClientRequestID: doc.ClientRequestID, RequestFingerprint: doc.RequestFingerprint,
		RoomDraftID: doc.RoomDraftID, BaseRoomDraftRevision: doc.BaseRoomDraftRevision, ResultingRoomDraftRevision: doc.ResultingRoomDraftRevision,
		AppliedOperationIDs: doc.AppliedOperationIDs, AcceptedDesign: fromWorkingDesignDoc(doc.AcceptedDesign),
		CreatedByUserID: doc.CreatedByUserID, CreatedAt: doc.CreatedAt, SchemaVersion: doc.SchemaVersion,
	}
}

// --- DesignGenerationAttemptRepository ---

var errDesignGenerationAlreadyRecordedConflict = errors.New("spatial: internal — design generation request conflict inside transaction")

func (r *MongoDesignGenerationRepository) ReserveAttempt(ctx context.Context, attempt DesignGenerationAttempt) (DesignGenerationAttempt, bool, error) {
	sessionObjID, err := bson.ObjectIDFromHex(attempt.SessionID)
	if err != nil {
		return DesignGenerationAttempt{}, false, ErrDesignSessionNotFound
	}

	mongoSession, err := r.client.StartSession()
	if err != nil {
		return DesignGenerationAttempt{}, false, err
	}
	defer mongoSession.EndSession(ctx)

	type txResult struct {
		attempt  DesignGenerationAttempt
		replayed bool
	}

	result, err := mongoSession.WithTransaction(ctx, func(txCtx context.Context) (any, error) {
		var existingDoc spatialDesignGenerationAttemptDoc
		findErr := r.attemptsCollection.FindOne(txCtx, bson.M{
			"companyId": attempt.CompanyID, "sessionId": attempt.SessionID, "clientRequestId": attempt.ClientRequestID,
		}).Decode(&existingDoc)
		if findErr == nil {
			existing := fromDesignGenerationAttemptDoc(existingDoc)
			if existing.RequestFingerprint != attempt.RequestFingerprint {
				return nil, errDesignGenerationAlreadyRecordedConflict
			}
			return txResult{attempt: existing, replayed: true}, nil
		}
		if !errors.Is(findErr, mongo.ErrNoDocuments) {
			return nil, findErr
		}

		attemptDoc := toDesignGenerationAttemptDoc(attempt)
		insertResult, insertErr := r.attemptsCollection.InsertOne(txCtx, attemptDoc)
		if insertErr != nil {
			return nil, insertErr
		}
		attemptDoc.ID = insertResult.InsertedID.(bson.ObjectID)

		if _, updateErr := r.sessionCollection.UpdateOne(txCtx,
			bson.M{"_id": sessionObjID, "companyId": attempt.CompanyID},
			bson.M{"$set": bson.M{"latestGenerationAttemptId": attemptDoc.ID.Hex(), "updatedAt": time.Now()}},
		); updateErr != nil {
			return nil, updateErr
		}

		return txResult{attempt: fromDesignGenerationAttemptDoc(attemptDoc), replayed: false}, nil
	})

	if err != nil {
		if errors.Is(err, errDesignGenerationAlreadyRecordedConflict) {
			return DesignGenerationAttempt{}, false, ErrDesignGenerationRequestConflict
		}
		if mongo.IsDuplicateKeyError(err) {
			return DesignGenerationAttempt{}, false, ErrDesignGenerationInProgress
		}
		return DesignGenerationAttempt{}, false, err
	}
	tx := result.(txResult)
	return tx.attempt, tx.replayed, nil
}

func (r *MongoDesignGenerationRepository) CompleteWithConcept(ctx context.Context, companyID, attemptID string, candidate DesignConcept) (DesignGenerationAttempt, error) {
	objID, err := bson.ObjectIDFromHex(attemptID)
	if err != nil {
		return DesignGenerationAttempt{}, ErrDesignGenerationAttemptNotFound
	}
	now := time.Now()
	filter := bson.M{"_id": objID, "companyId": companyID, "activeSlot": bson.M{"$exists": true}}
	update := bson.M{
		"$set": bson.M{
			"status": string(DesignGenerationStatusConceptReady), "candidate": toDesignConceptDoc(candidate),
			"completedAt": now, "updatedAt": now,
		},
		"$unset": bson.M{"activeSlot": ""},
	}
	opts := options.FindOneAndUpdate().SetReturnDocument(options.After)
	var doc spatialDesignGenerationAttemptDoc
	if err := r.attemptsCollection.FindOneAndUpdate(ctx, filter, update, opts).Decode(&doc); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return DesignGenerationAttempt{}, ErrDesignGenerationNotClaimable
		}
		return DesignGenerationAttempt{}, err
	}
	attempt := fromDesignGenerationAttemptDoc(doc)

	// Advance the session's LatestReadyAttemptID only if this attempt's
	// number is not older than whatever is already recorded for this turn —
	// an older/superseded attempt completing late never regresses it. The
	// simplest correct comparison available without a second attempt lookup
	// is "always set it to the attempt that JUST became ready," which is
	// safe here because CompleteWithConcept is only ever called on the
	// current ActiveSlot holder for its turn, and Regenerate never creates a
	// new active attempt while an older one is still non-terminal.
	sessionObjID, parseErr := bson.ObjectIDFromHex(attempt.SessionID)
	if parseErr == nil {
		_, _ = r.sessionCollection.UpdateOne(ctx,
			bson.M{"_id": sessionObjID, "companyId": companyID},
			bson.M{"$set": bson.M{"latestReadyAttemptId": attempt.ID, "updatedAt": time.Now()}},
		)
	}
	return attempt, nil
}

func (r *MongoDesignGenerationRepository) Fail(ctx context.Context, companyID, attemptID string, status DesignGenerationStatus, safeFailureCode string) (DesignGenerationAttempt, error) {
	objID, err := bson.ObjectIDFromHex(attemptID)
	if err != nil {
		return DesignGenerationAttempt{}, ErrDesignGenerationAttemptNotFound
	}
	now := time.Now()
	filter := bson.M{"_id": objID, "companyId": companyID, "activeSlot": bson.M{"$exists": true}}
	update := bson.M{
		"$set":   bson.M{"status": string(status), "safeFailureCode": safeFailureCode, "completedAt": now, "updatedAt": now},
		"$unset": bson.M{"activeSlot": ""},
	}
	opts := options.FindOneAndUpdate().SetReturnDocument(options.After)
	var doc spatialDesignGenerationAttemptDoc
	if err := r.attemptsCollection.FindOneAndUpdate(ctx, filter, update, opts).Decode(&doc); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return DesignGenerationAttempt{}, ErrDesignGenerationNotClaimable
		}
		return DesignGenerationAttempt{}, err
	}
	return fromDesignGenerationAttemptDoc(doc), nil
}

func (r *MongoDesignGenerationRepository) Cancel(ctx context.Context, companyID, attemptID, cancelClientRequestID, cancelRequestFingerprint string) (DesignGenerationAttempt, error) {
	objID, err := bson.ObjectIDFromHex(attemptID)
	if err != nil {
		return DesignGenerationAttempt{}, ErrDesignGenerationAttemptNotFound
	}

	var existing spatialDesignGenerationAttemptDoc
	if err := r.attemptsCollection.FindOne(ctx, bson.M{"_id": objID, "companyId": companyID}).Decode(&existing); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return DesignGenerationAttempt{}, ErrDesignGenerationAttemptNotFound
		}
		return DesignGenerationAttempt{}, err
	}
	if isTerminalDesignGenerationStatus(DesignGenerationStatus(existing.Status)) {
		// Idempotent: repeated cancel calls (or a cancel after the attempt
		// already reached another terminal status) return the stored
		// result unchanged rather than erroring — matching FinishTurn's
		// duplicate-completion convention.
		return fromDesignGenerationAttemptDoc(existing), nil
	}

	now := time.Now()
	filter := bson.M{"_id": objID, "companyId": companyID, "activeSlot": bson.M{"$exists": true}}
	update := bson.M{
		"$set": bson.M{
			"status": string(DesignGenerationStatusAbandoned), "abandonedAt": now, "updatedAt": now,
			"cancelClientRequestId": cancelClientRequestID, "cancelRequestFingerprint": cancelRequestFingerprint,
		},
		"$unset": bson.M{"activeSlot": ""},
	}
	opts := options.FindOneAndUpdate().SetReturnDocument(options.After)
	var doc spatialDesignGenerationAttemptDoc
	if err := r.attemptsCollection.FindOneAndUpdate(ctx, filter, update, opts).Decode(&doc); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			// Lost the race to a concurrent terminal transition — re-read
			// and return whatever landed, which is now guaranteed terminal.
			var reread spatialDesignGenerationAttemptDoc
			if rereadErr := r.attemptsCollection.FindOne(ctx, bson.M{"_id": objID, "companyId": companyID}).Decode(&reread); rereadErr != nil {
				return DesignGenerationAttempt{}, rereadErr
			}
			return fromDesignGenerationAttemptDoc(reread), nil
		}
		return DesignGenerationAttempt{}, err
	}
	return fromDesignGenerationAttemptDoc(doc), nil
}

// ClaimNextGenerationPhase implements the interface's documented claim
// filter exactly: Status=reserved AND ReferenceProviderStartedAt unset —
// this excludes both attempts that haven't reached Confirm's geometry/mixed
// branch yet (never true here, ReserveAttempt always sets reserved for
// those) and the one dangerous state (reference call started, no
// checkpoint yet). No company scoping in the filter is possible (the
// caller has no companyID yet — that's what claiming discovers), matching
// AssetGenerationJobRepository.ClaimNext's own cross-tenant claim
// precedent (the worker is a trusted internal process, never
// tenant-scoped).
func (r *MongoDesignGenerationRepository) ClaimNextGenerationPhase(ctx context.Context) (DesignGenerationAttempt, error) {
	filter := bson.M{
		"status": string(DesignGenerationStatusReserved),
		"$or": []bson.M{
			{"referenceProviderStartedAt": bson.M{"$exists": false}},
			{"referenceProviderStartedAt": nil},
		},
	}
	update := bson.M{"$set": bson.M{"status": string(DesignGenerationStatusGeneratingReference), "updatedAt": time.Now()}}
	opts := options.FindOneAndUpdate().SetReturnDocument(options.After)
	var doc spatialDesignGenerationAttemptDoc
	if err := r.attemptsCollection.FindOneAndUpdate(ctx, filter, update, opts).Decode(&doc); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return DesignGenerationAttempt{}, ErrDesignGenerationNotClaimable
		}
		return DesignGenerationAttempt{}, err
	}
	return fromDesignGenerationAttemptDoc(doc), nil
}

func (r *MongoDesignGenerationRepository) SetReferenceProviderStarted(ctx context.Context, companyID, attemptID string, startedAt time.Time) error {
	objID, err := bson.ObjectIDFromHex(attemptID)
	if err != nil {
		return ErrDesignGenerationAttemptNotFound
	}
	filter := bson.M{"_id": objID, "companyId": companyID, "activeSlot": bson.M{"$exists": true}}
	update := bson.M{"$set": bson.M{"referenceProviderStartedAt": startedAt, "updatedAt": time.Now()}}
	res, err := r.attemptsCollection.UpdateOne(ctx, filter, update)
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return ErrDesignGenerationNotClaimable
	}
	return nil
}

func (r *MongoDesignGenerationRepository) SetReferenceReady(ctx context.Context, companyID, attemptID string, image DesignReferenceImage) (DesignGenerationAttempt, error) {
	objID, err := bson.ObjectIDFromHex(attemptID)
	if err != nil {
		return DesignGenerationAttempt{}, ErrDesignGenerationAttemptNotFound
	}
	now := time.Now()
	filter := bson.M{"_id": objID, "companyId": companyID, "activeSlot": bson.M{"$exists": true}}
	update := bson.M{"$set": bson.M{
		"status": string(DesignGenerationStatusReferenceReady), "referenceImage": toDesignReferenceImageDoc(image), "updatedAt": now,
	}}
	opts := options.FindOneAndUpdate().SetReturnDocument(options.After)
	var doc spatialDesignGenerationAttemptDoc
	if err := r.attemptsCollection.FindOneAndUpdate(ctx, filter, update, opts).Decode(&doc); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return DesignGenerationAttempt{}, ErrDesignGenerationNotClaimable
		}
		return DesignGenerationAttempt{}, err
	}
	return fromDesignGenerationAttemptDoc(doc), nil
}

func (r *MongoDesignGenerationRepository) SetAssetGenerationPending(ctx context.Context, companyID, attemptID, assetGenerationJobID string) (DesignGenerationAttempt, error) {
	objID, err := bson.ObjectIDFromHex(attemptID)
	if err != nil {
		return DesignGenerationAttempt{}, ErrDesignGenerationAttemptNotFound
	}
	now := time.Now()
	filter := bson.M{"_id": objID, "companyId": companyID, "activeSlot": bson.M{"$exists": true}}
	update := bson.M{"$set": bson.M{
		"status": string(DesignGenerationStatusAssetGenerationPending), "assetGenerationJobId": assetGenerationJobID, "updatedAt": now,
	}}
	opts := options.FindOneAndUpdate().SetReturnDocument(options.After)
	var doc spatialDesignGenerationAttemptDoc
	if err := r.attemptsCollection.FindOneAndUpdate(ctx, filter, update, opts).Decode(&doc); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return DesignGenerationAttempt{}, ErrDesignGenerationNotClaimable
		}
		return DesignGenerationAttempt{}, err
	}
	return fromDesignGenerationAttemptDoc(doc), nil
}

func (r *MongoDesignGenerationRepository) SetAssetGenerationProcessing(ctx context.Context, companyID, attemptID string) (DesignGenerationAttempt, error) {
	objID, err := bson.ObjectIDFromHex(attemptID)
	if err != nil {
		return DesignGenerationAttempt{}, ErrDesignGenerationAttemptNotFound
	}
	now := time.Now()
	filter := bson.M{"_id": objID, "companyId": companyID, "activeSlot": bson.M{"$exists": true}}
	update := bson.M{"$set": bson.M{"status": string(DesignGenerationStatusAssetGenerationProcessing), "updatedAt": now}}
	opts := options.FindOneAndUpdate().SetReturnDocument(options.After)
	var doc spatialDesignGenerationAttemptDoc
	if err := r.attemptsCollection.FindOneAndUpdate(ctx, filter, update, opts).Decode(&doc); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return DesignGenerationAttempt{}, ErrDesignGenerationNotClaimable
		}
		return DesignGenerationAttempt{}, err
	}
	return fromDesignGenerationAttemptDoc(doc), nil
}

func (r *MongoDesignGenerationRepository) FindAttempt(ctx context.Context, companyID, id string) (DesignGenerationAttempt, error) {
	objID, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return DesignGenerationAttempt{}, ErrDesignGenerationAttemptNotFound
	}
	var doc spatialDesignGenerationAttemptDoc
	if err := r.attemptsCollection.FindOne(ctx, bson.M{"_id": objID, "companyId": companyID}).Decode(&doc); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return DesignGenerationAttempt{}, ErrDesignGenerationAttemptNotFound
		}
		return DesignGenerationAttempt{}, err
	}
	return fromDesignGenerationAttemptDoc(doc), nil
}

func (r *MongoDesignGenerationRepository) FindAttemptByClientRequestID(ctx context.Context, companyID, sessionID, clientRequestID string) (DesignGenerationAttempt, error) {
	var doc spatialDesignGenerationAttemptDoc
	err := r.attemptsCollection.FindOne(ctx, bson.M{
		"companyId": companyID, "sessionId": sessionID, "clientRequestId": clientRequestID,
	}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return DesignGenerationAttempt{}, ErrDesignGenerationAttemptNotFound
	}
	if err != nil {
		return DesignGenerationAttempt{}, err
	}
	return fromDesignGenerationAttemptDoc(doc), nil
}

func (r *MongoDesignGenerationRepository) ListAttempts(ctx context.Context, companyID, sessionID, turnID string, limit int) ([]DesignGenerationAttempt, error) {
	filter := bson.M{"companyId": companyID, "sessionId": sessionID}
	if turnID != "" {
		filter["turnId"] = turnID
	}
	cursor, err := r.attemptsCollection.Find(ctx, filter,
		options.Find().SetSort(bson.D{{Key: "createdAt", Value: -1}}).SetLimit(int64(limit)))
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var docs []spatialDesignGenerationAttemptDoc
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, err
	}
	out := make([]DesignGenerationAttempt, 0, len(docs))
	for _, doc := range docs {
		out = append(out, fromDesignGenerationAttemptDoc(doc))
	}
	return out, nil
}

func (r *MongoDesignGenerationRepository) FindAttemptByAssetGenerationJobID(ctx context.Context, companyID, jobID string) (DesignGenerationAttempt, error) {
	var doc spatialDesignGenerationAttemptDoc
	err := r.attemptsCollection.FindOne(ctx, bson.M{"companyId": companyID, "assetGenerationJobId": jobID}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return DesignGenerationAttempt{}, ErrDesignGenerationAttemptNotFound
	}
	if err != nil {
		return DesignGenerationAttempt{}, err
	}
	return fromDesignGenerationAttemptDoc(doc), nil
}

// --- DesignAcceptanceRepository ---

func (r *MongoDesignGenerationRepository) FindAcceptanceByClientRequestID(ctx context.Context, companyID, sessionID, clientRequestID string) (DesignAcceptance, error) {
	var doc spatialDesignAcceptanceDoc
	err := r.acceptances.FindOne(ctx, bson.M{
		"companyId": companyID, "sessionId": sessionID, "clientRequestId": clientRequestID,
	}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return DesignAcceptance{}, ErrDesignAcceptanceNotFound
	}
	if err != nil {
		return DesignAcceptance{}, err
	}
	return fromDesignAcceptanceDoc(doc), nil
}

func (r *MongoDesignGenerationRepository) FindAcceptanceByAttemptID(ctx context.Context, companyID, attemptID string) (DesignAcceptance, error) {
	var doc spatialDesignAcceptanceDoc
	err := r.acceptances.FindOne(ctx, bson.M{"companyId": companyID, "attemptId": attemptID}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return DesignAcceptance{}, ErrDesignAcceptanceNotFound
	}
	if err != nil {
		return DesignAcceptance{}, err
	}
	return fromDesignAcceptanceDoc(doc), nil
}

// --- DesignAcceptanceApplier ---

var errDesignAcceptanceAlreadyRecordedConflict = errors.New("spatial: internal — design acceptance request conflict inside transaction")

// ApplyAcceptance is Use Design's one atomic transaction: idempotency
// check first (matching every other durable pipeline in this package),
// then — only for a genuinely new request — apply input.Operations to the
// authoritative RoomDraft in fixed order under one CAS check, insert one
// RoomDraftEditRecord per applied operation, insert the DesignAcceptance,
// mark the attempt accepted, and update the session's accepted/working
// state. All five collections change together or none does.
func (r *MongoDesignGenerationRepository) ApplyAcceptance(ctx context.Context, input ApplyAcceptanceInput) (ApplyAcceptanceResult, error) {
	draftObjID, err := bson.ObjectIDFromHex(input.RoomDraftID)
	if err != nil {
		return ApplyAcceptanceResult{}, ErrRoomDraftNotFound
	}
	sessionObjID, err := bson.ObjectIDFromHex(input.SessionID)
	if err != nil {
		return ApplyAcceptanceResult{}, ErrDesignSessionNotFound
	}
	attemptObjID, err := bson.ObjectIDFromHex(input.AttemptID)
	if err != nil {
		return ApplyAcceptanceResult{}, ErrDesignGenerationAttemptNotFound
	}

	mongoSession, err := r.client.StartSession()
	if err != nil {
		return ApplyAcceptanceResult{}, err
	}
	defer mongoSession.EndSession(ctx)

	result, err := mongoSession.WithTransaction(ctx, func(txCtx context.Context) (any, error) {
		// Idempotency check FIRST, inside the transaction, before any
		// mutation — the same ordering ApplyAndRecord/ReserveTurn/
		// ReserveAttempt all use, for the same WithTransaction-silent-replay
		// safety reason documented on ApplyAndRecord.
		var existingDoc spatialDesignAcceptanceDoc
		findErr := r.acceptances.FindOne(txCtx, bson.M{
			"companyId": input.CompanyID, "sessionId": input.SessionID, "clientRequestId": input.ClientRequestID,
		}).Decode(&existingDoc)
		if findErr == nil {
			existing := fromDesignAcceptanceDoc(existingDoc)
			if existing.RequestFingerprint != input.RequestFingerprint {
				return nil, errDesignAcceptanceAlreadyRecordedConflict
			}
			var draftDoc spatialRoomDraftDoc
			if draftErr := r.draftsCollection.FindOne(txCtx, bson.M{"_id": draftObjID, "companyId": input.CompanyID}).Decode(&draftDoc); draftErr != nil {
				if errors.Is(draftErr, mongo.ErrNoDocuments) {
					return nil, ErrRoomDraftNotFound
				}
				return nil, draftErr
			}
			return ApplyAcceptanceResult{
				RoomDraft: fromRoomDraftDoc(draftDoc), Acceptance: existing,
				AppliedOperationIDs: existing.AppliedOperationIDs, Replayed: true,
			}, nil
		}
		if !errors.Is(findErr, mongo.ErrNoDocuments) {
			return nil, findErr
		}

		// Apply every operation in the caller-supplied fixed order,
		// in-memory, against ONE loaded draft snapshot — the CAS guard below
		// checks the ORIGINAL revision once, exactly like every other
		// mutation pipeline in this package (never a per-operation CAS
		// check, which would let a partial batch land at intermediate
		// revisions).
		var draftDoc spatialRoomDraftDoc
		if draftErr := r.draftsCollection.FindOne(txCtx, bson.M{"_id": draftObjID, "companyId": input.CompanyID, "revision": input.ExpectedRoomDraftRevision}).Decode(&draftDoc); draftErr != nil {
			if errors.Is(draftErr, mongo.ErrNoDocuments) {
				return nil, ErrRoomDraftRevisionMismatch
			}
			return nil, draftErr
		}
		draft := fromRoomDraftDoc(draftDoc)

		operationIDs := make([]string, 0, len(input.Operations))
		editDocs := make([]any, 0, len(input.Operations))
		now := time.Now()
		baseRevision := input.ExpectedRoomDraftRevision
		for i, op := range input.Operations {
			updated, applyErr := op.Transform(draft)
			if applyErr != nil {
				return nil, applyErr
			}
			draft = updated

			operationID := fmt.Sprintf("%s:%d", input.ClientRequestID, i)
			operationIDs = append(operationIDs, operationID)
			fingerprint := computeOperationFingerprint(op.Kind, []byte(op.Payload))
			editDocs = append(editDocs, toRoomDraftEditDoc(RoomDraftEditRecord{
				CompanyID: input.CompanyID, RoomDraftID: input.RoomDraftID,
				OperationID: operationID, OperationFingerprint: fingerprint,
				OperationKind: string(op.Kind), OperationPayload: op.Payload,
				BaseRevision: baseRevision, ResultingRevision: baseRevision + int64(i) + 1,
				ActorUserID: input.ActorUserID, CreatedAt: now, SchemaVersion: 1,
			}))
		}

		resultingRevision := baseRevision + int64(len(input.Operations))
		draftUpdate := bson.M{
			"$set": bson.M{
				"walls": draft.Walls, "openings": draft.Openings, "objects": draft.Objects,
				"fixtures": draft.Fixtures, "servicePoints": draft.ServicePoints, "constraints": draft.Constraints,
				"originalBaseline": draft.OriginalBaseline, "revision": resultingRevision, "updatedAt": now,
			},
		}
		updateFilter := bson.M{"_id": draftObjID, "companyId": input.CompanyID, "revision": baseRevision}
		opts := options.FindOneAndUpdate().SetReturnDocument(options.After)
		var updatedDraftDoc spatialRoomDraftDoc
		if updateErr := r.draftsCollection.FindOneAndUpdate(txCtx, updateFilter, draftUpdate, opts).Decode(&updatedDraftDoc); updateErr != nil {
			if errors.Is(updateErr, mongo.ErrNoDocuments) {
				return nil, ErrRoomDraftRevisionMismatch
			}
			return nil, updateErr
		}

		if len(editDocs) > 0 {
			if _, insertErr := r.editsCollection.InsertMany(txCtx, editDocs); insertErr != nil {
				return nil, insertErr
			}
		}

		acceptance := DesignAcceptance{
			CompanyID: input.CompanyID, SessionID: input.SessionID, TurnID: input.TurnID, AttemptID: input.AttemptID,
			PlanFingerprint: input.PlanFingerprint, ClientRequestID: input.ClientRequestID, RequestFingerprint: input.RequestFingerprint,
			RoomDraftID: input.RoomDraftID, BaseRoomDraftRevision: baseRevision, ResultingRoomDraftRevision: resultingRevision,
			AppliedOperationIDs: operationIDs, AcceptedDesign: input.AcceptedDesign,
			CreatedByUserID: input.ActorUserID, CreatedAt: now, SchemaVersion: 1,
		}
		acceptanceDoc := toDesignAcceptanceDoc(acceptance)
		insertResult, insertErr := r.acceptances.InsertOne(txCtx, acceptanceDoc)
		if insertErr != nil {
			return nil, insertErr
		}
		acceptanceDoc.ID = insertResult.InsertedID.(bson.ObjectID)
		acceptance = fromDesignAcceptanceDoc(acceptanceDoc)

		acceptedAt := now
		if _, updateErr := r.attemptsCollection.UpdateOne(txCtx,
			bson.M{"_id": attemptObjID, "companyId": input.CompanyID},
			bson.M{"$set": bson.M{"status": string(DesignGenerationStatusAccepted), "acceptedAt": acceptedAt, "updatedAt": now}},
		); updateErr != nil {
			return nil, updateErr
		}

		if _, updateErr := r.sessionCollection.UpdateOne(txCtx,
			bson.M{"_id": sessionObjID, "companyId": input.CompanyID},
			bson.M{"$set": bson.M{
				"acceptedDesign": toWorkingDesignDoc(input.AcceptedDesign), "acceptedTurnId": input.TurnID, "acceptedAttemptId": input.AttemptID,
				"currentWorkingDesign": toWorkingDesignDoc(input.AcceptedDesign), "basedOnRoomDraftRevision": resultingRevision,
				"updatedAt": now,
			}},
		); updateErr != nil {
			return nil, updateErr
		}

		return ApplyAcceptanceResult{
			RoomDraft: fromRoomDraftDoc(updatedDraftDoc), Acceptance: acceptance,
			AppliedOperationIDs: operationIDs, Replayed: false,
		}, nil
	})

	if err != nil {
		if errors.Is(err, errDesignAcceptanceAlreadyRecordedConflict) {
			return ApplyAcceptanceResult{}, ErrDesignAcceptanceRequestConflict
		}
		return ApplyAcceptanceResult{}, err
	}
	return result.(ApplyAcceptanceResult), nil
}
