package access

import (
	"context"
	"errors"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// Explicit index names — every index carries one, mirroring the M4/M5
// convention (design spec §13).
const (
	indexNameAccessGrantsCompanyProject          = "idx_access_grants_company_project"
	indexNameUniqueAccessGrantsTokenHash         = "uq_access_grants_token_hash"
	indexNameAccessGrantsCompanyResourceCreated  = "idx_access_grants_company_resource_created_at"
	indexNameUniqueAccessGroupStatesCompanyGroup = "uq_access_group_states_company_group"
)

// --- AccessGrant repository ---

// MongoAccessGrantRepository owns the "access_grants" collection exclusively.
type MongoAccessGrantRepository struct {
	collection *mongo.Collection
}

func NewMongoAccessGrantRepository(db *mongo.Database) *MongoAccessGrantRepository {
	return &MongoAccessGrantRepository{collection: db.Collection("access_grants")}
}

// EnsureIndexes creates the tenant listing index, the UNIQUE token-hash
// lookup index (the hottest external path), and the per-resource history
// index. Note there is deliberately NO per-resource unique index: multiple
// historical grants per Quotation version are expected (design spec §2.1).
func (r *MongoAccessGrantRepository) EnsureIndexes(ctx context.Context) error {
	_, err := r.collection.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "companyId", Value: 1}, {Key: "projectId", Value: 1}},
			Options: options.Index().SetName(indexNameAccessGrantsCompanyProject)},
		{Keys: bson.D{{Key: "tokenHash", Value: 1}},
			Options: options.Index().SetUnique(true).SetName(indexNameUniqueAccessGrantsTokenHash)},
		{Keys: bson.D{
			{Key: "companyId", Value: 1}, {Key: "resourceType", Value: 1},
			{Key: "resourceId", Value: 1}, {Key: "createdAt", Value: -1}},
			Options: options.Index().SetName(indexNameAccessGrantsCompanyResourceCreated)},
	})
	return err
}

type accessGrantDoc struct {
	ID               bson.ObjectID `bson:"_id,omitempty"`
	CompanyID        string        `bson:"companyId"`
	ProjectID        string        `bson:"projectId"`
	ResourceType     string        `bson:"resourceType"`
	ResourceID       string        `bson:"resourceId"`
	ResourceGroupKey string        `bson:"resourceGroupKey"`
	QuotationNumber  string        `bson:"quotationNumber"`
	GranteeType      string        `bson:"granteeType"`
	GranteeID        string        `bson:"granteeId"`
	Permissions      []string      `bson:"permissions"`
	TokenHash        string        `bson:"tokenHash"`
	Status           string        `bson:"status"`
	Revision         int64         `bson:"revision"`
	CreatedByUserID  string        `bson:"createdByUserId"`
	CreatedAt        time.Time     `bson:"createdAt"`
	ExpiresAt        time.Time     `bson:"expiresAt"`
	RevokedAt        *time.Time    `bson:"revokedAt,omitempty"`
	RevokedReason    string        `bson:"revokedReason,omitempty"`
	SchemaVersion    int           `bson:"schemaVersion"`
}

func toGrantDoc(g AccessGrant) accessGrantDoc {
	return accessGrantDoc{
		CompanyID: g.CompanyID, ProjectID: g.ProjectID,
		ResourceType: g.ResourceType, ResourceID: g.ResourceID,
		ResourceGroupKey: g.ResourceGroupKey, QuotationNumber: g.QuotationNumber,
		GranteeType: g.GranteeType, GranteeID: g.GranteeID,
		Permissions: g.Permissions, TokenHash: g.TokenHash,
		Status: string(g.Status), Revision: g.Revision,
		CreatedByUserID: g.CreatedByUserID, CreatedAt: g.CreatedAt,
		ExpiresAt: g.ExpiresAt, RevokedAt: g.RevokedAt, RevokedReason: g.RevokedReason,
		SchemaVersion: g.SchemaVersion,
	}
}

