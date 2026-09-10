package supplieraccess

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
	collectionSessionInvitationBindings = "supplier_session_invitation_bindings"
	indexBindingSessionInvitation       = "uq_supplier_bindings_company_session_invitation"
	indexBindingSession                 = "idx_supplier_bindings_company_session"
	indexBindingInvitationGen           = "idx_supplier_bindings_company_invitation_generation"
)

type MongoSessionInvitationBindingRepository struct {
	collection *mongo.Collection
}

func NewMongoSessionInvitationBindingRepository(
	db *mongo.Database) *MongoSessionInvitationBindingRepository {
	return &MongoSessionInvitationBindingRepository{
		collection: db.Collection(collectionSessionInvitationBindings),
	}
}

func (r *MongoSessionInvitationBindingRepository) EnsureIndexes(ctx context.Context) error {
	_, err := r.collection.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{
			Keys: bson.D{
				{Key: "companyId", Value: 1},
				{Key: "supplierSessionId", Value: 1},
				{Key: "invitationId", Value: 1},
			},
			Options: options.Index().SetUnique(true).SetName(indexBindingSessionInvitation),
		},
		{
			Keys: bson.D{
				{Key: "companyId", Value: 1},
				{Key: "supplierSessionId", Value: 1},
			},
			Options: options.Index().SetName(indexBindingSession),
		},
		{
			Keys: bson.D{
				{Key: "companyId", Value: 1},
				{Key: "invitationId", Value: 1},
				{Key: "accessGeneration", Value: 1},
			},
			Options: options.Index().SetName(indexBindingInvitationGen),
		},
	})
	return err
}

type sessionInvitationBindingDoc struct {
	ID                       string    `bson:"_id"`
	CompanyID                string    `bson:"companyId"`
	SupplierSessionID        string    `bson:"supplierSessionId"`
	SupplierID               string    `bson:"supplierId"`
	NormalizedRecipientEmail string    `bson:"normalizedRecipientEmail"`
	InvitationID             string    `bson:"invitationId"`
	AccessGeneration         int64     `bson:"accessGeneration"`
	BoundAt                  time.Time `bson:"boundAt"`
	LastValidatedAt          time.Time `bson:"lastValidatedAt"`
	Revision                 int64     `bson:"revision"`
}

func toSessionInvitationBindingDoc(
	binding SupplierSessionInvitationBinding) sessionInvitationBindingDoc {
	return sessionInvitationBindingDoc{
		ID: binding.ID, CompanyID: binding.CompanyID,
		SupplierSessionID:        binding.SupplierSessionID,
		SupplierID:               binding.SupplierID,
		NormalizedRecipientEmail: binding.NormalizedRecipientEmail,
		InvitationID:             binding.InvitationID, AccessGeneration: binding.AccessGeneration,
		BoundAt: binding.BoundAt, LastValidatedAt: binding.LastValidatedAt,
		Revision: binding.Revision,
	}
}

func fromSessionInvitationBindingDoc(
	doc sessionInvitationBindingDoc) SupplierSessionInvitationBinding {
	return SupplierSessionInvitationBinding{
		ID: doc.ID, CompanyID: doc.CompanyID,
		SupplierSessionID: doc.SupplierSessionID, SupplierID: doc.SupplierID,
		NormalizedRecipientEmail: doc.NormalizedRecipientEmail,
		InvitationID:             doc.InvitationID, AccessGeneration: doc.AccessGeneration,
		BoundAt: doc.BoundAt, LastValidatedAt: doc.LastValidatedAt,
		Revision: doc.Revision,
	}
}

func validateSessionInvitationBinding(
	binding SupplierSessionInvitationBinding) error {
	if binding.ID == "" || binding.CompanyID == "" ||
		binding.SupplierSessionID == "" || binding.SupplierID == "" ||
		binding.NormalizedRecipientEmail == "" || binding.InvitationID == "" ||
		binding.AccessGeneration < 1 || binding.BoundAt.IsZero() ||
		binding.LastValidatedAt.IsZero() || binding.Revision < 1 {
		return ErrInvalidSessionInvitationBinding
	}
	return nil
}

