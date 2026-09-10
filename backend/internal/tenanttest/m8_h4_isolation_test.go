package tenanttest_test

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/shananth/renovation-platform/backend/internal/identity"
	"github.com/shananth/renovation-platform/backend/internal/platform/secrets"
	"github.com/shananth/renovation-platform/backend/internal/supplieraccess"
)

// Phase H4 — isolation and exposure closure (spec §21.2, §21.6).
//
// A manifest-driven matrix over the complete router: contractor tenant
// isolation, Supplier-to-Supplier isolation, stale-generation denial, the role
// boundary, and a global forbidden-field scan of every external response.
//
// The rule that must never break: a same-tenant caller with an insufficient
// role gets 403, while a privileged caller from ANOTHER tenant gets 404. A 403
// to a foreign tenant would confirm the resource exists.

// Company A cannot reach Company B's M8 resources through any owner-visible
// route, and no foreign mutation takes effect.
func TestM8PhaseH4_ContractorTenantIsolationMatrix(t *testing.T) {
	router, db, _ := setupRouterWithMail(t)
	challenges := supplieraccess.NewMongoVerificationChallengeRepository(db)
	owner := registerCompany(t, router, "h4a-owner@example.com", "H4A Contractor")
	foreign := registerCompany(t, router, "h4a-foreign@example.com", "H4A Foreign")

	rfqChainID, versionID := issueRFQForAwards(t, router, owner, "h4a")
	supplier := inviteAndVerifySupplier(t, router, challenges, owner,
		rfqChainID, "H4A Supplier", "sales@h4a.example", "h4a")
	offerVersionID, _ := submitOffer(t, supplier, 15000, "h4a")

	base := awardBase(rfqChainID, versionID)
	comparison := doJSON(t, router, http.MethodGet, base+"/comparison",
		owner.accessToken, nil)
	offer := comparisonOfferFor(t, comparison, offerVersionID)
	revisionID := publishAward(t, router, owner, base, offerVersionID,
		lineSlice(offer["lines"].([]any)), nil, "h4a")

	// One representative READ per M8 owner module.
	reads := map[string]string{
		"issued version":  "/rfq-versions/" + versionID,
		"version history": "/rfq-chains/" + rfqChainID + "/versions",
		"invitation":      "/rfq-chains/" + rfqChainID + "/invitations/" + supplier.invitationID,
		"comparison":      base + "/comparison",
		"award draft":     base + "/award-draft",
		"award revisions": base + "/award-revisions",
		"award revision":  base + "/award-revisions/" + revisionID,
		"outcomes":        base + "/award-revisions/" + revisionID + "/outcomes",
		"notifications":   base + "/award-revisions/" + revisionID + "/notifications",
	}
	for name, path := range reads {
		response := doJSON(t, router, http.MethodGet, path,
			foreign.accessToken, nil)
		if response.Code != http.StatusNotFound {
			t.Errorf("foreign read of %s = %d, want a non-disclosing 404: %s",
				name, response.Code, response.Body.String())
		}
	}

	// Issuance collapses "draft", "absent" and "another tenant's" into ONE 409
	// by design, so a foreign caller cannot use the status to prove a chain
	// exists. The invariant is indistinguishability, not the 404 used elsewhere.
	foreignReal := doJSON(t, router, http.MethodPost,
		"/rfq-chains/"+rfqChainID+"/issue", foreign.accessToken,
		map[string]any{"currency": "MYR", "operationId": "h4a-foreign-issue"})
	foreignAbsent := doJSON(t, router, http.MethodPost,
		"/rfq-chains/"+versionID+"/issue", foreign.accessToken,
		map[string]any{"currency": "MYR", "operationId": "h4a-absent-issue"})
	if foreignReal.Code != foreignAbsent.Code ||
		foreignReal.Body.String() != foreignAbsent.Body.String() {
		t.Errorf("issuing another tenant's chain (%d %s) is distinguishable from "+
			"an absent chain (%d %s)", foreignReal.Code, foreignReal.Body.String(),
			foreignAbsent.Code, foreignAbsent.Body.String())
	}

	// One representative MUTATION per M8 owner module.
	mutations := []struct {
		name, method, path string
		body               any
	}{
		{"send invitation", http.MethodPost,
			"/rfq-chains/" + rfqChainID + "/invitations/" + supplier.invitationID + "/send",
			map[string]any{"operationId": "h4a-foreign-send"}},
		{"copy link", http.MethodPost,
			"/rfq-chains/" + rfqChainID + "/invitations/" + supplier.invitationID + "/copy-link",
			nil},
		{"create award draft", http.MethodPost, base + "/award-draft", nil},
		{"finalise award", http.MethodPost, base + "/award-revisions",
			map[string]any{"operationId": "h4a-foreign-finalise",
				"changeReason": "foreign"}},
		{"correct award", http.MethodPost, base + "/award-revisions/corrections",
			map[string]any{"operationId": "h4a-foreign-correct",
				"changeReason": "foreign", "decisions": []map[string]any{}}},
		{"reconcile award", http.MethodPost, base + "/award-reconciliation",
			map[string]any{"operationId": "h4a-foreign-reconcile"}},
	}
	for _, mutation := range mutations {
		response := doJSON(t, router, mutation.method, mutation.path,
			foreign.accessToken, mutation.body)
		if response.Code != http.StatusNotFound {
			t.Errorf("foreign %s = %d, want a non-disclosing 404: %s",
				mutation.name, response.Code, response.Body.String())
		}
	}

	// No foreign mutation took effect: exactly one award revision survives.
	list := doJSON(t, router, http.MethodGet, base+"/award-revisions",
		owner.accessToken, nil)
	if got := len(revisionsOf(t, list)); got != 1 {
		t.Fatalf("award revisions = %d after foreign mutations, want 1", got)
	}
}

