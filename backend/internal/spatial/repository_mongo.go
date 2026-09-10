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
	indexNameSpatialCapturesCompanySpace       = "idx_spatial_captures_company_space"
	indexNameSpatialRoomVersionsCompanySpace   = "idx_spatial_room_versions_company_space"
	indexNameUniqueSpatialSpaceStatesCompanySp = "uq_spatial_space_states_company_space"
	indexNameUniqueSpatialRoomDraftsCapture    = "uq_spatial_room_drafts_capture"
	// indexNameUniqueSpatialCapturesClientID is a PARTIAL unique index —
	// applies only where clientCaptureId exists and is a non-empty string
	// (plan §RP3.5/§RP4B0). A plain (non-partial) unique index would make
	// every pre-existing capture with no ClientCaptureID collide on the
	// shared empty/missing value; the partial filter is what makes this
	// additive field backward-compatible with every capture created before
	// it existed.
	indexNameUniqueSpatialCapturesClientID = "uq_spatial_captures_company_client_capture_id"
	// indexNameUniqueSpatialVisualAssetVersionsAssetVersion is the
	// authoritative race guard for concurrent VisualAssetVersion publish
	// attempts (RP4D) — not an app-level check-then-insert.
	indexNameUniqueSpatialVisualAssetVersionsAssetVersion = "uq_spatial_visual_asset_versions_company_asset_version"
	// indexNameUniqueSpatialAssetGenerationJobsClientRequestID is the
	// authoritative idempotency race guard for concurrent identical
	// SubmitAssetGenerationJob submissions (RP4E0).
	indexNameUniqueSpatialAssetGenerationJobsClientRequestID = "uq_spatial_asset_generation_jobs_company_client_request_id"
	indexNameSpatialAssetGenerationJobsStatusAvailableAt     = "idx_spatial_asset_generation_jobs_status_available_at"
)

// --- SpatialCapture repository ---

// MongoCaptureRepository owns the "spatial_captures" collection exclusively.
type MongoCaptureRepository struct {
	collection *mongo.Collection
}

func NewMongoCaptureRepository(db *mongo.Database) *MongoCaptureRepository {
	return &MongoCaptureRepository{collection: db.Collection("spatial_captures")}
}

func (r *MongoCaptureRepository) EnsureIndexes(ctx context.Context) error {
	_, err := r.collection.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "companyId", Value: 1}, {Key: "spaceId", Value: 1}, {Key: "createdAt", Value: -1}},
			Options: options.Index().SetName(indexNameSpatialCapturesCompanySpace)},
		{Keys: bson.D{{Key: "companyId", Value: 1}, {Key: "clientCaptureId", Value: 1}},
			Options: options.Index().SetUnique(true).SetName(indexNameUniqueSpatialCapturesClientID).
				SetPartialFilterExpression(bson.M{"clientCaptureId": bson.M{"$exists": true, "$gt": ""}})},
	})
	return err
}

type spatialCaptureDoc struct {
	ID              bson.ObjectID `bson:"_id,omitempty"`
	CompanyID       string        `bson:"companyId"`
	ProjectID       string        `bson:"projectId"`
	SpaceID         string        `bson:"spaceId"`
	Status          string        `bson:"status"`
	CreatedAt       time.Time     `bson:"createdAt"`
	UpdatedAt       time.Time     `bson:"updatedAt"`
	RoomVersionID   string        `bson:"roomVersionId,omitempty"`
	Provider        string        `bson:"provider"`
	CaptureNumber   int           `bson:"captureNumber"`
	RoomDraftID     string        `bson:"roomDraftId,omitempty"`
	ClientCaptureID string        `bson:"clientCaptureId,omitempty"`
	SchemaVersion   int           `bson:"schemaVersion"`
}

func toCaptureDoc(c SpatialCapture) spatialCaptureDoc {
	doc := spatialCaptureDoc{
		CompanyID: c.CompanyID, ProjectID: c.ProjectID, SpaceID: c.SpaceID,
		Status: string(c.Status), CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt,
		RoomVersionID: c.RoomVersionID, Provider: string(c.Provider),
		CaptureNumber: c.CaptureNumber, RoomDraftID: c.RoomDraftID,
		ClientCaptureID: c.ClientCaptureID, SchemaVersion: c.SchemaVersion,
	}
	if c.ID != "" {
		objID, _ := bson.ObjectIDFromHex(c.ID)
		doc.ID = objID
	}
	return doc
}

