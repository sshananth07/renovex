package supplieraccess_test

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/shananth/renovation-platform/backend/internal/platform/secrets"
	"github.com/shananth/renovation-platform/backend/internal/supplieraccess"
)

type fakeInvitationAccess struct {
	snapshot   supplieraccess.InvitationAccessSnapshot
	found      bool
	err        error
	validateFn func(
		companyID, invitationID string, accessedAt time.Time,
	) (supplieraccess.InvitationAccessSnapshot, bool, error)

	resolveCalls  int
	gotHash       string
	gotAt         time.Time
	validateCalls int
	gotValidate   struct {
		companyID, invitationID string
		at                      time.Time
	}
	viewCalls int
	gotView   struct {
		companyID, invitationID string
		generation              int64
		at                      time.Time
	}
}

func (f *fakeInvitationAccess) ValidateInvitationAccess(
	_ context.Context, companyID, invitationID string, accessedAt time.Time,
) (supplieraccess.InvitationAccessSnapshot, bool, error) {
	f.validateCalls++
	f.gotValidate.companyID = companyID
	f.gotValidate.invitationID = invitationID
	f.gotValidate.at = accessedAt
	if f.validateFn != nil {
		return f.validateFn(companyID, invitationID, accessedAt)
	}
	return f.snapshot, f.found, f.err
}

func (f *fakeInvitationAccess) ResolveInvitationAccess(
	_ context.Context, hash string, accessedAt time.Time,
) (supplieraccess.InvitationAccessSnapshot, bool, error) {
	f.resolveCalls++
	f.gotHash = hash
	f.gotAt = accessedAt
	return f.snapshot, f.found, f.err
}

func (f *fakeInvitationAccess) RecordInvitationViewed(
	_ context.Context, companyID, invitationID string,
	accessGeneration int64, viewedAt time.Time,
) error {
	f.viewCalls++
	f.gotView.companyID = companyID
	f.gotView.invitationID = invitationID
	f.gotView.generation = accessGeneration
	f.gotView.at = viewedAt
	return f.err
}

type fakeAccessExchangeStore struct {
	createCalls int
	created     supplieraccess.SupplierAccessExchange
	err         error
}

func (f *fakeAccessExchangeStore) CreateExchange(
	_ context.Context, exchange supplieraccess.SupplierAccessExchange,
) (supplieraccess.SupplierAccessExchange, error) {
	f.createCalls++
	f.created = exchange
	return exchange, f.err
}

type sequenceOpaqueTokenGenerator struct {
	values []string
	calls  int
}

func (g *sequenceOpaqueTokenGenerator) Generate() (string, error) {
	if g.calls >= len(g.values) {
		return "", errors.New("test token sequence exhausted")
	}
	value := g.values[g.calls]
	g.calls++
	return value, nil
}

func opaqueToken(fill byte) string {
	return base64.RawURLEncoding.EncodeToString(bytesOf(fill, 32))
}

func bytesOf(fill byte, count int) []byte {
	result := make([]byte, count)
	for i := range result {
		result[i] = fill + byte(i)
	}
	return result
}

func TestOpenInvitationValidatesEncodingBeforeAnyResolverOrStoreCall(t *testing.T) {
	access := &fakeInvitationAccess{}
	store := &fakeAccessExchangeStore{}
	service := supplieraccess.NewService(
		supplieraccess.WithInvitationAccess(access, access),
		supplieraccess.WithAccessExchangeStore(store),
		supplieraccess.WithOpaqueTokenGenerator(
			&sequenceOpaqueTokenGenerator{values: []string{opaqueToken(1), opaqueToken(2)}}),
	)
	now := time.Date(2026, 7, 29, 19, 0, 0, 0, time.UTC)

	for _, malformed := range []string{
		"", "short", strings.Repeat("a", 42), strings.Repeat("a", 44),
		strings.Repeat("!", 43), opaqueToken(3) + "=",
	} {
		if _, err := service.OpenInvitation(
			context.Background(), malformed, now); !errors.Is(
			err, supplieraccess.ErrInvalidSupplierCredential) {
			t.Errorf("token %q error = %v", malformed, err)
		}
	}
	if access.resolveCalls != 0 || access.viewCalls != 0 || store.createCalls != 0 {
		t.Fatalf("malformed tokens reached collaborators: resolve/view/store = %d/%d/%d",
			access.resolveCalls, access.viewCalls, store.createCalls)
	}
}

