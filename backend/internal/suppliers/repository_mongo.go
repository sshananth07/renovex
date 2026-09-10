package suppliers

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/shananth/renovation-platform/backend/internal/foundation/money"
)

const (
	collectionSuppliers   = "suppliers"
	collectionOfferings   = "supplier_offerings"
	collectionPreferences = "material_supplier_preferences"

	indexNameSuppliersCompany           = "idx_suppliers_company"
	indexNameUniqueSuppliersCompanyName = "uq_suppliers_company_name"
	indexNameSuppliersCompanyActive     = "idx_suppliers_company_active"
	indexNameSuppliersCompanyCategories = "idx_suppliers_company_categories"

	indexNameOfferingsCompany         = "idx_supplier_offerings_company"
	indexNameOfferingsCompanySupplier = "idx_supplier_offerings_company_supplier"
	indexNameOfferingsCompanyMaterial = "idx_supplier_offerings_company_material"
	indexNameOfferingsCompanyActive   = "idx_supplier_offerings_company_active"

	indexNameUniquePreferencesMaterial = "uq_material_supplier_preferences_material"
	indexNamePreferencesSupplier       = "idx_material_supplier_preferences_supplier"
)

// --- Supplier ---

// MongoSupplierRepository is the MongoDB-backed SupplierRepository.
type MongoSupplierRepository struct {
	collection *mongo.Collection
}

// NewMongoSupplierRepository constructs the repository over db's suppliers
// collection.
func NewMongoSupplierRepository(db *mongo.Database) *MongoSupplierRepository {
	return &MongoSupplierRepository{collection: db.Collection(collectionSuppliers)}
}

// EnsureIndexes creates the §12.3 supplier indexes. Idempotent.
func (r *MongoSupplierRepository) EnsureIndexes(ctx context.Context) error {
	_, err := r.collection.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "companyId", Value: 1}},
			Options: options.Index().SetName(indexNameSuppliersCompany)},
		// Unique and deliberately NOT partial on active: an inactive record
		// still blocks its name, which is what stops retirement from being
		// followed by accidental duplication (design spec §4.1).
		{Keys: bson.D{{Key: "companyId", Value: 1}, {Key: "nameNormalized", Value: 1}},
			Options: options.Index().SetUnique(true).SetName(indexNameUniqueSuppliersCompanyName)},
		{Keys: bson.D{{Key: "companyId", Value: 1}, {Key: "active", Value: 1}},
			Options: options.Index().SetName(indexNameSuppliersCompanyActive)},
		// Multikey, for the category filter.
		{Keys: bson.D{{Key: "companyId", Value: 1}, {Key: "materialCategories", Value: 1}},
			Options: options.Index().SetName(indexNameSuppliersCompanyCategories)},
	})
	return err
}

type supplierDoc struct {
	ID             bson.ObjectID `bson:"_id,omitempty"`
	CompanyID      string        `bson:"companyId"`
	Name           string        `bson:"name"`
	NameNormalized string        `bson:"nameNormalized"`

	ContactPerson string `bson:"contactPerson,omitempty"`
	Email         string `bson:"email,omitempty"`
	Phone         string `bson:"phone,omitempty"`
	Address       string `bson:"address,omitempty"`

	MaterialCategories []string `bson:"materialCategories,omitempty"`
	Notes              string   `bson:"notes,omitempty"`

	Active bool `bson:"active"`

	Revision        int64     `bson:"revision"`
	CreatedByUserID string    `bson:"createdByUserId,omitempty"`
	CreatedAt       time.Time `bson:"createdAt"`
	UpdatedAt       time.Time `bson:"updatedAt"`
	SchemaVersion   int       `bson:"schemaVersion"`
}

func toSupplierDoc(s Supplier) supplierDoc {
	return supplierDoc{
		CompanyID: s.CompanyID, Name: s.Name, NameNormalized: s.NameNormalized,
		ContactPerson: s.ContactPerson, Email: s.Email, Phone: s.Phone, Address: s.Address,
		MaterialCategories: s.MaterialCategories, Notes: s.Notes,
		Active:   s.Active,
		Revision: s.Revision, CreatedByUserID: s.CreatedByUserID,
		CreatedAt: s.CreatedAt, UpdatedAt: s.UpdatedAt, SchemaVersion: s.SchemaVersion,
	}
}

