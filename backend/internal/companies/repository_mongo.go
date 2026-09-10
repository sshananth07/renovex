package companies

import (
	"context"
	"errors"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/shananth/renovation-platform/backend/internal/foundation/pagination"
)

// MongoCompanyRepository is the MongoDB-backed CompanyRepository implementation.
type MongoCompanyRepository struct {
	collection *mongo.Collection
}

// NewMongoCompanyRepository constructs a MongoCompanyRepository against db's
// "companies" collection.
func NewMongoCompanyRepository(db *mongo.Database) *MongoCompanyRepository {
	return &MongoCompanyRepository{collection: db.Collection("companies")}
}

type companyDoc struct {
	ID        bson.ObjectID `bson:"_id,omitempty"`
	Name      string        `bson:"name"`
	CreatedAt time.Time     `bson:"createdAt"`
}

func (r *MongoCompanyRepository) Create(ctx context.Context, c Company) (Company, error) {
	doc := companyDoc{Name: c.Name, CreatedAt: c.CreatedAt}
	res, err := r.collection.InsertOne(ctx, doc)
	if err != nil {
		return Company{}, err
	}
	c.ID = res.InsertedID.(bson.ObjectID).Hex()
	return c, nil
}

func (r *MongoCompanyRepository) FindByID(ctx context.Context, id string) (Company, error) {
	objID, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return Company{}, ErrCompanyNotFound
	}
	var doc companyDoc
	err = r.collection.FindOne(ctx, bson.M{"_id": objID}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return Company{}, ErrCompanyNotFound
	}
	if err != nil {
		return Company{}, err
	}
	return Company{ID: doc.ID.Hex(), Name: doc.Name, CreatedAt: doc.CreatedAt}, nil
}

// FindByName looks up a Company by its exact display name. Used only by
// demoseed's positive-identification checks (design spec §6.2).
func (r *MongoCompanyRepository) FindByName(ctx context.Context, name string) (Company, error) {
	var doc companyDoc
	err := r.collection.FindOne(ctx, bson.M{"name": name}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return Company{}, ErrCompanyNotFound
	}
	if err != nil {
		return Company{}, err
	}
	return Company{ID: doc.ID.Hex(), Name: doc.Name, CreatedAt: doc.CreatedAt}, nil
}

func (r *MongoCompanyRepository) Delete(ctx context.Context, id string) error {
	objID, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return ErrCompanyNotFound
	}
	res, err := r.collection.DeleteOne(ctx, bson.M{"_id": objID})
	if err != nil {
		return err
	}
	if res.DeletedCount == 0 {
		return ErrCompanyNotFound
	}
	return nil
}

// EnsureIndexes creates indexes required by MongoCompanyRepository. Currently none
// beyond the default _id index; present for symmetry with MongoMembershipRepository
// and future extension.
func (r *MongoCompanyRepository) EnsureIndexes(ctx context.Context) error {
	return nil
}

// MongoMembershipRepository is the MongoDB-backed MembershipRepository
// implementation.
type MongoMembershipRepository struct {
	collection *mongo.Collection
}

// NewMongoMembershipRepository constructs a MongoMembershipRepository against
// db's "company_members" collection.
func NewMongoMembershipRepository(db *mongo.Database) *MongoMembershipRepository {
	return &MongoMembershipRepository{collection: db.Collection("company_members")}
}

// EnsureIndexes creates the unique userId index, the companyId index required
// by the tenancy design, and a companyId+createdAt+_id compound index
// matching the default member-list query/sort.
func (r *MongoMembershipRepository) EnsureIndexes(ctx context.Context) error {
	_, err := r.collection.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "userId", Value: 1}}, Options: options.Index().SetUnique(true)},
		{Keys: bson.D{{Key: "companyId", Value: 1}}},
		{Keys: bson.D{{Key: "companyId", Value: 1}, {Key: "createdAt", Value: -1}, {Key: "_id", Value: -1}}},
	})
	return err
}

type membershipDoc struct {
	ID        bson.ObjectID `bson:"_id,omitempty"`
	UserID    string        `bson:"userId"`
	CompanyID string        `bson:"companyId"`
	Role      string        `bson:"role"`
	CreatedAt time.Time     `bson:"createdAt"`
}

func (r *MongoMembershipRepository) Create(ctx context.Context, m CompanyMembership) (CompanyMembership, error) {
	doc := membershipDoc{UserID: m.UserID, CompanyID: m.CompanyID, Role: string(m.Role), CreatedAt: m.CreatedAt}
	res, err := r.collection.InsertOne(ctx, doc)
	if mongo.IsDuplicateKeyError(err) {
		return CompanyMembership{}, ErrUserAlreadyHasMembership
	}
	if err != nil {
		return CompanyMembership{}, err
	}
	m.ID = res.InsertedID.(bson.ObjectID).Hex()
	return m, nil
}

func (r *MongoMembershipRepository) FindByUserID(ctx context.Context, userID string) (CompanyMembership, error) {
	var doc membershipDoc
	err := r.collection.FindOne(ctx, bson.M{"userId": userID}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return CompanyMembership{}, ErrMembershipNotFound
	}
	if err != nil {
		return CompanyMembership{}, err
	}
	return toMembership(doc), nil
}

// ListPaginated builds one filter — tenant scope plus an optional
// case-insensitive regex search across userId/role — and uses that SAME
// filter for both CountDocuments and the sorted, paginated Find.
func (r *MongoMembershipRepository) ListPaginated(ctx context.Context, companyID string, req pagination.Request) ([]CompanyMembership, int, error) {
	filter := bson.M{"companyId": companyID}
	if pattern := req.SearchRegexPattern(); pattern != "" {
		regex := bson.M{"$regex": pattern, "$options": "i"}
		filter["$or"] = bson.A{
			bson.M{"userId": regex},
			bson.M{"role": regex},
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

	var docs []membershipDoc
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, 0, err
	}

	memberships := make([]CompanyMembership, 0, len(docs))
	for _, doc := range docs {
		memberships = append(memberships, toMembership(doc))
	}
	return memberships, int(total), nil
}

func (r *MongoMembershipRepository) DeleteByUserID(ctx context.Context, userID string) error {
	res, err := r.collection.DeleteOne(ctx, bson.M{"userId": userID})
	if err != nil {
		return err
	}
	if res.DeletedCount == 0 {
		return ErrMembershipNotFound
	}
	return nil
}

func toMembership(doc membershipDoc) CompanyMembership {
	return CompanyMembership{
		ID:        doc.ID.Hex(),
		UserID:    doc.UserID,
		CompanyID: doc.CompanyID,
		Role:      Role(doc.Role),
		CreatedAt: doc.CreatedAt,
	}
}
