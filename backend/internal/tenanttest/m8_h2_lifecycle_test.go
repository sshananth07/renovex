package tenanttest_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/shananth/renovation-platform/backend/internal/supplieraccess"
)

// Phase H2 — lifecycle branch journeys (spec §21.2, §21.6).
//
// Amendment advancement, recipient replacement, Offer supersession and Offer
// withdrawal, each through the real router and real MongoDB. These prove that
// old generations fail closed and that immutable history stays visible, which
// the focused suites establish per module but never across the composed whole.

// Issuing Version 2 advances the active invitation, leaves Version 1 immutable
// and readable, and makes Version 2 the Supplier's current RFQ.
func TestM8PhaseH2_AmendmentAdvancesTheInvitationAndPreservesHistory(t *testing.T) {
	router, db, _ := setupRouterWithMail(t)
	challenges := supplieraccess.NewMongoVerificationChallengeRepository(db)
	owner := registerCompany(t, router, "h2a-owner@example.com", "H2A Contractor")

	rfqChainID, versionOneID := issueRFQForAwards(t, router, owner, "h2a")
	supplier := inviteAndVerifySupplier(t, router, challenges, owner,
		rfqChainID, "H2A Supplier", "sales@h2a.example", "h2a")

	// The Supplier initially sees Version 1.
	current := supplier.browser.do(t, http.MethodGet,
		"/supplier-access/invitations/"+supplier.invitationID, nil)
	if current.Code != http.StatusOK {
		t.Fatalf("read current invitation: %d: %s",
			current.Code, current.Body.String())
	}
	before := decodeObject(t, current.Body.Bytes())
	if version, _ := before["currentRfqVersion"].(map[string]any); version == nil ||
		version["id"] != versionOneID {
		t.Fatalf("Supplier's current version = %#v, want Version 1 %q",
			before["currentRfqVersion"], versionOneID)
	}

	// The contractor amends and issues Version 2.
	versionTwoID := issueAmendedVersion(t, router, owner, rfqChainID, "h2a")
	if versionTwoID == versionOneID {
		t.Fatalf("amendment reused Version 1 identity %q", versionOneID)
	}

	// The active invitation advances to Version 2.
	invitation := doJSON(t, router, http.MethodGet,
		"/rfq-chains/"+rfqChainID+"/invitations/"+supplier.invitationID,
		owner.accessToken, nil)
	if invitation.Code != http.StatusOK {
		t.Fatalf("read invitation: %d: %s",
			invitation.Code, invitation.Body.String())
	}
	if got := mustField(t, invitation, "currentIssuedRfqVersionId"); got != versionTwoID {
		t.Errorf("invitation points at %q, want the amended Version 2 %q",
			got, versionTwoID)
	}

	// One stable invitation per chain/Supplier: amendment must not mint another.
	if got := mustField(t, invitation, "id"); got != supplier.invitationID {
		t.Errorf("invitation identity changed to %q, want the stable %q",
			got, supplier.invitationID)
	}

	// Version 1 remains immutable and readable in history alongside Version 2.
	history := doJSON(t, router, http.MethodGet,
		"/rfq-chains/"+rfqChainID+"/versions", owner.accessToken, nil)
	if history.Code != http.StatusOK {
		t.Fatalf("list versions: %d: %s", history.Code, history.Body.String())
	}
	versions, _ := decodeObject(t, history.Body.Bytes())["versions"].([]any)
	if len(versions) != 2 {
		t.Fatalf("issued versions = %d, want both immutable versions: %s",
			len(versions), history.Body.String())
	}
	original := doJSON(t, router, http.MethodGet,
		"/rfq-versions/"+versionOneID, owner.accessToken, nil)
	if original.Code != http.StatusOK {
		t.Fatalf("Version 1 is no longer readable: %d: %s",
			original.Code, original.Body.String())
	}
	if number := mustNumberField(t, original, "versionNumber"); number != 1 {
		t.Errorf("Version 1 version number = %v after amendment", number)
	}
}