func fromSupplierDoc(doc supplierDoc) Supplier {
	return Supplier{
		ID: doc.ID.Hex(), CompanyID: doc.CompanyID,
		Name: doc.Name, NameNormalized: doc.NameNormalized,
		ContactPerson: doc.ContactPerson, Email: doc.Email,
		Phone: doc.Phone, Address: doc.Address,
		MaterialCategories: doc.MaterialCategories, Notes: doc.Notes,
		Active:   doc.Active,
		Revision: doc.Revision, CreatedByUserID: doc.CreatedByUserID,
		CreatedAt: doc.CreatedAt, UpdatedAt: doc.UpdatedAt, SchemaVersion: doc.SchemaVersion,
	}
}

// classifySupplierDuplicate translates a duplicate key into the named sentinel
// by matching the EXPLICIT index name. A duplicate this function cannot
// positively identify returns ErrUnclassifiedDuplicateKey rather than being
// assumed to be a name collision (design spec §12.4).
func classifySupplierDuplicate(err error) error {
	if !mongo.IsDuplicateKeyError(err) {
		return err
	}
	var writeException mongo.WriteException
	if errors.As(err, &writeException) {
		for _, we := range writeException.WriteErrors {
			if strings.Contains(we.Message, indexNameUniqueSuppliersCompanyName) {
				return ErrSupplierNameTaken
			}
		}
	}
	return ErrUnclassifiedDuplicateKey
}

func (r *MongoSupplierRepository) Create(ctx context.Context, s Supplier) (Supplier, error) {
	res, err := r.collection.InsertOne(ctx, toSupplierDoc(s))
	if err != nil {
		return Supplier{}, classifySupplierDuplicate(err)
	}
	s.ID = res.InsertedID.(bson.ObjectID).Hex()
	return s, nil
}

func (r *MongoSupplierRepository) FindByID(ctx context.Context, companyID, id string) (Supplier, error) {
	objID, err := bson.ObjectIDFromHex(id)
	if err != nil {
		// A malformed id is reported as not-found so the handler maps it to 404
		// rather than 500.
		return Supplier{}, ErrSupplierNotFound
	}
	var doc supplierDoc
	err = r.collection.FindOne(ctx, bson.M{"_id": objID, "companyId": companyID}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return Supplier{}, ErrSupplierNotFound
	}
	if err != nil {
		return Supplier{}, err
	}
	return fromSupplierDoc(doc), nil
}

func (r *MongoSupplierRepository) List(ctx context.Context, companyID string,
	filter SupplierFilter) ([]Supplier, error) {

	q := bson.M{"companyId": companyID}
	if trimmed := strings.TrimSpace(filter.Query); trimmed != "" {
		// Matched against the normalized name, so a search behaves the same way
		// uniqueness does. The term is quoted, so a user typing regex
		// metacharacters searches for those literal characters.
		q["nameNormalized"] = bson.M{"$regex": regexp.QuoteMeta(strings.ToLower(trimmed))}
	}
	if category := strings.TrimSpace(filter.Category); category != "" {
		q["materialCategories"] = category
	}
	if filter.Active != nil {
		q["active"] = *filter.Active
	}

	cursor, err := r.collection.Find(ctx, q)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var docs []supplierDoc
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, err
	}
	out := make([]Supplier, 0, len(docs))
	for _, doc := range docs {
		out = append(out, fromSupplierDoc(doc))
	}
	return out, nil
}

// conditionalUpdate applies set under a Revision guard, bumping revision by
// one. On a 0-match it re-reads to distinguish a missing/foreign document (404)
// from a revision conflict (409).
func (r *MongoSupplierRepository) conditionalUpdate(ctx context.Context, companyID, id string,
	expectedRevision int64, set bson.M) (Supplier, error) {
	objID, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return Supplier{}, ErrSupplierNotFound
	}
	set["revision"] = expectedRevision + 1

	res, err := r.collection.UpdateOne(ctx,
		bson.M{"_id": objID, "companyId": companyID, "revision": expectedRevision},
		bson.M{"$set": set})
	if err != nil {
		return Supplier{}, classifySupplierDuplicate(err)
	}
	if res.MatchedCount == 0 {
		if _, findErr := r.FindByID(ctx, companyID, id); errors.Is(findErr, ErrSupplierNotFound) {
			return Supplier{}, ErrSupplierNotFound
		}
		return Supplier{}, ErrRevisionMismatch
	}
	return r.FindByID(ctx, companyID, id)
}