func fromGrantDoc(d accessGrantDoc) AccessGrant {
	return AccessGrant{
		ID: d.ID.Hex(), CompanyID: d.CompanyID, ProjectID: d.ProjectID,
		ResourceType: d.ResourceType, ResourceID: d.ResourceID,
		ResourceGroupKey: d.ResourceGroupKey, QuotationNumber: d.QuotationNumber,
		GranteeType: d.GranteeType, GranteeID: d.GranteeID,
		Permissions: d.Permissions, TokenHash: d.TokenHash,
		Status: AccessGrantStatus(d.Status), Revision: d.Revision,
		CreatedByUserID: d.CreatedByUserID, CreatedAt: d.CreatedAt,
		ExpiresAt: d.ExpiresAt, RevokedAt: d.RevokedAt, RevokedReason: d.RevokedReason,
		SchemaVersion: d.SchemaVersion,
	}
}

// classifyGrantCreateError translates a raw duplicate-key error by explicit
// index name. A token-hash collision is astronomically unlikely (256 bits of
// entropy) and is NOT a retriable "try again" case — it signals something
// deeply wrong (e.g. a broken RNG), so it surfaces loudly as a 500.
func classifyGrantCreateError(err error) error {
	if !mongo.IsDuplicateKeyError(err) {
		return err
	}
	var writeException mongo.WriteException
	if errors.As(err, &writeException) {
		for _, we := range writeException.WriteErrors {
			if strings.Contains(we.Message, indexNameUniqueAccessGrantsTokenHash) {
				return ErrUnclassifiedDuplicateKey
			}
		}
	}
	return ErrUnclassifiedDuplicateKey
}

func (r *MongoAccessGrantRepository) Create(ctx context.Context, g AccessGrant) (AccessGrant, error) {
	res, err := r.collection.InsertOne(ctx, toGrantDoc(g))
	if err != nil {
		return AccessGrant{}, classifyGrantCreateError(err)
	}
	g.ID = res.InsertedID.(bson.ObjectID).Hex()
	return g, nil
}

func (r *MongoAccessGrantRepository) FindByID(ctx context.Context, companyID, id string) (AccessGrant, error) {
	objID, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return AccessGrant{}, ErrGrantNotFound
	}
	var doc accessGrantDoc
	err = r.collection.FindOne(ctx, bson.M{"_id": objID, "companyId": companyID}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return AccessGrant{}, ErrGrantNotFound
	}
	if err != nil {
		return AccessGrant{}, err
	}
	return fromGrantDoc(doc), nil
}

// FindByTokenHash intentionally takes no companyID — the external Client path
// has no authenticated tenant. The returned grant's own CompanyID is what all
// downstream lookups use (design spec §14).
func (r *MongoAccessGrantRepository) FindByTokenHash(ctx context.Context, tokenHash string) (AccessGrant, error) {
	var doc accessGrantDoc
	err := r.collection.FindOne(ctx, bson.M{"tokenHash": tokenHash}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return AccessGrant{}, ErrGrantNotFound
	}
	if err != nil {
		return AccessGrant{}, err
	}
	return fromGrantDoc(doc), nil
}

func (r *MongoAccessGrantRepository) FindLatestByResource(ctx context.Context, companyID, resourceType, resourceID string) (AccessGrant, error) {
	var doc accessGrantDoc
	opts := options.FindOne().SetSort(bson.D{{Key: "createdAt", Value: -1}})
	err := r.collection.FindOne(ctx, bson.M{
		"companyId": companyID, "resourceType": resourceType, "resourceId": resourceID,
	}, opts).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return AccessGrant{}, ErrGrantNotFound
	}
	if err != nil {
		return AccessGrant{}, err
	}
	return fromGrantDoc(doc), nil
}

