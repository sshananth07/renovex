package rfqissuance

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
	collectionSupplierInvitations = "supplier_invitations"

	indexNameUniqueInvitation           = "uq_supplier_invitations_company_chain_supplier"
	indexNameInvitationCompanyChain     = "idx_supplier_invitations_company_chain"
	indexNameUniqueInvitationAccessHash = "uq_supplier_invitations_access_secret_hash"
)

// MongoInvitationRepository is the MongoDB-backed store of stable Supplier
// Invitations.
type MongoInvitationRepository struct {
	collection *mongo.Collection
}

// NewMongoInvitationRepository constructs the repository over db's
// supplier_invitations collection.
func NewMongoInvitationRepository(db *mongo.Database) *MongoInvitationRepository {
	return &MongoInvitationRepository{
		collection: db.Collection(collectionSupplierInvitations),
	}
}

// EnsureIndexes creates the §11.2 indexes. Idempotent.
func (r *MongoInvitationRepository) EnsureIndexes(ctx context.Context) error {
	_, err := r.collection.Indexes().CreateMany(ctx, []mongo.IndexModel{
		// THE stable-invitation invariant: one per Company + RFQChain +
		// Supplier. A second invitation would split one Supplier's offer
		// history across two identities and give them two competing links.
		{Keys: bson.D{{Key: "companyId", Value: 1}, {Key: "rfqChainId", Value: 1},
			{Key: "supplierId", Value: 1}},
			Options: options.Index().SetUnique(true).SetName(indexNameUniqueInvitation)},
		// Serves listing and the bulk advance on amendment issuance.
		{Keys: bson.D{{Key: "companyId", Value: 1}, {Key: "rfqChainId", Value: 1}},
			Options: options.Index().SetName(indexNameInvitationCompanyChain)},
		// Public open starts with only an opaque token hash, so uniqueness must
		// be global rather than tenant-scoped. The partial predicate lets legacy
		// documents with no credential coexist while every usable hash remains
		// an unambiguous lookup key (Revision 6, §1A.5).
		{Keys: bson.D{{Key: "accessSecretHash", Value: 1}},
			Options: options.Index().
				SetUnique(true).
				SetName(indexNameUniqueInvitationAccessHash).
				SetPartialFilterExpression(bson.M{
					"accessSecretHash": bson.M{"$type": "string", "$gt": ""},
				})},
	})
	return err
}

type invitationDoc struct {
	ID                        bson.ObjectID `bson:"_id,omitempty"`
	CompanyID                 string        `bson:"companyId"`
	RFQChainID                string        `bson:"rfqChainId"`
	SupplierID                string        `bson:"supplierId"`
	CurrentIssuedRFQVersionID string        `bson:"currentIssuedRfqVersionId"`

	RecipientName            string `bson:"recipientName"`
	RecipientEmail           string `bson:"recipientEmail"`
	RecipientEmailNormalized string `bson:"recipientEmailNormalized"`

	Status    string     `bson:"status"`
	ExpiresAt time.Time  `bson:"expiresAt"`
	RevokedAt *time.Time `bson:"revokedAt,omitempty"`

	// Only the HASH is persisted. The raw token is re-derived from the keyring
	// on demand and never stored (§6.1A).
	AccessSecretHash string `bson:"accessSecretHash"`
	AccessGeneration int64  `bson:"accessGeneration"`
	SecretKeyVersion int    `bson:"secretKeyVersion"`

	FirstViewedAt *time.Time `bson:"firstViewedAt,omitempty"`
	LastViewedAt  *time.Time `bson:"lastViewedAt,omitempty"`

	Revision        int64     `bson:"revision"`
	CreatedByUserID string    `bson:"createdByUserId,omitempty"`
	CreatedAt       time.Time `bson:"createdAt"`
	UpdatedAt       time.Time `bson:"updatedAt"`
	SchemaVersion   int       `bson:"schemaVersion"`

	// LastReactivation is repository-private idempotency metadata. Keeping it
	// out of SupplierInvitation prevents an infrastructure recovery detail
	// from leaking into contractor-facing DTOs.
	LastReactivation *invitationReactivationDoc `bson:"lastReactivation,omitempty"`

	// LastRecipientReplacement is likewise repository-private idempotency
	// metadata, kept out of SupplierInvitation for the same reason.
	LastRecipientReplacement *invitationRecipientReplacementDoc `bson:"lastRecipientReplacement,omitempty"`
}