func fromCaptureDoc(doc spatialCaptureDoc) SpatialCapture {
	return SpatialCapture{
		ID: doc.ID.Hex(), CompanyID: doc.CompanyID, ProjectID: doc.ProjectID, SpaceID: doc.SpaceID,
		Status: CaptureStatus(doc.Status), CreatedAt: doc.CreatedAt, UpdatedAt: doc.UpdatedAt,
		RoomVersionID: doc.RoomVersionID, Provider: CaptureProvider(doc.Provider),
		CaptureNumber: doc.CaptureNumber, RoomDraftID: doc.RoomDraftID,
		ClientCaptureID: doc.ClientCaptureID, SchemaVersion: doc.SchemaVersion,
	}
}

// Create inserts c. If c.ClientCaptureID is non-empty and a capture with
// the same (companyId, clientCaptureId) already exists (the partial unique
// index rejects the insert with a duplicate-key error — the authoritative
// race guard for concurrent first attempts, not merely the lookup-before-
// insert callers may also perform), Create does NOT create a second
// capture or surface an error: it returns the existing capture instead,
// exactly like a successful retry should observe (plan §RP3.5/§RP4B0).
func (r *MongoCaptureRepository) Create(ctx context.Context, c SpatialCapture) (SpatialCapture, error) {
	doc := toCaptureDoc(c)
	res, err := r.collection.InsertOne(ctx, doc)
	if err == nil {
		c.ID = res.InsertedID.(bson.ObjectID).Hex()
		return c, nil
	}
	if c.ClientCaptureID == "" || !mongo.IsDuplicateKeyError(err) {
		return SpatialCapture{}, err
	}
	return r.FindByClientCaptureID(ctx, c.CompanyID, c.ClientCaptureID)
}

// FindByClientCaptureID resolves an existing capture by its
// ClientCaptureID, tenant-scoped to companyID.
func (r *MongoCaptureRepository) FindByClientCaptureID(ctx context.Context, companyID, clientCaptureID string) (SpatialCapture, error) {
	var doc spatialCaptureDoc
	err := r.collection.FindOne(ctx, bson.M{"companyId": companyID, "clientCaptureId": clientCaptureID}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return SpatialCapture{}, ErrCaptureNotFound
	}
	if err != nil {
		return SpatialCapture{}, err
	}
	return fromCaptureDoc(doc), nil
}

func (r *MongoCaptureRepository) FindByID(ctx context.Context, companyID, id string) (SpatialCapture, error) {
	objID, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return SpatialCapture{}, ErrCaptureNotFound
	}
	var doc spatialCaptureDoc
	err = r.collection.FindOne(ctx, bson.M{"_id": objID, "companyId": companyID}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return SpatialCapture{}, ErrCaptureNotFound
	}
	if err != nil {
		return SpatialCapture{}, err
	}
	return fromCaptureDoc(doc), nil
}

func (r *MongoCaptureRepository) UpdateStatus(ctx context.Context, companyID, id string, expectedStatus, newStatus CaptureStatus) (SpatialCapture, error) {
	objID, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return SpatialCapture{}, ErrCaptureNotFound
	}
	filter := bson.M{"_id": objID, "companyId": companyID, "status": string(expectedStatus)}
	update := bson.M{"$set": bson.M{"status": string(newStatus), "updatedAt": time.Now()}}
	return r.findOneAndUpdate(ctx, filter, update)
}

func (r *MongoCaptureRepository) MarkConfirmed(ctx context.Context, companyID, id, roomVersionID string) (SpatialCapture, error) {
	objID, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return SpatialCapture{}, ErrCaptureNotFound
	}
	filter := bson.M{"_id": objID, "companyId": companyID, "status": string(CaptureStatusReview)}
	update := bson.M{"$set": bson.M{
		"status": string(CaptureStatusConfirmed), "roomVersionId": roomVersionID, "updatedAt": time.Now(),
	}}
	return r.findOneAndUpdate(ctx, filter, update)
}

