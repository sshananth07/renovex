package spatial

import (
	"context"
	"errors"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// Explicit index names, matching this package's established
// "every index carries one" convention (internal/access/repository_mongo.go).
const (
	indexNameUniqueSpatialDesignSessionsClientID   = "uq_spatial_design_sessions_company_client_session_id"
	indexNameSpatialDesignSessionsByRoomDraft      = "idx_spatial_design_sessions_company_roomdraft_updated"
	indexNameUniqueSpatialDesignTurnsClientRequest = "uq_spatial_design_turns_company_session_client_request_id"
	indexNameUniqueSpatialDesignTurnsSequence      = "uq_spatial_design_turns_company_session_sequence"
	indexNameSpatialDesignTurnsBrowse              = "idx_spatial_design_turns_company_session_sequence_desc"
	indexNameSpatialDesignTurnsRecovery            = "idx_spatial_design_turns_status_provider_started_at"
)

// MongoDesignRepository owns the "spatial_design_sessions" and
// "spatial_design_turns" collections exclusively, and implements both
// DesignSessionRepository and DesignTurnRepository from one type — the two
// collections' atomicity requirements (ReserveTurn, FinishTurn,
// RecoverInterruptedTurns) all span both collections in a single Mongo
// transaction, matching MongoRoomDraftEditRepository's established
// "one type owns both concerns since they share the same transaction
// boundary" precedent.
type MongoDesignRepository struct {
	client            *mongo.Client
	sessionCollection *mongo.Collection
	turnCollection    *mongo.Collection
}

func NewMongoDesignRepository(db *mongo.Database) *MongoDesignRepository {
	return &MongoDesignRepository{
		client:            db.Client(),
		sessionCollection: db.Collection("spatial_design_sessions"),
		turnCollection:    db.Collection("spatial_design_turns"),
	}
}

// EnsureIndexes creates every index this repository requires. No TTL index
// is created: plan lineage is durable (RP4E1 plan's explicit requirement).
func (r *MongoDesignRepository) EnsureIndexes(ctx context.Context) error {
	if _, err := r.sessionCollection.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "companyId", Value: 1}, {Key: "clientSessionId", Value: 1}},
			Options: options.Index().SetUnique(true).SetName(indexNameUniqueSpatialDesignSessionsClientID)},
		{Keys: bson.D{{Key: "companyId", Value: 1}, {Key: "roomDraftId", Value: 1}, {Key: "updatedAt", Value: -1}, {Key: "_id", Value: 1}},
			Options: options.Index().SetName(indexNameSpatialDesignSessionsByRoomDraft)},
	}); err != nil {
		return err
	}
	_, err := r.turnCollection.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "companyId", Value: 1}, {Key: "sessionId", Value: 1}, {Key: "clientRequestId", Value: 1}},
			Options: options.Index().SetUnique(true).SetName(indexNameUniqueSpatialDesignTurnsClientRequest)},
		{Keys: bson.D{{Key: "companyId", Value: 1}, {Key: "sessionId", Value: 1}, {Key: "sequence", Value: 1}},
			Options: options.Index().SetUnique(true).SetName(indexNameUniqueSpatialDesignTurnsSequence)},
		{Keys: bson.D{{Key: "companyId", Value: 1}, {Key: "sessionId", Value: 1}, {Key: "sequence", Value: -1}},
			Options: options.Index().SetName(indexNameSpatialDesignTurnsBrowse)},
		{Keys: bson.D{{Key: "status", Value: 1}, {Key: "providerStartedAt", Value: 1}},
			Options: options.Index().SetName(indexNameSpatialDesignTurnsRecovery)},
	})
	return err
}

// --- BSON document shapes ---

type spatialDesignTargetDoc struct {
	Kind string `bson:"kind"`
	ID   string `bson:"id"`
}

func toDesignTargetDoc(t SpatialDesignTarget) spatialDesignTargetDoc {
	return spatialDesignTargetDoc{Kind: string(t.Kind), ID: t.ID}
}

func fromDesignTargetDoc(doc spatialDesignTargetDoc) SpatialDesignTarget {
	return SpatialDesignTarget{Kind: DesignTargetKind(doc.Kind), ID: doc.ID}
}

type spatialWorkingDesignGeometryDoc struct {
	Category                    string `bson:"category"`
	ShapeDescription            string `bson:"shapeDescription"`
	PreserveCanonicalDimensions bool   `bson:"preserveCanonicalDimensions"`
}

type spatialWorkingDesignMaterialDoc struct {
	BaseColor      string `bson:"baseColor"`
	MaterialFamily string `bson:"materialFamily"`
	Roughness      string `bson:"roughness"`
	Metallic       bool   `bson:"metallic"`
}

type spatialResolvedSpatialOperationDoc struct {
	Kind    string         `bson:"kind"`
	Payload map[string]any `bson:"payload"`
}

type spatialWorkingDesignDoc struct {
	Geometry                  *spatialWorkingDesignGeometryDoc     `bson:"geometry,omitempty"`
	Material                  *spatialWorkingDesignMaterialDoc     `bson:"material,omitempty"`
	ResolvedSpatialOperations []spatialResolvedSpatialOperationDoc `bson:"resolvedSpatialOperations"`
}