// Update writes NameNormalized alongside Name, so the uniqueness key can never
// drift from the display name.
// DeleteAllForCompany permanently removes every Supplier owned by
// companyID. Never errors when zero documents match. Development-tool use
// only.
func (r *MongoSupplierRepository) DeleteAllForCompany(ctx context.Context, companyID string) error {
	_, err := r.collection.DeleteMany(ctx, bson.M{"companyId": companyID})
	return err
}

func (r *MongoSupplierRepository) Update(ctx context.Context, companyID, id string,
	expectedRevision int64, updated Supplier) (Supplier, error) {
	set := bson.M{
		"name":               updated.Name,
		"nameNormalized":     updated.NameNormalized,
		"contactPerson":      updated.ContactPerson,
		"email":              updated.Email,
		"phone":              updated.Phone,
		"address":            updated.Address,
		"materialCategories": updated.MaterialCategories,
		"notes":              updated.Notes,
		"updatedAt":          time.Now(),
	}
	return r.conditionalUpdate(ctx, companyID, id, expectedRevision, set)
}

// SetActive retires or reactivates. Soft only — nothing is ever deleted, and no
// cascade touches offerings or preferences (design spec §4.1).
func (r *MongoSupplierRepository) SetActive(ctx context.Context, companyID, id string,
	expectedRevision int64, active bool) (Supplier, error) {
	return r.conditionalUpdate(ctx, companyID, id, expectedRevision, bson.M{
		"active":    active,
		"updatedAt": time.Now(),
	})
}

// --- Offering ---

// MongoSupplierOfferingRepository is the MongoDB-backed
// SupplierOfferingRepository.
type MongoSupplierOfferingRepository struct {
	collection *mongo.Collection
}

// NewMongoSupplierOfferingRepository constructs the repository over db's
// supplier_offerings collection.
func NewMongoSupplierOfferingRepository(db *mongo.Database) *MongoSupplierOfferingRepository {
	return &MongoSupplierOfferingRepository{collection: db.Collection(collectionOfferings)}
}

// EnsureIndexes creates the §12.3 offering indexes. Idempotent.
func (r *MongoSupplierOfferingRepository) EnsureIndexes(ctx context.Context) error {
	_, err := r.collection.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "companyId", Value: 1}},
			Options: options.Index().SetName(indexNameOfferingsCompany)},
		{Keys: bson.D{{Key: "companyId", Value: 1}, {Key: "supplierId", Value: 1}},
			Options: options.Index().SetName(indexNameOfferingsCompanySupplier)},
		{Keys: bson.D{{Key: "companyId", Value: 1}, {Key: "materialId", Value: 1}},
			Options: options.Index().SetName(indexNameOfferingsCompanyMaterial)},
		{Keys: bson.D{{Key: "companyId", Value: 1}, {Key: "active", Value: 1}},
			Options: options.Index().SetName(indexNameOfferingsCompanyActive)},
	})
	return err
}

type moneyDoc struct {
	Amount   int64  `bson:"amount"`
	Currency string `bson:"currency"`
}

type offeringDoc struct {
	ID         bson.ObjectID `bson:"_id,omitempty"`
	CompanyID  string        `bson:"companyId"`
	SupplierID string        `bson:"supplierId"`
	MaterialID *string       `bson:"materialId,omitempty"`

	ProductName string `bson:"productName"`
	Brand       string `bson:"brand,omitempty"`
	SKU         string `bson:"sku,omitempty"`
	Description string `bson:"description,omitempty"`
	Category    string `bson:"category,omitempty"`
	Unit        string `bson:"unit,omitempty"`

	ImageURL   *string `bson:"imageUrl,omitempty"`
	ProductURL *string `bson:"productUrl,omitempty"`

	IndicativePrice     *moneyDoc  `bson:"indicativePrice,omitempty"`
	IndicativePriceAsOf *time.Time `bson:"indicativePriceAsOf,omitempty"`
	LastVerifiedAt      *time.Time `bson:"lastVerifiedAt,omitempty"`

	Active bool `bson:"active"`

	Revision        int64     `bson:"revision"`
	CreatedByUserID string    `bson:"createdByUserId,omitempty"`
	CreatedAt       time.Time `bson:"createdAt"`
	UpdatedAt       time.Time `bson:"updatedAt"`
	SchemaVersion   int       `bson:"schemaVersion"`
}