type invitationReactivationDoc struct {
	OperationID      string    `bson:"operationId"`
	ExpectedRevision int64     `bson:"expectedRevision"`
	ExpiresAt        time.Time `bson:"expiresAt"`
}

// invitationRecipientReplacementDoc is repository-private idempotency metadata
// for recipient replacement, mirroring LastReactivation. Recovery reads it to
// decide whether the authoritative write already landed (§5.3A).
type invitationRecipientReplacementDoc struct {
	OperationID               string    `bson:"operationId"`
	PreviousRecipientIdentity string    `bson:"previousRecipientIdentity"`
	NewRecipientIdentity      string    `bson:"newRecipientIdentity"`
	ReplacedAt                time.Time `bson:"replacedAt"`
}

func toInvitationDoc(i SupplierInvitation) invitationDoc {
	return invitationDoc{
		CompanyID: i.CompanyID, RFQChainID: i.RFQChainID, SupplierID: i.SupplierID,
		CurrentIssuedRFQVersionID: i.CurrentIssuedRFQVersionID,
		RecipientName:             i.RecipientName,
		RecipientEmail:            i.RecipientEmail,
		RecipientEmailNormalized:  i.RecipientEmailNormalized,
		Status:                    string(i.Status),
		ExpiresAt:                 i.ExpiresAt,
		RevokedAt:                 i.RevokedAt,
		AccessSecretHash:          i.AccessSecretHash,
		AccessGeneration:          i.AccessGeneration,
		SecretKeyVersion:          i.SecretKeyVersion,
		FirstViewedAt:             i.FirstViewedAt,
		LastViewedAt:              i.LastViewedAt,
		Revision:                  i.Revision,
		CreatedByUserID:           i.CreatedByUserID,
		CreatedAt:                 i.CreatedAt,
		UpdatedAt:                 i.UpdatedAt,
		SchemaVersion:             i.SchemaVersion,
	}
}

func fromInvitationDoc(doc invitationDoc) SupplierInvitation {
	return SupplierInvitation{
		ID: doc.ID.Hex(), CompanyID: doc.CompanyID, RFQChainID: doc.RFQChainID,
		SupplierID:                doc.SupplierID,
		CurrentIssuedRFQVersionID: doc.CurrentIssuedRFQVersionID,
		RecipientName:             doc.RecipientName,
		RecipientEmail:            doc.RecipientEmail,
		RecipientEmailNormalized:  doc.RecipientEmailNormalized,
		Status:                    InvitationStatus(doc.Status),
		ExpiresAt:                 doc.ExpiresAt,
		RevokedAt:                 doc.RevokedAt,
		AccessSecretHash:          doc.AccessSecretHash,
		AccessGeneration:          doc.AccessGeneration,
		SecretKeyVersion:          doc.SecretKeyVersion,
		FirstViewedAt:             doc.FirstViewedAt,
		LastViewedAt:              doc.LastViewedAt,
		Revision:                  doc.Revision,
		CreatedByUserID:           doc.CreatedByUserID,
		CreatedAt:                 doc.CreatedAt,
		UpdatedAt:                 doc.UpdatedAt,
		SchemaVersion:             doc.SchemaVersion,
	}
}

