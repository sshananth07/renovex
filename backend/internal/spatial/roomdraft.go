package spatial

import "time"

// SourceProvider identifies which capture provider produced a RoomDraft
// element's initial evidence — the Go mirror of the iOS
// SpatialCaptureSourceProvider (ios/RenovexCapture/Sources/RenovexCaptureCore/Domain/SourceProvenance.swift).
// Provenance metadata only — never a substitute for a Renovex-owned stable
// ID (design spec §8.9).
type SourceProvider string

const (
	SourceProviderRoomPlan SourceProvider = "roomplan"
	SourceProviderArCore   SourceProvider = "arcore"
	SourceProviderFixture  SourceProvider = "fixture"
)

// ElementProvenance mirrors the iOS SourceProvenance type: which provider
// produced one RoomDraft element, and that provider's own identifier for it
// (never the element's Renovex stable ID).
type ElementProvenance struct {
	Provider                SourceProvider `bson:"provider" json:"provider"`
	SourceElementIdentifier string         `bson:"sourceElementIdentifier" json:"sourceElementIdentifier"`
	SourceCaptureIdentifier string         `bson:"sourceCaptureIdentifier,omitempty" json:"sourceCaptureIdentifier,omitempty"`
}

// RoomLocalPoint mirrors the iOS canonical room-local coordinate contract
// (ios/RenovexCapture/Sources/RenovexCaptureCore/Domain/RoomLocalTransform.swift):
// metres, right-handed, Y-up.
type RoomLocalPoint struct {
	X float64 `bson:"x" json:"x"`
	Y float64 `bson:"y" json:"y"`
	Z float64 `bson:"z" json:"z"`
}

// RoomLocalQuaternion mirrors the iOS RoomLocalQuaternion — a unit
// quaternion in the Renovex room-local frame.
type RoomLocalQuaternion struct {
	X float64 `bson:"x" json:"x"`
	Y float64 `bson:"y" json:"y"`
	Z float64 `bson:"z" json:"z"`
	W float64 `bson:"w" json:"w"`
}

// RoomLocalTransform mirrors the iOS RoomLocalTransform — position +
// rotation in the Renovex room-local frame.
type RoomLocalTransform struct {
	Position RoomLocalPoint      `bson:"position" json:"position"`
	Rotation RoomLocalQuaternion `bson:"rotation" json:"rotation"`
}

// MeasurementStatus mirrors the iOS MeasurementStatus: how confident/
// authoritative a measurement value is. The full evidence-provenance model
// with AR/physical verification tiers is RP5 scope.
type MeasurementStatus string

const (
	MeasurementStatusEstimated   MeasurementStatus = "estimated"
	MeasurementStatusUnconfirmed MeasurementStatus = "unconfirmed"
)

// OpeningKind mirrors the iOS OpeningKind opening classification. Aligned
// literally with design spec §6.1's authoritative opening types (RP4A) —
// NOT RP2's original door|window|opening set. RoomPlan's generic-opening
// detection (no stronger evidence for door/window/archway) normalizes to
// OpeningKindOther, not a dedicated "opening" case — see
// RoomDraftNormalizer.openingKind(for:) on the iOS side for the exact
// mapping this replaced.
type OpeningKind string

const (
	OpeningKindDoor    OpeningKind = "door"
	OpeningKindWindow  OpeningKind = "window"
	OpeningKindArchway OpeningKind = "archway"
	OpeningKindOther   OpeningKind = "other"
)

// OpeningProfile is orthogonal to OpeningKind (design spec §6.2) — a door
// or window may have either profile; "archway" as a Kind describes function
// (no door/window purpose), while Profile describes the opening's actual
// geometric shape.
type OpeningProfile string

const (
	OpeningProfileRectangle OpeningProfile = "rectangle"
	OpeningProfileArch      OpeningProfile = "arch"
)

