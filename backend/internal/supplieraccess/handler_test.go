package supplieraccess_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"testing"
	"time"

	platformhttp "github.com/shananth/renovation-platform/backend/internal/platform/http"
	"github.com/shananth/renovation-platform/backend/internal/platform/secrets"
	"github.com/shananth/renovation-platform/backend/internal/supplieraccess"
)

func challengeHandlerService(rig *challengeServiceRig,
	rates supplieraccess.VerificationRateClaimer,
	options ...supplieraccess.ServiceOption) *supplieraccess.Service {
	base := []supplieraccess.ServiceOption{
		supplieraccess.WithInvitationAccess(rig.access, rig.access),
		supplieraccess.WithAccessExchangeStore(rig.exchanges),
		supplieraccess.WithOpaqueTokenGenerator(
			supplieraccess.CryptographicOpaqueTokenGenerator{}),
		supplieraccess.WithVerificationStores(rig.challenges, rig.deliveries),
		supplieraccess.WithVerificationSecurity(
			rig.codeKeys, rig.fingerprints, rates),
		supplieraccess.WithVerificationMailer(rig.mailer),
	}
	return supplieraccess.NewService(append(base, options...)...)
}

type countingChallengeExchangeStore struct {
	*supplieraccess.MongoAccessExchangeRepository
	findCalls int
}

func (s *countingChallengeExchangeStore) FindExchangeByTokenHash(
	ctx context.Context, tokenHash string,
) (supplieraccess.SupplierAccessExchange, error) {
	s.findCalls++
	return s.MongoAccessExchangeRepository.FindExchangeByTokenHash(ctx, tokenHash)
}

func openHandlerService(access *fakeInvitationAccess,
	store *fakeAccessExchangeStore, exchangeID, exchangeToken string,
) *supplieraccess.Service {
	return supplieraccess.NewService(
		supplieraccess.WithInvitationAccess(access, access),
		supplieraccess.WithAccessExchangeStore(store),
		supplieraccess.WithOpaqueTokenGenerator(
			&sequenceOpaqueTokenGenerator{values: []string{exchangeID, exchangeToken}}),
	)
}

func TestRegisterHandlersUsesBodyBasedChallengeAndResendRoutes(t *testing.T) {
	router, api := platformhttp.NewRouter("supplier-access-test", "0.0.0")
	supplieraccess.RegisterHandlers(api, nil, false)

	request := httptest.NewRequest(http.MethodGet, "/openapi.json", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("OpenAPI status = %d", response.Code)
	}
	var document struct {
		Paths map[string]map[string]struct {
			OperationID string `json:"operationId"`
		} `json:"paths"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &document); err != nil {
		t.Fatalf("decoding OpenAPI: %v", err)
	}
	for path, operationID := range map[string]string{
		"/supplier-access/challenges":        "supplier-access-create-challenge",
		"/supplier-access/challenges/resend": "supplier-access-resend-challenge",
		"/supplier-access/challenges/verify": "supplier-access-verify-challenge",
		"/supplier-access/session/logout":    "supplier-access-logout",
	} {
		operation, ok := document.Paths[path]["post"]
		if !ok || operation.OperationID != operationID {
			t.Fatalf("POST %s = %#v", path, operation)
		}
	}
	open, ok := document.Paths["/supplier-access/open"]["get"]
	if !ok || open.OperationID != "supplier-access-open" {
		t.Fatalf("GET /supplier-access/open = %#v", open)
	}
	for path := range document.Paths {
		if strings.Contains(path, "{challengeId}") {
			t.Fatalf("challenge identifier leaked into route path %q", path)
		}
	}
}

func TestChallengeHandlerConsumesExchangeCookieAndReturnsOpaqueChallengeHandle(t *testing.T) {
	now := time.Now().UTC()
	rig := newChallengeServiceRig(t, now)
	router, api := platformhttp.NewRouter("supplier-access-test", "0.0.0")
	supplieraccess.RegisterHandlers(api, rig.service, false)

	requestBody, _ := json.Marshal(map[string]string{
		"operationId": "challenge-operation-http-1",
	})
	request := httptest.NewRequest(http.MethodPost,
		"/supplier-access/challenges", bytes.NewReader(requestBody))
	request.Header.Set("Content-Type", "application/json")
	request.RemoteAddr = "198.51.100.44:54321"
	request.AddCookie(&http.Cookie{
		Name:  supplieraccess.AccessExchangeCookieName,
		Value: rig.rawExchangeToken,
	})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d: %s", response.Code, response.Body.String())
	}
	if response.Header().Get("Cache-Control") != "no-store, max-age=0" ||
		response.Header().Get("Pragma") != "no-cache" ||
		response.Header().Get("Referrer-Policy") != "no-referrer" {
		t.Fatalf("security headers = %#v", response.Header())
	}
	cookies := response.Result().Cookies()
	if len(cookies) != 1 ||
		cookies[0].Name != supplieraccess.AccessExchangeCookieName ||
		cookies[0].Value != "" || cookies[0].MaxAge >= 0 ||
		cookies[0].Path != "/supplier-access" {
		t.Fatalf("cleared exchange cookie = %#v", cookies)
	}
	var body struct {
		ChallengeID    string `json:"challengeId"`
		DeliveryStatus string `json:"deliveryStatus"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding challenge response: %v", err)
	}
	if body.ChallengeID == "" || body.DeliveryStatus != "sent" {
		t.Fatalf("challenge body = %#v", body)
	}
	if strings.Contains(response.Body.String(), rig.rawExchangeToken) ||
		strings.Contains(response.Body.String(), rig.exchange.CompanyID) ||
		strings.Contains(response.Body.String(), rig.exchange.SupplierID) {
		t.Fatalf("challenge response leaked exchange/identity: %s", response.Body.String())
	}
}