// CreateInvitation persists a new stable invitation in ONE write.
//
// The caller supplies the ID, because the invitation's secret is derived from
// it and must therefore exist before the insert. That is what makes creation
// atomic: the record lands complete — identity, generation, key version and
// secret hash together — with no window in which a crash could leave a
// healthy-looking invitation whose link can never be reproduced (§5.1, §6.1A).
// DeleteAllForCompany permanently removes every SupplierInvitation owned by
// companyID. Never errors when zero documents match. Development-tool use
// only.
func (r *MongoInvitationRepository) DeleteAllForCompany(ctx context.Context, companyID string) error {
	_, err := r.collection.DeleteMany(ctx, bson.M{"companyId": companyID})
	return err
}

func (r *MongoInvitationRepository) CreateInvitation(ctx context.Context,
	invitation SupplierInvitation) (SupplierInvitation, error) {

	doc := toInvitationDoc(invitation)

	// A service-supplied ID is the normal path. The fallback exists only so a
	// caller with no derivation requirement is not forced to mint one.
	if invitation.ID != "" {
		objID, err := bson.ObjectIDFromHex(invitation.ID)
		if err != nil {
			return SupplierInvitation{}, ErrInvalidInvitationID
		}
		doc.ID = objID
	}

	res, err := r.collection.InsertOne(ctx, doc)
	if err != nil {
		if mongo.IsDuplicateKeyError(err) {
			// Duplicate keys have different meanings now that the public access
			// hash is globally unique. Classify by the explicit index name so a
			// secret collision is never laundered into a normal tenant conflict.
			switch message := err.Error(); {
			case strings.Contains(message, indexNameUniqueInvitationAccessHash):
				return SupplierInvitation{}, ErrAccessSecretHashCollision
			case strings.Contains(message, indexNameUniqueInvitation):
				return SupplierInvitation{}, ErrInvitationAlreadyExists
			default:
				// Unknown duplicate metadata remains an internal error. Guessing
				// would risk exposing the wrong client-visible meaning.
				return SupplierInvitation{}, err
			}
		}
		return SupplierInvitation{}, err
	}
	invitation.ID = res.InsertedID.(bson.ObjectID).Hex()
	return invitation, nil
}

// FindInvitation reads one invitation, tenant-scoped.
func (r *MongoInvitationRepository) FindInvitation(ctx context.Context,
	companyID, invitationID string) (SupplierInvitation, error) {

	objID, err := bson.ObjectIDFromHex(invitationID)
	if err != nil {
		// A malformed id is reported as not-found so the handler maps it to 404
		// rather than 500.
		return SupplierInvitation{}, ErrInvitationNotFound
	}

	var doc invitationDoc
	err = r.collection.FindOne(ctx, bson.M{"_id": objID, "companyId": companyID}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return SupplierInvitation{}, ErrInvitationNotFound
	}
	if err != nil {
		return SupplierInvitation{}, err
	}
	return fromInvitationDoc(doc), nil
}

// FindInvitationByAccessSecretHash is intentionally global. A Supplier opening
// an opaque link has no trusted CompanyID or InvitationID; the invitation
// selected by the globally unique hash supplies the tenant for every later
// lookup. No caller-provided tenant is accepted on this boundary.
func (r *MongoInvitationRepository) FindInvitationByAccessSecretHash(ctx context.Context,
	accessSecretHash string) (SupplierInvitation, error) {

	var doc invitationDoc
	err := r.collection.FindOne(ctx,
		bson.M{"accessSecretHash": accessSecretHash}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return SupplierInvitation{}, ErrInvitationNotFound
	}
	if err != nil {
		return SupplierInvitation{}, err
	}
	return fromInvitationDoc(doc), nil
}

