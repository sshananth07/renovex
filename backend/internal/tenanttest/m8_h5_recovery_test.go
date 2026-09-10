package tenanttest_test

import (
	"net/http"
	"testing"

	"github.com/shananth/renovation-platform/backend/internal/identity"
	"github.com/shananth/renovation-platform/backend/internal/supplieraccess"
)

// Phase H5 — public recovery acceptance (spec §21.3, §21.6).
//
// Recovery happens ONLY through the existing owner/admin or same-operation
// entry points: no generic reconciler and no cross-module repository access.
// Every case asserts the persisted end state, and no case uses a sleep.
//
// The shared invariant: recovery completes forward or is idempotent, and never
// invents content, re-numbers a version, or releases a terminal claim.

// Chain reconciliation is idempotent and never rewinds or invents a version.
func TestM8PhaseH5_RFQChainReconciliationIsIdempotent(t *testing.T) {
	router, _, _ := setupRouterWithMail(t)
	owner := registerCompany(t, router, "h5a-owner@example.com", "H5A Contractor")
	employee := tokenWithRole(t, owner.accessToken, identity.RoleEmployee)

	rfqChainID, versionID := issueRFQForAwards(t, router, owner, "h5a")

	// Reconciliation moves authoritative state, so it is owner/admin only.
	if refused := doJSON(t, router, http.MethodPost,
		"/rfq-chains/"+rfqChainID+"/reconcile", employee,
		nil); refused.Code != http.StatusForbidden {
		t.Fatalf("employee reconcile = %d, want 403: %s",
			refused.Code, refused.Body.String())
	}

	// A complete chain reconciles to itself, repeatedly.
	for attempt := 1; attempt <= 2; attempt++ {
		reconciled := doJSON(t, router, http.MethodPost,
			"/rfq-chains/"+rfqChainID+"/reconcile", owner.accessToken, nil)
		if reconciled.Code != http.StatusOK {
			t.Fatalf("reconcile attempt %d = %d: %s",
				attempt, reconciled.Code, reconciled.Body.String())
		}
	}

	// The chain still points at the SAME immutable version, and no second
	// version was synthesized.
	history := doJSON(t, router, http.MethodGet,
		"/rfq-chains/"+rfqChainID+"/versions", owner.accessToken, nil)
	versions, _ := decodeObject(t, history.Body.Bytes())["versions"].([]any)
	if len(versions) != 1 {
		t.Fatalf("issued versions = %d after reconciliation, want 1", len(versions))
	}
	if got := versions[0].(map[string]any)["id"]; got != versionID {
		t.Errorf("chain points at %v after reconciliation, want %q", got, versionID)
	}
}