func TestChallengeHandlerUsesForwardedClientOnlyBehindTrustedProxy(t *testing.T) {
	now := time.Now().UTC()
	rig := newChallengeServiceRig(t, now)
	limiter := supplieraccess.NewVerificationRateLimiter(
		rig.rates, supplieraccess.CryptographicOpaqueTokenGenerator{})
	service := challengeHandlerService(rig, limiter,
		supplieraccess.WithTrustedProxyCIDRs([]netip.Prefix{
			netip.MustParsePrefix("10.0.0.0/8"),
		}))
	router, api := platformhttp.NewRouter("supplier-access-test", "0.0.0")
	supplieraccess.RegisterHandlers(api, service, false)

	requestBody, _ := json.Marshal(map[string]string{
		"operationId": "challenge-operation-trusted-proxy",
	})
	request := httptest.NewRequest(http.MethodPost,
		"/supplier-access/challenges", bytes.NewReader(requestBody))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Forwarded-For", "198.51.100.91")
	request.RemoteAddr = "10.1.2.3:443"
	request.AddCookie(&http.Cookie{
		Name: supplieraccess.AccessExchangeCookieName, Value: rig.rawExchangeToken,
	})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d: %s", response.Code, response.Body.String())
	}

	forwardedHash, _ := rig.fingerprints.FingerprintClientAddress(
		netip.MustParseAddr("198.51.100.91"))
	if _, err := rig.rates.FindState(context.Background(),
		supplieraccess.VerificationRateScopeClient, forwardedHash); err != nil {
		t.Fatalf("forwarded client rate state: %v", err)
	}
	directHash, _ := rig.fingerprints.FingerprintClientAddress(
		netip.MustParseAddr("10.1.2.3"))
	if _, err := rig.rates.FindState(context.Background(),
		supplieraccess.VerificationRateScopeClient, directHash); !errors.Is(err, supplieraccess.ErrVerificationRateLimitStateNotFound) {
		t.Fatalf("trusted proxy was rate-limited as the client: %v", err)
	}
}

func TestChallengeHandlerReturnsBoundedRateLimitWithoutConsumingCookie(t *testing.T) {
	now := time.Now().UTC()
	rig := newChallengeServiceRig(t, now)
	rates := &recordingRateClaimer{
		err: &supplieraccess.VerificationRateLimitExceeded{
			RetryAt: now.Add(48 * time.Hour),
		},
	}
	service := challengeHandlerService(rig, rates)
	router, api := platformhttp.NewRouter("supplier-access-test", "0.0.0")
	supplieraccess.RegisterHandlers(api, service, false)

	requestBody, _ := json.Marshal(map[string]string{
		"operationId": "challenge-operation-rate-limited",
	})
	request := httptest.NewRequest(http.MethodPost,
		"/supplier-access/challenges", bytes.NewReader(requestBody))
	request.Header.Set("Content-Type", "application/json")
	request.RemoteAddr = "198.51.100.44:54321"
	request.AddCookie(&http.Cookie{
		Name: supplieraccess.AccessExchangeCookieName, Value: rig.rawExchangeToken,
	})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusTooManyRequests ||
		response.Header().Get("Retry-After") != "3600" {
		t.Fatalf("rate response = %d Retry-After=%q: %s",
			response.Code, response.Header().Get("Retry-After"),
			response.Body.String())
	}
	if len(response.Result().Cookies()) != 0 {
		t.Fatalf("rate rejection cleared a retryable exchange cookie: %#v",
			response.Result().Cookies())
	}
	if strings.Contains(response.Body.String(), "client_address") ||
		strings.Contains(response.Body.String(), rig.exchange.InvitationID) {
		t.Fatalf("rate response disclosed scope or identity: %s",
			response.Body.String())
	}
}

