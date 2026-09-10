package companies

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/shananth/renovation-platform/backend/internal/foundation/pagination"
	"github.com/shananth/renovation-platform/backend/internal/platform/mail"
)

// MemberSortFields is the sort allowlist for GET /companies/me/members.
var MemberSortFields = []string{"createdAt", "role", "userId"}

const (
	MemberDefaultSort  = "createdAt"
	MemberDefaultOrder = pagination.OrderDesc
)

// Principal mirrors identity.Principal's shape using only primitive types, so
// companies never imports identity. Handlers construct this from
// identity.Principal at the HTTP boundary (cmd/api wiring / handler layer),
// not by importing identity's type directly into this package.
type Principal struct {
	UserID    string
	CompanyID string
	Role      string
}

// ErrForbidden is returned when a Principal's role does not permit the
// requested action (e.g. an employee attempting to add a member).
var ErrForbidden = errors.New("companies: forbidden for this role")

// ErrInvalidRole is returned when an operation is given a Role it does not accept
// (e.g. attempting to create a second owner via AddMember).
var ErrInvalidRole = errors.New("companies: invalid role for this operation")

// UserProvisioner is the capability companies needs from identity: finding or
// creating a User by email, and deleting a User that this package itself
// provisioned (compensation only — never used to delete a pre-existing User).
// Defined here (consumer-defines-interface); satisfied structurally by
// identity.UserService with no import in either direction.
type UserProvisioner interface {
	FindOrCreateUser(ctx context.Context, email string) (userID, tempPassword string, created bool, err error)
	DeleteProvisionedUser(ctx context.Context, userID string) error
}

// Service implements company/membership CRUD, AddMember authorization, and the
// two capability interfaces identity.AuthService depends on
// (CompanyProvisioner, MembershipLookup). It depends directly on
// platform/mail.EmailSender — platform packages have zero domain knowledge, so
// domain modules depending on them (unlike domain-to-domain imports) does not
// violate ADR 0002's module boundaries.
type Service struct {
	companyRepo    CompanyRepository
	membershipRepo MembershipRepository
	users          UserProvisioner
	mailer         mail.EmailSender
}

// NewService constructs a Service. users and mailer are the only dependencies
// reaching outside this package's own collections.
func NewService(companyRepo CompanyRepository, membershipRepo MembershipRepository, users UserProvisioner, mailer mail.EmailSender) *Service {
	return &Service{companyRepo: companyRepo, membershipRepo: membershipRepo, users: users, mailer: mailer}
}

// CreateOwnerCompany creates a Company and an owner CompanyMembership for
// userID. If Membership creation fails, the just-created Company is deleted
// before returning — companies compensates within its own collections only.
// Satisfies identity.CompanyProvisioner structurally.
func (s *Service) CreateOwnerCompany(ctx context.Context, userID, companyName string) (string, error) {
	company, err := s.companyRepo.Create(ctx, Company{Name: companyName, CreatedAt: time.Now()})
	if err != nil {
		return "", err
	}

	_, err = s.membershipRepo.Create(ctx, CompanyMembership{
		UserID: userID, CompanyID: company.ID, Role: RoleOwner, CreatedAt: time.Now(),
	})
	if err != nil {
		if delErr := s.companyRepo.Delete(ctx, company.ID); delErr != nil {
			// Compensation failure: log with both IDs. Milestone 1 accepts this
			// as a known gap (see design doc §9) — no transaction backstop yet.
			fmt.Printf("companies: compensation failed deleting company %s after membership-create error: %v (original error: %v)\n", company.ID, delErr, err)
		}
		return "", err
	}

	return company.ID, nil
}

// DeleteProvisionedCompany deletes companyID's Membership for ownerUserID, then
// the Company itself. Used only by identity.AuthService's registration
// compensation when a later step (AuthSession creation) fails. Satisfies
// identity.CompanyProvisioner structurally.
func (s *Service) DeleteProvisionedCompany(ctx context.Context, companyID, ownerUserID string) error {
	if err := s.membershipRepo.DeleteByUserID(ctx, ownerUserID); err != nil && err != ErrMembershipNotFound {
		return err
	}
	if err := s.companyRepo.Delete(ctx, companyID); err != nil && err != ErrCompanyNotFound {
		return err
	}
	return nil
}

// DeleteMembershipByUserID removes userID's CompanyMembership.
// Development-tool use only (demoseed reset) — no production code path
// calls this directly (AddMember/registration use compensation paths, not
// this). Does not swallow ErrMembershipNotFound — the caller decides
// whether "already deleted" is acceptable, matching every other
// repository-backed method in this file.
func (s *Service) DeleteMembershipByUserID(ctx context.Context, userID string) error {
	return s.membershipRepo.DeleteByUserID(ctx, userID)
}