// FindInvitationForSupplier resolves the stable invitation by its natural key.
func (r *MongoInvitationRepository) FindInvitationForSupplier(ctx context.Context,
	companyID, rfqChainID, supplierID string) (SupplierInvitation, error) {

	var doc invitationDoc
	err := r.collection.FindOne(ctx, bson.M{
		"companyId": companyID, "rfqChainId": rfqChainID, "supplierId": supplierID,
	}).Decode(&doc)

	if errors.Is(err, mongo.ErrNoDocuments) {
		return SupplierInvitation{}, ErrInvitationNotFound
	}
	if err != nil {
		return SupplierInvitation{}, err
	}
	return fromInvitationDoc(doc), nil
}

// ListInvitationsForChain returns every invitation on one chain, tenant-scoped.
func (r *MongoInvitationRepository) ListInvitationsForChain(ctx context.Context,
	companyID, rfqChainID string) ([]SupplierInvitation, error) {

	cursor, err := r.collection.Find(ctx, bson.M{
		"companyId": companyID, "rfqChainId": rfqChainID,
	})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var docs []invitationDoc
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, err
	}

	out := make([]SupplierInvitation, 0, len(docs))
	for _, doc := range docs {
		out = append(out, fromInvitationDoc(doc))
	}
	return out, nil
}

// UpdateInvitation applies an edit under a revision guard.
//
// AccessSecretHash, AccessGeneration and SecretKeyVersion are deliberately
// absent from the $set: they move only through RotateSecret, so an ordinary
// edit can never silently change the secret or its generation.
func (r *MongoInvitationRepository) UpdateInvitation(ctx context.Context,
	companyID, invitationID string, expectedRevision int64,
	updated SupplierInvitation) (SupplierInvitation, error) {

	objID, err := bson.ObjectIDFromHex(invitationID)
	if err != nil {
		return SupplierInvitation{}, ErrInvitationNotFound
	}

	doc := toInvitationDoc(updated)
	opts := options.FindOneAndUpdate().SetReturnDocument(options.After)

	var result invitationDoc
	err = r.collection.FindOneAndUpdate(ctx,
		bson.M{"_id": objID, "companyId": companyID, "revision": expectedRevision},
		bson.M{"$set": bson.M{
			"currentIssuedRfqVersionId": doc.CurrentIssuedRFQVersionID,
			"recipientName":             doc.RecipientName,
			"recipientEmail":            doc.RecipientEmail,
			"recipientEmailNormalized":  doc.RecipientEmailNormalized,
			"status":                    doc.Status,
			"expiresAt":                 doc.ExpiresAt,
			"revokedAt":                 doc.RevokedAt,
			"revision":                  expectedRevision + 1,
			"updatedAt":                 time.Now(),
		}},
		opts,
	).Decode(&result)

	if errors.Is(err, mongo.ErrNoDocuments) {
		if _, findErr := r.FindInvitation(ctx, companyID, invitationID); errors.Is(findErr,
			ErrInvitationNotFound) {
			return SupplierInvitation{}, ErrInvitationNotFound
		}
		return SupplierInvitation{}, ErrRevisionMismatch
	}
	if err != nil {
		return SupplierInvitation{}, err
	}
	return fromInvitationDoc(result), nil
}

// RotateSecret increments the access generation and replaces the stored hash
// ATOMICALLY (§6.1A).
//
// The two must move together: a generation without its matching hash would
// leave every derivable link failing verification, and a hash without the
// generation bump would leave the OLD link still valid — precisely the
// invalidation that rotation exists to perform.
func (r *MongoInvitationRepository) RotateSecret(ctx context.Context,
	companyID, invitationID string, expectedRevision int64,
	newSecretHash string, keyVersion int) (SupplierInvitation, error) {

	objID, err := bson.ObjectIDFromHex(invitationID)
	if err != nil {
		return SupplierInvitation{}, ErrInvitationNotFound
	}

	opts := options.FindOneAndUpdate().SetReturnDocument(options.After)

	var result invitationDoc
	err = r.collection.FindOneAndUpdate(ctx,
		bson.M{"_id": objID, "companyId": companyID, "revision": expectedRevision},
		bson.M{
			"$set": bson.M{
				"accessSecretHash": newSecretHash,
				"secretKeyVersion": keyVersion,
				"revision":         expectedRevision + 1,
				"updatedAt":        time.Now(),
			},
			"$inc": bson.M{"accessGeneration": 1},
		},
		opts,
	).Decode(&result)

	if errors.Is(err, mongo.ErrNoDocuments) {
		if _, findErr := r.FindInvitation(ctx, companyID, invitationID); errors.Is(findErr,
			ErrInvitationNotFound) {
			return SupplierInvitation{}, ErrInvitationNotFound
		}
		return SupplierInvitation{}, ErrRevisionMismatch
	}
	if err != nil {
		return SupplierInvitation{}, err
	}
	return fromInvitationDoc(result), nil
}