func toWorkingDesignDoc(w WorkingDesign) spatialWorkingDesignDoc {
	doc := spatialWorkingDesignDoc{ResolvedSpatialOperations: []spatialResolvedSpatialOperationDoc{}}
	if w.Geometry != nil {
		doc.Geometry = &spatialWorkingDesignGeometryDoc{
			Category: w.Geometry.Category, ShapeDescription: w.Geometry.ShapeDescription,
			PreserveCanonicalDimensions: w.Geometry.PreserveCanonicalDimensions,
		}
	}
	if w.Material != nil {
		doc.Material = &spatialWorkingDesignMaterialDoc{
			BaseColor: w.Material.BaseColor, MaterialFamily: w.Material.MaterialFamily,
			Roughness: w.Material.Roughness, Metallic: w.Material.Metallic,
		}
	}
	for _, op := range w.ResolvedSpatialOperations {
		doc.ResolvedSpatialOperations = append(doc.ResolvedSpatialOperations, spatialResolvedSpatialOperationDoc{
			Kind: string(op.Kind), Payload: op.Payload,
		})
	}
	return doc
}

func fromWorkingDesignDoc(doc spatialWorkingDesignDoc) WorkingDesign {
	w := WorkingDesign{ResolvedSpatialOperations: []ResolvedSpatialOperation{}}
	if doc.Geometry != nil {
		w.Geometry = &WorkingDesignGeometry{
			Category: doc.Geometry.Category, ShapeDescription: doc.Geometry.ShapeDescription,
			PreserveCanonicalDimensions: doc.Geometry.PreserveCanonicalDimensions,
		}
	}
	if doc.Material != nil {
		w.Material = &WorkingDesignMaterial{
			BaseColor: doc.Material.BaseColor, MaterialFamily: doc.Material.MaterialFamily,
			Roughness: doc.Material.Roughness, Metallic: doc.Material.Metallic,
		}
	}
	for _, op := range doc.ResolvedSpatialOperations {
		w.ResolvedSpatialOperations = append(w.ResolvedSpatialOperations, ResolvedSpatialOperation{
			Kind: EditOperationKind(op.Kind), Payload: op.Payload,
		})
	}
	return w
}

type spatialDesignSessionDoc struct {
	ID                        bson.ObjectID            `bson:"_id,omitempty"`
	CompanyID                 string                   `bson:"companyId"`
	ProjectID                 string                   `bson:"projectId"`
	SpaceID                   string                   `bson:"spaceId"`
	RoomDraftID               string                   `bson:"roomDraftId"`
	CreatedByUserID           string                   `bson:"createdByUserId"`
	ClientSessionID           string                   `bson:"clientSessionId"`
	SessionRequestFingerprint string                   `bson:"sessionRequestFingerprint"`
	Target                    spatialDesignTargetDoc   `bson:"target"`
	BasedOnRoomDraftRevision  int64                    `bson:"basedOnRoomDraftRevision"`
	Status                    string                   `bson:"status"`
	CurrentWorkingDesign      spatialWorkingDesignDoc  `bson:"currentWorkingDesign"`
	AcceptedDesign            *spatialWorkingDesignDoc `bson:"acceptedDesign,omitempty"`
	AcceptedTurnID            string                   `bson:"acceptedTurnId,omitempty"`
	AcceptedAttemptID         string                   `bson:"acceptedAttemptId,omitempty"`
	LatestTurnID              string                   `bson:"latestTurnId,omitempty"`
	LatestReadyPlanTurnID     string                   `bson:"latestReadyPlanTurnId,omitempty"`
	LatestGenerationAttemptID string                   `bson:"latestGenerationAttemptId,omitempty"`
	LatestReadyAttemptID      string                   `bson:"latestReadyAttemptId,omitempty"`
	ActiveTurnID              string                   `bson:"activeTurnId,omitempty"`
	LastTurnSequence          int64                    `bson:"lastTurnSequence"`
	Revision                  int64                    `bson:"revision"`
	CreatedAt                 time.Time                `bson:"createdAt"`
	UpdatedAt                 time.Time                `bson:"updatedAt"`
	SchemaVersion             int                      `bson:"schemaVersion"`
}

func toDesignSessionDoc(s SpatialDesignSession) spatialDesignSessionDoc {
	doc := spatialDesignSessionDoc{
		CompanyID: s.CompanyID, ProjectID: s.ProjectID, SpaceID: s.SpaceID,
		RoomDraftID: s.RoomDraftID, CreatedByUserID: s.CreatedByUserID,
		ClientSessionID: s.ClientSessionID, SessionRequestFingerprint: s.SessionRequestFingerprint,
		Target: toDesignTargetDoc(s.Target), BasedOnRoomDraftRevision: s.BasedOnRoomDraftRevision,
		Status: string(s.Status), CurrentWorkingDesign: toWorkingDesignDoc(s.CurrentWorkingDesign),
		AcceptedTurnID: s.AcceptedTurnID, AcceptedAttemptID: s.AcceptedAttemptID,
		LatestTurnID: s.LatestTurnID, LatestReadyPlanTurnID: s.LatestReadyPlanTurnID,
		LatestGenerationAttemptID: s.LatestGenerationAttemptID, LatestReadyAttemptID: s.LatestReadyAttemptID,
		ActiveTurnID: s.ActiveTurnID, LastTurnSequence: s.LastTurnSequence,
		Revision: s.Revision, CreatedAt: s.CreatedAt, UpdatedAt: s.UpdatedAt, SchemaVersion: s.SchemaVersion,
	}
	if s.ID != "" {
		objID, _ := bson.ObjectIDFromHex(s.ID)
		doc.ID = objID
	}
	if s.AcceptedDesign != nil {
		acceptedDoc := toWorkingDesignDoc(*s.AcceptedDesign)
		doc.AcceptedDesign = &acceptedDoc
	}
	return doc
}

