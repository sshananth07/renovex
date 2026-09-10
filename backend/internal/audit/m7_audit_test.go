package audit_test

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/shananth/renovation-platform/backend/internal/audit"
)

// M7 adds 24 audit methods across THREE consumer-owned interfaces
// (materialrequirements 8, rfqs 8, suppliers 8). audit.Service satisfies all
// three structurally; no shared cross-module audit interface exists (M7 design
// spec §1.5).

// --- New constants (M7 design spec §1.5) ---

func TestM7SubjectTypesAreDistinct(t *testing.T) {
	subjects := []string{
		audit.SubjectTypeMaterialRequirement,
		audit.SubjectTypeRFQ,
		audit.SubjectTypeSupplier,
		audit.SubjectTypeSupplierOffering,
		audit.SubjectTypeMaterialSupplierPreference,
	}
	seen := map[string]bool{}
	for _, s := range subjects {
		if s == "" {
			t.Error("a subject type must not be empty")
		}
		if seen[s] {
			t.Errorf("duplicate subject type %q", s)
		}
		seen[s] = true
	}
	// Must not collide with the M6 subject types.
	for _, existing := range []string{audit.SubjectTypeQuotation, audit.SubjectTypeAccessGrant} {
		if seen[existing] {
			t.Errorf("M7 subject type collides with the existing %q", existing)
		}
	}
}

func TestM7EventTypesAreDistinct(t *testing.T) {
	eventTypes := []string{
		audit.EventTypeMaterialRequirementsGenerated,
		audit.EventTypeMaterialRequirementCreated,
		audit.EventTypeMaterialRequirementUpdated,
		audit.EventTypeMaterialRequirementReviewed,
		audit.EventTypeMaterialRequirementUnitAcknowledged,
		audit.EventTypeMaterialRequirementDiscrepancyResolved,
		audit.EventTypeMaterialRequirementSplit,
		audit.EventTypeMaterialRequirementArchived,
		audit.EventTypeRFQCreated,
		audit.EventTypeRFQUpdated,
		audit.EventTypeRFQLineAdded,
		audit.EventTypeRFQLineRemoved,
		audit.EventTypeRFQMarkedReady,
		audit.EventTypeRFQReopened,
		audit.EventTypeRFQDeleted,
		audit.EventTypeRFQClaimReconciled,
		audit.EventTypeSupplierCreated,
		audit.EventTypeSupplierUpdated,
		audit.EventTypeSupplierActiveStateChanged,
		audit.EventTypeSupplierOfferingCreated,
		audit.EventTypeSupplierOfferingUpdated,
		audit.EventTypeSupplierOfferingActiveStateChanged,
		audit.EventTypePreferredSupplierChanged,
		audit.EventTypePreferredSupplierCleared,
	}
	if len(eventTypes) != 24 {
		t.Fatalf("expected 24 M7 event types, got %d", len(eventTypes))
	}
	seen := map[string]bool{}
	for _, e := range eventTypes {
		if e == "" {
			t.Error("an event type must not be empty")
		}
		if seen[e] {
			t.Errorf("duplicate event type %q", e)
		}
		seen[e] = true
	}
}

// --- materialrequirements interface: 8 methods ---