// ReplaceRecipientAtomicInput carries a complete recipient replacement.
type ReplaceRecipientAtomicInput struct {
	CompanyID        string
	InvitationID     string
	ExpectedRevision int64
	// PreviousRecipientIdentity is part of the CAS FILTER, not a pre-check, so
	// an operation working from a stale read cannot overwrite a replacement
	// another operation already performed.
	PreviousRecipientIdentity string
	RecipientName             string
	RecipientEmail            string
	RecipientEmailNormalized  string
	NewSecretHash             string
	SecretKeyVersion          int
	ReplacementOperationID    string
	ReplacedAt                time.Time
}

// ReplaceRecipientAtomically performs the authoritative recipient replacement
// as ONE conditional write (§5.3A).
//
// The recipient snapshot and the secret rotation must move together. Splitting
// them into two sequential CAS operations leaves a crash window in which the
// recipient has already changed while the PREVIOUS recipient's link still
// resolves — the stale-link exposure replacement exists to close. This is the
// same reasoning RotateSecret documents for generation and hash, extended to
// the recipient snapshot.
//
// It is step two of claim -> authoritative invitation replacement -> archival.
// The supplieroffers barrier is already held when this runs, so the previous
// recipient cannot edit, submit or create a draft during this window.
func (r *MongoInvitationRepository) ReplaceRecipientAtomically(ctx context.Context,
	input ReplaceRecipientAtomicInput) (SupplierInvitation, error) {

	objID, err := bson.ObjectIDFromHex(input.InvitationID)
	if err != nil {
		return SupplierInvitation{}, ErrInvitationNotFound
	}
	if input.ReplacedAt.IsZero() {
		input.ReplacedAt = time.Now()
	}

	operation := invitationRecipientReplacementDoc{
		OperationID:               input.ReplacementOperationID,
		PreviousRecipientIdentity: input.PreviousRecipientIdentity,
		NewRecipientIdentity:      input.RecipientEmailNormalized,
		ReplacedAt:                input.ReplacedAt,
	}

	opts := options.FindOneAndUpdate().SetReturnDocument(options.After)

	var result invitationDoc
	err = r.collection.FindOneAndUpdate(ctx,
		bson.M{
			"_id": objID, "companyId": input.CompanyID,
			"revision":                 input.ExpectedRevision,
			"recipientEmailNormalized": input.PreviousRecipientIdentity,
		},
		bson.M{
			"$set": bson.M{
				"recipientName":            input.RecipientName,
				"recipientEmail":           input.RecipientEmail,
				"recipientEmailNormalized": input.RecipientEmailNormalized,
				"accessSecretHash":         input.NewSecretHash,
				"secretKeyVersion":         input.SecretKeyVersion,
				"lastRecipientReplacement": operation,
				"updatedAt":                input.ReplacedAt,
			},
			"$inc": bson.M{
				"accessGeneration": 1,
				"revision":         1,
			},
		},
		opts,
	).Decode(&result)

	if err == nil {
		return fromInvitationDoc(result), nil
	}
	if !errors.Is(err, mongo.ErrNoDocuments) {
		return SupplierInvitation{}, err
	}

	// Classify the miss. The read stays company-scoped so an absent and a
	// foreign invitation collapse to the same not-found.
	var current invitationDoc
	if findErr := r.collection.FindOne(ctx,
		bson.M{"_id": objID, "companyId": input.CompanyID}).Decode(&current); findErr != nil {
		if errors.Is(findErr, mongo.ErrNoDocuments) {
			return SupplierInvitation{}, ErrInvitationNotFound
		}
		return SupplierInvitation{}, findErr
	}

	// A completed same-operation replacement converges without minting another
	// generation, which would invalidate the link the new recipient just got.
	if current.LastRecipientReplacement != nil &&
		current.LastRecipientReplacement.OperationID == input.ReplacementOperationID {
		if current.LastRecipientReplacement.PreviousRecipientIdentity !=
			input.PreviousRecipientIdentity ||
			current.LastRecipientReplacement.NewRecipientIdentity !=
				input.RecipientEmailNormalized {
			return SupplierInvitation{}, ErrOperationAlreadyUsed
		}
		return fromInvitationDoc(current), nil
	}

	return SupplierInvitation{}, ErrRevisionMismatch
}