func fromDesignSessionDoc(doc spatialDesignSessionDoc) SpatialDesignSession {
	session := SpatialDesignSession{
		ID: doc.ID.Hex(), CompanyID: doc.CompanyID, ProjectID: doc.ProjectID, SpaceID: doc.SpaceID,
		RoomDraftID: doc.RoomDraftID, CreatedByUserID: doc.CreatedByUserID,
		ClientSessionID: doc.ClientSessionID, SessionRequestFingerprint: doc.SessionRequestFingerprint,
		Target: fromDesignTargetDoc(doc.Target), BasedOnRoomDraftRevision: doc.BasedOnRoomDraftRevision,
		Status: SpatialDesignSessionStatus(doc.Status), CurrentWorkingDesign: fromWorkingDesignDoc(doc.CurrentWorkingDesign),
		AcceptedTurnID: doc.AcceptedTurnID, AcceptedAttemptID: doc.AcceptedAttemptID,
		LatestTurnID: doc.LatestTurnID, LatestReadyPlanTurnID: doc.LatestReadyPlanTurnID,
		LatestGenerationAttemptID: doc.LatestGenerationAttemptID, LatestReadyAttemptID: doc.LatestReadyAttemptID,
		ActiveTurnID: doc.ActiveTurnID, LastTurnSequence: doc.LastTurnSequence,
		Revision: doc.Revision, CreatedAt: doc.CreatedAt, UpdatedAt: doc.UpdatedAt, SchemaVersion: doc.SchemaVersion,
	}
	if doc.AcceptedDesign != nil {
		accepted := fromWorkingDesignDoc(*doc.AcceptedDesign)
		session.AcceptedDesign = &accepted
	}
	return session
}

// spatialProposedBlockerDoc/spatialSectionChangeDoc/spatialProposedDeltaDoc
// persist ProposedSceneEditDelta — the untrusted-but-Go-revalidated turn
// proposal. Without these, ProposedDelta/ValidatedPlan are silently
// dropped on every Mongo round-trip (there is no bson tag for them at
// all), which a fake-repository-backed service test can never catch since
// the fake simply keeps the Go struct in memory — only a real persistence
// round-trip surfaces it.
type spatialSpatialSpecDoc struct {
	Kind           string  `bson:"kind"`
	Relationship   string  `bson:"relationship,omitempty"`
	DistanceMeters float64 `bson:"distanceMeters,omitempty"`
	Axis           string  `bson:"axis,omitempty"`
	DeltaMeters    float64 `bson:"deltaMeters,omitempty"`
	HasDelta       bool    `bson:"hasDelta,omitempty"`
	TargetMeters   float64 `bson:"targetMeters,omitempty"`
	HasTarget      bool    `bson:"hasTarget,omitempty"`
}

type spatialSectionChangeDoc struct {
	Mode         string                           `bson:"mode"`
	GeometrySpec *spatialWorkingDesignGeometryDoc `bson:"geometrySpec,omitempty"`
	MaterialSpec *spatialWorkingDesignMaterialDoc `bson:"materialSpec,omitempty"`
	SpatialSpec  *spatialSpatialSpecDoc           `bson:"spatialSpec,omitempty"`
}

func toSectionChangeDoc(sc SectionChange) spatialSectionChangeDoc {
	doc := spatialSectionChangeDoc{Mode: string(sc.Mode)}
	if sc.GeometrySpec != nil {
		doc.GeometrySpec = &spatialWorkingDesignGeometryDoc{
			Category: sc.GeometrySpec.Category, ShapeDescription: sc.GeometrySpec.ShapeDescription,
			PreserveCanonicalDimensions: sc.GeometrySpec.PreserveCanonicalDimensions,
		}
	}
	if sc.MaterialSpec != nil {
		doc.MaterialSpec = &spatialWorkingDesignMaterialDoc{
			BaseColor: sc.MaterialSpec.BaseColor, MaterialFamily: sc.MaterialSpec.MaterialFamily,
			Roughness: sc.MaterialSpec.Roughness, Metallic: sc.MaterialSpec.Metallic,
		}
	}
	if sc.SpatialSpec != nil {
		doc.SpatialSpec = &spatialSpatialSpecDoc{
			Kind: string(sc.SpatialSpec.Kind), Relationship: string(sc.SpatialSpec.Relationship),
			DistanceMeters: sc.SpatialSpec.DistanceMeters, Axis: string(sc.SpatialSpec.Axis),
			DeltaMeters: sc.SpatialSpec.DeltaMeters, HasDelta: sc.SpatialSpec.HasDelta,
			TargetMeters: sc.SpatialSpec.TargetMeters, HasTarget: sc.SpatialSpec.HasTarget,
		}
	}
	return doc
}

func fromSectionChangeDoc(doc spatialSectionChangeDoc) SectionChange {
	sc := SectionChange{Mode: SectionMode(doc.Mode)}
	if doc.GeometrySpec != nil {
		sc.GeometrySpec = &WorkingDesignGeometry{
			Category: doc.GeometrySpec.Category, ShapeDescription: doc.GeometrySpec.ShapeDescription,
			PreserveCanonicalDimensions: doc.GeometrySpec.PreserveCanonicalDimensions,
		}
	}
	if doc.MaterialSpec != nil {
		sc.MaterialSpec = &WorkingDesignMaterial{
			BaseColor: doc.MaterialSpec.BaseColor, MaterialFamily: doc.MaterialSpec.MaterialFamily,
			Roughness: doc.MaterialSpec.Roughness, Metallic: doc.MaterialSpec.Metallic,
		}
	}
	if doc.SpatialSpec != nil {
		sc.SpatialSpec = &ProposedSpatialSpec{
			Kind: SpatialOperationKind(doc.SpatialSpec.Kind), Relationship: SpatialRelationship(doc.SpatialSpec.Relationship),
			DistanceMeters: doc.SpatialSpec.DistanceMeters, Axis: SpatialAxis(doc.SpatialSpec.Axis),
			DeltaMeters: doc.SpatialSpec.DeltaMeters, HasDelta: doc.SpatialSpec.HasDelta,
			TargetMeters: doc.SpatialSpec.TargetMeters, HasTarget: doc.SpatialSpec.HasTarget,
		}
	}
	return sc
}