// A same-tenant employee is refused privileged actions with 403, which is
// distinct from the 404 a foreign tenant receives for the same route.
func TestM8PhaseH4_RoleBoundaryIsDistinctFromTenantIsolation(t *testing.T) {
	router, _, _ := setupRouterWithMail(t)
	owner := registerCompany(t, router, "h4b-owner@example.com", "H4B Contractor")
	foreign := registerCompany(t, router, "h4b-foreign@example.com", "H4B Foreign")
	employee := tokenWithRole(t, owner.accessToken, identity.RoleEmployee)

	rfqChainID, versionID := issueRFQForAwards(t, router, owner, "h4b")
	base := awardBase(rfqChainID, versionID)

	// Every privileged action category: employees are refused, foreign owners
	// are not even told the resource exists.
	privileged := []struct {
		name, path string
		body       any
	}{
		{"finalise award", base + "/award-revisions",
			map[string]any{"operationId": "h4b-role-finalise",
				"changeReason": "role check"}},
		{"correct award", base + "/award-revisions/corrections",
			map[string]any{"operationId": "h4b-role-correct",
				"changeReason": "role check", "decisions": []map[string]any{}}},
		{"reconcile award", base + "/award-reconciliation",
			map[string]any{"operationId": "h4b-role-reconcile"}},
	}
	for _, action := range privileged {
		sameTenant := doJSON(t, router, http.MethodPost, action.path,
			employee, action.body)
		if sameTenant.Code != http.StatusForbidden {
			t.Errorf("employee %s = %d, want 403: %s",
				action.name, sameTenant.Code, sameTenant.Body.String())
		}
		foreignTenant := doJSON(t, router, http.MethodPost, action.path,
			foreign.accessToken, action.body)
		if foreignTenant.Code != http.StatusNotFound {
			t.Errorf("foreign %s = %d, want 404: %s",
				action.name, foreignTenant.Code, foreignTenant.Body.String())
		}
	}

	// Issuance is privileged too, but its refusal collapses into the shared
	// not-ready 409 rather than 403, so it is asserted separately.
	if employeeIssue := doJSON(t, router, http.MethodPost,
		"/rfq-chains/"+rfqChainID+"/issue", employee,
		map[string]any{"currency": "MYR",
			"operationId": "h4b-role-issue"}); employeeIssue.Code != http.StatusForbidden {
		t.Errorf("employee issue = %d, want 403: %s",
			employeeIssue.Code, employeeIssue.Body.String())
	}

	// Preparation and reads stay open to every company role. An award draft is
	// opened first so the award reads address an existing chain, isolating the
	// ROLE boundary from a plain missing-resource 404.
	if draft := doJSON(t, router, http.MethodPost, base+"/award-draft",
		employee, nil); draft.Code != http.StatusCreated {
		t.Fatalf("employee create award draft = %d, want 201: %s",
			draft.Code, draft.Body.String())
	}
	for name, path := range map[string]string{
		"comparison":      base + "/comparison",
		"award revisions": base + "/award-revisions",
		"version history": "/rfq-chains/" + rfqChainID + "/versions",
	} {
		if response := doJSON(t, router, http.MethodGet, path, employee,
			nil); response.Code != http.StatusOK {
			t.Errorf("employee read of %s = %d, want 200: %s",
				name, response.Code, response.Body.String())
		}
	}
}