// Invitation reconciliation advances active invitations and leaves a revoked
// invitation untouched, creating no new invitation or delivery.
func TestM8PhaseH5_InvitationReconciliationSkipsRevokedInvitations(t *testing.T) {
	router, db, _ := setupRouterWithMail(t)
	challenges := supplieraccess.NewMongoVerificationChallengeRepository(db)
	owner := registerCompany(t, router, "h5b-owner@example.com", "H5B Contractor")

	rfqChainID, _ := issueRFQForAwards(t, router, owner, "h5b")
	active := inviteAndVerifySupplier(t, router, challenges, owner,
		rfqChainID, "H5B Active", "active@h5b.example", "h5b-active")
	revoked := inviteAndVerifySupplier(t, router, challenges, owner,
		rfqChainID, "H5B Revoked", "revoked@h5b.example", "h5b-revoked")

	revokedRead := doJSON(t, router, http.MethodGet,
		"/rfq-chains/"+rfqChainID+"/invitations/"+revoked.invitationID,
		owner.accessToken, nil)
	revocation := doJSON(t, router, http.MethodPost,
		"/rfq-chains/"+rfqChainID+"/invitations/"+revoked.invitationID+"/revoke",
		owner.accessToken, map[string]any{
			"expectedRevision": mustNumberField(t, revokedRead, "revision"),
		})
	if revocation.Code != http.StatusOK {
		t.Fatalf("revoke invitation: %d: %s",
			revocation.Code, revocation.Body.String())
	}

	versionTwoID := issueAmendedVersion(t, router, owner, rfqChainID, "h5b")

	// Reconciliation is safe to repeat.
	for attempt := 1; attempt <= 2; attempt++ {
		reconciled := doJSON(t, router, http.MethodPost,
			"/rfq-chains/"+rfqChainID+"/invitations/reconcile",
			owner.accessToken, nil)
		if reconciled.Code != http.StatusOK {
			t.Fatalf("invitation reconcile %d = %d: %s",
				attempt, reconciled.Code, reconciled.Body.String())
		}
	}

	// The active invitation points at Version 2; the revoked one never advanced.
	activeAfter := doJSON(t, router, http.MethodGet,
		"/rfq-chains/"+rfqChainID+"/invitations/"+active.invitationID,
		owner.accessToken, nil)
	if got := mustField(t, activeAfter, "currentIssuedRfqVersionId"); got != versionTwoID {
		t.Errorf("active invitation points at %q, want Version 2 %q",
			got, versionTwoID)
	}
	revokedAfter := doJSON(t, router, http.MethodGet,
		"/rfq-chains/"+rfqChainID+"/invitations/"+revoked.invitationID,
		owner.accessToken, nil)
	if got := mustField(t, revokedAfter, "currentIssuedRfqVersionId"); got == versionTwoID {
		t.Errorf("a revoked invitation was advanced to Version 2")
	}
	if status := mustField(t, revokedAfter, "status"); status != "revoked" {
		t.Errorf("revoked invitation status = %q after reconciliation", status)
	}

	// The revoked Supplier's session stays closed after reconciliation.
	if stale := revoked.browser.do(t, http.MethodGet,
		"/supplier-access/invitations/"+revoked.invitationID,
		nil); stale.Code != http.StatusNotFound {
		t.Errorf("revoked Supplier reached its invitation = %d, want 404: %s",
			stale.Code, stale.Body.String())
	}
}

// The Supplier Offer reconciliation locator resolves by tuple, takes Company
// only from the principal, and returns the settled status codes.
func TestM8PhaseH5_SupplierOfferReconciliationLocator(t *testing.T) {
	router, db, _ := setupRouterWithMail(t)
	challenges := supplieraccess.NewMongoVerificationChallengeRepository(db)
	owner := registerCompany(t, router, "h5c-owner@example.com", "H5C Contractor")
	foreign := registerCompany(t, router, "h5c-foreign@example.com", "H5C Foreign")
	employee := tokenWithRole(t, owner.accessToken, identity.RoleEmployee)

	rfqChainID, versionID := issueRFQForAwards(t, router, owner, "h5c")
	supplier := inviteAndVerifySupplier(t, router, challenges, owner,
		rfqChainID, "H5C Supplier", "sales@h5c.example", "h5c")
	submitOffer(t, supplier, 15000, "h5c")

	// The exact operation ID the submission used: recovery targets one named
	// operation, never "whatever is outstanding".
	body := map[string]any{
		"invitationId":       supplier.invitationID,
		"issuedRfqVersionId": versionID,
		"operationId":        "h1-submit-h5c",
	}

	// Owner/admin only.
	if refused := doJSON(t, router, http.MethodPost,
		"/supplier-offer-reconciliations", employee,
		body); refused.Code != http.StatusForbidden {
		t.Fatalf("employee reconciliation = %d, want 403: %s",
			refused.Code, refused.Body.String())
	}

	// The completed operation resolves and is safe to repeat.
	for attempt := 1; attempt <= 2; attempt++ {
		reconciled := doJSON(t, router, http.MethodPost,
			"/supplier-offer-reconciliations", owner.accessToken, body)
		if reconciled.Code != http.StatusOK {
			t.Fatalf("reconciliation attempt %d = %d: %s",
				attempt, reconciled.Code, reconciled.Body.String())
		}
	}

	// A foreign principal must not resolve another tenant's tuple, even naming
	// it exactly: Company comes only from the authenticated principal.
	if foreignAttempt := doJSON(t, router, http.MethodPost,
		"/supplier-offer-reconciliations", foreign.accessToken,
		body); foreignAttempt.Code != http.StatusNotFound {
		t.Errorf("foreign reconciliation = %d, want 404: %s",
			foreignAttempt.Code, foreignAttempt.Body.String())
	}

	// A tuple that names no offer is a plain not-found.
	missing := doJSON(t, router, http.MethodPost,
		"/supplier-offer-reconciliations", owner.accessToken, map[string]any{
			"invitationId":       supplier.invitationID,
			"issuedRfqVersionId": rfqChainID, // not an issued version
			"operationId":        "h5c-missing",
		})
	if missing.Code != http.StatusNotFound {
		t.Errorf("missing tuple = %d, want 404: %s",
			missing.Code, missing.Body.String())
	}

	// Exactly one immutable version survives every reconciliation.
	history := supplier.browser.do(t, http.MethodGet,
		"/supplier-offers/"+supplier.invitationID+"/versions", nil)
	versions, _ := decodeObject(t, history.Body.Bytes())["versions"].([]any)
	if len(versions) != 1 {
		t.Fatalf("offer versions = %d after reconciliation, want 1", len(versions))
	}
}