// SetRoomDraft records roomDraftID on id, tenant-scoped to companyID, with
// no status guard — a capture may have its RoomDraft association set at any
// point in its lifecycle (plan §RP3's persistence ordering happens before
// the capture necessarily reaches status=review).
func (r *MongoCaptureRepository) SetRoomDraft(ctx context.Context, companyID, id, roomDraftID string) (SpatialCapture, error) {
	objID, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return SpatialCapture{}, ErrCaptureNotFound
	}
	filter := bson.M{"_id": objID, "companyId": companyID}
	update := bson.M{"$set": bson.M{"roomDraftId": roomDraftID, "updatedAt": time.Now()}}
	opts := options.FindOneAndUpdate().SetReturnDocument(options.After)
	var doc spatialCaptureDoc
	err = r.collection.FindOneAndUpdate(ctx, filter, update, opts).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return SpatialCapture{}, ErrCaptureNotFound
	}
	if err != nil {
		return SpatialCapture{}, err
	}
	return fromCaptureDoc(doc), nil
}

// findOneAndUpdate performs the update and, on no match, distinguishes
// "capture does not exist" from "capture exists but status guard failed" so
// callers see the correct sentinel — mirroring
// access.MongoAccessGrantRepository's single onNoMatch pattern would collapse
// that distinction, so this repository re-checks existence instead.
func (r *MongoCaptureRepository) findOneAndUpdate(ctx context.Context, filter, update bson.M) (SpatialCapture, error) {
	var doc spatialCaptureDoc
	opts := options.FindOneAndUpdate().SetReturnDocument(options.After)
	err := r.collection.FindOneAndUpdate(ctx, filter, update, opts).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		existsFilter := bson.M{"_id": filter["_id"], "companyId": filter["companyId"]}
		count, countErr := r.collection.CountDocuments(ctx, existsFilter)
		if countErr == nil && count == 0 {
			return SpatialCapture{}, ErrCaptureNotFound
		}
		return SpatialCapture{}, ErrIllegalCaptureTransition
	}
	if err != nil {
		return SpatialCapture{}, err
	}
	return fromCaptureDoc(doc), nil
}

// DeleteAllForCompany permanently removes every capture owned by companyID.
// Never errors when zero documents match. Development-tool use only.
func (r *MongoCaptureRepository) DeleteAllForCompany(ctx context.Context, companyID string) error {
	_, err := r.collection.DeleteMany(ctx, bson.M{"companyId": companyID})
	return err
}

func (r *MongoCaptureRepository) ListBySpace(ctx context.Context, companyID, spaceID string) ([]SpatialCapture, error) {
	cursor, err := r.collection.Find(ctx,
		bson.M{"companyId": companyID, "spaceId": spaceID},
		options.Find().SetSort(bson.D{{Key: "createdAt", Value: -1}}))
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var docs []spatialCaptureDoc
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, err
	}
	out := make([]SpatialCapture, 0, len(docs))
	for _, doc := range docs {
		out = append(out, fromCaptureDoc(doc))
	}
	return out, nil
}

// --- SpatialRoomVersion repository ---

// MongoRoomVersionRepository owns the "spatial_room_versions" collection
// exclusively.
type MongoRoomVersionRepository struct {
	collection *mongo.Collection
}

func NewMongoRoomVersionRepository(db *mongo.Database) *MongoRoomVersionRepository {
	return &MongoRoomVersionRepository{collection: db.Collection("spatial_room_versions")}
}

func (r *MongoRoomVersionRepository) EnsureIndexes(ctx context.Context) error {
	_, err := r.collection.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "companyId", Value: 1}, {Key: "spaceId", Value: 1}, {Key: "createdAt", Value: -1}},
			Options: options.Index().SetName(indexNameSpatialRoomVersionsCompanySpace)},
	})
	return err
}