func TestResendHandlerUsesBodyHandleAndReturnsTheExistingChallenge(t *testing.T) {
	createdAt := time.Now().UTC().Add(-2 * time.Minute)
	rig := newChallengeServiceRig(t, createdAt)
	created, err := rig.service.CreateChallenge(context.Background(),
		supplieraccess.CreateChallengeInput{
			ExchangeToken: rig.rawExchangeToken,
			OperationID:   "challenge-operation-before-http-resend",
			ClientAddress: netip.MustParseAddr("198.51.100.44"),
			RequestedAt:   createdAt,
		})
	if err != nil {
		t.Fatalf("creating challenge before resend: %v", err)
	}
	router, api := platformhttp.NewRouter("supplier-access-test", "0.0.0")
	supplieraccess.RegisterHandlers(api, rig.service, false)
	requestBody, _ := json.Marshal(map[string]string{
		"challengeId": created.ChallengeID,
		"operationId": "resend-operation-http-1",
	})
	request := httptest.NewRequest(http.MethodPost,
		"/supplier-access/challenges/resend", bytes.NewReader(requestBody))
	request.Header.Set("Content-Type", "application/json")
	request.RemoteAddr = "198.51.100.44:54321"
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", response.Code, response.Body.String())
	}
	var body struct {
		ChallengeID    string `json:"challengeId"`
		DeliveryStatus string `json:"deliveryStatus"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding resend response: %v", err)
	}
	if body.ChallengeID != created.ChallengeID ||
		body.DeliveryStatus != "sent" || len(rig.mailer.messages) != 2 {
		t.Fatalf("resend body/messages = %#v/%d", body, len(rig.mailer.messages))
	}
	if response.Header().Get("Cache-Control") != "no-store, max-age=0" ||
		response.Header().Get("Referrer-Policy") != "no-referrer" ||
		len(response.Result().Cookies()) != 0 {
		t.Fatalf("resend boundary headers/cookies = %#v/%#v",
			response.Header(), response.Result().Cookies())
	}
}

func TestChallengeHandlerReturnsFailedDeliveryHandleWithoutProviderDetails(t *testing.T) {
	now := time.Now().UTC()
	rig := newChallengeServiceRig(t, now)
	providerDetail := "smtp unavailable at private-mail-host:2525"
	rig.mailer.err = errors.New(providerDetail)
	router, api := platformhttp.NewRouter("supplier-access-test", "0.0.0")
	supplieraccess.RegisterHandlers(api, rig.service, false)

	requestBody, _ := json.Marshal(map[string]string{
		"operationId": "challenge-operation-mail-failed",
	})
	request := httptest.NewRequest(http.MethodPost,
		"/supplier-access/challenges", bytes.NewReader(requestBody))
	request.Header.Set("Content-Type", "application/json")
	request.RemoteAddr = "198.51.100.44:54321"
	request.AddCookie(&http.Cookie{
		Name: supplieraccess.AccessExchangeCookieName, Value: rig.rawExchangeToken,
	})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d: %s", response.Code, response.Body.String())
	}
	var body struct {
		ChallengeID    string `json:"challengeId"`
		DeliveryStatus string `json:"deliveryStatus"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding failed-delivery response: %v", err)
	}
	if body.ChallengeID == "" || body.DeliveryStatus != "failed" {
		t.Fatalf("failed-delivery body = %#v", body)
	}
	if strings.Contains(response.Body.String(), providerDetail) ||
		strings.Contains(response.Body.String(), "private-mail-host") {
		t.Fatalf("failed-delivery response leaked provider detail: %s",
			response.Body.String())
	}
	cookies := response.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name !=
		supplieraccess.AccessExchangeCookieName || cookies[0].MaxAge >= 0 {
		t.Fatalf("persisted challenge did not clear exchange cookie: %#v", cookies)
	}
}

func TestChallengeHandlerClearsUnknownExchangeCookie(t *testing.T) {
	now := time.Now().UTC()
	rig := newChallengeServiceRig(t, now)
	router, api := platformhttp.NewRouter("supplier-access-test", "0.0.0")
	supplieraccess.RegisterHandlers(api, rig.service, true)

	requestBody, _ := json.Marshal(map[string]string{
		"operationId": "challenge-operation-unknown-exchange",
	})
	request := httptest.NewRequest(http.MethodPost,
		"/supplier-access/challenges", bytes.NewReader(requestBody))
	request.Header.Set("Content-Type", "application/json")
	request.RemoteAddr = "198.51.100.44:54321"
	request.AddCookie(&http.Cookie{
		Name: supplieraccess.AccessExchangeCookieName, Value: opaqueToken(219),
	})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d: %s", response.Code, response.Body.String())
	}
	cookies := response.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name !=
		supplieraccess.AccessExchangeCookieName || cookies[0].MaxAge >= 0 ||
		!cookies[0].Secure {
		t.Fatalf("unknown exchange cookie was not cleared: %#v", cookies)
	}
}