func TestRecordMaterialRequirementsGenerated(t *testing.T) {
	svc, repo := newService()

	err := svc.RecordMaterialRequirementsGenerated(context.Background(),
		"company_a", "project_1", "user_1", 3, 5, 1, 1, 2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	e := singleEvent(t, repo)
	if e.EventType != audit.EventTypeMaterialRequirementsGenerated {
		t.Errorf("EventType = %q", e.EventType)
	}
	if e.CompanyID != "company_a" || e.ProjectID != "project_1" {
		t.Errorf("tenant/project scoping wrong: %+v", e)
	}
	if e.ActorType != audit.ActorTypeContractor || e.ActorID != "user_1" {
		t.Errorf("actor = %q/%q, want contractor/user_1", e.ActorType, e.ActorID)
	}
	// The generation summary is a count-only projection: no requirement bodies.
	for key, want := range map[string]int{
		"createdCount": 3, "unchangedCount": 5, "discrepancyCount": 1,
		"sourceRemovedCount": 1, "skippedCount": 2,
	} {
		if e.Metadata[key] != want {
			t.Errorf("Metadata[%q] = %v, want %d", key, e.Metadata[key], want)
		}
	}
}

func TestRecordMaterialRequirementLifecycleMethods(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name      string
		call      func(*audit.Service) error
		eventType string
		checkMeta func(*testing.T, map[string]any)
	}{
		{
			name: "created",
			call: func(s *audit.Service) error {
				return s.RecordMaterialRequirementCreated(ctx, "company_a", "project_1", "user_1", "mr_1", "material_1", "manual")
			},
			eventType: audit.EventTypeMaterialRequirementCreated,
			checkMeta: func(t *testing.T, m map[string]any) {
				if m["materialId"] != "material_1" || m["sourceType"] != "manual" {
					t.Errorf("metadata = %+v", m)
				}
			},
		},
		{
			name: "updated with review reset",
			call: func(s *audit.Service) error {
				return s.RecordMaterialRequirementUpdated(ctx, "company_a", "project_1", "user_1", "mr_1", true)
			},
			eventType: audit.EventTypeMaterialRequirementUpdated,
			checkMeta: func(t *testing.T, m map[string]any) {
				if m["reviewReset"] != true {
					t.Errorf("reviewReset = %v, want true", m["reviewReset"])
				}
			},
		},
		{
			name: "reviewed",
			call: func(s *audit.Service) error {
				return s.RecordMaterialRequirementReviewed(ctx, "company_a", "project_1", "user_1", "mr_1", "material_1")
			},
			eventType: audit.EventTypeMaterialRequirementReviewed,
			checkMeta: func(t *testing.T, m map[string]any) {
				if m["materialId"] != "material_1" {
					t.Errorf("materialId = %v", m["materialId"])
				}
			},
		},
		{
			name: "unit acknowledged",
			call: func(s *audit.Service) error {
				return s.RecordMaterialRequirementUnitAcknowledged(ctx, "company_a", "project_1", "user_1", "mr_1", "kg", "bag")
			},
			eventType: audit.EventTypeMaterialRequirementUnitAcknowledged,
			checkMeta: func(t *testing.T, m map[string]any) {
				if m["procurementUnit"] != "kg" || m["catalogUnit"] != "bag" {
					t.Errorf("units = %v/%v, want kg/bag", m["procurementUnit"], m["catalogUnit"])
				}
			},
		},
		{
			name: "discrepancy resolved",
			call: func(s *audit.Service) error {
				return s.RecordMaterialRequirementDiscrepancyResolved(ctx, "company_a", "project_1", "user_1",
					"mr_1", "merge", "change_detected", "reviewed")
			},
			eventType: audit.EventTypeMaterialRequirementDiscrepancyResolved,
			checkMeta: func(t *testing.T, m map[string]any) {
				if m["action"] != "merge" || m["syncStateBefore"] != "change_detected" || m["anchorStatus"] != "reviewed" {
					t.Errorf("metadata = %+v", m)
				}
			},
		},
		{
			name: "split",
			call: func(s *audit.Service) error {
				return s.RecordMaterialRequirementSplit(ctx, "company_a", "project_1", "user_1", "mr_1", "group_1", 2)
			},
			eventType: audit.EventTypeMaterialRequirementSplit,
			checkMeta: func(t *testing.T, m map[string]any) {
				if m["splitGroupId"] != "group_1" || m["childCount"] != 2 {
					t.Errorf("metadata = %+v", m)
				}
			},
		},
		{
			name: "archived",
			call: func(s *audit.Service) error {
				return s.RecordMaterialRequirementArchived(ctx, "company_a", "project_1", "user_1", "mr_1")
			},
			eventType: audit.EventTypeMaterialRequirementArchived,
			checkMeta: func(t *testing.T, m map[string]any) {},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, repo := newService()
			if err := tc.call(svc); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			e := singleEvent(t, repo)
			if e.EventType != tc.eventType {
				t.Errorf("EventType = %q, want %q", e.EventType, tc.eventType)
			}
			if e.SubjectType != audit.SubjectTypeMaterialRequirement {
				t.Errorf("SubjectType = %q, want %q", e.SubjectType, audit.SubjectTypeMaterialRequirement)
			}
			if e.SubjectID != "mr_1" {
				t.Errorf("SubjectID = %q, want mr_1", e.SubjectID)
			}
			if e.ActorType != audit.ActorTypeContractor || e.ActorID != "user_1" {
				t.Errorf("actor = %q/%q", e.ActorType, e.ActorID)
			}
			tc.checkMeta(t, e.Metadata)
		})
	}
}