// RecordViewed stamps a successful secure-link open (§6.5).
//
// accessGeneration is part of the FILTER, not a post-check. Without it, a
// request validated under an older generation could land after a recipient
// replacement and mark the REPLACEMENT recipient's invitation as viewed — an
// attribution error the contractor would have no way to detect.
//
// $min/$max rather than $set: opens may arrive out of order after a retry, and
// FirstViewedAt must remain the earliest while LastViewedAt remains the latest
// regardless of arrival order.
//
// This deliberately does NOT touch Revision. A view is an observation, not a
// contractor edit; bumping the revision would make a Supplier opening their
// link invalidate the contractor's in-flight edit.
// ReactivateInvitation atomically replaces revoked or expired access.
//
// Generation, hash, key version, status, expiry, revocation marker, current RFQ
// version, revision, and idempotency identity move in ONE MongoDB update. A
// split write could briefly revive the old token or leave an active invitation
// pointing at a stale issued version.
//
// The bool reports whether this call applied the transition. False with no
// error means the same operation ID already applied it, allowing the service to
// re-drive post-write delivery cleanup without auditing or incrementing again.
func (r *MongoInvitationRepository) ReactivateInvitation(ctx context.Context,
	companyID, invitationID string, command ReactivateInvitationCommand) (
	SupplierInvitation, bool, error) {

	objID, err := bson.ObjectIDFromHex(invitationID)
	if err != nil {
		return SupplierInvitation{}, false, ErrInvitationNotFound
	}
	if command.Now.IsZero() {
		command.Now = time.Now()
	}

	opts := options.FindOneAndUpdate().SetReturnDocument(options.After)
	operation := invitationReactivationDoc{
		OperationID: command.OperationID, ExpectedRevision: command.ExpectedRevision,
		ExpiresAt: command.ExpiresAt,
	}

	var result invitationDoc
	err = r.collection.FindOneAndUpdate(ctx,
		bson.M{
			"_id": objID, "companyId": companyID,
			"revision": command.ExpectedRevision,
			// Reactivation is deliberately narrower than rotation: an active,
			// unexpired invitation cannot match and therefore cannot lose its
			// working link through an accidental call to this endpoint.
			"$or": bson.A{
				bson.M{"status": string(InvitationStatusRevoked)},
				bson.M{"status": string(InvitationStatusExpired)},
				bson.M{"revokedAt": bson.M{"$type": "date"}},
				bson.M{"expiresAt": bson.M{"$lte": command.Now}},
			},
		},
		bson.M{
			"$set": bson.M{
				"status":                    string(InvitationStatusActive),
				"expiresAt":                 command.ExpiresAt,
				"currentIssuedRfqVersionId": command.CurrentIssuedRFQVersionID,
				"accessSecretHash":          command.NewAccessSecretHash,
				"secretKeyVersion":          command.SecretKeyVersion,
				"lastReactivation":          operation,
				"updatedAt":                 command.Now,
			},
			"$unset": bson.M{"revokedAt": ""},
			"$inc": bson.M{
				"accessGeneration": 1,
				"revision":         1,
			},
		},
		opts,
	).Decode(&result)

	if err == nil {
		return fromInvitationDoc(result), true, nil
	}
	if !errors.Is(err, mongo.ErrNoDocuments) {
		return SupplierInvitation{}, false, err
	}

	// A miss needs classification. The read remains company-scoped so an
	// absent and a foreign invitation collapse to the same 404.
	var current invitationDoc
	if findErr := r.collection.FindOne(ctx,
		bson.M{"_id": objID, "companyId": companyID}).Decode(&current); findErr != nil {
		if errors.Is(findErr, mongo.ErrNoDocuments) {
			return SupplierInvitation{}, false, ErrInvitationNotFound
		}
		return SupplierInvitation{}, false, findErr
	}

	if current.LastReactivation != nil &&
		current.LastReactivation.OperationID == command.OperationID {
		// An operation ID identifies the whole logical request. Reusing it with
		// another revision or expiry is a conflict, not a retry.
		storedExpiry := current.LastReactivation.ExpiresAt
		requestedExpiry := command.ExpiresAt.UTC().Truncate(time.Millisecond)
		if current.LastReactivation.ExpectedRevision != command.ExpectedRevision ||
			!storedExpiry.Equal(requestedExpiry) {
			return SupplierInvitation{}, false, ErrOperationAlreadyUsed
		}
		return fromInvitationDoc(current), false, nil
	}

	if current.Revision != command.ExpectedRevision {
		return SupplierInvitation{}, false, ErrRevisionMismatch
	}
	if current.RevokedAt == nil &&
		current.Status == string(InvitationStatusActive) &&
		command.Now.Before(current.ExpiresAt) {
		return SupplierInvitation{}, false, ErrInvitationAlreadyActive
	}
	return SupplierInvitation{}, false, ErrInvitationNotReactivatable
}

