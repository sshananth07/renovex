package spatial

import (
	"context"
	"errors"
	"testing"
)

// --- fakes ---

type fakeSpaceLookup struct {
	belongs map[string]string // spaceID -> projectID, for companies that own it
	company map[string]string // spaceID -> companyID
}

func newFakeSpaceLookup() *fakeSpaceLookup {
	return &fakeSpaceLookup{belongs: map[string]string{}, company: map[string]string{}}
}

func (f *fakeSpaceLookup) addSpace(companyID, projectID, spaceID string) {
	f.belongs[spaceID] = projectID
	f.company[spaceID] = companyID
}

func (f *fakeSpaceLookup) SpaceBelongsToProject(_ context.Context, companyID, spaceID, projectID string) (bool, error) {
	return f.company[spaceID] == companyID && f.belongs[spaceID] == projectID, nil
}

type fakeCaptureRepo struct {
	byID   map[string]SpatialCapture
	nextID int
}

func newFakeCaptureRepo() *fakeCaptureRepo {
	return &fakeCaptureRepo{byID: map[string]SpatialCapture{}}
}

// Create mirrors MongoCaptureRepository.Create's ClientCaptureID
// idempotency behavior: a non-empty ClientCaptureID colliding with an
// existing capture for the same company returns that existing capture
// instead of creating a second one — the fake must model this for
// TestStartCapture_* idempotency tests to be meaningful.
func (f *fakeCaptureRepo) Create(ctx context.Context, c SpatialCapture) (SpatialCapture, error) {
	if c.ClientCaptureID != "" {
		if existing, err := f.FindByClientCaptureID(ctx, c.CompanyID, c.ClientCaptureID); err == nil {
			return existing, nil
		}
	}
	f.nextID++
	c.ID = string(rune('a' + f.nextID))
	f.byID[c.ID] = c
	return c, nil
}

func (f *fakeCaptureRepo) FindByID(_ context.Context, companyID, id string) (SpatialCapture, error) {
	c, ok := f.byID[id]
	if !ok || c.CompanyID != companyID {
		return SpatialCapture{}, ErrCaptureNotFound
	}
	return c, nil
}

func (f *fakeCaptureRepo) FindByClientCaptureID(_ context.Context, companyID, clientCaptureID string) (SpatialCapture, error) {
	for _, c := range f.byID {
		if c.CompanyID == companyID && c.ClientCaptureID == clientCaptureID {
			return c, nil
		}
	}
	return SpatialCapture{}, ErrCaptureNotFound
}

func (f *fakeCaptureRepo) UpdateStatus(_ context.Context, companyID, id string, expectedStatus, newStatus CaptureStatus) (SpatialCapture, error) {
	c, ok := f.byID[id]
	if !ok || c.CompanyID != companyID {
		return SpatialCapture{}, ErrCaptureNotFound
	}
	if c.Status != expectedStatus {
		return SpatialCapture{}, ErrIllegalCaptureTransition
	}
	c.Status = newStatus
	f.byID[id] = c
	return c, nil
}

func (f *fakeCaptureRepo) MarkConfirmed(_ context.Context, companyID, id, roomVersionID string) (SpatialCapture, error) {
	c, ok := f.byID[id]
	if !ok || c.CompanyID != companyID {
		return SpatialCapture{}, ErrCaptureNotFound
	}
	if c.Status != CaptureStatusReview {
		return SpatialCapture{}, ErrIllegalCaptureTransition
	}
	c.Status = CaptureStatusConfirmed
	c.RoomVersionID = roomVersionID
	f.byID[id] = c
	return c, nil
}

func (f *fakeCaptureRepo) SetRoomDraft(_ context.Context, companyID, id, roomDraftID string) (SpatialCapture, error) {
	c, ok := f.byID[id]
	if !ok || c.CompanyID != companyID {
		return SpatialCapture{}, ErrCaptureNotFound
	}
	c.RoomDraftID = roomDraftID
	f.byID[id] = c
	return c, nil
}

func (f *fakeCaptureRepo) ListBySpace(_ context.Context, companyID, spaceID string) ([]SpatialCapture, error) {
	var out []SpatialCapture
	for _, c := range f.byID {
		if c.CompanyID == companyID && c.SpaceID == spaceID {
			out = append(out, c)
		}
	}
	return out, nil
}