// ArchParameters is only meaningful when an opening's Profile is
// OpeningProfileArch (design spec §6.2).
type ArchParameters struct {
	SpringHeight float64  `bson:"springHeight" json:"springHeight"`
	ArchRise     float64  `bson:"archRise" json:"archRise"`
	Radius       *float64 `bson:"radius,omitempty" json:"radius,omitempty"`
}

// DoorHinge is which side of the opening a door's hinge is on, viewed from
// the room-local frame's canonical orientation.
type DoorHinge string

const (
	DoorHingeLeft  DoorHinge = "left"
	DoorHingeRight DoorHinge = "right"
)

// DoorSwing is which direction a door swings relative to the wall plane.
type DoorSwing string

const (
	DoorSwingInward  DoorSwing = "inward"
	DoorSwingOutward DoorSwing = "outward"
)

// DoorMetadata is only meaningful when a RoomDraftOpening's Kind is
// OpeningKindDoor (design spec §8.15: "leaf count, hinge side, swing
// direction, inward/outward/open direction").
type DoorMetadata struct {
	LeafCount     int       `bson:"leafCount" json:"leafCount"`
	Hinge         DoorHinge `bson:"hinge,omitempty" json:"hinge,omitempty"`
	Swing         DoorSwing `bson:"swing,omitempty" json:"swing,omitempty"`
	OpenDirection string    `bson:"openDirection,omitempty" json:"openDirection,omitempty"`
}

// RoomDraftWall mirrors the iOS RoomDraftWall — the smallest architectural
// element in a RoomDraft.
type RoomDraftWall struct {
	ID              string            `bson:"id" json:"id"`
	Start           RoomLocalPoint    `bson:"start" json:"start"`
	End             RoomLocalPoint    `bson:"end" json:"end"`
	Height          *float64          `bson:"height,omitempty" json:"height,omitempty"`
	Thickness       *float64          `bson:"thickness,omitempty" json:"thickness,omitempty"`
	ThicknessStatus MeasurementStatus `bson:"thicknessStatus" json:"thicknessStatus"`
	Provenance      ElementProvenance `bson:"provenance" json:"provenance"`
}

// RoomDraftOpening mirrors the iOS RoomDraftOpening — a door, window,
// archway, or other opening normalized from a capture provider's detected
// element or added by a contractor (RP4A: add_opening).
type RoomDraftOpening struct {
	ID           string             `bson:"id" json:"id"`
	ParentWallID string             `bson:"parentWallId,omitempty" json:"parentWallId,omitempty"`
	Kind         OpeningKind        `bson:"kind" json:"kind"`
	Profile      OpeningProfile     `bson:"profile" json:"profile"`
	Transform    RoomLocalTransform `bson:"transform" json:"transform"`
	Width        *float64           `bson:"width,omitempty" json:"width,omitempty"`
	Height       *float64           `bson:"height,omitempty" json:"height,omitempty"`
	// OffsetAlongWall is the opening's position measured along ParentWallID
	// from its Start point (design spec §6.2) — an explicit wall-relative
	// coordinate for 2D plan editing, distinct from Transform.position's
	// room-local (not wall-relative) coordinates.
	OffsetAlongWall *float64 `bson:"offsetAlongWall,omitempty" json:"offsetAlongWall,omitempty"`
	// SillHeight is only meaningful for windows (design spec §8.15).
	SillHeight *float64 `bson:"sillHeight,omitempty" json:"sillHeight,omitempty"`
	// ArchParameters is only meaningful when Profile is OpeningProfileArch.
	ArchParameters *ArchParameters `bson:"archParameters,omitempty" json:"archParameters,omitempty"`
	// Door is only meaningful when Kind is OpeningKindDoor.
	Door       *DoorMetadata     `bson:"door,omitempty" json:"door,omitempty"`
	Provenance ElementProvenance `bson:"provenance" json:"provenance"`
}