// --- rfqs interface: 8 methods ---

func TestRecordRFQLifecycleMethods(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name      string
		call      func(*audit.Service) error
		eventType string
		checkMeta func(*testing.T, map[string]any)
	}{
		{
			name: "created",
			call: func(s *audit.Service) error {
				return s.RecordRFQCreated(ctx, "company_a", "project_1", "user_1", "rfq_1", "RFQ-000001")
			},
			eventType: audit.EventTypeRFQCreated,
		},
		{
			name: "updated",
			call: func(s *audit.Service) error {
				return s.RecordRFQUpdated(ctx, "company_a", "project_1", "user_1", "rfq_1", "RFQ-000001")
			},
			eventType: audit.EventTypeRFQUpdated,
		},
		{
			name: "line added",
			call: func(s *audit.Service) error {
				return s.RecordRFQLineAdded(ctx, "company_a", "project_1", "user_1", "rfq_1", "RFQ-000001", "mr_1", "line_1")
			},
			eventType: audit.EventTypeRFQLineAdded,
			checkMeta: func(t *testing.T, m map[string]any) {
				if m["materialRequirementId"] != "mr_1" || m["lineId"] != "line_1" {
					t.Errorf("metadata = %+v", m)
				}
			},
		},
		{
			name: "line removed",
			call: func(s *audit.Service) error {
				return s.RecordRFQLineRemoved(ctx, "company_a", "project_1", "user_1", "rfq_1", "RFQ-000001", "mr_1", "line_1")
			},
			eventType: audit.EventTypeRFQLineRemoved,
			checkMeta: func(t *testing.T, m map[string]any) {
				if m["materialRequirementId"] != "mr_1" || m["lineId"] != "line_1" {
					t.Errorf("metadata = %+v", m)
				}
			},
		},
		{
			name: "marked ready",
			call: func(s *audit.Service) error {
				return s.RecordRFQMarkedReady(ctx, "company_a", "project_1", "user_1", "rfq_1", "RFQ-000001", 3)
			},
			eventType: audit.EventTypeRFQMarkedReady,
			checkMeta: func(t *testing.T, m map[string]any) {
				if m["lineCount"] != 3 {
					t.Errorf("lineCount = %v, want 3", m["lineCount"])
				}
			},
		},
		{
			name: "reopened",
			call: func(s *audit.Service) error {
				return s.RecordRFQReopened(ctx, "company_a", "project_1", "user_1", "rfq_1", "RFQ-000001")
			},
			eventType: audit.EventTypeRFQReopened,
		},
		{
			name: "deleted",
			call: func(s *audit.Service) error {
				return s.RecordRFQDeleted(ctx, "company_a", "project_1", "user_1", "rfq_1", "RFQ-000001")
			},
			eventType: audit.EventTypeRFQDeleted,
		},
		{
			name: "claim reconciled",
			call: func(s *audit.Service) error {
				return s.RecordRFQClaimReconciled(ctx, "company_a", "project_1", "user_1", "rfq_1", "mr_1", "release")
			},
			eventType: audit.EventTypeRFQClaimReconciled,
			checkMeta: func(t *testing.T, m map[string]any) {
				if m["materialRequirementId"] != "mr_1" || m["action"] != "release" {
					t.Errorf("metadata = %+v", m)
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, repo := newService()
			if err := tc.call(svc); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			e := singleEvent(t, repo)
			if e.EventType != tc.eventType {
				t.Errorf("EventType = %q, want %q", e.EventType, tc.eventType)
			}
			if e.SubjectType != audit.SubjectTypeRFQ {
				t.Errorf("SubjectType = %q, want %q", e.SubjectType, audit.SubjectTypeRFQ)
			}
			// The subject is the stable RFQ CHAIN id, never the display number
			// (M7 design spec §6.1).
			if e.SubjectID != "rfq_1" {
				t.Errorf("SubjectID = %q, want the chain id rfq_1", e.SubjectID)
			}
			if e.ActorType != audit.ActorTypeContractor || e.ActorID != "user_1" {
				t.Errorf("actor = %q/%q", e.ActorType, e.ActorID)
			}
			if tc.checkMeta != nil {
				tc.checkMeta(t, e.Metadata)
			}
		})
	}
}