// DeleteAllForCompany permanently removes every AccessGrant owned by
// companyID. Never errors when zero documents match. Development-tool use
// only.
func (r *MongoAccessGrantRepository) DeleteAllForCompany(ctx context.Context, companyID string) error {
	_, err := r.collection.DeleteMany(ctx, bson.M{"companyId": companyID})
	return err
}

func (r *MongoAccessGrantRepository) Revoke(ctx context.Context, companyID, id string, expectedRevision int64, reason string, revokedAt time.Time) (AccessGrant, error) {
	objID, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return AccessGrant{}, ErrGrantNotFound
	}
	filter := bson.M{
		"_id": objID, "companyId": companyID,
		"status": string(AccessGrantStatusActive), "revision": expectedRevision,
	}
	update := bson.M{
		"$set": bson.M{"status": string(AccessGrantStatusRevoked), "revokedReason": reason, "revokedAt": revokedAt},
		"$inc": bson.M{"revision": 1},
	}
	return r.findOneAndUpdate(ctx, filter, update, ErrGrantRevisionMismatch)
}

// RevokeBestEffort is the post-claim cleanup path (design spec §2.4 step 4).
// It carries no Revision guard because the coordinator has already switched
// away from this grant — the grant is externally dead regardless of whether
// this write lands. Its failure is logged by the caller, never propagated as
// an authoritative error.
func (r *MongoAccessGrantRepository) RevokeBestEffort(ctx context.Context, companyID, id, reason string, revokedAt time.Time) (AccessGrant, error) {
	objID, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return AccessGrant{}, ErrGrantNotFound
	}
	filter := bson.M{"_id": objID, "companyId": companyID, "status": string(AccessGrantStatusActive)}
	update := bson.M{
		"$set": bson.M{"status": string(AccessGrantStatusRevoked), "revokedReason": reason, "revokedAt": revokedAt},
		"$inc": bson.M{"revision": 1},
	}
	return r.findOneAndUpdate(ctx, filter, update, ErrGrantRevisionMismatch)
}

func (r *MongoAccessGrantRepository) UpdateExpiry(ctx context.Context, companyID, id string, expectedRevision int64, expiresAt time.Time) (AccessGrant, error) {
	objID, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return AccessGrant{}, ErrGrantNotFound
	}
	filter := bson.M{
		"_id": objID, "companyId": companyID,
		"status": string(AccessGrantStatusActive), "revision": expectedRevision,
	}
	update := bson.M{
		"$set": bson.M{"expiresAt": expiresAt},
		"$inc": bson.M{"revision": 1},
	}
	return r.findOneAndUpdate(ctx, filter, update, ErrGrantRevisionMismatch)
}

func (r *MongoAccessGrantRepository) findOneAndUpdate(ctx context.Context, filter, update bson.M, onNoMatch error) (AccessGrant, error) {
	var doc accessGrantDoc
	opts := options.FindOneAndUpdate().SetReturnDocument(options.After)
	err := r.collection.FindOneAndUpdate(ctx, filter, update, opts).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return AccessGrant{}, onNoMatch
	}
	if err != nil {
		return AccessGrant{}, err
	}
	return fromGrantDoc(doc), nil
}

// --- AccessGroupState (coordinator) repository ---

// MongoAccessGroupStateRepository owns the "access_group_states" collection
// exclusively. Every method that mutates is a single-document conditional
// write — this collection is the serialization point for every cross-grant
// race in M6 (design spec §2.5/§11).
type MongoAccessGroupStateRepository struct {
	collection *mongo.Collection
}

func NewMongoAccessGroupStateRepository(db *mongo.Database) *MongoAccessGroupStateRepository {
	return &MongoAccessGroupStateRepository{collection: db.Collection("access_group_states")}
}

// EnsureIndexes creates the UNIQUE per-chain coordinator index. This is what
// makes concurrent FindOrCreate for a brand-new chain converge on exactly one
// document (design spec §13).
func (r *MongoAccessGroupStateRepository) EnsureIndexes(ctx context.Context) error {
	_, err := r.collection.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{
			{Key: "companyId", Value: 1}, {Key: "resourceType", Value: 1},
			{Key: "resourceGroupKey", Value: 1}},
			Options: options.Index().SetUnique(true).SetName(indexNameUniqueAccessGroupStatesCompanyGroup)},
	})
	return err
}