func TestChallengeHandlerCollapsesMalformedAndUnknownExchangeCredentials(t *testing.T) {
	now := time.Now().UTC()
	rig := newChallengeServiceRig(t, now)
	store := &countingChallengeExchangeStore{
		MongoAccessExchangeRepository: rig.exchanges,
	}
	limiter := supplieraccess.NewVerificationRateLimiter(
		rig.rates, supplieraccess.CryptographicOpaqueTokenGenerator{})
	service := supplieraccess.NewService(
		supplieraccess.WithInvitationAccess(rig.access, rig.access),
		supplieraccess.WithAccessExchangeStore(store),
		supplieraccess.WithOpaqueTokenGenerator(
			supplieraccess.CryptographicOpaqueTokenGenerator{}),
		supplieraccess.WithVerificationStores(rig.challenges, rig.deliveries),
		supplieraccess.WithVerificationSecurity(
			rig.codeKeys, rig.fingerprints, limiter),
		supplieraccess.WithVerificationMailer(rig.mailer),
	)
	router, api := platformhttp.NewRouter("supplier-access-test", "0.0.0")
	supplieraccess.RegisterHandlers(api, service, false)

	send := func(cookieValue, operationID string) *httptest.ResponseRecorder {
		requestBody, _ := json.Marshal(map[string]string{
			"operationId": operationID,
		})
		request := httptest.NewRequest(http.MethodPost,
			"/supplier-access/challenges", bytes.NewReader(requestBody))
		request.Header.Set("Content-Type", "application/json")
		request.RemoteAddr = "198.51.100.44:54321"
		request.AddCookie(&http.Cookie{
			Name: supplieraccess.AccessExchangeCookieName, Value: cookieValue,
		})
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		return response
	}

	malformed := send("not-a-canonical-exchange-token", "malformed-operation")
	if store.findCalls != 0 {
		t.Fatalf("malformed exchange reached MongoDB %d times", store.findCalls)
	}
	unknown := send(opaqueToken(220), "unknown-operation")
	if store.findCalls != 1 {
		t.Fatalf("unknown canonical exchange lookup calls = %d, want 1",
			store.findCalls)
	}
	if malformed.Code != http.StatusNotFound ||
		unknown.Code != http.StatusNotFound ||
		malformed.Body.String() != unknown.Body.String() {
		t.Fatalf("malformed response %d/%q differs from unknown %d/%q",
			malformed.Code, malformed.Body.String(),
			unknown.Code, unknown.Body.String())
	}
	for name, response := range map[string]*httptest.ResponseRecorder{
		"malformed": malformed, "unknown": unknown,
	} {
		cookies := response.Result().Cookies()
		if len(cookies) != 1 || cookies[0].Name !=
			supplieraccess.AccessExchangeCookieName || cookies[0].MaxAge >= 0 {
			t.Fatalf("%s exchange cookie was not cleared: %#v", name, cookies)
		}
	}
}

func TestOpenHandlerExchangesTokenForCookieAndClean303(t *testing.T) {
	rawInvitationToken := opaqueToken(61)
	exchangeID := opaqueToken(71)
	rawExchangeToken := opaqueToken(81)
	access := &fakeInvitationAccess{
		found: true,
		snapshot: supplieraccess.InvitationAccessSnapshot{
			CompanyID: "company-1", SupplierID: "supplier-1",
			InvitationID:              "invitation-1",
			NormalizedRecipientEmail:  "recipient@supplier.test",
			AccessGeneration:          3,
			CurrentIssuedRFQVersionID: "version-2",
		},
	}
	store := &fakeAccessExchangeStore{}
	service := openHandlerService(access, store, exchangeID, rawExchangeToken)
	router, api := platformhttp.NewRouter("supplier-access-test", "0.0.0")
	supplieraccess.RegisterHandlers(api, service, true)

	requestPath := "/supplier-access/open?token=" + url.QueryEscape(rawInvitationToken)
	request := httptest.NewRequest(http.MethodGet, requestPath, nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303: %s", response.Code, response.Body.String())
	}
	if location := response.Header().Get("Location"); location != "/supplier-access/open" {
		t.Fatalf("Location = %q, want clean path", location)
	}
	if response.Header().Get("Cache-Control") != "no-store, max-age=0" ||
		response.Header().Get("Pragma") != "no-cache" ||
		response.Header().Get("Referrer-Policy") != "no-referrer" {
		t.Fatalf("security headers = %#v", response.Header())
	}
	cookies := response.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookies = %#v, want one exchange cookie", cookies)
	}
	cookie := cookies[0]
	if cookie.Name != supplieraccess.AccessExchangeCookieName ||
		cookie.Value != rawExchangeToken || !cookie.HttpOnly || !cookie.Secure ||
		cookie.SameSite != http.SameSiteLaxMode ||
		cookie.Path != "/supplier-access" || cookie.Domain != "" ||
		cookie.MaxAge <= 0 || cookie.MaxAge > 600 {
		t.Fatalf("exchange cookie = %#v", cookie)
	}

	for _, forbidden := range []string{
		rawInvitationToken,
		url.QueryEscape(rawInvitationToken),
		requestPath,
	} {
		if strings.Contains(response.Body.String(), forbidden) ||
			strings.Contains(response.Header().Get("Location"), forbidden) ||
			strings.Contains(response.Header().Get("Cache-Control"), forbidden) {
			t.Fatalf("invitation credential %q leaked into response", forbidden)
		}
	}
}