// --- suppliers interface: 8 methods ---

// Supplier records are COMPANY-scoped, not project-scoped, so ProjectID is
// deliberately empty on every supplier event (M7 design spec §4.1).
func TestRecordSupplierMethodsAreCompanyScopedNotProjectScoped(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name        string
		call        func(*audit.Service) error
		eventType   string
		subjectType string
		subjectID   string
		checkMeta   func(*testing.T, map[string]any)
	}{
		{
			name: "supplier created",
			call: func(s *audit.Service) error {
				return s.RecordSupplierCreated(ctx, "company_a", "user_1", "sup_1", "ABC Building Materials")
			},
			eventType: audit.EventTypeSupplierCreated, subjectType: audit.SubjectTypeSupplier, subjectID: "sup_1",
			checkMeta: func(t *testing.T, m map[string]any) {
				if m["supplierName"] != "ABC Building Materials" {
					t.Errorf("supplierName = %v", m["supplierName"])
				}
			},
		},
		{
			name: "supplier updated",
			call: func(s *audit.Service) error {
				return s.RecordSupplierUpdated(ctx, "company_a", "user_1", "sup_1")
			},
			eventType: audit.EventTypeSupplierUpdated, subjectType: audit.SubjectTypeSupplier, subjectID: "sup_1",
		},
		{
			name: "supplier retired",
			call: func(s *audit.Service) error {
				return s.RecordSupplierActiveStateChanged(ctx, "company_a", "user_1", "sup_1", false)
			},
			eventType: audit.EventTypeSupplierActiveStateChanged, subjectType: audit.SubjectTypeSupplier, subjectID: "sup_1",
			checkMeta: func(t *testing.T, m map[string]any) {
				if m["active"] != false {
					t.Errorf("active = %v, want false", m["active"])
				}
			},
		},
		{
			name: "offering created",
			call: func(s *audit.Service) error {
				return s.RecordSupplierOfferingCreated(ctx, "company_a", "user_1", "sup_1", "off_1")
			},
			eventType: audit.EventTypeSupplierOfferingCreated, subjectType: audit.SubjectTypeSupplierOffering, subjectID: "off_1",
			checkMeta: func(t *testing.T, m map[string]any) {
				if m["supplierId"] != "sup_1" {
					t.Errorf("supplierId = %v", m["supplierId"])
				}
			},
		},
		{
			name: "offering updated",
			call: func(s *audit.Service) error {
				return s.RecordSupplierOfferingUpdated(ctx, "company_a", "user_1", "sup_1", "off_1")
			},
			eventType: audit.EventTypeSupplierOfferingUpdated, subjectType: audit.SubjectTypeSupplierOffering, subjectID: "off_1",
		},
		{
			name: "offering retired",
			call: func(s *audit.Service) error {
				return s.RecordSupplierOfferingActiveStateChanged(ctx, "company_a", "user_1", "sup_1", "off_1", false)
			},
			eventType:   audit.EventTypeSupplierOfferingActiveStateChanged,
			subjectType: audit.SubjectTypeSupplierOffering, subjectID: "off_1",
			checkMeta: func(t *testing.T, m map[string]any) {
				if m["active"] != false {
					t.Errorf("active = %v, want false", m["active"])
				}
			},
		},
		{
			name: "preference set",
			call: func(s *audit.Service) error {
				return s.RecordPreferredSupplierChanged(ctx, "company_a", "user_1", "material_1", "sup_1")
			},
			eventType:   audit.EventTypePreferredSupplierChanged,
			subjectType: audit.SubjectTypeMaterialSupplierPreference, subjectID: "material_1",
			checkMeta: func(t *testing.T, m map[string]any) {
				if m["materialId"] != "material_1" || m["supplierId"] != "sup_1" {
					t.Errorf("metadata = %+v", m)
				}
			},
		},
		{
			name: "preference cleared",
			call: func(s *audit.Service) error {
				return s.RecordPreferredSupplierCleared(ctx, "company_a", "user_1", "material_1", "sup_1")
			},
			eventType:   audit.EventTypePreferredSupplierCleared,
			subjectType: audit.SubjectTypeMaterialSupplierPreference, subjectID: "material_1",
			checkMeta: func(t *testing.T, m map[string]any) {
				if m["previousSupplierId"] != "sup_1" {
					t.Errorf("previousSupplierId = %v", m["previousSupplierId"])
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, repo := newService()
			if err := tc.call(svc); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			e := singleEvent(t, repo)
			if e.EventType != tc.eventType {
				t.Errorf("EventType = %q, want %q", e.EventType, tc.eventType)
			}
			if e.SubjectType != tc.subjectType {
				t.Errorf("SubjectType = %q, want %q", e.SubjectType, tc.subjectType)
			}
			if e.SubjectID != tc.subjectID {
				t.Errorf("SubjectID = %q, want %q", e.SubjectID, tc.subjectID)
			}
			if e.CompanyID != "company_a" {
				t.Errorf("CompanyID = %q", e.CompanyID)
			}
			if e.ProjectID != "" {
				t.Errorf("ProjectID = %q, want empty: suppliers are company-scoped", e.ProjectID)
			}
			if e.ActorType != audit.ActorTypeContractor || e.ActorID != "user_1" {
				t.Errorf("actor = %q/%q", e.ActorType, e.ActorID)
			}
			if tc.checkMeta != nil {
				tc.checkMeta(t, e.Metadata)
			}
		})
	}
}