type spatialProposedDeltaDoc struct {
	Target      spatialDesignTargetDoc      `bson:"target"`
	Intent      string                      `bson:"intent"`
	Summary     []string                    `bson:"summary"`
	Geometry    spatialSectionChangeDoc     `bson:"geometry"`
	Material    spatialSectionChangeDoc     `bson:"material"`
	Spatial     spatialSectionChangeDoc     `bson:"spatial"`
	Blockers    []spatialProposedBlockerDoc `bson:"blockers"`
	Assumptions []string                    `bson:"assumptions"`
	ReviewNotes []string                    `bson:"reviewNotes"`
	Confidence  float64                     `bson:"confidence"`
}

type spatialProposedBlockerDoc struct {
	Code    string `bson:"code"`
	Message string `bson:"message"`
}

func toProposedDeltaDoc(d ProposedSceneEditDelta) spatialProposedDeltaDoc {
	blockers := make([]spatialProposedBlockerDoc, 0, len(d.Blockers))
	for _, b := range d.Blockers {
		blockers = append(blockers, spatialProposedBlockerDoc{Code: b.Code, Message: b.Message})
	}
	return spatialProposedDeltaDoc{
		Target: toDesignTargetDoc(d.Target), Intent: string(d.Intent), Summary: d.Summary,
		Geometry: toSectionChangeDoc(d.Geometry), Material: toSectionChangeDoc(d.Material), Spatial: toSectionChangeDoc(d.Spatial),
		Blockers: blockers, Assumptions: d.Assumptions, ReviewNotes: d.ReviewNotes, Confidence: d.Confidence,
	}
}

func fromProposedDeltaDoc(doc spatialProposedDeltaDoc) ProposedSceneEditDelta {
	blockers := make([]ProposedBlocker, 0, len(doc.Blockers))
	for _, b := range doc.Blockers {
		blockers = append(blockers, ProposedBlocker{Code: b.Code, Message: b.Message})
	}
	return ProposedSceneEditDelta{
		Target: fromDesignTargetDoc(doc.Target), Intent: DesignIntent(doc.Intent), Summary: doc.Summary,
		Geometry: fromSectionChangeDoc(doc.Geometry), Material: fromSectionChangeDoc(doc.Material), Spatial: fromSectionChangeDoc(doc.Spatial),
		Blockers: blockers, Assumptions: doc.Assumptions, ReviewNotes: doc.ReviewNotes, Confidence: doc.Confidence,
	}
}

type spatialFitObservationDoc struct {
	Code    string `bson:"code"`
	Message string `bson:"message"`
}
type spatialFitWarningDoc struct {
	Code            string   `bson:"code"`
	Message         string   `bson:"message"`
	ClearanceBefore *float64 `bson:"clearanceBefore,omitempty"`
	ClearanceAfter  *float64 `bson:"clearanceAfter,omitempty"`
}
type spatialFitBlockerDoc struct {
	Code    string `bson:"code"`
	Message string `bson:"message"`
}
type spatialFitAnalysisDoc struct {
	Status       string                     `bson:"status"`
	Observations []spatialFitObservationDoc `bson:"observations"`
	Warnings     []spatialFitWarningDoc     `bson:"warnings"`
	Blockers     []spatialFitBlockerDoc     `bson:"blockers"`
}

func toFitAnalysisDoc(f FitAnalysis) spatialFitAnalysisDoc {
	doc := spatialFitAnalysisDoc{Status: string(f.Status)}
	for _, o := range f.Observations {
		doc.Observations = append(doc.Observations, spatialFitObservationDoc{Code: o.Code, Message: o.Message})
	}
	for _, w := range f.Warnings {
		doc.Warnings = append(doc.Warnings, spatialFitWarningDoc{Code: w.Code, Message: w.Message, ClearanceBefore: w.ClearanceBefore, ClearanceAfter: w.ClearanceAfter})
	}
	for _, b := range f.Blockers {
		doc.Blockers = append(doc.Blockers, spatialFitBlockerDoc{Code: b.Code, Message: b.Message})
	}
	return doc
}

func fromFitAnalysisDoc(doc spatialFitAnalysisDoc) FitAnalysis {
	f := FitAnalysis{Status: FitStatus(doc.Status)}
	for _, o := range doc.Observations {
		f.Observations = append(f.Observations, FitObservation{Code: o.Code, Message: o.Message})
	}
	for _, w := range doc.Warnings {
		f.Warnings = append(f.Warnings, FitWarning{Code: w.Code, Message: w.Message, ClearanceBefore: w.ClearanceBefore, ClearanceAfter: w.ClearanceAfter})
	}
	for _, b := range doc.Blockers {
		f.Blockers = append(f.Blockers, FitBlocker{Code: b.Code, Message: b.Message})
	}
	return f
}