func TestCleanOpenHandlerDisclosesNoInvitationIdentity(t *testing.T) {
	service := openHandlerService(
		&fakeInvitationAccess{}, &fakeAccessExchangeStore{},
		opaqueToken(1), opaqueToken(2))
	router, api := platformhttp.NewRouter("supplier-access-test", "0.0.0")
	supplieraccess.RegisterHandlers(api, service, false)

	request := httptest.NewRequest(http.MethodGet, "/supplier-access/open", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", response.Code, response.Body.String())
	}
	if contentType := response.Header().Get("Content-Type"); !strings.HasPrefix(contentType, "application/json") {
		t.Fatalf("Content-Type = %q, want application/json", contentType)
	}
	if response.Header().Get("Cache-Control") != "no-store, max-age=0" ||
		response.Header().Get("Pragma") != "no-cache" ||
		response.Header().Get("Referrer-Policy") != "no-referrer" {
		t.Fatalf("security headers = %#v", response.Header())
	}
	for _, forbidden := range []string{
		"companyId", "supplierId", "invitationId", "recipient",
		"accessGeneration", "issuedRfqVersion",
	} {
		if strings.Contains(strings.ToLower(response.Body.String()),
			strings.ToLower(forbidden)) {
			t.Fatalf("clean response leaked %q: %s", forbidden, response.Body.String())
		}
	}
}

func TestOpenHandlerUsesOneNeutralCredentialFailureAndBounded503(t *testing.T) {
	rawToken := opaqueToken(91)

	run := func(access *fakeInvitationAccess, raw string) *httptest.ResponseRecorder {
		service := openHandlerService(
			access, &fakeAccessExchangeStore{}, opaqueToken(1), opaqueToken(2))
		router, api := platformhttp.NewRouter("supplier-access-test", "0.0.0")
		supplieraccess.RegisterHandlers(api, service, false)
		request := httptest.NewRequest(http.MethodGet,
			"/supplier-access/open?token="+url.QueryEscape(raw), nil)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		return response
	}

	malformed := run(&fakeInvitationAccess{}, "malformed")
	unknown := run(&fakeInvitationAccess{found: false}, rawToken)
	if malformed.Code != http.StatusNotFound || unknown.Code != http.StatusNotFound ||
		malformed.Body.String() != unknown.Body.String() {
		t.Fatalf("malformed response %d/%q differs from unknown %d/%q",
			malformed.Code, malformed.Body.String(), unknown.Code, unknown.Body.String())
	}

	internalDetail := "mongo topology unavailable at secret-host:27017"
	infrastructure := run(&fakeInvitationAccess{err: errors.New(internalDetail)}, rawToken)
	if infrastructure.Code != http.StatusServiceUnavailable {
		t.Fatalf("infrastructure status = %d: %s",
			infrastructure.Code, infrastructure.Body.String())
	}
	if strings.Contains(infrastructure.Body.String(), internalDetail) ||
		strings.Contains(infrastructure.Body.String(), "secret-host") {
		t.Fatalf("503 leaked infrastructure detail: %s", infrastructure.Body.String())
	}
}

func TestVerifyHandlerReturnsOnlyStatusAfterSettingBoundSessionCookies(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	rig := newVerificationServiceRig(t, now)
	challengeID, code := rig.createChallenge(t, now)
	router, api := platformhttp.NewRouter("supplier-access-test", "0.0.0")
	supplieraccess.RegisterHandlers(api, rig.service, false)

	requestBody, _ := json.Marshal(map[string]string{
		"challengeId": challengeID,
		"code":        code,
		"operationId": "verify-operation-http-1",
	})
	request := httptest.NewRequest(http.MethodPost,
		"/supplier-access/challenges/verify", bytes.NewReader(requestBody))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", response.Code, response.Body.String())
	}
	if response.Header().Get("Cache-Control") != "no-store, max-age=0" ||
		response.Header().Get("Pragma") != "no-cache" ||
		response.Header().Get("Referrer-Policy") != "no-referrer" {
		t.Fatalf("security headers = %#v", response.Header())
	}
	var body map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding verification response: %v", err)
	}
	if len(body) != 1 || body["status"] != "verified" {
		t.Fatalf("verification body = %#v", body)
	}

	cookies := response.Result().Cookies()
	if len(cookies) != 2 {
		t.Fatalf("verification cookies = %#v, want session and CSRF", cookies)
	}
	var sessionCookie, csrfCookie *http.Cookie
	for _, cookie := range cookies {
		switch cookie.Name {
		case supplieraccess.SupplierSessionCookieName:
			sessionCookie = cookie
		case supplieraccess.SupplierCSRFCookieName:
			csrfCookie = cookie
		}
	}
	if sessionCookie == nil || csrfCookie == nil {
		t.Fatalf("verification cookies = %#v", cookies)
	}
	for name, cookie := range map[string]*http.Cookie{
		"session": sessionCookie,
		"csrf":    csrfCookie,
	} {
		if cookie.Value == "" || cookie.Path != "/supplier-access" ||
			cookie.Domain != "" || cookie.Secure ||
			cookie.SameSite != http.SameSiteLaxMode ||
			cookie.MaxAge <= 0 || cookie.MaxAge > 30*24*60*60 {
			t.Fatalf("%s cookie = %#v", name, cookie)
		}
	}
	if !sessionCookie.HttpOnly || csrfCookie.HttpOnly {
		t.Fatalf("session/CSRF HttpOnly flags = %v/%v",
			sessionCookie.HttpOnly, csrfCookie.HttpOnly)
	}

	session, err := rig.sessions.FindSessionByTokenHash(
		context.Background(),
		secrets.HashSupplierSessionToken(sessionCookie.Value))
	if err != nil {
		t.Fatalf("session cookie did not resolve after response: %v", err)
	}
	binding, err := rig.bindings.FindBinding(
		context.Background(), session.CompanyID, session.ID,
		rig.exchange.InvitationID)
	if err != nil ||
		binding.AccessGeneration != rig.exchange.AccessGeneration {
		t.Fatalf("binding after verification = %#v, %v", binding, err)
	}
}

