package tenanttest_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/shananth/renovation-platform/backend/internal/platform/secrets"
	"github.com/shananth/renovation-platform/backend/internal/supplieraccess"
)

// Phase H1 — the golden composed journey (spec §21.2, §21.6).
//
// One complete Contractor/Supplier journey through the REAL router and real
// MongoDB: M7 RFQ creation, M8 issuance, invitation, Supplier link exchange and
// email verification, Offer drafting and submission, comparison, Award
// publication, explicit successful and unsuccessful notifications reaching
// `sent`, each Supplier reading ONLY its own outcome, and each acknowledging.
//
// Focused suites remain the authority for calculation properties and repository
// one-winner mechanics. What this proves is that the shipped modules, adapters,
// middleware, authorization and persistence compose into the promised business
// result — something no unit test can establish.
//
// "Persisted result" is asserted through the owning routes after the journey,
// never inferred from a single response body.

// --- Supplier browser ---

// supplierBrowser is a minimal cookie-holding HTTP client. A Supplier's whole
// authority lives in cookies, so a journey that dropped them would prove
// nothing about the real credential flow.
//
// cookies is shared, mutable state: tests such as
// TestM8PhaseH6_ConcurrentAcknowledgementsCreateOneReceipt deliberately drive
// many goroutines through ONE browser. mu guards every read and write of
// cookies; it is never held across the HTTP round-trip itself, only around
// the snapshot taken before the request and the Set-Cookie mutations applied
// after the response.
type supplierBrowser struct {
	router  http.Handler
	mu      sync.RWMutex
	cookies map[string]string
}

func newSupplierBrowser(router http.Handler) *supplierBrowser {
	return &supplierBrowser{router: router, cookies: map[string]string{}}
}

// do issues a request carrying the current cookie jar and the canonical CSRF
// header, then absorbs any Set-Cookie the response returns.
func (b *supplierBrowser) do(t *testing.T, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	return b.request(t, method, path, body, true)
}

// doWithoutCSRF sends the same cookies but withholds the X-CSRF-Token header,
// which is how a cross-site request would arrive: the browser attaches cookies
// automatically but cannot read the CSRF cookie to echo it.
func (b *supplierBrowser) doWithoutCSRF(
	t *testing.T, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	return b.request(t, method, path, body, false)
}

func (b *supplierBrowser) request(
	t *testing.T, method, path string, body any, sendCSRF bool,
) *httptest.ResponseRecorder {
	t.Helper()

	var reader *strings.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal supplier body: %v", err)
		}
		reader = strings.NewReader(string(encoded))
	} else {
		reader = strings.NewReader("")
	}
	request := httptest.NewRequest(method, path, reader)
	request.Header.Set("Content-Type", "application/json")

	// Snapshot the jar under RLock so concurrent callers never iterate or read
	// b.cookies while another goroutine's response is writing to it. The lock
	// is released before the network round-trip below.
	requestCookies := b.cookiesSnapshot()
	for name, value := range requestCookies {
		request.AddCookie(&http.Cookie{Name: name, Value: value})
	}
	// The CSRF cookie is readable by the client precisely so it can be echoed
	// in the header; that double-submit is the mechanism under test.
	if csrf, ok := requestCookies[supplieraccess.SupplierCSRFCookieName]; ok && sendCSRF {
		request.Header.Set("X-CSRF-Token", csrf)
	}

	response := httptest.NewRecorder()
	b.router.ServeHTTP(response, request)

	b.mu.Lock()
	for _, cookie := range response.Result().Cookies() {
		if cookie.MaxAge < 0 {
			delete(b.cookies, cookie.Name)
			continue
		}
		b.cookies[cookie.Name] = cookie.Value
	}
	b.mu.Unlock()
	return response
}

// cookiesSnapshot returns a copy of the browser's current cookie jar, safe to
// read concurrently: callers get a request-local map they can range over or
// mutate freely without touching the shared jar.
func (b *supplierBrowser) cookiesSnapshot() map[string]string {
	b.mu.RLock()
	defer b.mu.RUnlock()
	snapshot := make(map[string]string, len(b.cookies))
	for name, value := range b.cookies {
		snapshot[name] = value
	}
	return snapshot
}

// verifiedSupplier is one Supplier that has completed the Phase D flow.
type verifiedSupplier struct {
	browser      *supplierBrowser
	supplierID   string
	invitationID string
	email        string
}