// Award reconciliation completes forward or is idempotent, and never publishes
// a second authoritative revision or releases a terminal claim.
func TestM8PhaseH5_AwardReconciliationIsIdempotentAndTerminal(t *testing.T) {
	router, db, _ := setupRouterWithMail(t)
	challenges := supplieraccess.NewMongoVerificationChallengeRepository(db)
	owner := registerCompany(t, router, "h5d-owner@example.com", "H5D Contractor")

	rfqChainID, versionID := issueRFQForAwards(t, router, owner, "h5d")
	supplier := inviteAndVerifySupplier(t, router, challenges, owner,
		rfqChainID, "H5D Supplier", "sales@h5d.example", "h5d")
	offerVersionID, _ := submitOffer(t, supplier, 15000, "h5d")

	base := awardBase(rfqChainID, versionID)
	comparison := doJSON(t, router, http.MethodGet, base+"/comparison",
		owner.accessToken, nil)
	offer := comparisonOfferFor(t, comparison, offerVersionID)
	revisionID := publishAward(t, router, owner, base, offerVersionID,
		lineSlice(offer["lines"].([]any)), nil, "h5d")

	// Reconciling a COMPLETE award is a no-op, repeatedly.
	for attempt := 1; attempt <= 2; attempt++ {
		reconciled := doJSON(t, router, http.MethodPost,
			base+"/award-reconciliation", owner.accessToken,
			map[string]any{"operationId": "h5d-reconcile"})
		if reconciled.Code != http.StatusOK {
			t.Fatalf("award reconciliation %d = %d: %s",
				attempt, reconciled.Code, reconciled.Body.String())
		}
		result := decodeObject(t, reconciled.Body.Bytes())
		if released, _ := result["releasedClaims"].(bool); released {
			t.Errorf("reconciliation released claims on a published award")
		}
	}

	// Still exactly one authoritative revision, with its identity unchanged.
	list := doJSON(t, router, http.MethodGet, base+"/award-revisions",
		owner.accessToken, nil)
	revisions := revisionsOf(t, list)
	if len(revisions) != 1 {
		t.Fatalf("award revisions = %d after reconciliation, want 1", len(revisions))
	}
	if got := revisions[0].(map[string]any)["id"]; got != revisionID {
		t.Errorf("reconciliation changed the revision identity to %v", got)
	}

	// The awarded lineage is terminal: re-awarding it on the same RFQ chain is
	// refused rather than silently re-claimed.
	secondDraft := doJSON(t, router, http.MethodPost, base+"/award-draft",
		owner.accessToken, nil)
	if secondDraft.Code != http.StatusCreated {
		t.Fatalf("reopen award draft: %d: %s",
			secondDraft.Code, secondDraft.Body.String())
	}
	line := offer["lines"].([]any)[0].(map[string]any)
	reselected := doJSON(t, router, http.MethodPut,
		base+"/award-draft/lines/"+line["issuedRfqLineId"].(string)+"/selection",
		owner.accessToken, map[string]any{
			"offerVersionId":   offerVersionID,
			"offerLineId":      line["offerLineId"].(string),
			"expectedRevision": mustNumberField(t, secondDraft, "revision"),
		})
	if reselected.Code == http.StatusOK {
		refinalised := doJSON(t, router, http.MethodPost, base+"/award-revisions",
			owner.accessToken, map[string]any{
				"operationId":  "h5d-refinalise",
				"changeReason": "attempt to re-award a terminal lineage",
			})
		if refinalised.Code == http.StatusCreated {
			t.Fatalf("an awarded lineage was awarded a second time: %s",
				refinalised.Body.String())
		}
	}
}