// One Supplier can never reach another Supplier's invitation, offer or
// outcome, even holding a valid session of its own.
func TestM8PhaseH4_SupplierToSupplierIsolationMatrix(t *testing.T) {
	router, db, _ := setupRouterWithMail(t)
	challenges := supplieraccess.NewMongoVerificationChallengeRepository(db)
	owner := registerCompany(t, router, "h4c-owner@example.com", "H4C Contractor")

	rfqChainID, versionID := issueRFQForAwards(t, router, owner, "h4c")
	first := inviteAndVerifySupplier(t, router, challenges, owner,
		rfqChainID, "H4C First", "first@h4c.example", "h4c-first")
	second := inviteAndVerifySupplier(t, router, challenges, owner,
		rfqChainID, "H4C Second", "second@h4c.example", "h4c-second")

	firstVersionID, _ := submitOffer(t, first, 15000, "h4c-first")
	secondVersionID, _ := submitOffer(t, second, 18000, "h4c-second")

	base := awardBase(rfqChainID, versionID)
	comparison := doJSON(t, router, http.MethodGet, base+"/comparison",
		owner.accessToken, nil)
	offer := comparisonOfferFor(t, comparison, firstVersionID)
	revisionID := publishAward(t, router, owner, base, firstVersionID,
		lineSlice(offer["lines"].([]any)), nil, "h4c")

	outcomes := doJSON(t, router, http.MethodGet,
		base+"/award-revisions/"+revisionID+"/outcomes", owner.accessToken, nil)
	outcomeBySupplier := map[string]string{}
	list, _ := decodeObject(t, outcomes.Body.Bytes())["outcomes"].([]any)
	for _, raw := range list {
		outcome := raw.(map[string]any)
		outcomeBySupplier[outcome["supplierId"].(string)] = outcome["id"].(string)
	}

	// Substituting the OTHER Supplier's identifiers must disclose nothing.
	substitutions := []struct {
		name, method, path string
		body               any
	}{
		{"other invitation", http.MethodGet,
			"/supplier-access/invitations/" + second.invitationID, nil},
		{"other offer draft", http.MethodGet,
			"/supplier-access/invitations/" + second.invitationID + "/offer", nil},
		{"other offer history", http.MethodGet,
			"/supplier-access/invitations/" + second.invitationID + "/offer/versions", nil},
		{"other offer version", http.MethodGet,
			"/supplier-access/invitations/" + first.invitationID + "/offer/versions/" + secondVersionID, nil},
		{"other outcome", http.MethodGet,
			"/supplier-access/outcomes/" + outcomeBySupplier[second.supplierID] +
				"?invitationId=" + first.invitationID, nil},
	}
	for _, substitution := range substitutions {
		response := first.browser.do(t, substitution.method,
			substitution.path, substitution.body)
		if response.Code != http.StatusNotFound {
			t.Errorf("Supplier reached %s = %d, want a neutral 404: %s",
				substitution.name, response.Code, response.Body.String())
		}
		if strings.Contains(response.Body.String(), secondVersionID) {
			t.Errorf("%s response leaked the other Supplier's version id",
				substitution.name)
		}
	}

	// Acknowledging the other Supplier's outcome must not create a record.
	acknowledged := first.browser.do(t, http.MethodPost,
		"/supplier-access/outcomes/"+outcomeBySupplier[second.supplierID]+
			"/acknowledgements", map[string]any{
			"invitationId": first.invitationID,
			"operationId":  "h4c-cross-ack",
		})
	if acknowledged.Code == http.StatusOK ||
		acknowledged.Code == http.StatusCreated {
		t.Fatalf("a Supplier acknowledged another Supplier's outcome: %s",
			acknowledged.Body.String())
	}

	// Each Supplier still reaches its OWN offer history, so the matrix above is
	// proving isolation rather than a uniformly broken surface.
	for name, supplier := range map[string]verifiedSupplier{
		"first": first, "second": second,
	} {
		own := supplier.browser.do(t, http.MethodGet,
			"/supplier-access/invitations/"+supplier.invitationID+"/offer/versions", nil)
		if own.Code != http.StatusOK {
			t.Errorf("%s Supplier cannot read its own history: %d: %s",
				name, own.Code, own.Body.String())
		}
	}
}