type accessGroupStateDoc struct {
	ID                 bson.ObjectID `bson:"_id,omitempty"`
	CompanyID          string        `bson:"companyId"`
	ResourceType       string        `bson:"resourceType"`
	ResourceGroupKey   string        `bson:"resourceGroupKey"`
	CurrentResourceID  string        `bson:"currentResourceId"`
	ActiveGrantID      *string       `bson:"activeGrantId"`
	AcceptedResourceID *string       `bson:"acceptedResourceId"`
	Revision           int64         `bson:"revision"`
	CreatedAt          time.Time     `bson:"createdAt"`
	SchemaVersion      int           `bson:"schemaVersion"`
}

func fromGroupStateDoc(d accessGroupStateDoc) AccessGroupState {
	return AccessGroupState{
		ID: d.ID.Hex(), CompanyID: d.CompanyID,
		ResourceType: d.ResourceType, ResourceGroupKey: d.ResourceGroupKey,
		CurrentResourceID: d.CurrentResourceID, ActiveGrantID: d.ActiveGrantID,
		AcceptedResourceID: d.AcceptedResourceID, Revision: d.Revision,
		CreatedAt: d.CreatedAt, SchemaVersion: d.SchemaVersion,
	}
}

func (r *MongoAccessGroupStateRepository) FindOrCreate(ctx context.Context, companyID, resourceType, resourceGroupKey, initialResourceID string, now time.Time) (AccessGroupState, error) {
	filter := bson.M{"companyId": companyID, "resourceType": resourceType, "resourceGroupKey": resourceGroupKey}
	update := bson.M{
		"$setOnInsert": bson.M{
			"companyId": companyID, "resourceType": resourceType, "resourceGroupKey": resourceGroupKey,
			"currentResourceId":  initialResourceID,
			"activeGrantId":      nil,
			"acceptedResourceId": nil,
			"revision":           int64(0),
			"createdAt":          now,
			"schemaVersion":      1,
		},
	}
	opts := options.FindOneAndUpdate().SetUpsert(true).SetReturnDocument(options.After)

	var doc accessGroupStateDoc
	err := r.collection.FindOneAndUpdate(ctx, filter, update, opts).Decode(&doc)
	if err != nil {
		// A concurrent upsert can lose the unique-index race; re-read.
		if mongo.IsDuplicateKeyError(err) {
			return r.Find(ctx, companyID, resourceType, resourceGroupKey)
		}
		return AccessGroupState{}, err
	}
	return fromGroupStateDoc(doc), nil
}

// DeleteAllForCompany permanently removes every AccessGroupState owned by
// companyID. Never errors when zero documents match. Development-tool use
// only.
func (r *MongoAccessGroupStateRepository) DeleteAllForCompany(ctx context.Context, companyID string) error {
	_, err := r.collection.DeleteMany(ctx, bson.M{"companyId": companyID})
	return err
}

func (r *MongoAccessGroupStateRepository) Find(ctx context.Context, companyID, resourceType, resourceGroupKey string) (AccessGroupState, error) {
	var doc accessGroupStateDoc
	err := r.collection.FindOne(ctx, bson.M{
		"companyId": companyID, "resourceType": resourceType, "resourceGroupKey": resourceGroupKey,
	}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return AccessGroupState{}, ErrGroupStateNotFound
	}
	if err != nil {
		return AccessGroupState{}, err
	}
	return fromGroupStateDoc(doc), nil
}

