package mongo

import (
	"context"
	"sort"

	"go.mongodb.org/mongo-driver/v2/mongo"
)

// CollectionManifest is the exact, checked-in list of every collection this
// application's repositories own — every domain module's collection name
// constant, transcribed once here so a clean Atlas bootstrap is
// reproducible as a separate deployment step (M8.5C plan). schema_migrations
// itself is part of the manifest (dbbootstrap creates and writes to it);
// _readiness_probe is deliberately NOT included — it is a transient probe
// collection (see VerifyTransactionSupport), never a durable part of the
// schema.
//
// A drift test (schema_manifest_test.go) compares this list against every
// repository's actual db.Collection(...) call across the module tree, so a
// future repository cannot be added without this manifest being updated —
// the same "manifest as the checked, not just documented, contract"
// discipline this package already applies to the readiness probe.
var CollectionManifest = []string{
	"users",
	"auth_sessions",
	"companies",
	"company_members",
	"clients",
	"projects",
	"properties",
	"spaces",
	"work_items",
	"materials",
	"cost_items",
	"workers",
	"labour_entries",
	"estimates",
	"quotations",
	"quotation_counters",
	"access_grants",
	"access_group_states",
	"approvals",
	"audit_events",
	"material_requirements",
	"rfqs",
	"rfq_counters",
	"suppliers",
	"supplier_offerings",
	"material_supplier_preferences",
	"issued_rfq_versions",
	"rfq_issuance_chains",
	"rfq_amendment_drafts",
	"supplier_invitations",
	"invitation_delivery_attempts",
	"supplier_access_exchanges",
	"email_verification_challenges",
	"verification_delivery_attempts",
	"supplier_verification_rate_limits",
	"supplier_sessions",
	"supplier_session_invitation_bindings",
	"supplier_offer_chains",
	"supplier_offer_drafts",
	"supplier_offer_versions",
	"supplier_offer_eligibilities",
	"supplier_offer_withdrawals",
	"award_decision_chains",
	"award_drafts",
	"award_revisions",
	"award_line_claims",
	"award_outcomes",
	"award_outcome_deliveries",
	"award_outcome_acknowledgements",
	"work_resource_requirements",
	"ai_generation_batches",
	"ai_suggestions",
	"spatial_captures",
	"spatial_room_versions",
	"spatial_space_states",
	"spatial_artifacts",
	"spatial_room_drafts",
	"spatial_room_draft_edits",
	"spatial_visual_asset_versions",
	"spatial_asset_generation_jobs",
	"spatial_design_sessions",
	"spatial_design_turns",
	"spatial_design_generation_attempts",
	"spatial_design_acceptances",
	"schema_migrations",
}

// SortedCollectionManifest returns CollectionManifest sorted — the
// canonical comparison shape dbbootstrap and its tests use.
func SortedCollectionManifest() []string {
	sorted := make([]string, len(CollectionManifest))
	copy(sorted, CollectionManifest)
	sort.Strings(sorted)
	return sorted
}

// ListCollectionNames returns every collection name that currently exists
// in db, sorted. Used by dbbootstrap to compare the manifest against
// reality (missing required collections get created; unexpected
// application collections are reported rather than silently ignored,
// M8.5C plan: "unexpected application collections are reported and
// require an explicit allow flag").
func ListCollectionNames(ctx context.Context, db *mongo.Database) ([]string, error) {
	names, err := db.ListCollectionNames(ctx, map[string]any{})
	if err != nil {
		return nil, err
	}
	sort.Strings(names)
	return names, nil
}

// isSystemCollection reports whether name is a MongoDB/Atlas-internal
// collection that the manifest comparison must ignore — the plan's "extra
// system collections are ignored" requirement.
func isSystemCollection(name string) bool {
	if len(name) >= 7 && name[:7] == "system." {
		return true
	}
	return name == transactionProbeCollection
}

// UnexpectedCollections returns every collection present in existing that
// is neither in the manifest nor a recognized system collection — the set
// dbbootstrap reports and requires an explicit allow flag to proceed past.
func UnexpectedCollections(existing []string) []string {
	manifestSet := make(map[string]bool, len(CollectionManifest))
	for _, name := range CollectionManifest {
		manifestSet[name] = true
	}
	var unexpected []string
	for _, name := range existing {
		if manifestSet[name] || isSystemCollection(name) {
			continue
		}
		unexpected = append(unexpected, name)
	}
	sort.Strings(unexpected)
	return unexpected
}

// MissingCollections returns every manifest collection absent from
// existing — the set dbbootstrap must create.
func MissingCollections(existing []string) []string {
	existingSet := make(map[string]bool, len(existing))
	for _, name := range existing {
		existingSet[name] = true
	}
	var missing []string
	for _, name := range CollectionManifest {
		if !existingSet[name] {
			missing = append(missing, name)
		}
	}
	sort.Strings(missing)
	return missing
}