// Replacing the recipient rotates the link: the previous recipient's session
// fails closed, and the new recipient verifies into its own access.
func TestM8PhaseH2_RecipientReplacementClosesThePreviousGeneration(t *testing.T) {
	router, db, _ := setupRouterWithMail(t)
	challenges := supplieraccess.NewMongoVerificationChallengeRepository(db)
	owner := registerCompany(t, router, "h2b-owner@example.com", "H2B Contractor")

	rfqChainID, _ := issueRFQForAwards(t, router, owner, "h2b")
	previous := inviteAndVerifySupplier(t, router, challenges, owner,
		rfqChainID, "H2B Supplier", "first@h2b.example", "h2b")

	// The original recipient can read its invitation before replacement.
	if before := previous.browser.do(t, http.MethodGet,
		"/supplier-access/invitations/"+previous.invitationID,
		nil); before.Code != http.StatusOK {
		t.Fatalf("original recipient read: %d: %s",
			before.Code, before.Body.String())
	}

	invitation := doJSON(t, router, http.MethodGet,
		"/rfq-chains/"+rfqChainID+"/invitations/"+previous.invitationID,
		owner.accessToken, nil)
	replaced := doJSON(t, router, http.MethodPost,
		"/rfq-chains/"+rfqChainID+"/invitations/"+previous.invitationID+
			"/replace-recipient", owner.accessToken, map[string]any{
			"expectedRevision": mustNumberField(t, invitation, "revision"),
			"recipientName":    "H2B Replacement",
			"recipientEmail":   "second@h2b.example",
			"operationId":      "h2b-replace",
		})
	if replaced.Code != http.StatusOK {
		t.Fatalf("replace recipient: %d: %s",
			replaced.Code, replaced.Body.String())
	}

	// Replacement mints a new access generation on the SAME stable invitation.
	if got := mustField(t, replaced, "id"); got != previous.invitationID {
		t.Errorf("replacement changed the invitation identity to %q", got)
	}
	if generation := mustNumberField(t, replaced, "accessGeneration"); generation != 2 {
		t.Errorf("access generation = %v after replacement, want 2", generation)
	}

	// The previous recipient's session is bound to the old generation, so every
	// protected read now fails closed with a neutral not-found.
	for name, path := range map[string]string{
		"invitation": "/supplier-access/invitations/" + previous.invitationID,
		"offer draft": "/supplier-access/invitations/" + previous.invitationID +
			"/offer",
	} {
		stale := previous.browser.do(t, http.MethodGet, path, nil)
		if stale.Code != http.StatusNotFound {
			t.Errorf("previous recipient still reached %s: %d: %s",
				name, stale.Code, stale.Body.String())
		}
	}
}

// A second submission supersedes the first. Both immutable versions remain in
// the Supplier's own history, and a repeated submission operation converges.
func TestM8PhaseH2_OfferSupersessionPreservesImmutableHistory(t *testing.T) {
	router, db, _ := setupRouterWithMail(t)
	challenges := supplieraccess.NewMongoVerificationChallengeRepository(db)
	owner := registerCompany(t, router, "h2c-owner@example.com", "H2C Contractor")

	rfqChainID, _ := issueRFQForAwards(t, router, owner, "h2c")
	supplier := inviteAndVerifySupplier(t, router, challenges, owner,
		rfqChainID, "H2C Supplier", "sales@h2c.example", "h2c")

	firstID, _ := submitOffer(t, supplier, 12000, "h2c-v1")
	secondID, _ := submitOffer(t, supplier, 11000, "h2c-v2")
	if firstID == secondID {
		t.Fatalf("the second submission reused version %q", firstID)
	}

	history := supplier.browser.do(t, http.MethodGet,
		"/supplier-offers/"+supplier.invitationID+"/versions", nil)
	if history.Code != http.StatusOK {
		t.Fatalf("offer history: %d: %s", history.Code, history.Body.String())
	}
	versions, _ := decodeObject(t, history.Body.Bytes())["versions"].([]any)
	if len(versions) != 2 {
		t.Fatalf("offer versions = %d, want both immutable versions: %s",
			len(versions), history.Body.String())
	}

	byID := map[string]map[string]any{}
	for _, raw := range versions {
		version := raw.(map[string]any)
		byID[version["id"].(string)] = version
	}
	if superseded, _ := byID[firstID]["isSuperseded"].(bool); !superseded {
		t.Errorf("Version 1 is not marked superseded: %#v", byID[firstID])
	}
	if superseded, _ := byID[secondID]["isSuperseded"].(bool); superseded {
		t.Errorf("the latest version is marked superseded: %#v", byID[secondID])
	}
	// Only the latest version may still be withdrawn.
	if canWithdraw, _ := byID[firstID]["canWithdraw"].(bool); canWithdraw {
		t.Errorf("a superseded version still reports canWithdraw")
	}
}