type spatialRoomVersionDoc struct {
	ID            bson.ObjectID `bson:"_id,omitempty"`
	CompanyID     string        `bson:"companyId"`
	ProjectID     string        `bson:"projectId"`
	SpaceID       string        `bson:"spaceId"`
	CaptureID     string        `bson:"captureId"`
	Status        string        `bson:"status"`
	CreatedAt     time.Time     `bson:"createdAt"`
	SchemaVersion int           `bson:"schemaVersion"`
}

func toRoomVersionDoc(v SpatialRoomVersion) spatialRoomVersionDoc {
	doc := spatialRoomVersionDoc{
		CompanyID: v.CompanyID, ProjectID: v.ProjectID, SpaceID: v.SpaceID, CaptureID: v.CaptureID,
		Status: string(v.Status), CreatedAt: v.CreatedAt, SchemaVersion: v.SchemaVersion,
	}
	if v.ID != "" {
		objID, _ := bson.ObjectIDFromHex(v.ID)
		doc.ID = objID
	}
	return doc
}

func fromRoomVersionDoc(doc spatialRoomVersionDoc) SpatialRoomVersion {
	return SpatialRoomVersion{
		ID: doc.ID.Hex(), CompanyID: doc.CompanyID, ProjectID: doc.ProjectID, SpaceID: doc.SpaceID,
		CaptureID: doc.CaptureID, Status: RoomVersionStatus(doc.Status), CreatedAt: doc.CreatedAt,
		SchemaVersion: doc.SchemaVersion,
	}
}

// Create inserts v with Status forced to RoomVersionStatusCurrent — the only
// status a freshly created immutable RoomVersion may have.
func (r *MongoRoomVersionRepository) Create(ctx context.Context, v SpatialRoomVersion) (SpatialRoomVersion, error) {
	v.Status = RoomVersionStatusCurrent
	doc := toRoomVersionDoc(v)
	res, err := r.collection.InsertOne(ctx, doc)
	if err != nil {
		return SpatialRoomVersion{}, err
	}
	v.ID = res.InsertedID.(bson.ObjectID).Hex()
	return v, nil
}

func (r *MongoRoomVersionRepository) FindByID(ctx context.Context, companyID, id string) (SpatialRoomVersion, error) {
	objID, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return SpatialRoomVersion{}, ErrRoomVersionNotFound
	}
	var doc spatialRoomVersionDoc
	err = r.collection.FindOne(ctx, bson.M{"_id": objID, "companyId": companyID}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return SpatialRoomVersion{}, ErrRoomVersionNotFound
	}
	if err != nil {
		return SpatialRoomVersion{}, err
	}
	return fromRoomVersionDoc(doc), nil
}

// Supersede is a no-op success if id does not exist (interface contract: see
// RoomVersionRepository.Supersede doc comment).
func (r *MongoRoomVersionRepository) Supersede(ctx context.Context, companyID, id string) error {
	objID, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return nil
	}
	_, err = r.collection.UpdateOne(ctx,
		bson.M{"_id": objID, "companyId": companyID, "status": string(RoomVersionStatusCurrent)},
		bson.M{"$set": bson.M{"status": string(RoomVersionStatusSuperseded)}},
	)
	return err
}

// DeleteAllForCompany permanently removes every room version owned by
// companyID. Never errors when zero documents match. Development-tool use
// only.
func (r *MongoRoomVersionRepository) DeleteAllForCompany(ctx context.Context, companyID string) error {
	_, err := r.collection.DeleteMany(ctx, bson.M{"companyId": companyID})
	return err
}

func (r *MongoRoomVersionRepository) ListBySpace(ctx context.Context, companyID, spaceID string) ([]SpatialRoomVersion, error) {
	cursor, err := r.collection.Find(ctx,
		bson.M{"companyId": companyID, "spaceId": spaceID},
		options.Find().SetSort(bson.D{{Key: "createdAt", Value: -1}}))
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var docs []spatialRoomVersionDoc
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, err
	}
	out := make([]SpatialRoomVersion, 0, len(docs))
	for _, doc := range docs {
		out = append(out, fromRoomVersionDoc(doc))
	}
	return out, nil
}

// --- VisualAssetVersion repository (RP4D) ---

