package clients

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/shananth/renovation-platform/backend/internal/foundation/pagination"
)

// ErrNameRequired is returned when CreateClient or UpdateClient is given an empty name.
var ErrNameRequired = errors.New("clients: name is required")

// ClientSortFields is the sort allowlist for GET /clients — enforced by
// pagination.ParseRequest so a caller cannot sort by an arbitrary Mongo
// field. ClientDefaultSort/ClientDefaultOrder is the endpoint's default when
// neither sort nor order is supplied.
var ClientSortFields = []string{"createdAt", "name"}

const (
	ClientDefaultSort  = "createdAt"
	ClientDefaultOrder = pagination.OrderDesc
)

// Service implements Client CRUD and exposes ClientLookup, the one capability
// projects.Service needs from this module.
type Service struct {
	repo ClientRepository
}

// NewService constructs a Service backed by repo.
func NewService(repo ClientRepository) *Service {
	return &Service{repo: repo}
}

// companyBulkDeleter is a private, unexported capability — deliberately
// NOT part of the public ClientRepository interface (see the Mongo
// repository's DeleteAllForCompany doc comment). Only the real Mongo
// repository implements it; a fake ClientRepository used in unrelated
// tests simply does not satisfy this interface and is unaffected.
type companyBulkDeleter interface {
	DeleteAllForCompany(ctx context.Context, companyID string) error
}

// DeleteAllForCompany permanently removes every Client owned by companyID.
// Development-tool use only (demoseed reset, design spec §6.6) — no
// production code path calls this. Idempotent: calling it when nothing
// remains for companyID is a no-op success, not an error.
//
// Reaches the underlying bulk-delete capability via a runtime type
// assertion against s.repo (typed as the public ClientRepository
// interface) rather than a method on that interface itself — see the
// companyBulkDeleter doc comment. Returns an error if s.repo was
// constructed with something other than the real Mongo repository (e.g. a
// fake in an unrelated test), since DeleteAllForCompany has no meaningful
// fallback in that case and demoseed only ever runs against the real
// repository.
func (s *Service) DeleteAllForCompany(ctx context.Context, companyID string) error {
	deleter, ok := s.repo.(companyBulkDeleter)
	if !ok {
		return fmt.Errorf("clients: repository %T does not support DeleteAllForCompany", s.repo)
	}
	return deleter.DeleteAllForCompany(ctx, companyID)
}

// CreateClient validates name is non-empty and persists a new Client scoped to
// companyID. Phone/email/address/billingAddress/notes are all optional (phase1.md §4
// does not mark them required).
func (s *Service) CreateClient(ctx context.Context, companyID, name, phone, email, address, billingAddress, notes string) (Client, error) {
	if name == "" {
		return Client{}, ErrNameRequired
	}
	return s.repo.Create(ctx, Client{
		CompanyID: companyID, Name: name, Phone: phone, Email: email,
		Address: address, BillingAddress: billingAddress, Notes: notes,
		CreatedAt: time.Now(), SchemaVersion: 1,
	})
}

// GetClient returns clientID's Client, tenant-scoped to companyID.
func (s *Service) GetClient(ctx context.Context, companyID, clientID string) (Client, error) {
	return s.repo.FindByID(ctx, companyID, clientID)
}

// ListClientsPaginated returns companyID's Clients matching req, plus the
// total count of matching documents (independent of page/pageSize).
func (s *Service) ListClientsPaginated(ctx context.Context, companyID string, req pagination.Request) ([]Client, int, error) {
	return s.repo.ListPaginated(ctx, companyID, req)
}

// ClientPatch carries a genuine partial update for UpdateClient: a nil field
// means "omitted, leave unchanged"; a non-nil field (including a pointer to
// "") means "the caller supplied this value, apply it exactly" — an empty
// string explicitly clears an optional field. Name is validated non-empty
// (after trimming) only when supplied; omitting Name leaves the stored name
// untouched.
type ClientPatch struct {
	Name           *string
	Phone          *string
	Email          *string
	Address        *string
	BillingAddress *string
	Notes          *string
}

// UpdateClient applies patch to clientID's fields, tenant-scoped to
// companyID. Validation happens before any repository write, so a rejected
// patch (e.g. an empty Name) never partially persists.
func (s *Service) UpdateClient(ctx context.Context, companyID, clientID string, patch ClientPatch) (Client, error) {
	if patch.Name != nil && strings.TrimSpace(*patch.Name) == "" {
		return Client{}, ErrNameRequired
	}
	return s.repo.Update(ctx, companyID, clientID, func(c *Client) {
		if patch.Name != nil {
			c.Name = *patch.Name
		}
		if patch.Phone != nil {
			c.Phone = *patch.Phone
		}
		if patch.Email != nil {
			c.Email = *patch.Email
		}
		if patch.Address != nil {
			c.Address = *patch.Address
		}
		if patch.BillingAddress != nil {
			c.BillingAddress = *patch.BillingAddress
		}
		if patch.Notes != nil {
			c.Notes = *patch.Notes
		}
	})
}

// ClientBelongsToCompany reports whether clientID exists and belongs to companyID.
// Satisfies projects.ClientLookup structurally. Returns (false, nil) — not an
// error — for both "does not exist" and "belongs to another company," since
// callers only ever need the boolean, never the Client struct itself (ADR 0002:
// no domain-type leakage across module boundaries).
func (s *Service) ClientBelongsToCompany(ctx context.Context, companyID, clientID string) (bool, error) {
	_, err := s.repo.FindByID(ctx, companyID, clientID)
	if err == ErrClientNotFound {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}
