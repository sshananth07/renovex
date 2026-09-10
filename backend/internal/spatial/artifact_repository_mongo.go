package spatial

import (
	"context"
	"errors"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

const (
	indexNameSpatialArtifactsCompanyCapture    = "idx_spatial_artifacts_company_capture"
	indexNameUniqueSpatialArtifactsUploadToken = "uq_spatial_artifacts_upload_token_hash"
)

// MongoArtifactRepository owns the "spatial_artifacts" collection
// exclusively.
type MongoArtifactRepository struct {
	collection *mongo.Collection
}

func NewMongoArtifactRepository(db *mongo.Database) *MongoArtifactRepository {
	return &MongoArtifactRepository{collection: db.Collection("spatial_artifacts")}
}

// EnsureIndexes creates the tenant listing index and a UNIQUE, sparse
// uploadTokenHash index — sparse because MarkUploaded $unsets the field on
// finalize, so most documents have no token at all, and uniqueness matters
// only while a token is genuinely active.
func (r *MongoArtifactRepository) EnsureIndexes(ctx context.Context) error {
	_, err := r.collection.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "companyId", Value: 1}, {Key: "captureId", Value: 1}},
			Options: options.Index().SetName(indexNameSpatialArtifactsCompanyCapture)},
		{Keys: bson.D{{Key: "uploadTokenHash", Value: 1}},
			Options: options.Index().SetUnique(true).SetSparse(true).SetName(indexNameUniqueSpatialArtifactsUploadToken)},
	})
	return err
}

type spatialArtifactDoc struct {
	ID                   bson.ObjectID `bson:"_id,omitempty"`
	CompanyID            string        `bson:"companyId"`
	CaptureID            string        `bson:"captureId"`
	Kind                 string        `bson:"kind"`
	ContentType          string        `bson:"contentType"`
	DeclaredSize         int64         `bson:"declaredSize"`
	ActualSize           int64         `bson:"actualSize,omitempty"`
	Checksum             string        `bson:"checksum"`
	Status               string        `bson:"status"`
	CreatedAt            time.Time     `bson:"createdAt"`
	UploadedAt           *time.Time    `bson:"uploadedAt,omitempty"`
	SchemaVersion        int           `bson:"schemaVersion"`
	UploadTokenHash      string        `bson:"uploadTokenHash,omitempty"`
	UploadTokenExpiresAt time.Time     `bson:"uploadTokenExpiresAt,omitempty"`
}

func toArtifactDoc(a SpatialArtifact) spatialArtifactDoc {
	doc := spatialArtifactDoc{
		CompanyID: a.CompanyID, CaptureID: a.CaptureID, Kind: string(a.Kind), ContentType: a.ContentType,
		DeclaredSize: a.DeclaredSize, ActualSize: a.ActualSize, Checksum: a.Checksum, Status: string(a.Status),
		CreatedAt: a.CreatedAt, UploadedAt: a.UploadedAt, SchemaVersion: a.SchemaVersion,
		UploadTokenHash: a.UploadTokenHash, UploadTokenExpiresAt: a.UploadTokenExpiresAt,
	}
	if a.ID != "" {
		objID, _ := bson.ObjectIDFromHex(a.ID)
		doc.ID = objID
	}
	return doc
}

func fromArtifactDoc(doc spatialArtifactDoc) SpatialArtifact {
	return SpatialArtifact{
		ID: doc.ID.Hex(), CompanyID: doc.CompanyID, CaptureID: doc.CaptureID, Kind: ArtifactKind(doc.Kind),
		ObjectKey:   objectKeyFor(doc.CompanyID, doc.CaptureID, doc.ID.Hex()),
		ContentType: doc.ContentType, DeclaredSize: doc.DeclaredSize, ActualSize: doc.ActualSize,
		Checksum: doc.Checksum, Status: ArtifactStatus(doc.Status), CreatedAt: doc.CreatedAt,
		UploadedAt: doc.UploadedAt, SchemaVersion: doc.SchemaVersion,
		UploadTokenHash: doc.UploadTokenHash, UploadTokenExpiresAt: doc.UploadTokenExpiresAt,
	}
}