// MongoVisualAssetVersionRepository owns the
// "spatial_visual_asset_versions" collection exclusively. Deliberately no
// Update method — a published VisualAssetVersion never mutates.
type MongoVisualAssetVersionRepository struct {
	collection *mongo.Collection
}

func NewMongoVisualAssetVersionRepository(db *mongo.Database) *MongoVisualAssetVersionRepository {
	return &MongoVisualAssetVersionRepository{collection: db.Collection("spatial_visual_asset_versions")}
}

func (r *MongoVisualAssetVersionRepository) EnsureIndexes(ctx context.Context) error {
	_, err := r.collection.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "companyId", Value: 1}, {Key: "assetId", Value: 1}, {Key: "version", Value: 1}},
			Options: options.Index().SetUnique(true).SetName(indexNameUniqueSpatialVisualAssetVersionsAssetVersion)},
	})
	return err
}

type spatialVisualAssetVersionDoc struct {
	ID            bson.ObjectID `bson:"_id,omitempty"`
	CompanyID     string        `bson:"companyId"`
	AssetID       string        `bson:"assetId"`
	Version       int           `bson:"version"`
	Format        string        `bson:"format"`
	DisplayName   string        `bson:"displayName"`
	Pivot         string        `bson:"pivot"`
	ContentType   string        `bson:"contentType"`
	ByteCount     int64         `bson:"byteCount"`
	Checksum      string        `bson:"checksum"`
	StorageKey    string        `bson:"storageKey"`
	CreatedAt     time.Time     `bson:"createdAt"`
	SchemaVersion int           `bson:"schemaVersion"`
}

func toVisualAssetVersionDoc(v VisualAssetVersion) spatialVisualAssetVersionDoc {
	doc := spatialVisualAssetVersionDoc{
		CompanyID: v.CompanyID, AssetID: v.AssetID, Version: v.Version,
		Format: string(v.Format), DisplayName: v.DisplayName, Pivot: string(v.Normalization.Pivot),
		ContentType: v.ContentType, ByteCount: v.ByteCount, Checksum: v.Checksum,
		StorageKey: v.StorageKey, CreatedAt: v.CreatedAt, SchemaVersion: v.SchemaVersion,
	}
	if v.ID != "" {
		objID, _ := bson.ObjectIDFromHex(v.ID)
		doc.ID = objID
	}
	return doc
}

func fromVisualAssetVersionDoc(doc spatialVisualAssetVersionDoc) VisualAssetVersion {
	return VisualAssetVersion{
		ID: doc.ID.Hex(), CompanyID: doc.CompanyID, AssetID: doc.AssetID, Version: doc.Version,
		Format: VisualAssetFormat(doc.Format), DisplayName: doc.DisplayName,
		Normalization: VisualAssetNormalization{Pivot: VisualAssetPivot(doc.Pivot)},
		ContentType:   doc.ContentType, ByteCount: doc.ByteCount, Checksum: doc.Checksum,
		StorageKey: doc.StorageKey, CreatedAt: doc.CreatedAt, SchemaVersion: doc.SchemaVersion,
	}
}

// Create inserts v. On mongo.IsDuplicateKeyError (a (companyId, assetId,
// version) triple already published), re-fetches the existing document and
// compares (Format, Checksum, ByteCount) — if byte-for-byte/metadata
// identical, adopts and returns the EXISTING record (idempotent publish,
// e.g. a retried seeder/test call); otherwise returns
// ErrVisualAssetVersionConflict, never silently overwriting or picking one.
func (r *MongoVisualAssetVersionRepository) Create(ctx context.Context, v VisualAssetVersion) (VisualAssetVersion, error) {
	doc := toVisualAssetVersionDoc(v)
	res, err := r.collection.InsertOne(ctx, doc)
	if err == nil {
		v.ID = res.InsertedID.(bson.ObjectID).Hex()
		return v, nil
	}
	if !mongo.IsDuplicateKeyError(err) {
		return VisualAssetVersion{}, err
	}
	existing, findErr := r.FindByCompanyAssetVersion(ctx, v.CompanyID, v.AssetID, v.Version)
	if findErr != nil {
		return VisualAssetVersion{}, findErr
	}
	if existing.Format == v.Format && existing.Checksum == v.Checksum && existing.ByteCount == v.ByteCount {
		return existing, nil
	}
	return VisualAssetVersion{}, ErrVisualAssetVersionConflict
}