type spatialDesignExecutionFlagsDoc struct {
	TurnRequiresAssetGeneration bool `bson:"turnRequiresAssetGeneration"`
	HunyuanRequired             bool `bson:"hunyuanRequired"`
	RequiresConfirmation        bool `bson:"requiresConfirmation"`
	Executable                  bool `bson:"executable"`
}

func toExecutionFlagsDoc(e DesignExecutionFlags) spatialDesignExecutionFlagsDoc {
	return spatialDesignExecutionFlagsDoc{
		TurnRequiresAssetGeneration: e.TurnRequiresAssetGeneration, HunyuanRequired: e.HunyuanRequired,
		RequiresConfirmation: e.RequiresConfirmation, Executable: e.Executable,
	}
}

func fromExecutionFlagsDoc(doc spatialDesignExecutionFlagsDoc) DesignExecutionFlags {
	return DesignExecutionFlags{
		TurnRequiresAssetGeneration: doc.TurnRequiresAssetGeneration, HunyuanRequired: doc.HunyuanRequired,
		RequiresConfirmation: doc.RequiresConfirmation, Executable: doc.Executable,
	}
}

type spatialValidatedPlanDoc struct {
	Target                   spatialDesignTargetDoc         `bson:"target"`
	BasedOnRoomDraftRevision int64                          `bson:"basedOnRoomDraftRevision"`
	WorkingDesign            spatialWorkingDesignDoc        `bson:"workingDesign"`
	Fit                      spatialFitAnalysisDoc          `bson:"fit"`
	Execution                spatialDesignExecutionFlagsDoc `bson:"execution"`
}

func toValidatedPlanDoc(p ValidatedSceneEditPlan) spatialValidatedPlanDoc {
	return spatialValidatedPlanDoc{
		Target: toDesignTargetDoc(p.Target), BasedOnRoomDraftRevision: p.BasedOnRoomDraftRevision,
		WorkingDesign: toWorkingDesignDoc(p.WorkingDesign), Fit: toFitAnalysisDoc(p.Fit), Execution: toExecutionFlagsDoc(p.Execution),
	}
}

func fromValidatedPlanDoc(doc spatialValidatedPlanDoc) ValidatedSceneEditPlan {
	return ValidatedSceneEditPlan{
		Target: fromDesignTargetDoc(doc.Target), BasedOnRoomDraftRevision: doc.BasedOnRoomDraftRevision,
		WorkingDesign: fromWorkingDesignDoc(doc.WorkingDesign), Fit: fromFitAnalysisDoc(doc.Fit), Execution: fromExecutionFlagsDoc(doc.Execution),
	}
}

type spatialDesignTurnDoc struct {
	ID                       bson.ObjectID            `bson:"_id,omitempty"`
	CompanyID                string                   `bson:"companyId"`
	SessionID                string                   `bson:"sessionId"`
	ClientRequestID          string                   `bson:"clientRequestId"`
	RequestFingerprint       string                   `bson:"requestFingerprint"`
	Sequence                 int64                    `bson:"sequence"`
	PreviousTurnID           string                   `bson:"previousTurnId,omitempty"`
	ParentPlanTurnID         string                   `bson:"parentPlanTurnId,omitempty"`
	BaseSessionRevision      int64                    `bson:"baseSessionRevision"`
	BasedOnRoomDraftRevision int64                    `bson:"basedOnRoomDraftRevision"`
	Instruction              string                   `bson:"instruction"`
	Status                   string                   `bson:"status"`
	ProviderStartedAt        *time.Time               `bson:"providerStartedAt,omitempty"`
	ProposedDelta            *spatialProposedDeltaDoc `bson:"proposedDelta,omitempty"`
	ValidatedPlan            *spatialValidatedPlanDoc `bson:"validatedPlan,omitempty"`
	PlanFingerprint          string                   `bson:"planFingerprint,omitempty"`
	SafeFailureCode          string                   `bson:"safeFailureCode,omitempty"`
	StartedAt                time.Time                `bson:"startedAt"`
	CreatedAt                time.Time                `bson:"createdAt"`
	CompletedAt              *time.Time               `bson:"completedAt,omitempty"`
	SchemaVersion            int                      `bson:"schemaVersion"`
}

func toDesignTurnDoc(t SpatialDesignTurn) spatialDesignTurnDoc {
	doc := spatialDesignTurnDoc{
		CompanyID: t.CompanyID, SessionID: t.SessionID,
		ClientRequestID: t.ClientRequestID, RequestFingerprint: t.RequestFingerprint,
		Sequence: t.Sequence, PreviousTurnID: t.PreviousTurnID, ParentPlanTurnID: t.ParentPlanTurnID,
		BaseSessionRevision: t.BaseSessionRevision, BasedOnRoomDraftRevision: t.BasedOnRoomDraftRevision,
		Instruction: t.Instruction, Status: string(t.Status), ProviderStartedAt: t.ProviderStartedAt,
		PlanFingerprint: t.PlanFingerprint, SafeFailureCode: t.SafeFailureCode,
		StartedAt: t.StartedAt, CreatedAt: t.CreatedAt, CompletedAt: t.CompletedAt, SchemaVersion: t.SchemaVersion,
	}
	if t.ID != "" {
		objID, _ := bson.ObjectIDFromHex(t.ID)
		doc.ID = objID
	}
	if t.ProposedDelta != nil {
		deltaDoc := toProposedDeltaDoc(*t.ProposedDelta)
		doc.ProposedDelta = &deltaDoc
	}
	if t.ValidatedPlan != nil {
		planDoc := toValidatedPlanDoc(*t.ValidatedPlan)
		doc.ValidatedPlan = &planDoc
	}
	return doc
}