// RoomDraftObject mirrors the iOS RoomDraftObject — one object normalized
// from a capture provider's detected category.
type RoomDraftObject struct {
	ID         string             `bson:"id" json:"id"`
	Category   string             `bson:"category" json:"category"`
	Transform  RoomLocalTransform `bson:"transform" json:"transform"`
	Dimensions *RoomLocalPoint    `bson:"dimensions,omitempty" json:"dimensions,omitempty"`
	Provenance ElementProvenance  `bson:"provenance" json:"provenance"`
	// VisualAsset is the optional canonical binding to a published
	// VisualAssetVersion (RP4D) — identity + exact version only, never a
	// URL/storage key. Absent means "no explicit binding," which the Web
	// renderer falls back to category-default/procedural resolution for.
	VisualAsset *VisualAssetRef `bson:"visualAsset,omitempty" json:"visualAsset,omitempty"`
	// Appearance is the optional material-only design override (RP4E2/
	// M8.5C amendment) — set_visual_appearance/clear_visual_appearance.
	// Independent of VisualAsset: a material-only concept can preview and
	// persist without ever generating new geometry.
	Appearance *VisualAppearance `bson:"appearance,omitempty" json:"appearance,omitempty"`
}

// ElementOrigin discriminates a RoomDraftFixture/RoomDraftServicePoint/
// RoomDraftConstraint's authorship (RP4A, design spec §8.16). Wall/Opening/
// Object elements do not carry this — §8.13's operation vocabulary has no
// add_wall, and every V1 wall/opening/object is capture-derived, so the
// discriminated-provenance concern is specific to the three element kinds
// a contractor can create from nothing.
type ElementOrigin string

const (
	ElementOriginCapture    ElementOrigin = "capture"
	ElementOriginContractor ElementOrigin = "contractor"
)

// FixtureCategory is design spec §8.16's FixedFixtures vocabulary.
type FixtureCategory string

const (
	FixtureCategoryAC               FixtureCategory = "ac"
	FixtureCategoryBoiler           FixtureCategory = "boiler"
	FixtureCategoryBuiltInCabinetry FixtureCategory = "built_in_cabinetry"
	FixtureCategoryWallFixture      FixtureCategory = "wall_fixture"
	FixtureCategoryElectricalPanel  FixtureCategory = "electrical_panel"
	FixtureCategoryOther            FixtureCategory = "other"
)

// RoomDraftFixture is a FixedFixture (design spec §8.16/§7) — a renovation-
// relevant fixed element RoomPlan's finite object vocabulary does not
// detect (AC units, boilers, built-in cabinetry, wall fixtures, electrical
// panels). Distinct from RoomDraftObject: fixtures are never RoomPlan-
// detected in V1 (CreatedBy is always ElementOriginContractor today; the
// field exists for forward-compatibility with a future capture provider
// that could detect fixtures directly).
type RoomDraftFixture struct {
	ID         string             `bson:"id" json:"id"`
	Category   FixtureCategory    `bson:"category" json:"category"`
	Transform  RoomLocalTransform `bson:"transform" json:"transform"`
	Dimensions *RoomLocalPoint    `bson:"dimensions,omitempty" json:"dimensions,omitempty"`
	// ParentWallID is set for a wall-mounted fixture (e.g. a wall lamp or
	// electrical panel); nil for a floor-standing fixture (e.g. a boiler).
	ParentWallID string        `bson:"parentWallId,omitempty" json:"parentWallId,omitempty"`
	CreatedBy    ElementOrigin `bson:"createdBy" json:"createdBy"`
	// Provenance is present only when CreatedBy is ElementOriginCapture. Nil
	// for a contractor-created fixture — never a synthesized provider ID.
	Provenance *ElementProvenance `bson:"provenance,omitempty" json:"provenance,omitempty"`
	// VisualAsset is the optional canonical binding to a published
	// VisualAssetVersion (RP4D) — identity + exact version only, never a
	// URL/storage key. Absent means "no explicit binding," which the Web
	// renderer falls back to category-default/procedural resolution for.
	VisualAsset *VisualAssetRef `bson:"visualAsset,omitempty" json:"visualAsset,omitempty"`
	// Appearance is the optional material-only design override (RP4E2/
	// M8.5C amendment) — set_visual_appearance/clear_visual_appearance.
	Appearance *VisualAppearance `bson:"appearance,omitempty" json:"appearance,omitempty"`
}