// FindByCompanyAssetVersion resolves an exact (companyID, assetID, version)
// triple. Returns ErrVisualAssetVersionNotFound if no match — including a
// version that exists but belongs to a different company (the query
// itself is the tenant boundary, matching this package's existing
// convention).
func (r *MongoVisualAssetVersionRepository) FindByCompanyAssetVersion(ctx context.Context, companyID, assetID string, version int) (VisualAssetVersion, error) {
	var doc spatialVisualAssetVersionDoc
	err := r.collection.FindOne(ctx, bson.M{"companyId": companyID, "assetId": assetID, "version": version}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return VisualAssetVersion{}, ErrVisualAssetVersionNotFound
	}
	if err != nil {
		return VisualAssetVersion{}, err
	}
	return fromVisualAssetVersionDoc(doc), nil
}

// --- SpatialSpaceState repository ---

// MongoSpaceStateRepository owns the "spatial_space_states" collection
// exclusively.
type MongoSpaceStateRepository struct {
	collection *mongo.Collection
}

func NewMongoSpaceStateRepository(db *mongo.Database) *MongoSpaceStateRepository {
	return &MongoSpaceStateRepository{collection: db.Collection("spatial_space_states")}
}

func (r *MongoSpaceStateRepository) EnsureIndexes(ctx context.Context) error {
	_, err := r.collection.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "companyId", Value: 1}, {Key: "spaceId", Value: 1}},
			Options: options.Index().SetUnique(true).SetName(indexNameUniqueSpatialSpaceStatesCompanySp)},
	})
	return err
}

type spatialSpaceStateDoc struct {
	ID                   bson.ObjectID `bson:"_id,omitempty"`
	CompanyID            string        `bson:"companyId"`
	ProjectID            string        `bson:"projectId"`
	SpaceID              string        `bson:"spaceId"`
	CurrentRoomVersionID string        `bson:"currentRoomVersionId,omitempty"`
	Revision             int64         `bson:"revision"`
	CreatedAt            time.Time     `bson:"createdAt"`
	UpdatedAt            time.Time     `bson:"updatedAt"`
	SchemaVersion        int           `bson:"schemaVersion"`
}

func fromSpaceStateDoc(doc spatialSpaceStateDoc) SpatialSpaceState {
	return SpatialSpaceState{
		ID: doc.ID.Hex(), CompanyID: doc.CompanyID, ProjectID: doc.ProjectID, SpaceID: doc.SpaceID,
		CurrentRoomVersionID: doc.CurrentRoomVersionID, Revision: doc.Revision,
		CreatedAt: doc.CreatedAt, UpdatedAt: doc.UpdatedAt, SchemaVersion: doc.SchemaVersion,
	}
}

// FindOrCreateBySpace uses an upsert with $setOnInsert so concurrent
// first-callers for the same Space race safely on the unique
// companyId+spaceId index rather than both succeeding with divergent
// Revision=0 documents.
func (r *MongoSpaceStateRepository) FindOrCreateBySpace(ctx context.Context, companyID, projectID, spaceID string) (SpatialSpaceState, error) {
	now := time.Now()
	filter := bson.M{"companyId": companyID, "spaceId": spaceID}
	update := bson.M{
		"$setOnInsert": bson.M{
			"companyId": companyID, "projectId": projectID, "spaceId": spaceID,
			"revision": int64(0), "createdAt": now, "updatedAt": now, "schemaVersion": 1,
		},
	}
	opts := options.FindOneAndUpdate().SetUpsert(true).SetReturnDocument(options.After)
	var doc spatialSpaceStateDoc
	err := r.collection.FindOneAndUpdate(ctx, filter, update, opts).Decode(&doc)
	if err != nil {
		return SpatialSpaceState{}, err
	}
	return fromSpaceStateDoc(doc), nil
}