func TestOpenInvitationPassesOnlyHashAndPersistsOnlyTheExchangeHash(t *testing.T) {
	rawInvitationToken := opaqueToken(31)
	fullURL := "https://api.test/supplier-access/open?token=" +
		url.QueryEscape(rawInvitationToken)
	now := time.Date(2026, 7, 29, 19, 0, 0, 0, time.UTC)
	access := &fakeInvitationAccess{
		found: true,
		snapshot: supplieraccess.InvitationAccessSnapshot{
			CompanyID: "company-1", SupplierID: "supplier-1",
			InvitationID:              "invitation-1",
			NormalizedRecipientEmail:  "recipient@supplier.test",
			AccessGeneration:          7,
			CurrentIssuedRFQVersionID: "version-3",
		},
	}
	store := &fakeAccessExchangeStore{}
	exchangeID := opaqueToken(41)
	rawExchangeToken := opaqueToken(51)
	service := supplieraccess.NewService(
		supplieraccess.WithInvitationAccess(access, access),
		supplieraccess.WithAccessExchangeStore(store),
		supplieraccess.WithOpaqueTokenGenerator(
			&sequenceOpaqueTokenGenerator{values: []string{exchangeID, rawExchangeToken}}),
	)

	result, err := service.OpenInvitation(
		context.Background(), rawInvitationToken, now)
	if err != nil {
		t.Fatalf("opening invitation: %v", err)
	}

	if access.gotHash != secrets.HashInvitationSecret(rawInvitationToken) {
		t.Fatalf("resolver received %q, want only the canonical hash", access.gotHash)
	}
	if access.gotHash == rawInvitationToken {
		t.Fatal("resolver received the raw invitation token")
	}
	if access.viewCalls != 1 || access.gotView.generation != 7 ||
		!access.gotView.at.Equal(now) {
		t.Fatalf("view recording = %#v", access.gotView)
	}
	if store.created.ID != exchangeID ||
		store.created.ExchangeTokenHash != supplieraccess.HashAccessExchangeToken(rawExchangeToken) ||
		!store.created.ExpiresAt.Equal(now.Add(10*time.Minute)) {
		t.Fatalf("persisted exchange = %#v", store.created)
	}
	if result.ExchangeToken != rawExchangeToken ||
		!result.ExpiresAt.Equal(store.created.ExpiresAt) {
		t.Fatalf("open result = %#v", result)
	}

	persisted := strings.ToLower(fmt.Sprintf("%#v", store.created))
	for _, forbidden := range []string{
		strings.ToLower(rawInvitationToken),
		strings.ToLower(fullURL),
		strings.ToLower(url.QueryEscape(rawInvitationToken)),
	} {
		if strings.Contains(persisted, forbidden) {
			t.Fatalf("persisted exchange contains invitation credential %q: %s",
				forbidden, persisted)
		}
	}
}

func TestOpenInvitationCollapsesUnknownAccessWithoutWritingViewOrExchange(t *testing.T) {
	access := &fakeInvitationAccess{found: false}
	store := &fakeAccessExchangeStore{}
	service := supplieraccess.NewService(
		supplieraccess.WithInvitationAccess(access, access),
		supplieraccess.WithAccessExchangeStore(store),
		supplieraccess.WithOpaqueTokenGenerator(
			&sequenceOpaqueTokenGenerator{values: []string{opaqueToken(1), opaqueToken(2)}}),
	)

	if _, err := service.OpenInvitation(context.Background(), opaqueToken(9),
		time.Now()); !errors.Is(err, supplieraccess.ErrInvalidSupplierCredential) {
		t.Fatalf("unknown invitation error = %v", err)
	}
	if access.viewCalls != 0 || store.createCalls != 0 {
		t.Fatalf("unknown invitation wrote view/exchange = %d/%d",
			access.viewCalls, store.createCalls)
	}
}