func fromDesignTurnDoc(doc spatialDesignTurnDoc) SpatialDesignTurn {
	t := SpatialDesignTurn{
		ID: doc.ID.Hex(), CompanyID: doc.CompanyID, SessionID: doc.SessionID,
		ClientRequestID: doc.ClientRequestID, RequestFingerprint: doc.RequestFingerprint,
		Sequence: doc.Sequence, PreviousTurnID: doc.PreviousTurnID, ParentPlanTurnID: doc.ParentPlanTurnID,
		BaseSessionRevision: doc.BaseSessionRevision, BasedOnRoomDraftRevision: doc.BasedOnRoomDraftRevision,
		Instruction: doc.Instruction, Status: SpatialDesignTurnStatus(doc.Status), ProviderStartedAt: doc.ProviderStartedAt,
		PlanFingerprint: doc.PlanFingerprint, SafeFailureCode: doc.SafeFailureCode,
		StartedAt: doc.StartedAt, CreatedAt: doc.CreatedAt, CompletedAt: doc.CompletedAt, SchemaVersion: doc.SchemaVersion,
	}
	if doc.ProposedDelta != nil {
		delta := fromProposedDeltaDoc(*doc.ProposedDelta)
		t.ProposedDelta = &delta
	}
	if doc.ValidatedPlan != nil {
		plan := fromValidatedPlanDoc(*doc.ValidatedPlan)
		t.ValidatedPlan = &plan
	}
	return t
}

// --- DesignSessionRepository ---

func (r *MongoDesignRepository) CreateOrGetSession(ctx context.Context, session SpatialDesignSession) (SpatialDesignSession, error) {
	doc := toDesignSessionDoc(session)
	res, err := r.sessionCollection.InsertOne(ctx, doc)
	if err == nil {
		session.ID = res.InsertedID.(bson.ObjectID).Hex()
		return session, nil
	}
	if !mongo.IsDuplicateKeyError(err) {
		return SpatialDesignSession{}, err
	}
	var existingDoc spatialDesignSessionDoc
	findErr := r.sessionCollection.FindOne(ctx, bson.M{"companyId": session.CompanyID, "clientSessionId": session.ClientSessionID}).Decode(&existingDoc)
	if findErr != nil {
		return SpatialDesignSession{}, findErr
	}
	if existingDoc.SessionRequestFingerprint != session.SessionRequestFingerprint {
		return SpatialDesignSession{}, ErrDesignSessionRequestConflict
	}
	return fromDesignSessionDoc(existingDoc), nil
}

func (r *MongoDesignRepository) FindSession(ctx context.Context, companyID, id string) (SpatialDesignSession, error) {
	objID, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return SpatialDesignSession{}, ErrDesignSessionNotFound
	}
	var doc spatialDesignSessionDoc
	if err := r.sessionCollection.FindOne(ctx, bson.M{"_id": objID, "companyId": companyID}).Decode(&doc); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return SpatialDesignSession{}, ErrDesignSessionNotFound
		}
		return SpatialDesignSession{}, err
	}
	return fromDesignSessionDoc(doc), nil
}

// --- DesignTurnRepository ---

// errDesignTurnAlreadyRecordedConflict/errDesignTurnInProgressInternal
// unwind WithTransaction with an unambiguous internal sentinel, mirroring
// MongoRoomDraftEditRepository.ApplyAndRecord's errOperationAlreadyRecordedConflict
// precedent exactly (same "translate back to the exported sentinel once
// outside the transaction" pattern).
var (
	errDesignTurnAlreadyRecordedConflict = errors.New("spatial: internal — design turn request conflict inside transaction")
	errDesignTurnInProgressInternal      = errors.New("spatial: internal — design turn in progress inside transaction")
)