// DeleteAllForCompany permanently removes companyID's space state documents.
// Never errors when zero documents match. Development-tool use only.
func (r *MongoSpaceStateRepository) DeleteAllForCompany(ctx context.Context, companyID string) error {
	_, err := r.collection.DeleteMany(ctx, bson.M{"companyId": companyID})
	return err
}

// SetCurrentRoomVersion mirrors access.MongoAccessGrantRepository.Revoke's
// CAS pattern: filter includes {revision: expectedRevision}, update
// $increments revision by 1, and a zero-match result means the stored
// revision had already moved on.
func (r *MongoSpaceStateRepository) SetCurrentRoomVersion(ctx context.Context, companyID, spaceID, roomVersionID string, expectedRevision int64) (SpatialSpaceState, error) {
	filter := bson.M{"companyId": companyID, "spaceId": spaceID, "revision": expectedRevision}
	update := bson.M{
		"$set": bson.M{"currentRoomVersionId": roomVersionID, "updatedAt": time.Now()},
		"$inc": bson.M{"revision": 1},
	}
	opts := options.FindOneAndUpdate().SetReturnDocument(options.After)
	var doc spatialSpaceStateDoc
	err := r.collection.FindOneAndUpdate(ctx, filter, update, opts).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return SpatialSpaceState{}, ErrSpaceStateRevisionMismatch
	}
	if err != nil {
		return SpatialSpaceState{}, err
	}
	return fromSpaceStateDoc(doc), nil
}

// --- RoomDraft repository ---

// MongoRoomDraftRepository owns the "spatial_room_drafts" collection
// exclusively.
type MongoRoomDraftRepository struct {
	collection *mongo.Collection
}

func NewMongoRoomDraftRepository(db *mongo.Database) *MongoRoomDraftRepository {
	return &MongoRoomDraftRepository{collection: db.Collection("spatial_room_drafts")}
}

func (r *MongoRoomDraftRepository) EnsureIndexes(ctx context.Context) error {
	_, err := r.collection.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "companyId", Value: 1}, {Key: "captureId", Value: 1}},
			Options: options.Index().SetUnique(true).SetName(indexNameUniqueSpatialRoomDraftsCapture)},
	})
	return err
}

// spatialRoomDraftDoc mirrors RoomDraft exactly (same nested types are
// reused directly since RoomDraftWall/RoomDraftOpening/RoomDraftObject
// already carry bson tags) — no separate doc-only field set is needed here,
// unlike spatialCaptureDoc, since RoomDraft has no derived/computed fields.
type spatialRoomDraftDoc struct {
	ID               bson.ObjectID           `bson:"_id,omitempty"`
	CompanyID        string                  `bson:"companyId"`
	CaptureID        string                  `bson:"captureId"`
	Walls            []RoomDraftWall         `bson:"walls"`
	Openings         []RoomDraftOpening      `bson:"openings"`
	Objects          []RoomDraftObject       `bson:"objects"`
	Fixtures         []RoomDraftFixture      `bson:"fixtures"`
	ServicePoints    []RoomDraftServicePoint `bson:"servicePoints"`
	Constraints      []RoomDraftConstraint   `bson:"constraints"`
	SourceProvider   string                  `bson:"sourceProvider"`
	OriginalBaseline *RoomDraftBaseline      `bson:"originalBaseline,omitempty"`
	CreatedAt        time.Time               `bson:"createdAt"`
	UpdatedAt        time.Time               `bson:"updatedAt"`
	Revision         int64                   `bson:"revision"`
	SchemaVersion    int                     `bson:"schemaVersion"`
}

func toRoomDraftDoc(d RoomDraft) spatialRoomDraftDoc {
	doc := spatialRoomDraftDoc{
		CompanyID: d.CompanyID, CaptureID: d.CaptureID,
		Walls: d.Walls, Openings: d.Openings, Objects: d.Objects,
		Fixtures: d.Fixtures, ServicePoints: d.ServicePoints, Constraints: d.Constraints,
		SourceProvider: string(d.SourceProvider), OriginalBaseline: d.OriginalBaseline,
		CreatedAt: d.CreatedAt, UpdatedAt: d.UpdatedAt, Revision: d.Revision, SchemaVersion: d.SchemaVersion,
	}
	if d.ID != "" {
		objID, _ := bson.ObjectIDFromHex(d.ID)
		doc.ID = objID
	}
	return doc
}