func (r *MongoInvitationRepository) RecordViewed(ctx context.Context,
	companyID, invitationID string, accessGeneration int64, viewedAt time.Time) error {

	objID, err := bson.ObjectIDFromHex(invitationID)
	if err != nil {
		return ErrInvitationNotFound
	}

	res, err := r.collection.UpdateOne(ctx,
		bson.M{
			"_id": objID, "companyId": companyID,
			"accessGeneration": accessGeneration,
		},
		bson.M{
			"$min": bson.M{"firstViewedAt": viewedAt},
			"$max": bson.M{"lastViewedAt": viewedAt},
		},
	)
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		// Missing, foreign, or an obsolete generation — all collapsed, since a
		// caller must not learn which.
		return ErrInvitationNotFound
	}
	return nil
}

// AdvanceInvitationsToVersion points every NON-REVOKED invitation on a chain at
// a newly issued version, returning how many moved (§4.2 step 6).
//
// A revoked invitation is deliberately skipped: revocation preserves all
// historical data, and advancing one would rewrite the record of what that
// Supplier was actually invited to.
func (r *MongoInvitationRepository) AdvanceInvitationsToVersion(ctx context.Context,
	companyID, rfqChainID, issuedVersionID string) (int64, error) {

	res, err := r.collection.UpdateMany(ctx,
		bson.M{
			"companyId": companyID, "rfqChainId": rfqChainID,
			"status":    bson.M{"$ne": string(InvitationStatusRevoked)},
			"revokedAt": nil,
		},
		bson.M{"$set": bson.M{
			"currentIssuedRfqVersionId": issuedVersionID,
			"updatedAt":                 time.Now(),
		}},
	)
	if err != nil {
		return 0, err
	}
	return res.ModifiedCount, nil
}