func TestSupplierSessionBootstrapReturnsOnlyItsBoundInvitation(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	rig := newVerificationServiceRig(t, now)
	verified := verifySupplierSession(t, rig, now)
	router, api := platformhttp.NewRouter("supplier-access-test", "0.0.0")
	supplieraccess.RegisterHandlers(api, rig.service, false)

	request := httptest.NewRequest(http.MethodGet,
		"/supplier-access/session", nil)
	request.AddCookie(&http.Cookie{
		Name:  supplieraccess.SupplierSessionCookieName,
		Value: verified.SessionToken,
	})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", response.Code, response.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding session bootstrap: %v", err)
	}
	if len(body) != 1 || body["invitationId"] != rig.exchange.InvitationID {
		t.Fatalf("bootstrap body = %#v", body)
	}
	if response.Header().Get("Cache-Control") != "no-store, max-age=0" ||
		response.Header().Get("Pragma") != "no-cache" ||
		response.Header().Get("Referrer-Policy") != "no-referrer" {
		t.Fatalf("security headers = %#v", response.Header())
	}
	cookies := response.Result().Cookies()
	if len(cookies) != 1 ||
		cookies[0].Name != supplieraccess.SupplierSessionCookieName ||
		cookies[0].Value != verified.SessionToken || !cookies[0].HttpOnly {
		t.Fatalf("renewed session cookie = %#v", cookies)
	}
}

func TestSupplierSessionBootstrapRejectsMissingInvalidExpiredAndRevokedSessions(
	t *testing.T) {
	now := time.Now().UTC().Add(-time.Hour).Truncate(time.Millisecond)
	rig := newVerificationServiceRig(t, now)
	verified := verifySupplierSession(t, rig, now)
	router, api := platformhttp.NewRouter("supplier-access-test", "0.0.0")
	supplieraccess.RegisterHandlers(api, rig.service, false)

	requestBootstrap := func(rawToken string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodGet,
			"/supplier-access/session", nil)
		if rawToken != "" {
			request.AddCookie(&http.Cookie{
				Name: supplieraccess.SupplierSessionCookieName, Value: rawToken,
			})
		}
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		return response
	}

	for name, rawToken := range map[string]string{
		"missing": "",
		"invalid": opaqueToken(217),
	} {
		t.Run(name, func(t *testing.T) {
			response := requestBootstrap(rawToken)
			if response.Code != http.StatusNotFound {
				t.Fatalf("status = %d: %s", response.Code, response.Body.String())
			}
		})
	}

	session, err := rig.sessions.FindSession(
		context.Background(), rig.exchange.CompanyID, verified.SessionID)
	if err != nil {
		t.Fatalf("loading session fixture: %v", err)
	}
	session.SlidingExpiresAt = now.Add(2 * time.Second)
	session.Revision++
	if _, err := rig.sessions.ReplaceSessionCAS(
		context.Background(), session, session.Revision-1); err != nil {
		t.Fatalf("expiring session fixture: %v", err)
	}
	if response := requestBootstrap(verified.SessionToken); response.Code != http.StatusNotFound {
		t.Fatalf("expired status = %d: %s", response.Code, response.Body.String())
	}

	revokedRig := newVerificationServiceRig(t, now)
	revoked := verifySupplierSession(t, revokedRig, now)
	revokedAt := now.Add(time.Second)
	if err := revokedRig.service.LogoutSupplierSession(
		context.Background(), supplieraccess.LogoutSupplierSessionInput{
			SessionToken: revoked.SessionToken,
			CSRFCookie:   revoked.CSRFToken,
			CSRFHeader:   revoked.CSRFToken,
			LoggedOutAt:  revokedAt,
		}); err != nil {
		t.Fatalf("revoking session fixture: %v", err)
	}
	revokedRouter, revokedAPI := platformhttp.NewRouter(
		"supplier-access-test", "0.0.0")
	supplieraccess.RegisterHandlers(revokedAPI, revokedRig.service, false)
	request := httptest.NewRequest(http.MethodGet,
		"/supplier-access/session", nil)
	request.AddCookie(&http.Cookie{
		Name: supplieraccess.SupplierSessionCookieName, Value: revoked.SessionToken,
	})
	response := httptest.NewRecorder()
	revokedRouter.ServeHTTP(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("revoked status = %d: %s", response.Code, response.Body.String())
	}
}