// inviteAndVerifySupplier drives the entire real Supplier access flow: the
// contractor creates and sends an invitation, the Supplier opens the secure
// link, requests a code and verifies it.
//
// The verification code is RE-DERIVED from the fixed test keyring and the
// persisted challenge rather than scraped from email. That is exactly how the
// production keyring recovers a code, so the journey exercises the real
// derivation instead of a test-only backdoor.
func inviteAndVerifySupplier(
	t *testing.T,
	router http.Handler,
	db supplierChallengeReader,
	owner testCompany,
	rfqChainID, supplierName, email, suffix string,
) verifiedSupplier {
	t.Helper()

	supplier := createSupplier(t, router, owner, supplierName)
	supplierID := mustField(t, supplier, "id")

	expiry := time.Now().UTC().Add(60 * 24 * time.Hour).Truncate(time.Second)
	invitation := doJSON(t, router, http.MethodPost,
		"/rfq-chains/"+rfqChainID+"/invitations", owner.accessToken,
		map[string]any{
			"supplierId":     supplierID,
			"recipientName":  supplierName + " Sales",
			"recipientEmail": email,
			"expiresAt":      expiry.Format(time.RFC3339),
		})
	if invitation.Code != http.StatusCreated {
		t.Fatalf("create invitation for %s: %d: %s",
			supplierName, invitation.Code, invitation.Body.String())
	}
	invitationID := mustField(t, invitation, "id")

	// An explicit SEND is what activates the invitation (§5): copy-link
	// deliberately returns the link "without changing the invitation", so a
	// draft invitation's link is not yet usable.
	sent := doJSON(t, router, http.MethodPost,
		"/rfq-chains/"+rfqChainID+"/invitations/"+invitationID+"/send",
		owner.accessToken, map[string]any{"operationId": "h1-send-" + suffix})
	if sent.Code != http.StatusOK {
		t.Fatalf("send invitation to %s: %d: %s",
			supplierName, sent.Code, sent.Body.String())
	}
	if status := mustField(t, sent, "status"); status != "sent" {
		t.Fatalf("invitation delivery status = %q, want sent", status)
	}

	// copy-link returns the CURRENT secure link without rotating it, which is
	// the production-supported way to obtain the link for manual sharing.
	link := doJSON(t, router, http.MethodPost,
		"/rfq-chains/"+rfqChainID+"/invitations/"+invitationID+"/copy-link",
		owner.accessToken, nil)
	if link.Code != http.StatusOK {
		t.Fatalf("copy invitation link: %d: %s", link.Code, link.Body.String())
	}
	rawLink := mustField(t, link, "url")
	parsed, err := url.Parse(rawLink)
	if err != nil {
		t.Fatalf("parse invitation link: %v", err)
	}
	token := parsed.Query().Get("token")
	if token == "" {
		t.Fatalf("invitation link carried no token: %q", rawLink)
	}

	browser := newSupplierBrowser(router)

	// Opening the link exchanges the invitation token for a short-lived
	// exchange cookie and redirects to a clean URL, so the raw credential
	// leaves the browser-visible address.
	opened := browser.do(t, http.MethodGet,
		"/supplier-access/open?token="+url.QueryEscape(token), nil)
	if opened.Code != http.StatusSeeOther {
		t.Fatalf("open invitation link: %d, want 303: %s",
			opened.Code, opened.Body.String())
	}
	if _, ok := browser.cookies[supplieraccess.AccessExchangeCookieName]; !ok {
		t.Fatalf("open set no exchange cookie: %#v", browser.cookies)
	}

	// The tenant test's mailer is not backed by a live SMTP relay, so delivery
	// legitimately fails. The challenge itself is authoritative and is returned
	// with a bounded 503 precisely so verification can still proceed.
	challenge := browser.do(t, http.MethodPost, "/supplier-access/challenges",
		map[string]any{"operationId": "h1-challenge-" + suffix})
	if challenge.Code != http.StatusCreated &&
		challenge.Code != http.StatusServiceUnavailable {
		t.Fatalf("create challenge: %d: %s",
			challenge.Code, challenge.Body.String())
	}
	challengeID := mustField(t, challenge, "challengeId")

	code := deriveChallengeCode(t, db, challengeID)
	verified := browser.do(t, http.MethodPost,
		"/supplier-access/challenges/verify", map[string]any{
			"challengeId": challengeID,
			"code":        code,
			"operationId": "h1-verify-" + suffix,
		})
	if verified.Code != http.StatusOK {
		t.Fatalf("verify challenge: %d: %s",
			verified.Code, verified.Body.String())
	}
	if _, ok := browser.cookies[supplieraccess.SupplierSessionCookieName]; !ok {
		t.Fatalf("verification set no session cookie: %#v", browser.cookies)
	}
	if _, ok := browser.cookies[supplieraccess.SupplierCSRFCookieName]; !ok {
		t.Fatalf("verification set no CSRF cookie: %#v", browser.cookies)
	}

	return verifiedSupplier{
		browser: browser, supplierID: supplierID,
		invitationID: invitationID, email: email,
	}
}