// A Supplier mutation without the CSRF header is refused with 403, which is
// distinct from the 404 used for an unusable credential.
func TestM8PhaseH4_SupplierMutationsRequireCSRF(t *testing.T) {
	router, db, _ := setupRouterWithMail(t)
	challenges := supplieraccess.NewMongoVerificationChallengeRepository(db)
	owner := registerCompany(t, router, "h4d-owner@example.com", "H4D Contractor")

	rfqChainID, _ := issueRFQForAwards(t, router, owner, "h4d")
	supplier := inviteAndVerifySupplier(t, router, challenges, owner,
		rfqChainID, "H4D Supplier", "sales@h4d.example", "h4d")

	// Same request, same session, only the CSRF header withheld.
	withoutCSRF := supplier.browser.doWithoutCSRF(t, http.MethodPost,
		"/supplier-access/invitations/"+supplier.invitationID+"/offer", nil)
	if withoutCSRF.Code != http.StatusForbidden {
		t.Fatalf("mutation without CSRF = %d, want 403: %s",
			withoutCSRF.Code, withoutCSRF.Body.String())
	}

	// With the header, the same mutation succeeds, proving the session itself
	// was valid and CSRF was the only thing missing.
	withCSRF := supplier.browser.do(t, http.MethodPost,
		"/supplier-access/invitations/"+supplier.invitationID+"/offer", nil)
	if withCSRF.Code != http.StatusOK {
		t.Fatalf("mutation with CSRF = %d, want 200: %s",
			withCSRF.Code, withCSRF.Body.String())
	}
}

func TestM8PhaseH4_SupplierAccessOfferRejectsMissingInvalidAndExpiredSessions(
	t *testing.T) {
	router, db, _ := setupRouterWithMail(t)
	challenges := supplieraccess.NewMongoVerificationChallengeRepository(db)
	owner := registerCompany(t, router, "h4session-owner@example.com", "H4 Session Contractor")
	rfqChainID, _ := issueRFQForAwards(t, router, owner, "h4session")
	supplier := inviteAndVerifySupplier(t, router, challenges, owner,
		rfqChainID, "H4 Session Supplier", "sales@h4session.example", "h4session")
	path := "/supplier-access/invitations/" + supplier.invitationID + "/offer"

	missing := newSupplierBrowser(router).do(t, http.MethodGet, path, nil)
	if missing.Code != http.StatusNotFound {
		t.Fatalf("missing session = %d, want neutral 404: %s",
			missing.Code, missing.Body.String())
	}

	rawToken := supplier.browser.cookiesSnapshot()[supplieraccess.SupplierSessionCookieName]
	invalidToken := "A" + rawToken[1:]
	if invalidToken == rawToken {
		invalidToken = "B" + rawToken[1:]
	}
	invalidBrowser := newSupplierBrowser(router)
	invalidBrowser.cookies[supplieraccess.SupplierSessionCookieName] = invalidToken
	invalid := invalidBrowser.do(t, http.MethodGet, path, nil)
	if invalid.Code != http.StatusNotFound {
		t.Fatalf("invalid session = %d, want neutral 404: %s",
			invalid.Code, invalid.Body.String())
	}

	sessions := supplieraccess.NewMongoSupplierSessionRepository(db)
	session, err := sessions.FindSessionByTokenHash(
		context.Background(), secrets.HashSupplierSessionToken(rawToken))
	if err != nil {
		t.Fatalf("load Supplier session: %v", err)
	}
	now := time.Now().UTC()
	session.LastUsedAt = now.Add(-2 * time.Hour)
	session.SlidingExpiresAt = now.Add(-time.Hour)
	session.Revision++
	if _, err := sessions.ReplaceSessionCAS(
		context.Background(), session, session.Revision-1); err != nil {
		t.Fatalf("expire Supplier session: %v", err)
	}
	expired := supplier.browser.do(t, http.MethodGet, path, nil)
	if expired.Code != http.StatusNotFound {
		t.Fatalf("expired session = %d, want neutral 404: %s",
			expired.Code, expired.Body.String())
	}
}