func toOfferingDoc(o SupplierOffering) offeringDoc {
	doc := offeringDoc{
		CompanyID: o.CompanyID, SupplierID: o.SupplierID, MaterialID: o.MaterialID,
		ProductName: o.ProductName, Brand: o.Brand, SKU: o.SKU,
		Description: o.Description, Category: o.Category, Unit: o.Unit,
		ImageURL: o.ImageURL, ProductURL: o.ProductURL,
		IndicativePriceAsOf: o.IndicativePriceAsOf, LastVerifiedAt: o.LastVerifiedAt,
		Active:   o.Active,
		Revision: o.Revision, CreatedByUserID: o.CreatedByUserID,
		CreatedAt: o.CreatedAt, UpdatedAt: o.UpdatedAt, SchemaVersion: o.SchemaVersion,
	}
	if o.IndicativePrice != nil {
		doc.IndicativePrice = &moneyDoc{
			Amount: o.IndicativePrice.Amount, Currency: o.IndicativePrice.Currency,
		}
	}
	return doc
}

func fromOfferingDoc(doc offeringDoc) SupplierOffering {
	o := SupplierOffering{
		ID: doc.ID.Hex(), CompanyID: doc.CompanyID,
		SupplierID: doc.SupplierID, MaterialID: doc.MaterialID,
		ProductName: doc.ProductName, Brand: doc.Brand, SKU: doc.SKU,
		Description: doc.Description, Category: doc.Category, Unit: doc.Unit,
		ImageURL: doc.ImageURL, ProductURL: doc.ProductURL,
		IndicativePriceAsOf: doc.IndicativePriceAsOf, LastVerifiedAt: doc.LastVerifiedAt,
		Active:   doc.Active,
		Revision: doc.Revision, CreatedByUserID: doc.CreatedByUserID,
		CreatedAt: doc.CreatedAt, UpdatedAt: doc.UpdatedAt, SchemaVersion: doc.SchemaVersion,
	}
	if doc.IndicativePrice != nil {
		m := money.New(doc.IndicativePrice.Amount, doc.IndicativePrice.Currency)
		o.IndicativePrice = &m
	}
	return o
}

func (r *MongoSupplierOfferingRepository) Create(ctx context.Context,
	o SupplierOffering) (SupplierOffering, error) {
	res, err := r.collection.InsertOne(ctx, toOfferingDoc(o))
	if err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return SupplierOffering{}, ErrUnclassifiedDuplicateKey
		}
		return SupplierOffering{}, err
	}
	o.ID = res.InsertedID.(bson.ObjectID).Hex()
	return o, nil
}

func (r *MongoSupplierOfferingRepository) FindByID(ctx context.Context,
	companyID, id string) (SupplierOffering, error) {
	objID, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return SupplierOffering{}, ErrSupplierOfferingNotFound
	}
	var doc offeringDoc
	err = r.collection.FindOne(ctx, bson.M{"_id": objID, "companyId": companyID}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return SupplierOffering{}, ErrSupplierOfferingNotFound
	}
	if err != nil {
		return SupplierOffering{}, err
	}
	return fromOfferingDoc(doc), nil
}

func (r *MongoSupplierOfferingRepository) find(ctx context.Context,
	filter bson.M) ([]SupplierOffering, error) {
	cursor, err := r.collection.Find(ctx, filter)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var docs []offeringDoc
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, err
	}
	out := make([]SupplierOffering, 0, len(docs))
	for _, doc := range docs {
		out = append(out, fromOfferingDoc(doc))
	}
	return out, nil
}

func (r *MongoSupplierOfferingRepository) List(ctx context.Context, companyID string,
	filter OfferingFilter) ([]SupplierOffering, error) {
	q := bson.M{"companyId": companyID}
	if filter.SupplierID != "" {
		q["supplierId"] = filter.SupplierID
	}
	if filter.MaterialID != "" {
		q["materialId"] = filter.MaterialID
	}
	if filter.Active != nil {
		q["active"] = *filter.Active
	}
	return r.find(ctx, q)
}

func (r *MongoSupplierOfferingRepository) ListBySupplier(ctx context.Context,
	companyID, supplierID string) ([]SupplierOffering, error) {
	return r.find(ctx, bson.M{"companyId": companyID, "supplierId": supplierID})
}