func (r *MongoArtifactRepository) Create(ctx context.Context, a SpatialArtifact) (SpatialArtifact, error) {
	doc := toArtifactDoc(a)
	res, err := r.collection.InsertOne(ctx, doc)
	if err != nil {
		return SpatialArtifact{}, err
	}
	a.ID = res.InsertedID.(bson.ObjectID).Hex()
	return a, nil
}

func (r *MongoArtifactRepository) FindByID(ctx context.Context, companyID, id string) (SpatialArtifact, error) {
	objID, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return SpatialArtifact{}, ErrArtifactNotFound
	}
	var doc spatialArtifactDoc
	err = r.collection.FindOne(ctx, bson.M{"_id": objID, "companyId": companyID}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return SpatialArtifact{}, ErrArtifactNotFound
	}
	if err != nil {
		return SpatialArtifact{}, err
	}
	return fromArtifactDoc(doc), nil
}

func (r *MongoArtifactRepository) FindByUploadTokenHash(ctx context.Context, tokenHash string) (SpatialArtifact, error) {
	var doc spatialArtifactDoc
	err := r.collection.FindOne(ctx, bson.M{"uploadTokenHash": tokenHash}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return SpatialArtifact{}, ErrArtifactNotFound
	}
	if err != nil {
		return SpatialArtifact{}, err
	}
	return fromArtifactDoc(doc), nil
}

func (r *MongoArtifactRepository) ReissueUploadToken(ctx context.Context, companyID, id, tokenHash string, expiresAt time.Time) (SpatialArtifact, error) {
	objID, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return SpatialArtifact{}, ErrArtifactNotFound
	}
	opts := options.FindOneAndUpdate().SetReturnDocument(options.After)
	var doc spatialArtifactDoc
	err = r.collection.FindOneAndUpdate(ctx,
		bson.M{"_id": objID, "companyId": companyID},
		bson.M{"$set": bson.M{"uploadTokenHash": tokenHash, "uploadTokenExpiresAt": expiresAt}},
		opts,
	).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return SpatialArtifact{}, ErrArtifactNotFound
	}
	if err != nil {
		return SpatialArtifact{}, err
	}
	return fromArtifactDoc(doc), nil
}

// MarkUploaded is idempotent by design: if the stored document is already
// status=uploaded, the $set below is a harmless no-op re-write of the same
// terminal state rather than a conditional check, since FindOneAndUpdate
// still matches and returns the current document either way.
func (r *MongoArtifactRepository) MarkUploaded(ctx context.Context, companyID, id string, actualSize int64, uploadedAt time.Time) (SpatialArtifact, error) {
	objID, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return SpatialArtifact{}, ErrArtifactNotFound
	}
	opts := options.FindOneAndUpdate().SetReturnDocument(options.After)
	var doc spatialArtifactDoc
	err = r.collection.FindOneAndUpdate(ctx,
		bson.M{"_id": objID, "companyId": companyID},
		bson.M{
			"$set":   bson.M{"status": string(ArtifactStatusUploaded), "actualSize": actualSize, "uploadedAt": uploadedAt},
			"$unset": bson.M{"uploadTokenHash": "", "uploadTokenExpiresAt": ""},
		},
		opts,
	).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return SpatialArtifact{}, ErrArtifactNotFound
	}
	if err != nil {
		return SpatialArtifact{}, err
	}
	return fromArtifactDoc(doc), nil
}

func (r *MongoArtifactRepository) ListByCapture(ctx context.Context, companyID, captureID string) ([]SpatialArtifact, error) {
	cursor, err := r.collection.Find(ctx, bson.M{"companyId": companyID, "captureId": captureID})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var docs []spatialArtifactDoc
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, err
	}
	out := make([]SpatialArtifact, 0, len(docs))
	for _, doc := range docs {
		out = append(out, fromArtifactDoc(doc))
	}
	return out, nil
}

// DeleteAllForCompany permanently removes every artifact owned by companyID.
// Never errors when zero documents match. Development-tool use only.
func (r *MongoArtifactRepository) DeleteAllForCompany(ctx context.Context, companyID string) error {
	_, err := r.collection.DeleteMany(ctx, bson.M{"companyId": companyID})
	return err
}