// Every external Supplier response carries the four privacy headers and none of
// the forbidden internal fields.
func TestM8PhaseH4_ExternalResponsesCarryHeadersAndNoForbiddenFields(t *testing.T) {
	router, db, _ := setupRouterWithMail(t)
	challenges := supplieraccess.NewMongoVerificationChallengeRepository(db)
	owner := registerCompany(t, router, "h4e-owner@example.com", "H4E Contractor")

	rfqChainID, versionID := issueRFQForAwards(t, router, owner, "h4e")
	supplier := inviteAndVerifySupplier(t, router, challenges, owner,
		rfqChainID, "H4E Supplier", "sales@h4e.example", "h4e")
	offerVersionID, _ := submitOffer(t, supplier, 15500, "h4e")

	base := awardBase(rfqChainID, versionID)
	comparison := doJSON(t, router, http.MethodGet, base+"/comparison",
		owner.accessToken, nil)
	offer := comparisonOfferFor(t, comparison, offerVersionID)
	revisionID := publishAward(t, router, owner, base, offerVersionID,
		lineSlice(offer["lines"].([]any)), nil, "h4e")
	outcomes := doJSON(t, router, http.MethodGet,
		base+"/award-revisions/"+revisionID+"/outcomes", owner.accessToken, nil)
	list, _ := decodeObject(t, outcomes.Body.Bytes())["outcomes"].([]any)
	outcomeID := list[0].(map[string]any)["id"].(string)

	// Success, not-found and validation responses across the Supplier surface.
	responses := map[string]*struct {
		method, path string
		body         any
	}{
		"invitation":    {http.MethodGet, "/supplier-access/invitations/" + supplier.invitationID, nil},
		"rfq versions":  {http.MethodGet, "/supplier-access/invitations/" + supplier.invitationID + "/rfq-versions", nil},
		"offer draft":   {http.MethodGet, "/supplier-offers/" + supplier.invitationID + "/draft", nil},
		"offer history": {http.MethodGet, "/supplier-offers/" + supplier.invitationID + "/versions", nil},
		"offer version": {http.MethodGet, "/supplier-offers/" + supplier.invitationID + "/versions/" + offerVersionID, nil},
		"outcome":       {http.MethodGet, "/supplier-access/outcomes/" + outcomeID + "?invitationId=" + supplier.invitationID, nil},
		"unknown offer": {http.MethodGet, "/supplier-offers/" + supplier.invitationID + "/versions/missing", nil},
	}

	for name, request := range responses {
		response := supplier.browser.do(t, request.method, request.path, request.body)

		if got := response.Header().Get("Cache-Control"); got != "no-store, max-age=0" {
			t.Errorf("%s Cache-Control = %q", name, got)
		}
		if got := response.Header().Get("Pragma"); got != "no-cache" {
			t.Errorf("%s Pragma = %q", name, got)
		}
		if got := response.Header().Get("Referrer-Policy"); got != "no-referrer" {
			t.Errorf("%s Referrer-Policy = %q", name, got)
		}
		if got := response.Header().Get("X-Content-Type-Options"); got != "nosniff" {
			t.Errorf("%s X-Content-Type-Options = %q", name, got)
		}

		// Nothing internal, and no other tenant's or Supplier's identifiers.
		body := strings.ToLower(response.Body.String())
		for _, forbidden := range []string{
			"companyid", "accesssecrethash", "tokenhash", "codeverifier",
			"operationid", "claimid", "internalnotes", "supplierid",
		} {
			if strings.Contains(body, forbidden) {
				t.Errorf("%s response exposed forbidden field %q: %s",
					name, forbidden, response.Body.String())
			}
		}
	}
}