func (r *MongoSupplierOfferingRepository) conditionalUpdate(ctx context.Context, companyID, id string,
	expectedRevision int64, set bson.M) (SupplierOffering, error) {
	objID, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return SupplierOffering{}, ErrSupplierOfferingNotFound
	}
	set["revision"] = expectedRevision + 1

	res, err := r.collection.UpdateOne(ctx,
		bson.M{"_id": objID, "companyId": companyID, "revision": expectedRevision},
		bson.M{"$set": set})
	if err != nil {
		return SupplierOffering{}, err
	}
	if res.MatchedCount == 0 {
		if _, findErr := r.FindByID(ctx, companyID, id); errors.Is(findErr, ErrSupplierOfferingNotFound) {
			return SupplierOffering{}, ErrSupplierOfferingNotFound
		}
		return SupplierOffering{}, ErrRevisionMismatch
	}
	return r.FindByID(ctx, companyID, id)
}

// Update deliberately omits supplierId from the $set. §4.2's immutability is
// therefore enforced by the WRITE, so even a caller that passes a different
// supplier cannot move the offering and rewrite its provenance.
// DeleteAllForCompany permanently removes every SupplierOffering owned by
// companyID. Never errors when zero documents match. Development-tool use
// only.
func (r *MongoSupplierOfferingRepository) DeleteAllForCompany(ctx context.Context, companyID string) error {
	_, err := r.collection.DeleteMany(ctx, bson.M{"companyId": companyID})
	return err
}

func (r *MongoSupplierOfferingRepository) Update(ctx context.Context, companyID, id string,
	expectedRevision int64, updated SupplierOffering) (SupplierOffering, error) {

	set := bson.M{
		"materialId":          updated.MaterialID,
		"productName":         updated.ProductName,
		"brand":               updated.Brand,
		"sku":                 updated.SKU,
		"description":         updated.Description,
		"category":            updated.Category,
		"unit":                updated.Unit,
		"imageUrl":            updated.ImageURL,
		"productUrl":          updated.ProductURL,
		"indicativePriceAsOf": updated.IndicativePriceAsOf,
		"lastVerifiedAt":      updated.LastVerifiedAt,
		"updatedAt":           time.Now(),
	}
	if updated.IndicativePrice != nil {
		set["indicativePrice"] = moneyDoc{
			Amount: updated.IndicativePrice.Amount, Currency: updated.IndicativePrice.Currency,
		}
	} else {
		// Clearing the price stores nil rather than a zero Money: zero would be
		// an ambiguous sentinel, and nil is what expresses "unknown" (§4.2).
		set["indicativePrice"] = nil
	}
	return r.conditionalUpdate(ctx, companyID, id, expectedRevision, set)
}

func (r *MongoSupplierOfferingRepository) SetActive(ctx context.Context, companyID, id string,
	expectedRevision int64, active bool) (SupplierOffering, error) {
	return r.conditionalUpdate(ctx, companyID, id, expectedRevision, bson.M{
		"active":    active,
		"updatedAt": time.Now(),
	})
}

// --- Preference ---

// MongoPreferenceRepository is the MongoDB-backed PreferenceRepository.
type MongoPreferenceRepository struct {
	collection *mongo.Collection
}

// NewMongoPreferenceRepository constructs the repository over db's
// material_supplier_preferences collection.
func NewMongoPreferenceRepository(db *mongo.Database) *MongoPreferenceRepository {
	return &MongoPreferenceRepository{collection: db.Collection(collectionPreferences)}
}

// EnsureIndexes creates the §12.3 preference indexes. Idempotent.
func (r *MongoPreferenceRepository) EnsureIndexes(ctx context.Context) error {
	_, err := r.collection.Indexes().CreateMany(ctx, []mongo.IndexModel{
		// At most one preferred Supplier per Material.
		{Keys: bson.D{{Key: "companyId", Value: 1}, {Key: "materialId", Value: 1}},
			Options: options.Index().SetUnique(true).SetName(indexNameUniquePreferencesMaterial)},
		{Keys: bson.D{{Key: "companyId", Value: 1}, {Key: "supplierId", Value: 1}},
			Options: options.Index().SetName(indexNamePreferencesSupplier)},
	})
	return err
}

type preferenceDoc struct {
	ID         bson.ObjectID `bson:"_id,omitempty"`
	CompanyID  string        `bson:"companyId"`
	MaterialID string        `bson:"materialId"`
	SupplierID string        `bson:"supplierId"`

	Revision      int64     `bson:"revision"`
	CreatedAt     time.Time `bson:"createdAt"`
	UpdatedAt     time.Time `bson:"updatedAt"`
	SchemaVersion int       `bson:"schemaVersion"`
}