// DeleteCompany removes companyID's Company record. Development-tool use
// only (demoseed reset) — no production code path calls this directly.
func (s *Service) DeleteCompany(ctx context.Context, companyID string) error {
	return s.companyRepo.Delete(ctx, companyID)
}

// FindMembershipByUserID returns companyID and role (as a raw string) for
// userID. Satisfies identity.MembershipLookup structurally.
func (s *Service) FindMembershipByUserID(ctx context.Context, userID string) (string, string, error) {
	membership, err := s.membershipRepo.FindByUserID(ctx, userID)
	if err != nil {
		return "", "", err
	}
	return membership.CompanyID, string(membership.Role), nil
}

// LookupCurrentMembership returns userID's current Company (id + name) and
// Role in one call, read fresh from both repositories rather than cached or
// trusted from a token claim. Satisfies identity.CurrentMembershipLookup
// structurally, for GET /auth/me — the one place identity needs the Company
// display name in addition to what FindMembershipByUserID already returns.
func (s *Service) LookupCurrentMembership(ctx context.Context, userID string) (
	companyID, companyName, role string, err error,
) {
	membership, err := s.membershipRepo.FindByUserID(ctx, userID)
	if err != nil {
		return "", "", "", err
	}
	company, err := s.companyRepo.FindByID(ctx, membership.CompanyID)
	if err != nil {
		return "", "", "", err
	}
	return company.ID, company.Name, string(membership.Role), nil
}

// GetCompany returns companyID's Company record.
func (s *Service) GetCompany(ctx context.Context, companyID string) (Company, error) {
	return s.companyRepo.FindByID(ctx, companyID)
}

// FindCompanyByName looks up a Company by its exact display name, or
// ErrCompanyNotFound. Used only by demoseed's positive-identification
// checks (design spec §6.2) — no production endpoint needs this.
func (s *Service) FindCompanyByName(ctx context.Context, name string) (Company, error) {
	return s.companyRepo.FindByName(ctx, name)
}

// GetCompanyName returns companyID's display name. Satisfies
// access.CompanyNameLookup structurally (M6 design spec §14) — it returns the
// bare name rather than a Company struct, so no domain type crosses the
// module boundary (ADR 0002). This is the only contractor identity the
// Client-facing quotation view exposes.
func (s *Service) GetCompanyName(ctx context.Context, companyID string) (string, error) {
	c, err := s.companyRepo.FindByID(ctx, companyID)
	if err != nil {
		return "", err
	}
	return c.Name, nil
}

// ListMembersPaginated returns companyID's CompanyMemberships matching req,
// plus the total matching count.
func (s *Service) ListMembersPaginated(ctx context.Context, companyID string, req pagination.Request) ([]CompanyMembership, int, error) {
	return s.membershipRepo.ListPaginated(ctx, companyID, req)
}

// AddMember adds email (with role) to principal.CompanyID. Only owner/admin
// principals may call this; role must be admin or employee (never owner via
// this path). If a new User is provisioned and Membership creation then fails,
// the just-provisioned User is deleted. If email delivery fails after
// Membership creation succeeds, the User and Membership are both kept and the
// delivery failure is returned as a distinct error the caller can report
// without implying the whole operation failed.
func (s *Service) AddMember(ctx context.Context, principal Principal, email string, role Role) error {
	if principal.Role != string(RoleOwner) && principal.Role != string(RoleAdmin) {
		return ErrForbidden
	}
	if role != RoleAdmin && role != RoleEmployee {
		return ErrInvalidRole
	}

	userID, tempPassword, created, err := s.users.FindOrCreateUser(ctx, email)
	if err != nil {
		return err
	}

	if _, err := s.membershipRepo.FindByUserID(ctx, userID); err == nil {
		return ErrUserAlreadyHasMembership
	} else if err != ErrMembershipNotFound {
		return err
	}

	_, err = s.membershipRepo.Create(ctx, CompanyMembership{
		UserID: userID, CompanyID: principal.CompanyID, Role: role, CreatedAt: time.Now(),
	})
	if err != nil {
		if created {
			if delErr := s.users.DeleteProvisionedUser(ctx, userID); delErr != nil {
				fmt.Printf("companies: compensation failed deleting user %s after membership-create error: %v (original error: %v)\n", userID, delErr, err)
			}
		}
		return err
	}

	if created {
		sendErr := s.mailer.Send(ctx, mail.Message{
			To:      email,
			Subject: "You've been added to a company on Renovation Project Intelligence Platform",
			Body:    fmt.Sprintf("Your temporary password is: %s\n\nYou will be asked to change it on first login.", tempPassword),
		})
		if sendErr != nil {
			return fmt.Errorf("companies: member added but temp-password email delivery failed: %w", sendErr)
		}
	}

	return nil
}