// ServicePointKind is design spec §7.2/§8.16's controlled-point vocabulary.
type ServicePointKind string

const (
	ServicePointKindPlumbing   ServicePointKind = "plumbing"
	ServicePointKindElectrical ServicePointKind = "electrical"
	ServicePointKindDrain      ServicePointKind = "drain"
	ServicePointKindGas        ServicePointKind = "gas"
	ServicePointKindData       ServicePointKind = "data"
)

// RoomDraftServicePoint is a controlled point (design spec §7.2) — not a
// complete building service network. Point-like (a single RoomLocalPoint,
// not a full Transform) since a service point has no meaningful rotation.
type RoomDraftServicePoint struct {
	ID           string             `bson:"id" json:"id"`
	Kind         ServicePointKind   `bson:"kind" json:"kind"`
	Position     RoomLocalPoint     `bson:"position" json:"position"`
	ParentWallID string             `bson:"parentWallId,omitempty" json:"parentWallId,omitempty"`
	CreatedBy    ElementOrigin      `bson:"createdBy" json:"createdBy"`
	Provenance   *ElementProvenance `bson:"provenance,omitempty" json:"provenance,omitempty"`
}

// ConstraintKind is design spec §7.1/§8.16's obstacle vocabulary.
type ConstraintKind string

const (
	ConstraintKindColumn            ConstraintKind = "column"
	ConstraintKindStaircase         ConstraintKind = "staircase"
	ConstraintKindImmovableObstacle ConstraintKind = "immovable_obstacle"
)

// RoomDraftConstraint is an obstacle (design spec §7.1) that participates
// in collision validation and layout constraints.
type RoomDraftConstraint struct {
	ID         string             `bson:"id" json:"id"`
	Kind       ConstraintKind     `bson:"kind" json:"kind"`
	Transform  RoomLocalTransform `bson:"transform" json:"transform"`
	Dimensions *RoomLocalPoint    `bson:"dimensions,omitempty" json:"dimensions,omitempty"`
	CreatedBy  ElementOrigin      `bson:"createdBy" json:"createdBy"`
	Provenance *ElementProvenance `bson:"provenance,omitempty" json:"provenance,omitempty"`
}