func (r *MongoDesignRepository) ReserveTurn(ctx context.Context, turn SpatialDesignTurn) (SpatialDesignTurn, bool, error) {
	sessionObjID, err := bson.ObjectIDFromHex(turn.SessionID)
	if err != nil {
		return SpatialDesignTurn{}, false, ErrDesignSessionNotFound
	}

	session, err := r.client.StartSession()
	if err != nil {
		return SpatialDesignTurn{}, false, err
	}
	defer session.EndSession(ctx)

	type txResult struct {
		turn     SpatialDesignTurn
		replayed bool
	}

	result, err := session.WithTransaction(ctx, func(txCtx context.Context) (any, error) {
		// Idempotency check FIRST, inside the transaction, before any
		// session mutation — the same ordering
		// MongoRoomDraftEditRepository.ApplyAndRecord uses, for the same
		// WithTransaction-silent-replay safety reason.
		var existingDoc spatialDesignTurnDoc
		findErr := r.turnCollection.FindOne(txCtx, bson.M{
			"companyId": turn.CompanyID, "sessionId": turn.SessionID, "clientRequestId": turn.ClientRequestID,
		}).Decode(&existingDoc)
		if findErr == nil {
			existing := fromDesignTurnDoc(existingDoc)
			if existing.RequestFingerprint != turn.RequestFingerprint {
				return nil, errDesignTurnAlreadyRecordedConflict
			}
			return txResult{turn: existing, replayed: true}, nil
		}
		if !errors.Is(findErr, mongo.ErrNoDocuments) {
			return nil, findErr
		}

		var sessionDoc spatialDesignSessionDoc
		if sessErr := r.sessionCollection.FindOne(txCtx, bson.M{"_id": sessionObjID, "companyId": turn.CompanyID}).Decode(&sessionDoc); sessErr != nil {
			if errors.Is(sessErr, mongo.ErrNoDocuments) {
				return nil, ErrDesignSessionNotFound
			}
			return nil, sessErr
		}
		if sessionDoc.ActiveTurnID != "" {
			return nil, errDesignTurnInProgressInternal
		}

		nextSequence := sessionDoc.LastTurnSequence + 1
		turn.Sequence = nextSequence
		turn.PreviousTurnID = sessionDoc.LatestTurnID
		turn.ParentPlanTurnID = sessionDoc.LatestReadyPlanTurnID

		turnDoc := toDesignTurnDoc(turn)
		insertResult, insertErr := r.turnCollection.InsertOne(txCtx, turnDoc)
		if insertErr != nil {
			return nil, insertErr
		}
		turnDoc.ID = insertResult.InsertedID.(bson.ObjectID)

		sessionUpdate := bson.M{"$set": bson.M{
			"activeTurnId": turnDoc.ID.Hex(), "latestTurnId": turnDoc.ID.Hex(),
			"lastTurnSequence": nextSequence, "updatedAt": time.Now(),
		}}
		if _, updateErr := r.sessionCollection.UpdateOne(txCtx, bson.M{"_id": sessionObjID, "companyId": turn.CompanyID}, sessionUpdate); updateErr != nil {
			return nil, updateErr
		}

		return txResult{turn: fromDesignTurnDoc(turnDoc), replayed: false}, nil
	})

	if err != nil {
		if errors.Is(err, errDesignTurnAlreadyRecordedConflict) {
			return SpatialDesignTurn{}, false, ErrDesignTurnRequestConflict
		}
		if errors.Is(err, errDesignTurnInProgressInternal) {
			return SpatialDesignTurn{}, false, ErrDesignTurnInProgress
		}
		return SpatialDesignTurn{}, false, err
	}
	tx := result.(txResult)
	return tx.turn, tx.replayed, nil
}

func (r *MongoDesignRepository) MarkProviderStarted(ctx context.Context, companyID, turnID string, startedAt time.Time) error {
	objID, err := bson.ObjectIDFromHex(turnID)
	if err != nil {
		return ErrDesignTurnNotFound
	}
	res, err := r.turnCollection.UpdateOne(ctx,
		bson.M{"_id": objID, "companyId": companyID, "status": string(SpatialDesignTurnStatusReserved)},
		bson.M{"$set": bson.M{"providerStartedAt": startedAt, "status": string(SpatialDesignTurnStatusReasoning)}},
	)
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return ErrDesignTurnNotClaimable
	}
	return nil
}

func (r *MongoDesignRepository) FinishTurn(ctx context.Context, input FinishTurnInput) (SpatialDesignTurn, error) {
	turnObjID, err := bson.ObjectIDFromHex(input.TurnID)
	if err != nil {
		return SpatialDesignTurn{}, ErrDesignTurnNotFound
	}
	sessionObjID, err := bson.ObjectIDFromHex(input.SessionID)
	if err != nil {
		return SpatialDesignTurn{}, ErrDesignSessionNotFound
	}

	session, err := r.client.StartSession()
	if err != nil {
		return SpatialDesignTurn{}, err
	}
	defer session.EndSession(ctx)

	result, err := session.WithTransaction(ctx, func(txCtx context.Context) (any, error) {
		var existingDoc spatialDesignTurnDoc
		if findErr := r.turnCollection.FindOne(txCtx, bson.M{"_id": turnObjID, "companyId": input.CompanyID}).Decode(&existingDoc); findErr != nil {
			if errors.Is(findErr, mongo.ErrNoDocuments) {
				return nil, ErrDesignTurnNotFound
			}
			return nil, findErr
		}
		// Idempotent duplicate-completion: if this turn already reached a
		// terminal status, adopt the STORED result rather than overwriting
		// it with a possibly-different freshly computed one.
		if isTerminalDesignTurnStatus(SpatialDesignTurnStatus(existingDoc.Status)) {
			return fromDesignTurnDoc(existingDoc), nil
		}

		now := time.Now()
		turnUpdate := bson.M{
			"status": string(input.Status), "completedAt": now,
			"planFingerprint": input.PlanFingerprint, "safeFailureCode": input.SafeFailureCode,
		}
		if input.ProposedDelta != nil {
			turnUpdate["proposedDelta"] = toProposedDeltaDoc(*input.ProposedDelta)
		}
		if input.ValidatedPlan != nil {
			turnUpdate["validatedPlan"] = toValidatedPlanDoc(*input.ValidatedPlan)
		}
		if _, updateErr := r.turnCollection.UpdateOne(txCtx, bson.M{"_id": turnObjID, "companyId": input.CompanyID}, bson.M{"$set": turnUpdate}); updateErr != nil {
			return nil, updateErr
		}

		sessionUpdate := bson.M{"$set": bson.M{"activeTurnId": "", "updatedAt": now}}
		if input.Status == SpatialDesignTurnStatusProposed {
			setFields := sessionUpdate["$set"].(bson.M)
			setFields["latestReadyPlanTurnId"] = input.TurnID
			setFields["currentWorkingDesign"] = toWorkingDesignDoc(input.WorkingDesign)
		}
		if _, updateErr := r.sessionCollection.UpdateOne(txCtx, bson.M{"_id": sessionObjID, "companyId": input.CompanyID}, sessionUpdate); updateErr != nil {
			return nil, updateErr
		}

		var finishedDoc spatialDesignTurnDoc
		if findErr := r.turnCollection.FindOne(txCtx, bson.M{"_id": turnObjID, "companyId": input.CompanyID}).Decode(&finishedDoc); findErr != nil {
			return nil, findErr
		}
		return fromDesignTurnDoc(finishedDoc), nil
	})
	if err != nil {
		return SpatialDesignTurn{}, err
	}
	return result.(SpatialDesignTurn), nil
}