func fromPreferenceDoc(doc preferenceDoc) MaterialSupplierPreference {
	return MaterialSupplierPreference{
		ID: doc.ID.Hex(), CompanyID: doc.CompanyID,
		MaterialID: doc.MaterialID, SupplierID: doc.SupplierID,
		Revision:  doc.Revision,
		CreatedAt: doc.CreatedAt, UpdatedAt: doc.UpdatedAt, SchemaVersion: doc.SchemaVersion,
	}
}

// Upsert is the Revision-guarded create-or-replace behind
// PUT /materials/{materialId}/preferred-supplier (design spec §4.3).
//
// The filter carries {companyId, materialId, revision} rather than an _id, so
// the caller does not need to know the preference's own identity — the material
// is the natural key. expectedRevision 0 with upsert:true creates.
// DeleteAllForCompany permanently removes every MaterialSupplierPreference
// owned by companyID. Never errors when zero documents match.
// Development-tool use only.
func (r *MongoPreferenceRepository) DeleteAllForCompany(ctx context.Context, companyID string) error {
	_, err := r.collection.DeleteMany(ctx, bson.M{"companyId": companyID})
	return err
}

func (r *MongoPreferenceRepository) Upsert(ctx context.Context, p MaterialSupplierPreference,
	expectedRevision int64) (MaterialSupplierPreference, error) {

	now := time.Now()
	filter := bson.M{
		"companyId": p.CompanyID, "materialId": p.MaterialID, "revision": expectedRevision,
	}
	update := bson.M{
		"$set": bson.M{
			"supplierId": p.SupplierID,
			"revision":   expectedRevision + 1,
			"updatedAt":  now,
		},
		"$setOnInsert": bson.M{
			"companyId": p.CompanyID, "materialId": p.MaterialID,
			"createdAt": now, "schemaVersion": 1,
		},
	}

	opts := options.FindOneAndUpdate().SetUpsert(true).SetReturnDocument(options.After)
	var doc preferenceDoc
	err := r.collection.FindOneAndUpdate(ctx, filter, update, opts).Decode(&doc)
	if err != nil {
		if mongo.IsDuplicateKeyError(err) {
			// A row exists at a DIFFERENT revision: the upsert filter missed it
			// and the unique index refused the insert. That is a stale
			// expectation, not a genuine duplicate.
			return MaterialSupplierPreference{}, ErrRevisionMismatch
		}
		return MaterialSupplierPreference{}, err
	}
	return fromPreferenceDoc(doc), nil
}

func (r *MongoPreferenceRepository) FindByMaterial(ctx context.Context,
	companyID, materialID string) (MaterialSupplierPreference, error) {
	var doc preferenceDoc
	err := r.collection.FindOne(ctx, bson.M{
		"companyId": companyID, "materialId": materialID,
	}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return MaterialSupplierPreference{}, ErrPreferenceNotFound
	}
	if err != nil {
		return MaterialSupplierPreference{}, err
	}
	return fromPreferenceDoc(doc), nil
}

func (r *MongoPreferenceRepository) ListBySupplier(ctx context.Context,
	companyID, supplierID string) ([]MaterialSupplierPreference, error) {
	cursor, err := r.collection.Find(ctx, bson.M{
		"companyId": companyID, "supplierId": supplierID,
	})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var docs []preferenceDoc
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, err
	}
	out := make([]MaterialSupplierPreference, 0, len(docs))
	for _, doc := range docs {
		out = append(out, fromPreferenceDoc(doc))
	}
	return out, nil
}

// Delete PHYSICALLY removes the preference. Unlike suppliers and offerings this
// is permitted, because the relationship is advisory only and audit preserves
// the history (design spec §4.3).
func (r *MongoPreferenceRepository) Delete(ctx context.Context, companyID, materialID string,
	expectedRevision int64) error {
	res, err := r.collection.DeleteOne(ctx, bson.M{
		"companyId": companyID, "materialId": materialID, "revision": expectedRevision,
	})
	if err != nil {
		return err
	}
	if res.DeletedCount == 0 {
		if _, findErr := r.FindByMaterial(ctx, companyID, materialID); errors.Is(findErr,
			ErrPreferenceNotFound) {
			return ErrPreferenceNotFound
		}
		return ErrRevisionMismatch
	}
	return nil
}