func (r *MongoAccessGroupStateRepository) ClaimActiveGrant(
	ctx context.Context,
	companyID, resourceType, resourceGroupKey string,
	expectedRevision int64,
	expectedCurrentResourceID string,
	expectedActiveGrantID *string,
	newCurrentResourceID string,
	candidateGrantID string,
	requireAcceptedNil bool,
) (AccessGroupState, error) {
	filter := bson.M{
		"companyId": companyID, "resourceType": resourceType, "resourceGroupKey": resourceGroupKey,
		"revision":          expectedRevision,
		"currentResourceId": expectedCurrentResourceID,
		"activeGrantId":     expectedActiveGrantID, // nil matches a stored null
	}
	if requireAcceptedNil {
		filter["acceptedResourceId"] = nil
	} else {
		// Rotation of the accepted version's own grant is permitted
		// (design spec §2.4): accepted must be nil OR exactly this version.
		filter["$or"] = []bson.M{
			{"acceptedResourceId": nil},
			{"acceptedResourceId": newCurrentResourceID},
		}
	}
	update := bson.M{
		"$set": bson.M{"currentResourceId": newCurrentResourceID, "activeGrantId": candidateGrantID},
		"$inc": bson.M{"revision": 1},
	}
	return r.conditionalUpdate(ctx, filter, update)
}

func (r *MongoAccessGroupStateRepository) ClearActiveGrant(ctx context.Context, companyID, resourceType, resourceGroupKey, expectedActiveGrantID string) (AccessGroupState, error) {
	filter := bson.M{
		"companyId": companyID, "resourceType": resourceType, "resourceGroupKey": resourceGroupKey,
		"activeGrantId": expectedActiveGrantID,
	}
	update := bson.M{
		"$set": bson.M{"activeGrantId": nil},
		"$inc": bson.M{"revision": 1},
	}
	return r.conditionalUpdate(ctx, filter, update)
}

// ClaimAcceptance is the acceptance fence. It runs BEFORE any Approval write,
// which is why no backward compensation is ever needed (design spec §7.2).
func (r *MongoAccessGroupStateRepository) ClaimAcceptance(ctx context.Context, companyID, resourceType, resourceGroupKey string, expectedRevision int64, resourceID, grantID string) (AccessGroupState, error) {
	filter := bson.M{
		"companyId": companyID, "resourceType": resourceType, "resourceGroupKey": resourceGroupKey,
		"revision":           expectedRevision,
		"currentResourceId":  resourceID,
		"activeGrantId":      grantID,
		"acceptedResourceId": nil,
	}
	update := bson.M{
		"$set": bson.M{"acceptedResourceId": resourceID},
		"$inc": bson.M{"revision": 1},
	}
	return r.conditionalUpdate(ctx, filter, update)
}

// FenceNonTerminalDecision serializes reject/request-changes against
// concurrent supersession and acceptance (design spec §7.3). It bumps the
// revision only — the decision itself lives in the approvals module.
func (r *MongoAccessGroupStateRepository) FenceNonTerminalDecision(ctx context.Context, companyID, resourceType, resourceGroupKey string, expectedRevision int64, resourceID, grantID string) (AccessGroupState, error) {
	filter := bson.M{
		"companyId": companyID, "resourceType": resourceType, "resourceGroupKey": resourceGroupKey,
		"revision":           expectedRevision,
		"currentResourceId":  resourceID,
		"activeGrantId":      grantID,
		"acceptedResourceId": nil,
	}
	update := bson.M{"$inc": bson.M{"revision": 1}}
	return r.conditionalUpdate(ctx, filter, update)
}

func (r *MongoAccessGroupStateRepository) conditionalUpdate(ctx context.Context, filter, update bson.M) (AccessGroupState, error) {
	var doc accessGroupStateDoc
	opts := options.FindOneAndUpdate().SetReturnDocument(options.After)
	err := r.collection.FindOneAndUpdate(ctx, filter, update, opts).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return AccessGroupState{}, ErrGroupStateRevisionMismatch
	}
	if err != nil {
		return AccessGroupState{}, err
	}
	return fromGroupStateDoc(doc), nil
}