func isTerminalDesignTurnStatus(status SpatialDesignTurnStatus) bool {
	switch status {
	case SpatialDesignTurnStatusProposed, SpatialDesignTurnStatusBlocked, SpatialDesignTurnStatusFailed,
		SpatialDesignTurnStatusNeedsAttention, SpatialDesignTurnStatusSuperseded, SpatialDesignTurnStatusStale:
		return true
	default:
		return false
	}
}

func (r *MongoDesignRepository) FindTurn(ctx context.Context, companyID, id string) (SpatialDesignTurn, error) {
	objID, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return SpatialDesignTurn{}, ErrDesignTurnNotFound
	}
	var doc spatialDesignTurnDoc
	if err := r.turnCollection.FindOne(ctx, bson.M{"_id": objID, "companyId": companyID}).Decode(&doc); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return SpatialDesignTurn{}, ErrDesignTurnNotFound
		}
		return SpatialDesignTurn{}, err
	}
	return fromDesignTurnDoc(doc), nil
}

func (r *MongoDesignRepository) FindTurnByClientRequestID(ctx context.Context, companyID, sessionID, clientRequestID string) (SpatialDesignTurn, error) {
	var doc spatialDesignTurnDoc
	err := r.turnCollection.FindOne(ctx, bson.M{
		"companyId": companyID, "sessionId": sessionID, "clientRequestId": clientRequestID,
	}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return SpatialDesignTurn{}, ErrDesignTurnNotFound
	}
	if err != nil {
		return SpatialDesignTurn{}, err
	}
	return fromDesignTurnDoc(doc), nil
}

func (r *MongoDesignRepository) ListTurns(ctx context.Context, companyID, sessionID string, beforeSequence int64, limit int) ([]SpatialDesignTurn, error) {
	filter := bson.M{"companyId": companyID, "sessionId": sessionID}
	if beforeSequence > 0 {
		filter["sequence"] = bson.M{"$lt": beforeSequence}
	}
	cursor, err := r.turnCollection.Find(ctx, filter,
		options.Find().SetSort(bson.D{{Key: "sequence", Value: -1}}).SetLimit(int64(limit)))
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var docs []spatialDesignTurnDoc
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, err
	}
	out := make([]SpatialDesignTurn, 0, len(docs))
	for _, doc := range docs {
		out = append(out, fromDesignTurnDoc(doc))
	}
	return out, nil
}

func (r *MongoDesignRepository) RecoverInterruptedTurns(ctx context.Context, olderThan time.Duration) ([]SpatialDesignTurn, error) {
	cutoff := time.Now().Add(-olderThan)
	filter := bson.M{
		"status":            string(SpatialDesignTurnStatusReasoning),
		"providerStartedAt": bson.M{"$ne": nil, "$lt": cutoff},
	}
	cursor, err := r.turnCollection.Find(ctx, filter)
	if err != nil {
		return nil, err
	}
	var candidateDocs []spatialDesignTurnDoc
	if err := cursor.All(ctx, &candidateDocs); err != nil {
		cursor.Close(ctx)
		return nil, err
	}
	cursor.Close(ctx)

	recovered := make([]SpatialDesignTurn, 0, len(candidateDocs))
	for _, candidate := range candidateDocs {
		mongoSession, err := r.client.StartSession()
		if err != nil {
			return recovered, err
		}
		result, txErr := mongoSession.WithTransaction(ctx, func(txCtx context.Context) (any, error) {
			// Re-check inside the transaction: another recovery pass or a
			// legitimate completion may have already resolved this turn.
			updateRes, updateErr := r.turnCollection.UpdateOne(txCtx,
				bson.M{"_id": candidate.ID, "companyId": candidate.CompanyID, "status": string(SpatialDesignTurnStatusReasoning)},
				bson.M{"$set": bson.M{
					"status": string(SpatialDesignTurnStatusNeedsAttention), "completedAt": time.Now(),
					"safeFailureCode": "design_reasoning_interrupted",
				}},
			)
			if updateErr != nil {
				return nil, updateErr
			}
			if updateRes.MatchedCount == 0 {
				return nil, nil // already resolved by something else; nothing to recover
			}
			sessionObjID, parseErr := bson.ObjectIDFromHex(candidate.SessionID)
			if parseErr != nil {
				return nil, parseErr
			}
			if _, updateSessErr := r.sessionCollection.UpdateOne(txCtx,
				bson.M{"_id": sessionObjID, "companyId": candidate.CompanyID, "activeTurnId": candidate.ID.Hex()},
				bson.M{"$set": bson.M{"activeTurnId": "", "updatedAt": time.Now()}},
			); updateSessErr != nil {
				return nil, updateSessErr
			}
			candidate.Status = string(SpatialDesignTurnStatusNeedsAttention)
			return fromDesignTurnDoc(candidate), nil
		})
		mongoSession.EndSession(ctx)
		if txErr != nil {
			return recovered, txErr
		}
		if result != nil {
			recovered = append(recovered, result.(SpatialDesignTurn))
		}
	}
	return recovered, nil
}
