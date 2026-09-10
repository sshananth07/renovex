package supplieroffers

import (
	"context"
	"errors"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

const (
	collectionSupplierOfferWithdrawals = "supplier_offer_withdrawals"

	indexNameUniqueSupplierOfferWithdrawal = "uq_supplier_offer_withdrawals_company_version"
	indexNameUniqueSupplierOfferWithdrawOp = "uq_supplier_offer_withdrawals_company_operation"
)

// MongoOfferWithdrawalRepository exposes insertion only because withdrawal
// records are immutable once reconstructed from the eligibility claim.
type MongoOfferWithdrawalRepository struct {
	collection *mongo.Collection
}

func NewMongoOfferWithdrawalRepository(
	db *mongo.Database,
) *MongoOfferWithdrawalRepository {
	return &MongoOfferWithdrawalRepository{
		collection: db.Collection(collectionSupplierOfferWithdrawals),
	}
}

func (repository *MongoOfferWithdrawalRepository) EnsureIndexes(
	ctx context.Context,
) error {
	_, err := repository.collection.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{
			Keys: bson.D{
				{Key: "companyId", Value: 1},
				{Key: "supplierOfferVersionId", Value: 1},
			},
			Options: options.Index().
				SetName(indexNameUniqueSupplierOfferWithdrawal).
				SetUnique(true),
		},
		{
			Keys: bson.D{
				{Key: "companyId", Value: 1},
				{Key: "operationId", Value: 1},
			},
			Options: options.Index().
				SetName(indexNameUniqueSupplierOfferWithdrawOp).
				SetUnique(true),
		},
	})
	return err
}

type supplierOfferWithdrawalDocument struct {
	ID                     string    `bson:"_id"`
	CompanyID              string    `bson:"companyId"`
	OfferChainID           string    `bson:"offerChainId"`
	SupplierOfferVersionID string    `bson:"supplierOfferVersionId"`
	InvitationID           string    `bson:"invitationId"`
	RecipientIdentity      string    `bson:"recipientIdentity"`
	OperationID            string    `bson:"operationId"`
	Reason                 string    `bson:"reason"`
	WithdrawnAt            time.Time `bson:"withdrawnAt"`
	SchemaVersion          int       `bson:"schemaVersion"`
}

func supplierOfferWithdrawalToDocument(
	withdrawal SupplierOfferWithdrawal,
) supplierOfferWithdrawalDocument {
	return supplierOfferWithdrawalDocument{
		ID:                     withdrawal.ID,
		CompanyID:              withdrawal.CompanyID,
		OfferChainID:           withdrawal.OfferChainID,
		SupplierOfferVersionID: withdrawal.SupplierOfferVersionID,
		InvitationID:           withdrawal.InvitationID,
		RecipientIdentity:      withdrawal.RecipientIdentity,
		OperationID:            withdrawal.OperationID,
		Reason:                 withdrawal.Reason,
		WithdrawnAt:            withdrawal.WithdrawnAt,
		SchemaVersion:          withdrawal.SchemaVersion,
	}
}

func supplierOfferWithdrawalFromDocument(
	document supplierOfferWithdrawalDocument,
) SupplierOfferWithdrawal {
	return SupplierOfferWithdrawal{
		ID:                     document.ID,
		CompanyID:              document.CompanyID,
		OfferChainID:           document.OfferChainID,
		SupplierOfferVersionID: document.SupplierOfferVersionID,
		InvitationID:           document.InvitationID,
		RecipientIdentity:      document.RecipientIdentity,
		OperationID:            document.OperationID,
		Reason:                 document.Reason,
		WithdrawnAt:            document.WithdrawnAt,
		SchemaVersion:          document.SchemaVersion,
	}
}

// DeleteAllForCompany permanently removes every SupplierOfferWithdrawal owned
// by companyID. Never errors when zero documents match. Development-tool use
// only.
func (repository *MongoOfferWithdrawalRepository) DeleteAllForCompany(ctx context.Context, companyID string) error {
	_, err := repository.collection.DeleteMany(ctx, bson.M{"companyId": companyID})
	return err
}

func (repository *MongoOfferWithdrawalRepository) InsertWithdrawal(
	ctx context.Context,
	withdrawal SupplierOfferWithdrawal,
) error {
	_, err := repository.collection.InsertOne(
		ctx,
		supplierOfferWithdrawalToDocument(withdrawal),
	)
	return err
}

func (repository *MongoOfferWithdrawalRepository) FindWithdrawal(
	ctx context.Context,
	companyID string,
	offerVersionID string,
) (SupplierOfferWithdrawal, bool, error) {
	var document supplierOfferWithdrawalDocument
	err := repository.collection.FindOne(ctx, bson.M{
		"companyId":              companyID,
		"supplierOfferVersionId": offerVersionID,
	}).Decode(&document)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return SupplierOfferWithdrawal{}, false, nil
	}
	if err != nil {
		return SupplierOfferWithdrawal{}, false, err
	}
	return supplierOfferWithdrawalFromDocument(document), true, nil
}