// --- The primitive-only contract (M7 design spec §1.5) ---

// Every M7 audit method must take ONLY primitives. A Money value, a
// quantity.Quantity, a decimal, or any domain struct in a signature would let
// an internal cost figure or a whole aggregate reach an audit record. The
// signature IS the allowlist, so this asserts it by reflection rather than
// trusting review.
func TestEveryM7AuditMethodTakesOnlyPrimitives(t *testing.T) {
	svc, _ := newService()
	svcType := reflect.TypeOf(svc)

	m7Methods := []string{
		"RecordMaterialRequirementsGenerated", "RecordMaterialRequirementCreated",
		"RecordMaterialRequirementUpdated", "RecordMaterialRequirementReviewed",
		"RecordMaterialRequirementUnitAcknowledged", "RecordMaterialRequirementDiscrepancyResolved",
		"RecordMaterialRequirementSplit", "RecordMaterialRequirementArchived",
		"RecordRFQCreated", "RecordRFQUpdated", "RecordRFQLineAdded", "RecordRFQLineRemoved",
		"RecordRFQMarkedReady", "RecordRFQReopened", "RecordRFQDeleted", "RecordRFQClaimReconciled",
		"RecordSupplierCreated", "RecordSupplierUpdated", "RecordSupplierActiveStateChanged",
		"RecordSupplierOfferingCreated", "RecordSupplierOfferingUpdated",
		"RecordSupplierOfferingActiveStateChanged",
		"RecordPreferredSupplierChanged", "RecordPreferredSupplierCleared",
	}
	if len(m7Methods) != 24 {
		t.Fatalf("expected 24 M7 methods, listed %d", len(m7Methods))
	}

	allowed := map[reflect.Kind]bool{
		reflect.String: true, reflect.Int: true, reflect.Int64: true, reflect.Bool: true,
	}

	for _, name := range m7Methods {
		m, ok := svcType.MethodByName(name)
		if !ok {
			t.Errorf("audit.Service is missing %s", name)
			continue
		}
		ft := m.Func.Type()
		// Param 0 is the receiver, param 1 is context.Context.
		for i := 2; i < ft.NumIn(); i++ {
			in := ft.In(i)
			if !allowed[in.Kind()] {
				t.Errorf("%s parameter %d is %s (kind %s): only string/int/int64/bool are permitted, "+
					"so no Money, Quantity, decimal or domain struct can reach an audit record",
					name, i-1, in, in.Kind())
			}
		}
		if ft.NumOut() != 1 || ft.Out(0).String() != "error" {
			t.Errorf("%s must return exactly one error", name)
		}
	}
}