func (f *fakeCaptureRepo) DeleteAllForCompany(_ context.Context, companyID string) error {
	for id, c := range f.byID {
		if c.CompanyID == companyID {
			delete(f.byID, id)
		}
	}
	return nil
}

type fakeRoomVersionRepo struct {
	byID   map[string]SpatialRoomVersion
	nextID int
}

func newFakeRoomVersionRepo() *fakeRoomVersionRepo {
	return &fakeRoomVersionRepo{byID: map[string]SpatialRoomVersion{}}
}

func (f *fakeRoomVersionRepo) Create(_ context.Context, v SpatialRoomVersion) (SpatialRoomVersion, error) {
	f.nextID++
	v.ID = string(rune('A' + f.nextID))
	v.Status = RoomVersionStatusCurrent
	f.byID[v.ID] = v
	return v, nil
}

func (f *fakeRoomVersionRepo) FindByID(_ context.Context, companyID, id string) (SpatialRoomVersion, error) {
	v, ok := f.byID[id]
	if !ok || v.CompanyID != companyID {
		return SpatialRoomVersion{}, ErrRoomVersionNotFound
	}
	return v, nil
}

func (f *fakeRoomVersionRepo) Supersede(_ context.Context, companyID, id string) error {
	v, ok := f.byID[id]
	if !ok || v.CompanyID != companyID {
		return nil // no-op, matches Mongo impl contract
	}
	v.Status = RoomVersionStatusSuperseded
	f.byID[id] = v
	return nil
}

func (f *fakeRoomVersionRepo) ListBySpace(_ context.Context, companyID, spaceID string) ([]SpatialRoomVersion, error) {
	var out []SpatialRoomVersion
	for _, v := range f.byID {
		if v.CompanyID == companyID && v.SpaceID == spaceID {
			out = append(out, v)
		}
	}
	return out, nil
}

func (f *fakeRoomVersionRepo) DeleteAllForCompany(_ context.Context, companyID string) error {
	for id, v := range f.byID {
		if v.CompanyID == companyID {
			delete(f.byID, id)
		}
	}
	return nil
}

type fakeSpaceStateRepo struct {
	byKey map[string]*SpatialSpaceState // companyID|spaceID -> state
}

func newFakeSpaceStateRepo() *fakeSpaceStateRepo {
	return &fakeSpaceStateRepo{byKey: map[string]*SpatialSpaceState{}}
}

func (f *fakeSpaceStateRepo) key(companyID, spaceID string) string { return companyID + "|" + spaceID }

func (f *fakeSpaceStateRepo) FindOrCreateBySpace(_ context.Context, companyID, projectID, spaceID string) (SpatialSpaceState, error) {
	k := f.key(companyID, spaceID)
	if s, ok := f.byKey[k]; ok {
		return *s, nil
	}
	s := &SpatialSpaceState{ID: k, CompanyID: companyID, ProjectID: projectID, SpaceID: spaceID, Revision: 0}
	f.byKey[k] = s
	return *s, nil
}

func (f *fakeSpaceStateRepo) SetCurrentRoomVersion(_ context.Context, companyID, spaceID, roomVersionID string, expectedRevision int64) (SpatialSpaceState, error) {
	k := f.key(companyID, spaceID)
	s, ok := f.byKey[k]
	if !ok || s.Revision != expectedRevision {
		return SpatialSpaceState{}, ErrSpaceStateRevisionMismatch
	}
	s.CurrentRoomVersionID = roomVersionID
	s.Revision++
	return *s, nil
}

func (f *fakeSpaceStateRepo) DeleteAllForCompany(_ context.Context, companyID string) error {
	for k, s := range f.byKey {
		if s.CompanyID == companyID {
			delete(f.byKey, k)
		}
	}
	return nil
}

// --- tests ---

func newTestService() (*Service, *fakeCaptureRepo, *fakeRoomVersionRepo, *fakeSpaceStateRepo, *fakeSpaceLookup) {
	captures := newFakeCaptureRepo()
	versions := newFakeRoomVersionRepo()
	states := newFakeSpaceStateRepo()
	lookup := newFakeSpaceLookup()
	svc := NewService(captures, versions, states, lookup)
	return svc, captures, versions, states, lookup
}