// DeleteAllForCompany permanently removes every
// SupplierSessionInvitationBinding owned by companyID. Never errors when zero
// documents match. Development-tool use only.
func (r *MongoSessionInvitationBindingRepository) DeleteAllForCompany(ctx context.Context, companyID string) error {
	_, err := r.collection.DeleteMany(ctx, bson.M{"companyId": companyID})
	return err
}

func (r *MongoSessionInvitationBindingRepository) CreateBinding(ctx context.Context,
	binding SupplierSessionInvitationBinding) (SupplierSessionInvitationBinding, error) {

	if err := validateSessionInvitationBinding(binding); err != nil {
		return SupplierSessionInvitationBinding{}, err
	}
	_, err := r.collection.InsertOne(ctx, toSessionInvitationBindingDoc(binding))
	if err == nil {
		return binding, nil
	}
	if mongo.IsDuplicateKeyError(err) &&
		strings.Contains(err.Error(), indexBindingSessionInvitation) {
		return SupplierSessionInvitationBinding{},
			ErrSessionInvitationBindingAlreadyExists
	}
	return SupplierSessionInvitationBinding{}, err
}

func (r *MongoSessionInvitationBindingRepository) FindBinding(ctx context.Context,
	companyID, sessionID, invitationID string) (
	SupplierSessionInvitationBinding, error) {

	var doc sessionInvitationBindingDoc
	err := r.collection.FindOne(ctx, bson.M{
		"companyId": companyID, "supplierSessionId": sessionID,
		"invitationId": invitationID,
	}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return SupplierSessionInvitationBinding{},
			ErrSessionInvitationBindingNotFound
	}
	if err != nil {
		return SupplierSessionInvitationBinding{}, err
	}
	return fromSessionInvitationBindingDoc(doc), nil
}

func (r *MongoSessionInvitationBindingRepository) ListSessionBindings(
	ctx context.Context, companyID, sessionID string) (
	[]SupplierSessionInvitationBinding, error) {

	cursor, err := r.collection.Find(ctx, bson.M{
		"companyId": companyID, "supplierSessionId": sessionID,
	})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	var docs []sessionInvitationBindingDoc
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, err
	}
	bindings := make([]SupplierSessionInvitationBinding, 0, len(docs))
	for _, doc := range docs {
		bindings = append(bindings, fromSessionInvitationBindingDoc(doc))
	}
	return bindings, nil
}

// ReplaceBindingCAS is the only way a verified session may advance an
// invitation binding to a new generation. Immutable identity fields are part
// of the predicate, so a caller cannot move a binding across tenants.
func (r *MongoSessionInvitationBindingRepository) ReplaceBindingCAS(ctx context.Context,
	binding SupplierSessionInvitationBinding,
	expectedRevision int64) (SupplierSessionInvitationBinding, error) {

	if expectedRevision < 1 || binding.Revision != expectedRevision+1 {
		return SupplierSessionInvitationBinding{}, ErrInvalidSessionInvitationBinding
	}
	if err := validateSessionInvitationBinding(binding); err != nil {
		return SupplierSessionInvitationBinding{}, err
	}
	var updated sessionInvitationBindingDoc
	err := r.collection.FindOneAndUpdate(ctx, bson.M{
		"_id": binding.ID, "companyId": binding.CompanyID,
		"supplierSessionId":        binding.SupplierSessionID,
		"supplierId":               binding.SupplierID,
		"normalizedRecipientEmail": binding.NormalizedRecipientEmail,
		"invitationId":             binding.InvitationID,
		"revision":                 expectedRevision,
	}, bson.M{"$set": bson.M{
		"accessGeneration": binding.AccessGeneration,
		"boundAt":          binding.BoundAt,
		"lastValidatedAt":  binding.LastValidatedAt,
		"revision":         binding.Revision,
	}}, options.FindOneAndUpdate().SetReturnDocument(options.After)).Decode(&updated)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return SupplierSessionInvitationBinding{},
			ErrSessionInvitationBindingConflict
	}
	if err != nil {
		return SupplierSessionInvitationBinding{}, err
	}
	return fromSessionInvitationBindingDoc(updated), nil
}