func TestSupplierSessionBootstrapCannotSelectAnotherInvitation(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	rig := newVerificationServiceRig(t, now)
	verified := verifySupplierSession(t, rig, now)
	router, api := platformhttp.NewRouter("supplier-access-test", "0.0.0")
	supplieraccess.RegisterHandlers(api, rig.service, false)

	request := httptest.NewRequest(http.MethodGet,
		"/supplier-access/session?invitationId=another-invitation", nil)
	request.AddCookie(&http.Cookie{
		Name: supplieraccess.SupplierSessionCookieName, Value: verified.SessionToken,
	})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", response.Code, response.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding session bootstrap: %v", err)
	}
	if body["invitationId"] != rig.exchange.InvitationID ||
		body["invitationId"] == "another-invitation" {
		t.Fatalf("caller selected bootstrap invitation: %#v", body)
	}
}

func TestVerifyHandlerRecoveryReturnsTheSameBodyAndCredentials(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	rig := newVerificationServiceRig(t, now)
	challengeID, code := rig.createChallenge(t, now)
	router, api := platformhttp.NewRouter("supplier-access-test", "0.0.0")
	supplieraccess.RegisterHandlers(api, rig.service, false)
	body, _ := json.Marshal(map[string]string{
		"challengeId": challengeID,
		"code":        code,
		"operationId": "verify-operation-http-recovery",
	})

	send := func() *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodPost,
			"/supplier-access/challenges/verify", bytes.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		return response
	}
	first := send()
	recovered := send()
	if first.Code != http.StatusOK || recovered.Code != http.StatusOK ||
		first.Body.String() != recovered.Body.String() ||
		first.Body.String() != "{\"status\":\"verified\"}\n" {
		t.Fatalf("first/recovered responses = %d %q / %d %q",
			first.Code, first.Body.String(), recovered.Code,
			recovered.Body.String())
	}

	values := func(response *httptest.ResponseRecorder) map[string]string {
		result := map[string]string{}
		for _, cookie := range response.Result().Cookies() {
			result[cookie.Name] = cookie.Value
		}
		return result
	}
	firstCookies, recoveredCookies := values(first), values(recovered)
	if firstCookies[supplieraccess.SupplierSessionCookieName] == "" ||
		firstCookies[supplieraccess.SupplierCSRFCookieName] == "" ||
		firstCookies[supplieraccess.SupplierSessionCookieName] !=
			recoveredCookies[supplieraccess.SupplierSessionCookieName] ||
		firstCookies[supplieraccess.SupplierCSRFCookieName] !=
			recoveredCookies[supplieraccess.SupplierCSRFCookieName] {
		t.Fatalf("first/recovered cookies = %#v / %#v",
			firstCookies, recoveredCookies)
	}
	session, err := rig.sessions.FindSessionByTokenHash(
		context.Background(), secrets.HashSupplierSessionToken(
			firstCookies[supplieraccess.SupplierSessionCookieName]))
	if err != nil {
		t.Fatalf("loading recovered session: %v", err)
	}
	if session.TokenGeneration != 1 || session.Revision != 1 {
		t.Fatalf("recovery mutated session = %#v", session)
	}
}

func TestFailedVerificationNeverIssuesSessionOrCSRFCookies(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	rig := newVerificationServiceRig(t, now)
	router, api := platformhttp.NewRouter("supplier-access-test", "0.0.0")
	supplieraccess.RegisterHandlers(api, rig.service, false)
	body, _ := json.Marshal(map[string]string{
		"challengeId": opaqueToken(250),
		"code":        "123456",
		"operationId": "unknown-verification-operation",
	})
	request := httptest.NewRequest(http.MethodPost,
		"/supplier-access/challenges/verify", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d: %s", response.Code, response.Body.String())
	}
	if cookies := response.Result().Cookies(); len(cookies) != 0 {
		t.Fatalf("failed verification issued cookies = %#v", cookies)
	}
	if response.Header().Get("Cache-Control") != "no-store, max-age=0" ||
		response.Header().Get("Pragma") != "no-cache" ||
		response.Header().Get("Referrer-Policy") != "no-referrer" {
		t.Fatalf("security headers = %#v", response.Header())
	}
}

