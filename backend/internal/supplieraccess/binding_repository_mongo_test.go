package supplieraccess_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shananth/renovation-platform/backend/internal/supplieraccess"
)

func bindingFixture(now time.Time, id, invitationID string) supplieraccess.SupplierSessionInvitationBinding {
	return supplieraccess.SupplierSessionInvitationBinding{
		ID:                       id,
		CompanyID:                "company-1",
		SupplierSessionID:        "session-1",
		SupplierID:               "supplier-1",
		NormalizedRecipientEmail: "recipient@supplier.test",
		InvitationID:             invitationID,
		AccessGeneration:         1,
		BoundAt:                  now,
		LastValidatedAt:          now,
		Revision:                 1,
	}
}

func TestBindingRepositoryLetsOneSessionBindToInvitationsAAndB(t *testing.T) {
	db := setupDB(t)
	repo := supplieraccess.NewMongoSessionInvitationBindingRepository(db)
	ctx := context.Background()
	if err := repo.EnsureIndexes(ctx); err != nil {
		t.Fatalf("ensuring indexes: %v", err)
	}
	now := time.Date(2026, 7, 29, 18, 0, 0, 0, time.UTC)
	first := bindingFixture(now, "binding-1", "invitation-a")
	second := bindingFixture(now, "binding-2", "invitation-b")
	if _, err := repo.CreateBinding(ctx, first); err != nil {
		t.Fatalf("binding invitation A: %v", err)
	}
	if _, err := repo.CreateBinding(ctx, second); err != nil {
		t.Fatalf("binding invitation B: %v", err)
	}

	duplicate := first
	duplicate.ID = "binding-duplicate"
	if _, err := repo.CreateBinding(ctx, duplicate); !errors.Is(
		err, supplieraccess.ErrSessionInvitationBindingAlreadyExists) {
		t.Fatalf("duplicate session/invitation error = %v", err)
	}
	bindings, err := repo.ListSessionBindings(ctx, first.CompanyID, first.SupplierSessionID)
	if err != nil {
		t.Fatalf("listing bindings: %v", err)
	}
	if len(bindings) != 2 {
		t.Fatalf("session binding count = %d, want 2", len(bindings))
	}
}

func TestBindingRepositoryConcurrentGenerationUpdateHasOneWinner(t *testing.T) {
	db := setupDB(t)
	repo := supplieraccess.NewMongoSessionInvitationBindingRepository(db)
	ctx := context.Background()
	if err := repo.EnsureIndexes(ctx); err != nil {
		t.Fatalf("ensuring indexes: %v", err)
	}
	now := time.Date(2026, 7, 29, 18, 0, 0, 0, time.UTC)
	binding := bindingFixture(now, "binding-1", "invitation-a")
	if _, err := repo.CreateBinding(ctx, binding); err != nil {
		t.Fatalf("creating binding: %v", err)
	}

	start := make(chan struct{})
	var winners atomic.Int32
	var conflicts atomic.Int32
	var wait sync.WaitGroup
	for worker := 0; worker < 2; worker++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			candidate := binding
			candidate.AccessGeneration = 2
			candidate.LastValidatedAt = now.Add(time.Minute)
			candidate.Revision = 2
			_, err := repo.ReplaceBindingCAS(ctx, candidate, 1)
			switch {
			case err == nil:
				winners.Add(1)
			case errors.Is(err, supplieraccess.ErrSessionInvitationBindingConflict):
				conflicts.Add(1)
			default:
				t.Errorf("generation update error: %v", err)
			}
		}()
	}
	close(start)
	wait.Wait()

	if winners.Load() != 1 || conflicts.Load() != 1 {
		t.Fatalf("winners/conflicts = %d/%d, want 1/1", winners.Load(), conflicts.Load())
	}
	loaded, err := repo.FindBinding(
		ctx, binding.CompanyID, binding.SupplierSessionID, binding.InvitationID)
	if err != nil {
		t.Fatalf("loading binding: %v", err)
	}
	if loaded.AccessGeneration != 2 || loaded.Revision != 2 {
		t.Fatalf("winning binding = %#v", loaded)
	}
}

func TestBindingRepositoryConcurrentCreationProducesOneDocument(t *testing.T) {
	db := setupDB(t)
	repo := supplieraccess.NewMongoSessionInvitationBindingRepository(db)
	ctx := context.Background()
	if err := repo.EnsureIndexes(ctx); err != nil {
		t.Fatalf("ensuring indexes: %v", err)
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	start := make(chan struct{})
	var winners atomic.Int32
	var duplicates atomic.Int32
	var wait sync.WaitGroup
	for worker := 0; worker < 2; worker++ {
		wait.Add(1)
		go func(worker int) {
			defer wait.Done()
			<-start
			binding := bindingFixture(
				now, "binding-"+string(rune('a'+worker)), "invitation-a")
			_, err := repo.CreateBinding(ctx, binding)
			switch {
			case err == nil:
				winners.Add(1)
			case errors.Is(
				err, supplieraccess.ErrSessionInvitationBindingAlreadyExists):
				duplicates.Add(1)
			default:
				t.Errorf("binding create %d: %v", worker, err)
			}
		}(worker)
	}
	close(start)
	wait.Wait()

	if winners.Load() != 1 || duplicates.Load() != 1 {
		t.Fatalf("winners/duplicates = %d/%d, want 1/1",
			winners.Load(), duplicates.Load())
	}
	bindings, err := repo.ListSessionBindings(
		ctx, "company-1", "session-1")
	if err != nil || len(bindings) != 1 {
		t.Fatalf("persisted bindings = %#v/%v", bindings, err)
	}
}

func TestBindingRepositoryCannotMoveBindingAcrossCompanyOrSupplier(t *testing.T) {
	db := setupDB(t)
	repo := supplieraccess.NewMongoSessionInvitationBindingRepository(db)
	ctx := context.Background()
	if err := repo.EnsureIndexes(ctx); err != nil {
		t.Fatalf("ensuring indexes: %v", err)
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	original := bindingFixture(now, "binding-1", "invitation-a")
	if _, err := repo.CreateBinding(ctx, original); err != nil {
		t.Fatalf("creating binding: %v", err)
	}
	for name, mutate := range map[string]func(*supplieraccess.SupplierSessionInvitationBinding){
		"company": func(binding *supplieraccess.SupplierSessionInvitationBinding) {
			binding.CompanyID = "company-2"
		},
		"supplier": func(binding *supplieraccess.SupplierSessionInvitationBinding) {
			binding.SupplierID = "supplier-2"
		},
	} {
		t.Run(name, func(t *testing.T) {
			candidate := original
			mutate(&candidate)
			candidate.AccessGeneration = 2
			candidate.Revision = 2
			if _, err := repo.ReplaceBindingCAS(
				ctx, candidate, 1); !errors.Is(
				err, supplieraccess.ErrSessionInvitationBindingConflict) {
				t.Fatalf("cross-%s update error = %v", name, err)
			}
		})
	}
	unchanged, err := repo.FindBinding(
		ctx, original.CompanyID, original.SupplierSessionID,
		original.InvitationID)
	if err != nil || unchanged.SupplierID != original.SupplierID ||
		unchanged.AccessGeneration != original.AccessGeneration ||
		unchanged.Revision != original.Revision {
		t.Fatalf("cross-identity attempt mutated binding: %#v/%v",
			unchanged, err)
	}
}