// RoomDraft is the canonical Renovex editable spatial model (design spec
// §8.11), persisted server-side per plan §RP3 so a contractor can leave the
// app and later resume the same unconfirmed capture/review with identical
// stable IDs. Mirrors the iOS RoomDraft
// (ios/RenovexCapture/Sources/RenovexCaptureCore/Domain/RoomDraft.swift)
// field-for-field; the frozen Kotlin RoomDraft type is not reused (different
// language/platform), only referenced as a prior field-shape precedent.
//
// A RoomDraft is mutable up to capture confirmation (design spec §8.11);
// confirmation copies its accepted geometry into an immutable
// SpatialRoomVersion (RP6 scope — the geometry payload on SpatialRoomVersion
// does not exist yet, see spatial.go's SpatialRoomVersion doc comment).
type RoomDraft struct {
	ID        string             `bson:"_id,omitempty" json:"id"`
	CompanyID string             `bson:"companyId" json:"companyId"`
	CaptureID string             `bson:"captureId" json:"captureId"`
	Walls     []RoomDraftWall    `bson:"walls" json:"walls"`
	Openings  []RoomDraftOpening `bson:"openings" json:"openings"`
	Objects   []RoomDraftObject  `bson:"objects" json:"objects"`
	// Fixtures/ServicePoints/Constraints are RP4A additions (design spec
	// §8.16, §7). Deliberately absent from RoomDraftBaseline: Reset to Scan
	// (design spec §8.14/§8.25) restores Walls/Openings/Objects to their
	// captured baseline but clears these three slices entirely, since a
	// contractor-created fixture/service-point/constraint never existed in
	// the capture baseline to begin with.
	Fixtures      []RoomDraftFixture      `bson:"fixtures" json:"fixtures"`
	ServicePoints []RoomDraftServicePoint `bson:"servicePoints" json:"servicePoints"`
	Constraints   []RoomDraftConstraint   `bson:"constraints" json:"constraints"`
	// SourceProvider is the provider that produced this draft as a whole
	// (distinct from each element's own Provenance.Provider — a RoomDraft
	// may in principle later merge/rebase evidence from more than one
	// capture, design spec §28).
	SourceProvider SourceProvider `bson:"sourceProvider" json:"sourceProvider"`
	// OriginalBaseline preserves the normalized-from-capture geometry
	// exactly as first produced, before any contractor edit (design spec
	// §8.14's "Reset to Scan" workflow, plan §RP3 §6). Nil until the first
	// edit is made — Walls/Openings/Objects above ARE the baseline until
	// then, so no redundant copy is stored for an unedited draft.
	OriginalBaseline *RoomDraftBaseline `bson:"originalBaseline,omitempty" json:"originalBaseline,omitempty"`
	CreatedAt        time.Time          `bson:"createdAt" json:"createdAt"`
	UpdatedAt        time.Time          `bson:"updatedAt" json:"updatedAt"`
	// Revision guards CAS-protected edits, mirroring
	// SpatialSpaceState.Revision's pattern (access.MongoAccessGrantRepository.Revoke
	// precedent). RP3 does not yet expose an edit endpoint (RP4 scope) but
	// the CAS guard is established now so persistence does not need a
	// breaking change when editing lands.
	Revision      int64 `bson:"revision" json:"-"`
	SchemaVersion int   `bson:"schemaVersion" json:"schemaVersion"`
}

// RoomDraftBaseline is the immutable snapshot of a RoomDraft's geometry at
// normalization time, captured once before any contractor edit (design spec
// §8.14 "Reset to Scan"). It never changes after being set.
type RoomDraftBaseline struct {
	Walls    []RoomDraftWall    `bson:"walls" json:"walls"`
	Openings []RoomDraftOpening `bson:"openings" json:"openings"`
	Objects  []RoomDraftObject  `bson:"objects" json:"objects"`
}

// Validate enforces the discriminated CreatedBy/Provenance invariant
// (RP4A): a capture-created fixture must carry real, non-empty Provenance;
// a contractor-created fixture must carry none — never a synthesized
// provider ID standing in for authorship that never happened.
func (f RoomDraftFixture) Validate() error {
	return validateElementOrigin(f.CreatedBy, f.Provenance)
}

// Validate enforces the same discriminated invariant as
// RoomDraftFixture.Validate.
func (s RoomDraftServicePoint) Validate() error {
	return validateElementOrigin(s.CreatedBy, s.Provenance)
}

// Validate enforces the same discriminated invariant as
// RoomDraftFixture.Validate.
func (c RoomDraftConstraint) Validate() error {
	return validateElementOrigin(c.CreatedBy, c.Provenance)
}

func validateElementOrigin(origin ElementOrigin, provenance *ElementProvenance) error {
	switch origin {
	case ElementOriginCapture:
		if provenance == nil || provenance.SourceElementIdentifier == "" {
			return ErrInvalidElementProvenance
		}
		return nil
	case ElementOriginContractor:
		if provenance != nil {
			return ErrInvalidElementProvenance
		}
		return nil
	default:
		return ErrInvalidElementProvenance
	}
}