func fromRoomDraftDoc(doc spatialRoomDraftDoc) RoomDraft {
	return RoomDraft{
		ID: doc.ID.Hex(), CompanyID: doc.CompanyID, CaptureID: doc.CaptureID,
		Walls: doc.Walls, Openings: doc.Openings, Objects: doc.Objects,
		Fixtures: doc.Fixtures, ServicePoints: doc.ServicePoints, Constraints: doc.Constraints,
		SourceProvider: SourceProvider(doc.SourceProvider), OriginalBaseline: doc.OriginalBaseline,
		CreatedAt: doc.CreatedAt, UpdatedAt: doc.UpdatedAt, Revision: doc.Revision, SchemaVersion: doc.SchemaVersion,
	}
}

// Create inserts d with Revision forced to 0 — the only revision a freshly
// created RoomDraft may have.
func (r *MongoRoomDraftRepository) Create(ctx context.Context, d RoomDraft) (RoomDraft, error) {
	d.Revision = 0
	doc := toRoomDraftDoc(d)
	res, err := r.collection.InsertOne(ctx, doc)
	if err != nil {
		return RoomDraft{}, err
	}
	d.ID = res.InsertedID.(bson.ObjectID).Hex()
	return d, nil
}

func (r *MongoRoomDraftRepository) FindByID(ctx context.Context, companyID, id string) (RoomDraft, error) {
	objID, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return RoomDraft{}, ErrRoomDraftNotFound
	}
	var doc spatialRoomDraftDoc
	err = r.collection.FindOne(ctx, bson.M{"_id": objID, "companyId": companyID}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return RoomDraft{}, ErrRoomDraftNotFound
	}
	if err != nil {
		return RoomDraft{}, err
	}
	return fromRoomDraftDoc(doc), nil
}

func (r *MongoRoomDraftRepository) FindByCaptureID(ctx context.Context, companyID, captureID string) (RoomDraft, error) {
	var doc spatialRoomDraftDoc
	err := r.collection.FindOne(ctx, bson.M{"companyId": companyID, "captureId": captureID}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return RoomDraft{}, ErrRoomDraftNotFound
	}
	if err != nil {
		return RoomDraft{}, err
	}
	return fromRoomDraftDoc(doc), nil
}

// Update mirrors MongoSpaceStateRepository.SetCurrentRoomVersion's CAS
// pattern: filter includes {revision: expectedRevision}, update
// $increments revision by 1, and a zero-match result means the stored
// revision had already moved on.
func (r *MongoRoomDraftRepository) Update(ctx context.Context, companyID, id string, d RoomDraft, expectedRevision int64) (RoomDraft, error) {
	objID, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return RoomDraft{}, ErrRoomDraftNotFound
	}
	filter := bson.M{"_id": objID, "companyId": companyID, "revision": expectedRevision}
	update := bson.M{
		"$set": bson.M{
			"walls": d.Walls, "openings": d.Openings, "objects": d.Objects,
			"fixtures": d.Fixtures, "servicePoints": d.ServicePoints, "constraints": d.Constraints,
			"originalBaseline": d.OriginalBaseline, "updatedAt": time.Now(),
		},
		"$inc": bson.M{"revision": 1},
	}
	opts := options.FindOneAndUpdate().SetReturnDocument(options.After)
	var doc spatialRoomDraftDoc
	err = r.collection.FindOneAndUpdate(ctx, filter, update, opts).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return RoomDraft{}, ErrRoomDraftRevisionMismatch
	}
	if err != nil {
		return RoomDraft{}, err
	}
	return fromRoomDraftDoc(doc), nil
}

// DeleteAllForCompany permanently removes every room draft owned by
// companyID. Never errors when zero documents match. Development-tool use
// only.
func (r *MongoRoomDraftRepository) DeleteAllForCompany(ctx context.Context, companyID string) error {
	_, err := r.collection.DeleteMany(ctx, bson.M{"companyId": companyID})
	return err
}