func TestLogoutHandlerReturnsEmpty204AndClearsBothCookies(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	rig := newVerificationServiceRig(t, now)
	verified := verifySupplierSession(t, rig, now)
	router, api := platformhttp.NewRouter("supplier-access-test", "0.0.0")
	supplieraccess.RegisterHandlers(api, rig.service, true)

	request := httptest.NewRequest(http.MethodPost,
		"/supplier-access/session/logout", nil)
	request.AddCookie(&http.Cookie{
		Name:  supplieraccess.SupplierSessionCookieName,
		Value: verified.SessionToken,
	})
	request.AddCookie(&http.Cookie{
		Name:  supplieraccess.SupplierCSRFCookieName,
		Value: verified.CSRFToken,
	})
	request.Header.Set("X-CSRF-Token", verified.CSRFToken)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusNoContent ||
		response.Body.Len() != 0 {
		t.Fatalf("logout response = %d/%q",
			response.Code, response.Body.String())
	}
	if response.Header().Get("Cache-Control") != "no-store, max-age=0" ||
		response.Header().Get("Pragma") != "no-cache" ||
		response.Header().Get("Referrer-Policy") != "no-referrer" {
		t.Fatalf("security headers = %#v", response.Header())
	}
	cookies := response.Result().Cookies()
	if len(cookies) != 2 {
		t.Fatalf("cleared cookies = %#v", cookies)
	}
	for _, cookie := range cookies {
		if cookie.Value != "" || cookie.MaxAge >= 0 ||
			cookie.Path != "/supplier-access" ||
			cookie.Domain != "" || !cookie.Secure ||
			cookie.SameSite != http.SameSiteLaxMode {
			t.Fatalf("cleared cookie = %#v", cookie)
		}
		switch cookie.Name {
		case supplieraccess.SupplierSessionCookieName:
			if !cookie.HttpOnly {
				t.Fatalf("cleared session cookie = %#v", cookie)
			}
		case supplieraccess.SupplierCSRFCookieName:
			if cookie.HttpOnly {
				t.Fatalf("cleared CSRF cookie = %#v", cookie)
			}
		default:
			t.Fatalf("unexpected cleared cookie = %#v", cookie)
		}
	}

	session, err := rig.sessions.FindSession(
		context.Background(), rig.exchange.CompanyID, verified.SessionID)
	if err != nil || session.RevokedAt == nil {
		t.Fatalf("session after logout = %#v, %v", session, err)
	}
}

func TestUnknownSessionLogoutUsesTheSame204CleanupResponse(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	rig := newVerificationServiceRig(t, now)
	router, api := platformhttp.NewRouter("supplier-access-test", "0.0.0")
	supplieraccess.RegisterHandlers(api, rig.service, false)
	csrf := opaqueToken(231)
	request := httptest.NewRequest(http.MethodPost,
		"/supplier-access/session/logout", nil)
	request.AddCookie(&http.Cookie{
		Name: supplieraccess.SupplierSessionCookieName, Value: opaqueToken(232),
	})
	request.AddCookie(&http.Cookie{
		Name: supplieraccess.SupplierCSRFCookieName, Value: csrf,
	})
	request.Header.Set("X-CSRF-Token", csrf)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusNoContent || response.Body.Len() != 0 {
		t.Fatalf("unknown logout response = %d/%q",
			response.Code, response.Body.String())
	}
	cookies := response.Result().Cookies()
	if len(cookies) != 2 {
		t.Fatalf("unknown logout cleared cookies = %#v", cookies)
	}
	for _, cookie := range cookies {
		if cookie.Value != "" || cookie.MaxAge >= 0 {
			t.Fatalf("unknown logout cookie = %#v", cookie)
		}
	}
}

func TestLogoutHandlerCSRF403DoesNotRevokeOrClearCookies(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Millisecond)
	rig := newVerificationServiceRig(t, now)
	verified := verifySupplierSession(t, rig, now)
	router, api := platformhttp.NewRouter("supplier-access-test", "0.0.0")
	supplieraccess.RegisterHandlers(api, rig.service, false)

	request := httptest.NewRequest(http.MethodPost,
		"/supplier-access/session/logout", nil)
	request.AddCookie(&http.Cookie{
		Name:  supplieraccess.SupplierSessionCookieName,
		Value: verified.SessionToken,
	})
	request.AddCookie(&http.Cookie{
		Name:  supplieraccess.SupplierCSRFCookieName,
		Value: verified.CSRFToken,
	})
	request.Header.Set("X-CSRF-Token", opaqueToken(222))
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d: %s", response.Code, response.Body.String())
	}
	if response.Header().Get("Cache-Control") != "no-store, max-age=0" ||
		response.Header().Get("Pragma") != "no-cache" ||
		response.Header().Get("Referrer-Policy") != "no-referrer" {
		t.Fatalf("security headers = %#v", response.Header())
	}
	if cookies := response.Result().Cookies(); len(cookies) != 0 {
		t.Fatalf("CSRF rejection cleared cookies = %#v", cookies)
	}
	session, err := rig.sessions.FindSession(
		context.Background(), rig.exchange.CompanyID, verified.SessionID)
	if err != nil {
		t.Fatalf("loading session after CSRF rejection: %v", err)
	}
	if session.RevokedAt != nil || session.Revision != 1 {
		t.Fatalf("CSRF rejection mutated session = %#v", session)
	}
}

func TestSupplierRouteValidationErrorsCarryRevision14SecurityHeaders(t *testing.T) {
	router, api := platformhttp.NewRouter("supplier-access-test", "0.0.0")
	supplieraccess.RegisterHandlers(api, nil, false)
	request := httptest.NewRequest(http.MethodPost,
		"/supplier-access/challenges/verify",
		bytes.NewBufferString(`{}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("validation status = %d: %s",
			response.Code, response.Body.String())
	}
	if response.Header().Get("Cache-Control") != "no-store, max-age=0" ||
		response.Header().Get("Pragma") != "no-cache" ||
		response.Header().Get("Referrer-Policy") != "no-referrer" {
		t.Fatalf("validation security headers = %#v", response.Header())
	}
}