// No M7 audit event may serialize anything resembling a monetary amount. The
// indicative price on a SupplierOffering is informational and must never be
// recorded as though it were a formal figure (M7 design spec §4.2).
func TestM7AuditEventsCarryNoMonetaryFields(t *testing.T) {
	svc, repo := newService()
	ctx := context.Background()

	if err := svc.RecordSupplierOfferingCreated(ctx, "company_a", "user_1", "sup_1", "off_1"); err != nil {
		t.Fatal(err)
	}
	if err := svc.RecordSupplierOfferingUpdated(ctx, "company_a", "user_1", "sup_1", "off_1"); err != nil {
		t.Fatal(err)
	}

	serialized, err := json.Marshal(repo.events)
	if err != nil {
		t.Fatal(err)
	}
	lowered := strings.ToLower(string(serialized))
	for _, banned := range []string{"price", "amount", "currency", "cost", "margin", "myr"} {
		if strings.Contains(lowered, banned) {
			t.Errorf("a supplier-offering audit event contains %q: %s", banned, serialized)
		}
	}
}

// Every M7 method stamps CreatedAt and SchemaVersion through the shared record
// helper, so no method can persist an unversioned or undated event.
func TestM7AuditEventsAreStampedAndVersioned(t *testing.T) {
	svc, repo := newService()
	ctx := context.Background()

	if err := svc.RecordRFQCreated(ctx, "company_a", "project_1", "user_1", "rfq_1", "RFQ-000001"); err != nil {
		t.Fatal(err)
	}
	e := singleEvent(t, repo)
	if e.CreatedAt.IsZero() {
		t.Error("CreatedAt must be stamped")
	}
	if e.SchemaVersion != 1 {
		t.Errorf("SchemaVersion = %d, want 1", e.SchemaVersion)
	}
}

// A structurally-triggered M7 operation with no human actor records a system
// actor, matching the M6 convention rather than inventing a second one.
func TestM7EmptyActorRecordsSystemActor(t *testing.T) {
	svc, repo := newService()

	if err := svc.RecordMaterialRequirementsGenerated(context.Background(),
		"company_a", "project_1", "", 1, 0, 0, 0, 0); err != nil {
		t.Fatal(err)
	}
	e := singleEvent(t, repo)
	if e.ActorType != audit.ActorTypeSystem || e.ActorID != "" {
		t.Errorf("actor = %q/%q, want system/empty", e.ActorType, e.ActorID)
	}
}

// singleEvent asserts exactly one event was recorded and returns it.
func singleEvent(t *testing.T, repo *fakeEventRepository) audit.Event {
	t.Helper()
	if len(repo.events) != 1 {
		t.Fatalf("expected exactly 1 event, got %d", len(repo.events))
	}
	return repo.events[0]
}