// supplierChallengeReader is the narrow read the journey needs to recover a
// code. It is satisfied by the real Mongo repository.
type supplierChallengeReader interface {
	FindChallenge(ctx context.Context, challengeID string) (
		supplieraccess.EmailVerificationChallenge, error)
}

// deriveChallengeCode recomputes the six-digit code from the persisted
// challenge identity, exactly as the service does when it sends the email.
func deriveChallengeCode(
	t *testing.T, reader supplierChallengeReader, challengeID string) string {
	t.Helper()

	challenge, err := reader.FindChallenge(context.Background(), challengeID)
	if err != nil {
		t.Fatalf("load challenge %s: %v", challengeID, err)
	}
	// The tenant router uses a FIXED verification keyring for the same reason
	// production forbids per-boot keys: an outstanding challenge must stay
	// verifiable. Re-deriving here exercises the real derivation rather than
	// adding a test-only way to read codes.
	keyring, err := secrets.NewSupplierVerificationCodeKeyring(
		1, map[int]string{
			1: "dGVuYW50dGVzdC12ZXJpZmljYXRpb24tY29kZS1rZXktdjE=",
		})
	if err != nil {
		t.Fatalf("build verification keyring: %v", err)
	}
	code, err := keyring.DeriveCode(challenge.CodeKeyVersion,
		secrets.SupplierVerificationCodeContext{
			ChallengeID:              challenge.ID,
			CompanyID:                challenge.CompanyID,
			SupplierID:               challenge.SupplierID,
			InvitationID:             challenge.InvitationID,
			AccessGeneration:         challenge.AccessGeneration,
			NormalizedRecipientEmail: challenge.RecipientEmailNormalized,
		})
	if err != nil {
		t.Fatalf("derive verification code: %v", err)
	}
	return code
}

// submitOffer drives one Supplier's complete offer: create the draft, price
// every line, state validity and submit an immutable version.
func submitOffer(
	t *testing.T,
	supplier verifiedSupplier,
	unitPriceMinor int64,
	suffix string,
) (versionID string, grandTotalMinor int64) {
	t.Helper()

	base := "/supplier-access/invitations/" + supplier.invitationID + "/offer"
	created := supplier.browser.do(t, http.MethodPost, base, nil)
	if created.Code != http.StatusOK {
		t.Fatalf("create offer draft: %d: %s", created.Code, created.Body.String())
	}
	draft := decodeObject(t, created.Body.Bytes())
	draftID, _ := draft["id"].(string)
	revision := draft["revision"].(float64)

	lines, ok := draft["lines"].([]any)
	if !ok || len(lines) == 0 {
		t.Fatalf("offer draft carried no RFQ lines: %s", created.Body.String())
	}
	for _, raw := range lines {
		line := raw.(map[string]any)
		quoted := supplier.browser.do(t, http.MethodPut,
			base+"/lines/"+line["id"].(string)+"/quote",
			map[string]any{
				"draftId":          draftID,
				"expectedRevision": int64(revision),
				"unitPriceMinor":   unitPriceMinor,
				"brand":            "Acme",
				"leadTime":         "14 days",
			})
		if quoted.Code != http.StatusOK {
			t.Fatalf("quote line: %d: %s", quoted.Code, quoted.Body.String())
		}
		revision = mustNumberField(t, quoted, "revision")
	}

	// Validity never copies forward, so it is stated explicitly per submission.
	validity := supplier.browser.do(t, http.MethodPut, base+"/validity",
		map[string]any{
			"draftId":          draftID,
			"expectedRevision": int64(revision),
			"offerValidUntil": time.Now().UTC().
				Add(45 * 24 * time.Hour).Format(time.RFC3339),
		})
	if validity.Code != http.StatusOK {
		t.Fatalf("set offer validity: %d: %s",
			validity.Code, validity.Body.String())
	}
	revision = mustNumberField(t, validity, "revision")

	submitted := supplier.browser.do(t, http.MethodPost, base+"/submissions",
		map[string]any{
			"draftId":          draftID,
			"expectedRevision": int64(revision),
			"operationId":      "h1-submit-" + suffix,
		})
	if submitted.Code != http.StatusCreated {
		t.Fatalf("submit offer: %d: %s", submitted.Code, submitted.Body.String())
	}
	body := decodeObject(t, submitted.Body.Bytes())
	total, _ := body["grandTotalMinor"].(float64)
	return mustField(t, submitted, "id"), int64(total)
}