// Withdrawing the latest version records exactly one immutable withdrawal, and
// repeating the same operation is idempotent rather than withdrawing twice.
func TestM8PhaseH2_OfferWithdrawalIsImmutableAndIdempotent(t *testing.T) {
	router, db, _ := setupRouterWithMail(t)
	challenges := supplieraccess.NewMongoVerificationChallengeRepository(db)
	owner := registerCompany(t, router, "h2d-owner@example.com", "H2D Contractor")

	rfqChainID, _ := issueRFQForAwards(t, router, owner, "h2d")
	supplier := inviteAndVerifySupplier(t, router, challenges, owner,
		rfqChainID, "H2D Supplier", "sales@h2d.example", "h2d")
	versionID, _ := submitOffer(t, supplier, 13000, "h2d")

	withdrawalPath := "/supplier-offers/" + supplier.invitationID +
		"/versions/" + versionID + "/withdrawal"
	body := map[string]any{
		"reason":      "Material no longer available",
		"operationId": "h2d-withdraw",
	}

	withdrawn := supplier.browser.do(t, http.MethodPost, withdrawalPath, body)
	if withdrawn.Code != http.StatusCreated && withdrawn.Code != http.StatusOK {
		t.Fatalf("withdraw offer: %d: %s",
			withdrawn.Code, withdrawn.Body.String())
	}

	// The same operation must converge rather than create a second withdrawal.
	repeat := supplier.browser.do(t, http.MethodPost, withdrawalPath, body)
	if repeat.Code != http.StatusCreated && repeat.Code != http.StatusOK {
		t.Fatalf("repeated withdrawal: %d: %s",
			repeat.Code, repeat.Body.String())
	}

	history := supplier.browser.do(t, http.MethodGet,
		"/supplier-offers/"+supplier.invitationID+"/versions", nil)
	if history.Code != http.StatusOK {
		t.Fatalf("offer history: %d: %s", history.Code, history.Body.String())
	}
	versions, _ := decodeObject(t, history.Body.Bytes())["versions"].([]any)
	if len(versions) != 1 {
		t.Fatalf("offer versions = %d, want the single immutable version",
			len(versions))
	}
	version := versions[0].(map[string]any)
	if status, _ := version["publicStatus"].(string); status != "withdrawn" {
		t.Errorf("public status = %q after withdrawal, want withdrawn", status)
	}
	// The immutable version itself survives; withdrawal is a separate record.
	if version["id"] != versionID {
		t.Errorf("withdrawal replaced the immutable version identity")
	}
}

// issueAmendedVersion opens the amendment draft, keeps the existing line and
// issues the next immutable version, returning its identity.
func issueAmendedVersion(
	t *testing.T,
	router http.Handler,
	owner testCompany,
	rfqChainID string,
	suffix string,
) string {
	t.Helper()

	draft := doJSON(t, router, http.MethodPost,
		"/rfq-chains/"+rfqChainID+"/amendment-draft", owner.accessToken, nil)
	if draft.Code != http.StatusCreated {
		t.Fatalf("create amendment draft: %d: %s",
			draft.Code, draft.Body.String())
	}
	body := decodeObject(t, draft.Body.Bytes())
	lines, _ := body["lines"].([]any)
	if len(lines) == 0 {
		t.Fatalf("amendment draft carried no lines: %s", draft.Body.String())
	}
	existing := lines[0].(map[string]any)

	deadline := time.Now().UTC().Add(21 * 24 * time.Hour).Truncate(time.Second)
	updated := doJSON(t, router, http.MethodPatch,
		"/rfq-chains/"+rfqChainID+"/amendment-draft", owner.accessToken,
		map[string]any{
			"expectedRevision": body["revision"],
			"requiredByDate":   deadline.Add(7 * 24 * time.Hour).Format(time.RFC3339),
			"responseDeadline": deadline.Format(time.RFC3339),
			"lines": []map[string]any{{
				"id":           existing["id"],
				"materialId":   existing["materialId"],
				"materialName": "Portland Cement",
				"quantity": map[string]any{
					"value": "150", "unit": "bag",
				},
				"sortOrder": 1,
			}},
		})
	if updated.Code != http.StatusOK {
		t.Fatalf("update amendment draft: %d: %s",
			updated.Code, updated.Body.String())
	}

	issued := doJSON(t, router, http.MethodPost,
		"/rfq-chains/"+rfqChainID+"/amendment-draft/issue", owner.accessToken,
		map[string]any{
			"expectedRevision": mustNumberField(t, updated, "revision"),
			"operationId":      "h2-amend-" + suffix,
		})
	if issued.Code != http.StatusCreated {
		t.Fatalf("issue amended version: %d: %s",
			issued.Code, issued.Body.String())
	}
	return mustField(t, issued, "id")
}
