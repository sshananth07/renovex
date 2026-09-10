package clients

import (
	"context"
	"errors"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/shananth/renovation-platform/backend/internal/foundation/pagination"
)

// MongoClientRepository is the MongoDB-backed ClientRepository implementation. It
// owns the "clients" collection exclusively.
type MongoClientRepository struct {
	collection *mongo.Collection
}

// NewMongoClientRepository constructs a MongoClientRepository against db's "clients"
// collection.
func NewMongoClientRepository(db *mongo.Database) *MongoClientRepository {
	return &MongoClientRepository{collection: db.Collection("clients")}
}

// EnsureIndexes creates the companyId index required for scoped list
// queries, plus a companyId+createdAt+_id compound index matching the
// default list-endpoint query/sort (createdAt desc) with its _id
// tie-breaker.
func (r *MongoClientRepository) EnsureIndexes(ctx context.Context) error {
	_, err := r.collection.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "companyId", Value: 1}}},
		{Keys: bson.D{{Key: "companyId", Value: 1}, {Key: "createdAt", Value: -1}, {Key: "_id", Value: -1}}},
	})
	return err
}

type clientDoc struct {
	ID             bson.ObjectID `bson:"_id,omitempty"`
	CompanyID      string        `bson:"companyId"`
	Name           string        `bson:"name"`
	Phone          string        `bson:"phone,omitempty"`
	Email          string        `bson:"email,omitempty"`
	Address        string        `bson:"address,omitempty"`
	BillingAddress string        `bson:"billingAddress,omitempty"`
	Notes          string        `bson:"notes,omitempty"`
	CreatedAt      time.Time     `bson:"createdAt"`
	SchemaVersion  int           `bson:"schemaVersion"`
}

func (r *MongoClientRepository) Create(ctx context.Context, c Client) (Client, error) {
	doc := toClientDoc(c)
	res, err := r.collection.InsertOne(ctx, doc)
	if err != nil {
		return Client{}, err
	}
	c.ID = res.InsertedID.(bson.ObjectID).Hex()
	return c, nil
}

func (r *MongoClientRepository) FindByID(ctx context.Context, companyID, id string) (Client, error) {
	objID, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return Client{}, ErrClientNotFound
	}
	var doc clientDoc
	err = r.collection.FindOne(ctx, bson.M{"_id": objID, "companyId": companyID}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return Client{}, ErrClientNotFound
	}
	if err != nil {
		return Client{}, err
	}
	return fromClientDoc(doc), nil
}

// ListPaginated builds one filter — tenant scope plus an optional
// case-insensitive regex search across name/email/phone — and uses that
// SAME filter for both CountDocuments and the sorted, paginated Find, so
// total can never diverge from what the page itself matched.
func (r *MongoClientRepository) ListPaginated(ctx context.Context, companyID string, req pagination.Request) ([]Client, int, error) {
	filter := bson.M{"companyId": companyID}
	if pattern := req.SearchRegexPattern(); pattern != "" {
		regex := bson.M{"$regex": pattern, "$options": "i"}
		filter["$or"] = bson.A{
			bson.M{"name": regex},
			bson.M{"email": regex},
			bson.M{"phone": regex},
		}
	}

	total, err := r.collection.CountDocuments(ctx, filter)
	if err != nil {
		return nil, 0, err
	}

	cursor, err := r.collection.Find(ctx, filter,
		options.Find().
			SetSort(req.MongoSort()).
			SetSkip(int64(req.Offset())).
			SetLimit(int64(req.PageSize)))
	if err != nil {
		return nil, 0, err
	}
	defer cursor.Close(ctx)

	var docs []clientDoc
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, 0, err
	}

	result := make([]Client, 0, len(docs))
	for _, doc := range docs {
		result = append(result, fromClientDoc(doc))
	}
	return result, int(total), nil
}

// DeleteAllForCompany permanently removes every Client owned by companyID.
// Never errors when zero documents match (Mongo's own DeleteMany
// semantics) — repeatable by design. Development-tool use only.
//
// Deliberately NOT part of the ClientRepository interface: adding it there
// would force every fake/mock ClientRepository elsewhere in the codebase
// to implement it too, or stop compiling, for a method only a
// development-only tool ever calls. Service.DeleteAllForCompany reaches
// this via an unexported type assertion instead (see service.go).
func (r *MongoClientRepository) DeleteAllForCompany(ctx context.Context, companyID string) error {
	_, err := r.collection.DeleteMany(ctx, bson.M{"companyId": companyID})
	return err
}

func (r *MongoClientRepository) Update(ctx context.Context, companyID, id string, fn func(*Client)) (Client, error) {
	existing, err := r.FindByID(ctx, companyID, id)
	if err != nil {
		return Client{}, err
	}
	fn(&existing)

	objID, _ := bson.ObjectIDFromHex(id) // already validated by FindByID above
	doc := toClientDoc(existing)
	res, err := r.collection.UpdateOne(ctx,
		bson.M{"_id": objID, "companyId": companyID},
		bson.M{"$set": doc},
	)
	if err != nil {
		return Client{}, err
	}
	if res.MatchedCount == 0 {
		return Client{}, ErrClientNotFound
	}
	return existing, nil
}

func toClientDoc(c Client) clientDoc {
	doc := clientDoc{
		CompanyID: c.CompanyID, Name: c.Name, Phone: c.Phone, Email: c.Email,
		Address: c.Address, BillingAddress: c.BillingAddress, Notes: c.Notes,
		CreatedAt: c.CreatedAt, SchemaVersion: c.SchemaVersion,
	}
	if c.ID != "" {
		objID, _ := bson.ObjectIDFromHex(c.ID)
		doc.ID = objID
	}
	return doc
}

func fromClientDoc(doc clientDoc) Client {
	return Client{
		ID: doc.ID.Hex(), CompanyID: doc.CompanyID, Name: doc.Name, Phone: doc.Phone,
		Email: doc.Email, Address: doc.Address, BillingAddress: doc.BillingAddress,
		Notes: doc.Notes, CreatedAt: doc.CreatedAt, SchemaVersion: doc.SchemaVersion,
	}
}