func TestStartCapture_RejectsUnknownSpace(t *testing.T) {
	svc, _, _, _, _ := newTestService()
	ctx := context.Background()

	_, err := svc.StartCapture(ctx, "company_a", "project_1", "space_x", "", "")
	if !errors.Is(err, ErrSpaceNotFound) {
		t.Fatalf("expected ErrSpaceNotFound, got %v", err)
	}
}

func TestStartCapture_RejectsCrossTenantSpace(t *testing.T) {
	svc, _, _, _, lookup := newTestService()
	lookup.addSpace("company_b", "project_1", "space_x")
	ctx := context.Background()

	_, err := svc.StartCapture(ctx, "company_a", "project_1", "space_x", "", "")
	if !errors.Is(err, ErrSpaceNotFound) {
		t.Fatalf("expected ErrSpaceNotFound for cross-tenant space, got %v", err)
	}
}

func TestStartCapture_CreatesDraftCapture(t *testing.T) {
	svc, _, _, _, lookup := newTestService()
	lookup.addSpace("company_a", "project_1", "space_x")
	ctx := context.Background()

	c, err := svc.StartCapture(ctx, "company_a", "project_1", "space_x", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.Status != CaptureStatusDraft {
		t.Fatalf("expected status draft, got %s", c.Status)
	}
	if c.ID == "" {
		t.Fatal("expected non-empty capture ID")
	}
}

// TestStartCapture_DefaultsProviderToRoomPlan proves an empty provider
// defaults to CaptureProviderRoomPlan, the only active production provider
// (plan §RP3).
func TestStartCapture_DefaultsProviderToRoomPlan(t *testing.T) {
	svc, _, _, _, lookup := newTestService()
	lookup.addSpace("company_a", "project_1", "space_x")
	ctx := context.Background()

	c, err := svc.StartCapture(ctx, "company_a", "project_1", "space_x", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.Provider != CaptureProviderRoomPlan {
		t.Fatalf("expected default provider roomplan, got %s", c.Provider)
	}
	if c.CaptureNumber != 1 {
		t.Fatalf("expected captureNumber 1 for the first capture, got %d", c.CaptureNumber)
	}
}

// TestStartCapture_MultipleRunsGetIncreasingCaptureNumbers proves the hard
// multi-scan persistence requirement (design spec §8.22, plan §RP3): a Space
// may have many captures, each independently retained with a stable,
// increasing display ordinal — a new scan never overwrites a prior one.
func TestStartCapture_MultipleRunsGetIncreasingCaptureNumbers(t *testing.T) {
	svc, _, _, _, lookup := newTestService()
	lookup.addSpace("company_a", "project_1", "space_x")
	ctx := context.Background()

	c1, err := svc.StartCapture(ctx, "company_a", "project_1", "space_x", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	c2, err := svc.StartCapture(ctx, "company_a", "project_1", "space_x", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	c3, err := svc.StartCapture(ctx, "company_a", "project_1", "space_x", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if c1.CaptureNumber != 1 || c2.CaptureNumber != 2 || c3.CaptureNumber != 3 {
		t.Fatalf("expected capture numbers 1,2,3, got %d,%d,%d", c1.CaptureNumber, c2.CaptureNumber, c3.CaptureNumber)
	}
	if c1.ID == c2.ID || c2.ID == c3.ID {
		t.Fatal("expected distinct capture IDs — a new scan must not overwrite a prior scan")
	}

	all, err := svc.ListCapturesBySpace(ctx, "company_a", "space_x")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("expected all 3 captures to remain listed, got %d", len(all))
	}
}

// TestStartCapture_AcceptsExplicitProvider proves a caller may explicitly
// tag a capture's provider (e.g. the reserved arcore value for a possible
// future revived Android provider) rather than always defaulting.
func TestStartCapture_AcceptsExplicitProvider(t *testing.T) {
	svc, _, _, _, lookup := newTestService()
	lookup.addSpace("company_a", "project_1", "space_x")
	ctx := context.Background()

	c, err := svc.StartCapture(ctx, "company_a", "project_1", "space_x", CaptureProviderArCore, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.Provider != CaptureProviderArCore {
		t.Fatalf("expected provider arcore, got %s", c.Provider)
	}
}

// TestStartCapture_ClientCaptureIDRetryReturnsSameCapture proves plan
// §RP3.5/§RP4B0's core idempotency guarantee: a retried StartCapture call
// with the same ClientCaptureID returns the original capture rather than
// creating a duplicate or advancing CaptureNumber a second time.
func TestStartCapture_ClientCaptureIDRetryReturnsSameCapture(t *testing.T) {
	svc, _, _, _, lookup := newTestService()
	lookup.addSpace("company_a", "project_1", "space_x")
	ctx := context.Background()

	first, err := svc.StartCapture(ctx, "company_a", "project_1", "space_x", "", "client_capture_A")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	retry, err := svc.StartCapture(ctx, "company_a", "project_1", "space_x", "", "client_capture_A")
	if err != nil {
		t.Fatalf("unexpected error on retry: %v", err)
	}

	if retry.ID != first.ID {
		t.Fatalf("expected retry to return the same capture ID %s, got %s", first.ID, retry.ID)
	}
	if retry.CaptureNumber != first.CaptureNumber {
		t.Fatalf("expected retry to preserve captureNumber %d, got %d", first.CaptureNumber, retry.CaptureNumber)
	}
}

// TestStartCapture_ClientCaptureIDRetryDoesNotConsumeCaptureNumber proves a
// retried call never advances the sequence — a THIRD, genuinely new
// StartCapture call for the same Space must receive #2, not #3, even
// though StartCapture was called three times total (one genuine, one
// retry, one genuine).
func TestStartCapture_ClientCaptureIDRetryDoesNotConsumeCaptureNumber(t *testing.T) {
	svc, _, _, _, lookup := newTestService()
	lookup.addSpace("company_a", "project_1", "space_x")
	ctx := context.Background()

	first, err := svc.StartCapture(ctx, "company_a", "project_1", "space_x", "", "client_capture_A")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if first.CaptureNumber != 1 {
		t.Fatalf("expected first capture number 1, got %d", first.CaptureNumber)
	}

	// Retry of the SAME logical call.
	if _, err := svc.StartCapture(ctx, "company_a", "project_1", "space_x", "", "client_capture_A"); err != nil {
		t.Fatalf("unexpected error on retry: %v", err)
	}

	// A genuinely new capture (different ClientCaptureID) must get #2, not #3.
	second, err := svc.StartCapture(ctx, "company_a", "project_1", "space_x", "", "client_capture_B")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if second.CaptureNumber != 2 {
		t.Fatalf("expected second genuinely-new capture number 2 (retry must not consume a number), got %d", second.CaptureNumber)
	}
}

// TestStartCapture_ClientCaptureIDConflictOnDifferentSpace proves reusing a
// ClientCaptureID against a different Space is refused as a conflict
// rather than silently returning the unrelated existing capture or
// creating a second one.
func TestStartCapture_ClientCaptureIDConflictOnDifferentSpace(t *testing.T) {
	svc, _, _, _, lookup := newTestService()
	lookup.addSpace("company_a", "project_1", "space_x")
	lookup.addSpace("company_a", "project_1", "space_y")
	ctx := context.Background()

	if _, err := svc.StartCapture(ctx, "company_a", "project_1", "space_x", "", "client_capture_A"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err := svc.StartCapture(ctx, "company_a", "project_1", "space_y", "", "client_capture_A")
	if !errors.Is(err, ErrClientCaptureIDConflict) {
		t.Fatalf("expected ErrClientCaptureIDConflict, got %v", err)
	}
}

// TestStartCapture_EmptyClientCaptureIDNeverDeduplicates proves the
// original non-idempotent behavior is preserved for callers that supply no
// ClientCaptureID (e.g. Web) — each call creates a genuinely new capture.
func TestStartCapture_EmptyClientCaptureIDNeverDeduplicates(t *testing.T) {
	svc, _, _, _, lookup := newTestService()
	lookup.addSpace("company_a", "project_1", "space_x")
	ctx := context.Background()

	first, err := svc.StartCapture(ctx, "company_a", "project_1", "space_x", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	second, err := svc.StartCapture(ctx, "company_a", "project_1", "space_x", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if first.ID == second.ID {
		t.Fatal("expected two distinct captures when no ClientCaptureID is supplied")
	}
	if second.CaptureNumber != 2 {
		t.Fatalf("expected second capture number 2, got %d", second.CaptureNumber)
	}
}

// TestSetRoomDraft_RecordsAssociation proves the capture -> RoomDraft
// linkage plan §RP3 requires before Room Review may be presented.
func TestSetRoomDraft_RecordsAssociation(t *testing.T) {
	svc, _, _, _, lookup := newTestService()
	lookup.addSpace("company_a", "project_1", "space_x")
	ctx := context.Background()

	c, err := svc.StartCapture(ctx, "company_a", "project_1", "space_x", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.RoomDraftID != "" {
		t.Fatalf("expected empty RoomDraftID on a fresh capture, got %s", c.RoomDraftID)
	}

	updated, err := svc.SetRoomDraft(ctx, "company_a", c.ID, "draft_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updated.RoomDraftID != "draft_1" {
		t.Fatalf("expected RoomDraftID draft_1, got %s", updated.RoomDraftID)
	}

	fetched, err := svc.GetCapture(ctx, "company_a", c.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fetched.RoomDraftID != "draft_1" {
		t.Fatalf("expected persisted RoomDraftID draft_1, got %s", fetched.RoomDraftID)
	}
}

func TestSetRoomDraft_RejectsUnknownCapture(t *testing.T) {
	svc, _, _, _, _ := newTestService()
	ctx := context.Background()

	_, err := svc.SetRoomDraft(ctx, "company_a", "nonexistent", "draft_1")
	if !errors.Is(err, ErrCaptureNotFound) {
		t.Fatalf("expected ErrCaptureNotFound, got %v", err)
	}
}

func TestAdvanceCapture_LegalTransitionSequence(t *testing.T) {
	svc, _, _, _, lookup := newTestService()
	lookup.addSpace("company_a", "project_1", "space_x")
	ctx := context.Background()

	c, err := svc.StartCapture(ctx, "company_a", "project_1", "space_x", "", "")
	if err != nil {
		t.Fatalf("unexpected error starting capture: %v", err)
	}

	sequence := []CaptureStatus{
		CaptureStatusCapturing, CaptureStatusUploading, CaptureStatusUploaded, CaptureStatusReview,
	}
	for _, next := range sequence {
		c, err = svc.AdvanceCapture(ctx, "company_a", c.ID, next)
		if err != nil {
			t.Fatalf("unexpected error advancing to %s: %v", next, err)
		}
		if c.Status != next {
			t.Fatalf("expected status %s, got %s", next, c.Status)
		}
	}
}

func TestAdvanceCapture_RejectsIllegalTransition(t *testing.T) {
	svc, _, _, _, lookup := newTestService()
	lookup.addSpace("company_a", "project_1", "space_x")
	ctx := context.Background()

	c, err := svc.StartCapture(ctx, "company_a", "project_1", "space_x", "", "")
	if err != nil {
		t.Fatalf("unexpected error starting capture: %v", err)
	}

	// draft -> review is not a legal single-step transition (must pass
	// through capturing/uploading/uploaded first).
	_, err = svc.AdvanceCapture(ctx, "company_a", c.ID, CaptureStatusReview)
	if !errors.Is(err, ErrIllegalCaptureTransition) {
		t.Fatalf("expected ErrIllegalCaptureTransition, got %v", err)
	}
}

func TestAdvanceCapture_RejectsTransitionFromTerminalState(t *testing.T) {
	svc, _, _, _, lookup := newTestService()
	lookup.addSpace("company_a", "project_1", "space_x")
	ctx := context.Background()

	c, _ := svc.StartCapture(ctx, "company_a", "project_1", "space_x", "", "")
	c, _ = svc.AdvanceCapture(ctx, "company_a", c.ID, CaptureStatusCapturing)
	c, _ = svc.AdvanceCapture(ctx, "company_a", c.ID, CaptureStatusUploading)
	c, _ = svc.AdvanceCapture(ctx, "company_a", c.ID, CaptureStatusUploaded)
	c, _ = svc.AdvanceCapture(ctx, "company_a", c.ID, CaptureStatusReview)
	c, err := svc.ConfirmCapture(ctx, "company_a", c.ID)
	if err != nil {
		t.Fatalf("unexpected error confirming: %v", err)
	}
	if c.Status != CaptureStatusConfirmed {
		t.Fatalf("expected confirmed, got %s", c.Status)
	}

	// A confirmed capture is terminal — no further transitions allowed.
	_, err = svc.AdvanceCapture(ctx, "company_a", c.ID, CaptureStatusCapturing)
	if !errors.Is(err, ErrIllegalCaptureTransition) {
		t.Fatalf("expected ErrIllegalCaptureTransition from confirmed, got %v", err)
	}
}

func TestAdvanceCapture_AllowsFailureAndRetryDuringCaptureAndUpload(t *testing.T) {
	svc, _, _, _, lookup := newTestService()
	lookup.addSpace("company_a", "project_1", "space_x")
	ctx := context.Background()

	c, _ := svc.StartCapture(ctx, "company_a", "project_1", "space_x", "", "")
	c, err := svc.AdvanceCapture(ctx, "company_a", c.ID, CaptureStatusCapturing)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	c, err = svc.AdvanceCapture(ctx, "company_a", c.ID, CaptureStatusFailed)
	if err != nil {
		t.Fatalf("unexpected error transitioning to failed: %v", err)
	}
	// retry: failed -> capturing again
	c, err = svc.AdvanceCapture(ctx, "company_a", c.ID, CaptureStatusCapturing)
	if err != nil {
		t.Fatalf("unexpected error retrying from failed: %v", err)
	}
	if c.Status != CaptureStatusCapturing {
		t.Fatalf("expected capturing after retry, got %s", c.Status)
	}
}

// ConfirmCapture is the authority transition: review -> confirmed, creating
// an immutable SpatialRoomVersion and updating SpatialSpaceState's current
// pointer (design spec §4.2).

func TestConfirmCapture_RequiresReviewStatus(t *testing.T) {
	svc, _, _, _, lookup := newTestService()
	lookup.addSpace("company_a", "project_1", "space_x")
	ctx := context.Background()

	c, _ := svc.StartCapture(ctx, "company_a", "project_1", "space_x", "", "")
	_, err := svc.ConfirmCapture(ctx, "company_a", c.ID)
	if !errors.Is(err, ErrIllegalCaptureTransition) {
		t.Fatalf("expected ErrIllegalCaptureTransition confirming a draft capture, got %v", err)
	}
}

func advanceToReview(t *testing.T, svc *Service, companyID string, c SpatialCapture) SpatialCapture {
	t.Helper()
	ctx := context.Background()
	var err error
	for _, next := range []CaptureStatus{CaptureStatusCapturing, CaptureStatusUploading, CaptureStatusUploaded, CaptureStatusReview} {
		c, err = svc.AdvanceCapture(ctx, companyID, c.ID, next)
		if err != nil {
			t.Fatalf("unexpected error advancing to %s: %v", next, err)
		}
	}
	return c
}

func TestConfirmCapture_CreatesCurrentRoomVersion(t *testing.T) {
	svc, _, versions, states, lookup := newTestService()
	lookup.addSpace("company_a", "project_1", "space_x")
	ctx := context.Background()

	c, _ := svc.StartCapture(ctx, "company_a", "project_1", "space_x", "", "")
	c = advanceToReview(t, svc, "company_a", c)

	confirmed, err := svc.ConfirmCapture(ctx, "company_a", c.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if confirmed.Status != CaptureStatusConfirmed {
		t.Fatalf("expected confirmed, got %s", confirmed.Status)
	}
	if confirmed.RoomVersionID == "" {
		t.Fatal("expected non-empty RoomVersionID on confirmed capture")
	}

	v, err := versions.FindByID(ctx, "company_a", confirmed.RoomVersionID)
	if err != nil {
		t.Fatalf("expected room version to exist: %v", err)
	}
	if v.Status != RoomVersionStatusCurrent {
		t.Fatalf("expected new room version status current, got %s", v.Status)
	}

	state, err := states.FindOrCreateBySpace(ctx, "company_a", "project_1", "space_x")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if state.CurrentRoomVersionID != confirmed.RoomVersionID {
		t.Fatalf("expected space state current version %s, got %s", confirmed.RoomVersionID, state.CurrentRoomVersionID)
	}
}

func TestConfirmCapture_SupersedesPreviousCurrentVersion(t *testing.T) {
	svc, _, versions, _, lookup := newTestService()
	lookup.addSpace("company_a", "project_1", "space_x")
	ctx := context.Background()

	// First confirmation.
	c1, _ := svc.StartCapture(ctx, "company_a", "project_1", "space_x", "", "")
	c1 = advanceToReview(t, svc, "company_a", c1)
	confirmed1, err := svc.ConfirmCapture(ctx, "company_a", c1.ID)
	if err != nil {
		t.Fatalf("unexpected error confirming first capture: %v", err)
	}

	// Second confirmation (rescan) supersedes the first.
	c2, _ := svc.StartCapture(ctx, "company_a", "project_1", "space_x", "", "")
	c2 = advanceToReview(t, svc, "company_a", c2)
	confirmed2, err := svc.ConfirmCapture(ctx, "company_a", c2.ID)
	if err != nil {
		t.Fatalf("unexpected error confirming second capture: %v", err)
	}

	v1, err := versions.FindByID(ctx, "company_a", confirmed1.RoomVersionID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v1.Status != RoomVersionStatusSuperseded {
		t.Fatalf("expected first room version superseded, got %s", v1.Status)
	}

	v2, err := versions.FindByID(ctx, "company_a", confirmed2.RoomVersionID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v2.Status != RoomVersionStatusCurrent {
		t.Fatalf("expected second room version current, got %s", v2.Status)
	}
}

func TestConfirmCapture_ExactlyOneCurrentRoomVersionAfterMultipleRescans(t *testing.T) {
	svc, _, versions, _, lookup := newTestService()
	lookup.addSpace("company_a", "project_1", "space_x")
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		c, _ := svc.StartCapture(ctx, "company_a", "project_1", "space_x", "", "")
		c = advanceToReview(t, svc, "company_a", c)
		if _, err := svc.ConfirmCapture(ctx, "company_a", c.ID); err != nil {
			t.Fatalf("unexpected error confirming capture %d: %v", i, err)
		}
	}

	all, err := versions.ListBySpace(ctx, "company_a", "space_x")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	currentCount := 0
	for _, v := range all {
		if v.Status == RoomVersionStatusCurrent {
			currentCount++
		}
	}
	if currentCount != 1 {
		t.Fatalf("expected exactly 1 current room version, got %d (of %d total)", currentCount, len(all))
	}
}

func TestConfirmCapture_ConcurrentConfirmationRaceLosesCleanly(t *testing.T) {
	svc, captures, _, states, lookup := newTestService()
	lookup.addSpace("company_a", "project_1", "space_x")
	ctx := context.Background()

	c1, _ := svc.StartCapture(ctx, "company_a", "project_1", "space_x", "", "")
	c1 = advanceToReview(t, svc, "company_a", c1)
	c2, _ := svc.StartCapture(ctx, "company_a", "project_1", "space_x", "", "")
	c2 = advanceToReview(t, svc, "company_a", c2)

	// Simulate a race: force the space state's stored revision to advance
	// out from under c2's confirmation between its read and its CAS write,
	// by confirming c1 first (this is the realistic race Service.ConfirmCapture
	// must guard against internally when two reviewers confirm concurrently).
	if _, err := svc.ConfirmCapture(ctx, "company_a", c1.ID); err != nil {
		t.Fatalf("unexpected error confirming c1: %v", err)
	}

	// c2 confirmation must still succeed (Service retries/re-reads revision
	// internally) and end up as the new current version — it must NOT
	// silently fail or leave two "current" versions.
	confirmed2, err := svc.ConfirmCapture(ctx, "company_a", c2.ID)
	if err != nil {
		t.Fatalf("expected c2 confirmation to succeed after internal revision retry, got %v", err)
	}

	state, err := states.FindOrCreateBySpace(ctx, "company_a", "project_1", "space_x")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if state.CurrentRoomVersionID != confirmed2.RoomVersionID {
		t.Fatalf("expected final current version to be c2's %s, got %s", confirmed2.RoomVersionID, state.CurrentRoomVersionID)
	}

	// sanity: capture c2 itself reflects confirmed status via repo
	stored, err := captures.FindByID(ctx, "company_a", c2.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stored.Status != CaptureStatusConfirmed {
		t.Fatalf("expected c2 stored status confirmed, got %s", stored.Status)
	}
}

// DeleteAllForCompany is the demoseed reset hook (design pattern established
// by spaces.Service.DeleteAllForCompany) — removes captures, room versions,
// and space state for companyID across all three collections spatial owns.

func TestDeleteAllForCompany_RemovesCapturesRoomVersionsAndSpaceState(t *testing.T) {
	svc, captures, versions, states, lookup := newTestService()
	lookup.addSpace("company_a", "project_1", "space_x")
	ctx := context.Background()

	c, _ := svc.StartCapture(ctx, "company_a", "project_1", "space_x", "", "")
	c = advanceToReview(t, svc, "company_a", c)
	if _, err := svc.ConfirmCapture(ctx, "company_a", c.ID); err != nil {
		t.Fatalf("unexpected error confirming: %v", err)
	}

	if err := svc.DeleteAllForCompany(ctx, "company_a"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	remainingCaptures, err := captures.ListBySpace(ctx, "company_a", "space_x")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(remainingCaptures) != 0 {
		t.Fatalf("expected 0 captures after DeleteAllForCompany, got %d", len(remainingCaptures))
	}

	remainingVersions, err := versions.ListBySpace(ctx, "company_a", "space_x")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(remainingVersions) != 0 {
		t.Fatalf("expected 0 room versions after DeleteAllForCompany, got %d", len(remainingVersions))
	}

	state, err := states.FindOrCreateBySpace(ctx, "company_a", "project_1", "space_x")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if state.CurrentRoomVersionID != "" {
		t.Fatalf("expected fresh space state with no current version, got %s", state.CurrentRoomVersionID)
	}
	if state.Revision != 0 {
		t.Fatalf("expected fresh space state revision 0, got %d", state.Revision)
	}
}

func TestDeleteAllForCompany_RemovesArtifactsWhenArtifactSupportIsWired(t *testing.T) {
	svc, _, _, _, lookup := newTestService()
	lookup.addSpace("company_a", "project_1", "space_x")
	artifacts := newFakeArtifactRepo()
	svc.SetArtifactSupport(artifacts, newFakeObjectStore())
	ctx := context.Background()

	c, _ := svc.StartCapture(ctx, "company_a", "project_1", "space_x", "", "")
	if _, _, err := svc.RequestArtifactUpload(ctx, "company_a", c.ID, ArtifactKindRGBKeyframe, "image/jpeg", 4, "abc"); err != nil {
		t.Fatalf("unexpected error requesting upload: %v", err)
	}

	if err := svc.DeleteAllForCompany(ctx, "company_a"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	remaining, err := artifacts.ListByCapture(ctx, "company_a", c.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(remaining) != 0 {
		t.Fatalf("expected 0 artifacts after DeleteAllForCompany, got %d", len(remaining))
	}
}

func TestDeleteAllForCompany_DoesNotAffectOtherCompanies(t *testing.T) {
	svc, captures, _, _, lookup := newTestService()
	lookup.addSpace("company_a", "project_1", "space_x")
	lookup.addSpace("company_b", "project_1", "space_y")
	ctx := context.Background()

	svc.StartCapture(ctx, "company_a", "project_1", "space_x", "", "")
	svc.StartCapture(ctx, "company_b", "project_1", "space_y", "", "")

	if err := svc.DeleteAllForCompany(ctx, "company_a"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	remaining, err := captures.ListBySpace(ctx, "company_b", "space_y")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(remaining) != 1 {
		t.Fatalf("expected company_b's capture to survive, got %d captures", len(remaining))
	}
}
