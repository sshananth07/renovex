package suppliers_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/shananth/renovation-platform/backend/internal/suppliers"
)

// D3 covers the supplier service: validation, the active-supplier gates, the
// no-cascade retirement rule and effective availability (design spec §4).

// --- fakes ---

type fakeSupplierRepo struct {
	byID map[string]suppliers.Supplier
	next int
}

func newFakeSupplierRepo() *fakeSupplierRepo {
	return &fakeSupplierRepo{byID: map[string]suppliers.Supplier{}}
}

func (f *fakeSupplierRepo) Create(_ context.Context, s suppliers.Supplier) (suppliers.Supplier, error) {
	// Mirror uq_suppliers_company_name — NOT partial on active.
	for _, existing := range f.byID {
		if existing.CompanyID == s.CompanyID && existing.NameNormalized == s.NameNormalized {
			return suppliers.Supplier{}, suppliers.ErrSupplierNameTaken
		}
	}
	f.next++
	s.ID = "sup_" + itoa(f.next)
	f.byID[s.ID] = s
	return s, nil
}

func (f *fakeSupplierRepo) FindByID(_ context.Context, companyID, id string) (suppliers.Supplier, error) {
	s, ok := f.byID[id]
	if !ok || s.CompanyID != companyID {
		return suppliers.Supplier{}, suppliers.ErrSupplierNotFound
	}
	return s, nil
}

func (f *fakeSupplierRepo) List(_ context.Context, companyID string,
	filter suppliers.SupplierFilter) ([]suppliers.Supplier, error) {
	var out []suppliers.Supplier
	for _, s := range f.byID {
		if s.CompanyID != companyID {
			continue
		}
		if filter.Active != nil && s.Active != *filter.Active {
			continue
		}
		out = append(out, s)
	}
	return out, nil
}

func (f *fakeSupplierRepo) Update(ctx context.Context, companyID, id string,
	expectedRevision int64, updated suppliers.Supplier) (suppliers.Supplier, error) {
	stored, err := f.FindByID(ctx, companyID, id)
	if err != nil {
		return suppliers.Supplier{}, err
	}
	if stored.Revision != expectedRevision {
		return suppliers.Supplier{}, suppliers.ErrRevisionMismatch
	}
	for otherID, other := range f.byID {
		if otherID != id && other.CompanyID == companyID &&
			other.NameNormalized == updated.NameNormalized {
			return suppliers.Supplier{}, suppliers.ErrSupplierNameTaken
		}
	}

	stored.Name = updated.Name
	stored.NameNormalized = updated.NameNormalized
	stored.ContactPerson = updated.ContactPerson
	stored.Email = updated.Email
	stored.Phone = updated.Phone
	stored.Address = updated.Address
	stored.MaterialCategories = updated.MaterialCategories
	stored.Notes = updated.Notes
	stored.Revision = expectedRevision + 1
	stored.UpdatedAt = time.Now()

	f.byID[id] = stored
	return stored, nil
}

func (f *fakeSupplierRepo) SetActive(ctx context.Context, companyID, id string,
	expectedRevision int64, active bool) (suppliers.Supplier, error) {
	stored, err := f.FindByID(ctx, companyID, id)
	if err != nil {
		return suppliers.Supplier{}, err
	}
	if stored.Revision != expectedRevision {
		return suppliers.Supplier{}, suppliers.ErrRevisionMismatch
	}
	stored.Active = active
	stored.Revision = expectedRevision + 1
	f.byID[id] = stored
	return stored, nil
}

type fakeOfferingRepo struct {
	byID map[string]suppliers.SupplierOffering
	next int
}

func newFakeOfferingRepo() *fakeOfferingRepo {
	return &fakeOfferingRepo{byID: map[string]suppliers.SupplierOffering{}}
}

func (f *fakeOfferingRepo) Create(_ context.Context,
	o suppliers.SupplierOffering) (suppliers.SupplierOffering, error) {
	f.next++
	o.ID = "off_" + itoa(f.next)
	f.byID[o.ID] = o
	return o, nil
}

func (f *fakeOfferingRepo) FindByID(_ context.Context, companyID, id string) (suppliers.SupplierOffering, error) {
	o, ok := f.byID[id]
	if !ok || o.CompanyID != companyID {
		return suppliers.SupplierOffering{}, suppliers.ErrSupplierOfferingNotFound
	}
	return o, nil
}

func (f *fakeOfferingRepo) List(_ context.Context, companyID string,
	filter suppliers.OfferingFilter) ([]suppliers.SupplierOffering, error) {
	var out []suppliers.SupplierOffering
	for _, o := range f.byID {
		if o.CompanyID != companyID {
			continue
		}
		if filter.SupplierID != "" && o.SupplierID != filter.SupplierID {
			continue
		}
		if filter.Active != nil && o.Active != *filter.Active {
			continue
		}
		out = append(out, o)
	}
	return out, nil
}

func (f *fakeOfferingRepo) ListBySupplier(_ context.Context,
	companyID, supplierID string) ([]suppliers.SupplierOffering, error) {
	var out []suppliers.SupplierOffering
	for _, o := range f.byID {
		if o.CompanyID == companyID && o.SupplierID == supplierID {
			out = append(out, o)
		}
	}
	return out, nil
}

func (f *fakeOfferingRepo) Update(ctx context.Context, companyID, id string,
	expectedRevision int64, updated suppliers.SupplierOffering) (suppliers.SupplierOffering, error) {
	stored, err := f.FindByID(ctx, companyID, id)
	if err != nil {
		return suppliers.SupplierOffering{}, err
	}
	if stored.Revision != expectedRevision {
		return suppliers.SupplierOffering{}, suppliers.ErrRevisionMismatch
	}
	// SupplierID is deliberately NOT copied — mirroring the real $set.
	stored.MaterialID = updated.MaterialID
	stored.ProductName = updated.ProductName
	stored.Brand = updated.Brand
	stored.SKU = updated.SKU
	stored.Description = updated.Description
	stored.Category = updated.Category
	stored.Unit = updated.Unit
	stored.ImageURL = updated.ImageURL
	stored.ProductURL = updated.ProductURL
	stored.IndicativePrice = updated.IndicativePrice
	stored.IndicativePriceAsOf = updated.IndicativePriceAsOf
	stored.LastVerifiedAt = updated.LastVerifiedAt
	stored.Revision = expectedRevision + 1

	f.byID[id] = stored
	return stored, nil
}

func (f *fakeOfferingRepo) SetActive(ctx context.Context, companyID, id string,
	expectedRevision int64, active bool) (suppliers.SupplierOffering, error) {
	stored, err := f.FindByID(ctx, companyID, id)
	if err != nil {
		return suppliers.SupplierOffering{}, err
	}
	if stored.Revision != expectedRevision {
		return suppliers.SupplierOffering{}, suppliers.ErrRevisionMismatch
	}
	stored.Active = active
	stored.Revision = expectedRevision + 1
	f.byID[id] = stored
	return stored, nil
}

type fakePreferenceRepo struct {
	byMaterial map[string]suppliers.MaterialSupplierPreference
}

func newFakePreferenceRepo() *fakePreferenceRepo {
	return &fakePreferenceRepo{byMaterial: map[string]suppliers.MaterialSupplierPreference{}}
}

func (f *fakePreferenceRepo) Upsert(_ context.Context, p suppliers.MaterialSupplierPreference,
	expectedRevision int64) (suppliers.MaterialSupplierPreference, error) {
	key := p.CompanyID + "|" + p.MaterialID
	existing, ok := f.byMaterial[key]
	if ok && existing.Revision != expectedRevision {
		return suppliers.MaterialSupplierPreference{}, suppliers.ErrRevisionMismatch
	}
	if !ok && expectedRevision != 0 {
		return suppliers.MaterialSupplierPreference{}, suppliers.ErrRevisionMismatch
	}
	p.ID = "pref_" + key
	p.Revision = expectedRevision + 1
	f.byMaterial[key] = p
	return p, nil
}

func (f *fakePreferenceRepo) FindByMaterial(_ context.Context,
	companyID, materialID string) (suppliers.MaterialSupplierPreference, error) {
	p, ok := f.byMaterial[companyID+"|"+materialID]
	if !ok {
		return suppliers.MaterialSupplierPreference{}, suppliers.ErrPreferenceNotFound
	}
	return p, nil
}

func (f *fakePreferenceRepo) ListBySupplier(_ context.Context,
	companyID, supplierID string) ([]suppliers.MaterialSupplierPreference, error) {
	var out []suppliers.MaterialSupplierPreference
	for _, p := range f.byMaterial {
		if p.CompanyID == companyID && p.SupplierID == supplierID {
			out = append(out, p)
		}
	}
	return out, nil
}

func (f *fakePreferenceRepo) Delete(_ context.Context, companyID, materialID string,
	expectedRevision int64) error {
	key := companyID + "|" + materialID
	p, ok := f.byMaterial[key]
	if !ok {
		return suppliers.ErrPreferenceNotFound
	}
	if p.Revision != expectedRevision {
		return suppliers.ErrRevisionMismatch
	}
	delete(f.byMaterial, key)
	return nil
}

// fakeMaterialLookup mirrors the M3 capability: it exposes NO active/status
// value, because materials.Material has no such field and M3 is frozen
// (design spec §0.3 conflict D).
type fakeMaterialLookup struct {
	materials map[string]string // "companyID|materialID" -> name
}

func (f fakeMaterialLookup) GetMaterialReference(_ context.Context, companyID, materialID string) (
	string, string, string, bool, error) {
	name, ok := f.materials[companyID+"|"+materialID]
	if !ok {
		return "", "", "", false, nil
	}
	return name, "bag", "", true, nil
}

type recordedAudit struct {
	supplierCreated, supplierUpdated, supplierActiveChanged int
	offeringCreated, offeringUpdated, offeringActiveChanged int
	preferenceSet, preferenceCleared                        int
}

type fakeAudit struct{ rec *recordedAudit }

func (f fakeAudit) RecordSupplierCreated(context.Context, string, string, string, string) error {
	f.rec.supplierCreated++
	return nil
}
func (f fakeAudit) RecordSupplierUpdated(context.Context, string, string, string) error {
	f.rec.supplierUpdated++
	return nil
}
func (f fakeAudit) RecordSupplierActiveStateChanged(_ context.Context, _, _, _ string, _ bool) error {
	f.rec.supplierActiveChanged++
	return nil
}
func (f fakeAudit) RecordSupplierOfferingCreated(context.Context, string, string, string, string) error {
	f.rec.offeringCreated++
	return nil
}
func (f fakeAudit) RecordSupplierOfferingUpdated(context.Context, string, string, string, string) error {
	f.rec.offeringUpdated++
	return nil
}
func (f fakeAudit) RecordSupplierOfferingActiveStateChanged(_ context.Context, _, _, _, _ string, _ bool) error {
	f.rec.offeringActiveChanged++
	return nil
}
func (f fakeAudit) RecordPreferredSupplierChanged(context.Context, string, string, string, string) error {
	f.rec.preferenceSet++
	return nil
}
func (f fakeAudit) RecordPreferredSupplierCleared(context.Context, string, string, string, string) error {
	f.rec.preferenceCleared++
	return nil
}

func newService(t *testing.T) (*suppliers.Service, *fakeSupplierRepo, *fakeOfferingRepo,
	*fakePreferenceRepo, *recordedAudit) {
	t.Helper()
	supRepo := newFakeSupplierRepo()
	offRepo := newFakeOfferingRepo()
	prefRepo := newFakePreferenceRepo()
	rec := &recordedAudit{}

	svc := suppliers.NewService(supRepo, offRepo, prefRepo,
		fakeMaterialLookup{materials: map[string]string{
			"company_a|material_1": "Portland Cement",
			"company_a|material_2": "River Sand",
		}},
		fakeAudit{rec: rec})
	return svc, supRepo, offRepo, prefRepo, rec
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

func createSupplier(t *testing.T, svc *suppliers.Service, name string) suppliers.Supplier {
	t.Helper()
	got, err := svc.CreateSupplier(context.Background(), "company_a", "user_1",
		suppliers.CreateSupplierInput{Name: name})
	if err != nil {
		t.Fatalf("unexpected error creating supplier: %v", err)
	}
	return got
}

// --- Supplier creation and validation ---

func TestCreateSupplierNormalizesNameAndCategories(t *testing.T) {
	svc, _, _, _, rec := newService(t)

	got, err := svc.CreateSupplier(context.Background(), "company_a", "user_1",
		suppliers.CreateSupplierInput{
			Name:               "  ABC   Materials ",
			MaterialCategories: []string{" Cement ", "cement", "Tiles"},
			Email:              "  Sales@ABC.Example  ",
		})
	if err != nil {
		t.Fatal(err)
	}

	if got.NameNormalized != "abc materials" {
		t.Errorf("NameNormalized = %q, want abc materials", got.NameNormalized)
	}
	if len(got.MaterialCategories) != 2 {
		t.Errorf("MaterialCategories = %v, want two deduped entries", got.MaterialCategories)
	}
	// Email follows the existing project normalization convention.
	if got.Email != "sales@abc.example" {
		t.Errorf("Email = %q, want it normalized", got.Email)
	}
	if !got.Active {
		t.Error("a new supplier must start active")
	}
	if rec.supplierCreated != 1 {
		t.Errorf("audit supplierCreated = %d, want 1", rec.supplierCreated)
	}
}

// Acceptance test 81: only the name is mandatory.
func TestCreateSupplierRequiresOnlyAName(t *testing.T) {
	svc, _, _, _, _ := newService(t)

	if _, err := svc.CreateSupplier(context.Background(), "company_a", "user_1",
		suppliers.CreateSupplierInput{Name: "Solo Supplier"}); err != nil {
		t.Fatalf("contact details are optional in M7: %v", err)
	}
}

func TestCreateSupplierRejectsAnEmptyName(t *testing.T) {
	svc, _, _, _, _ := newService(t)

	for _, name := range []string{"", "   ", "\t"} {
		if _, err := svc.CreateSupplier(context.Background(), "company_a", "user_1",
			suppliers.CreateSupplierInput{Name: name}); !errors.Is(err,
			suppliers.ErrSupplierNameRequired) {
			t.Errorf("name %q: error = %v, want ErrSupplierNameRequired", name, err)
		}
	}
}

// Acceptance test 82.
func TestCreateSupplierValidatesEmailWhenPresent(t *testing.T) {
	svc, _, _, _, _ := newService(t)

	for _, email := range []string{"not-an-email", "@example.com", "a@", "a b@example.com"} {
		if _, err := svc.CreateSupplier(context.Background(), "company_a", "user_1",
			suppliers.CreateSupplierInput{Name: "S " + email, Email: email}); !errors.Is(err,
			suppliers.ErrInvalidEmail) {
			t.Errorf("email %q: error = %v, want ErrInvalidEmail", email, err)
		}
	}
}

// Acceptance test 79, at the service boundary.
func TestCreateSupplierRejectsADuplicateNormalizedName(t *testing.T) {
	svc, _, _, _, _ := newService(t)
	createSupplier(t, svc, "ABC Materials")

	if _, err := svc.CreateSupplier(context.Background(), "company_a", "user_1",
		suppliers.CreateSupplierInput{Name: "  abc   MATERIALS "}); !errors.Is(err,
		suppliers.ErrSupplierNameTaken) {
		t.Fatalf("error = %v, want ErrSupplierNameTaken", err)
	}
}

func TestUpdateSupplierIsRevisionGuarded(t *testing.T) {
	svc, _, _, _, rec := newService(t)
	created := createSupplier(t, svc, "ABC Materials")
	ctx := context.Background()

	got, err := svc.UpdateSupplier(ctx, "company_a", "user_1", created.ID, created.Revision,
		suppliers.UpdateSupplierInput{ContactPerson: strPtr("Siti")})
	if err != nil {
		t.Fatal(err)
	}
	if got.ContactPerson != "Siti" {
		t.Errorf("ContactPerson = %q, want Siti", got.ContactPerson)
	}
	if rec.supplierUpdated != 1 {
		t.Errorf("audit supplierUpdated = %d, want 1", rec.supplierUpdated)
	}

	if _, err := svc.UpdateSupplier(ctx, "company_a", "user_1", created.ID, created.Revision,
		suppliers.UpdateSupplierInput{ContactPerson: strPtr("Ali")}); !errors.Is(err,
		suppliers.ErrRevisionMismatch) {
		t.Fatalf("error = %v, want ErrRevisionMismatch", err)
	}
}

// --- Retirement: NO cascade (design spec §4.1, acceptance test 84) ---

// Deactivating a supplier modifies no offering and no preference. They become
// merely unavailable, which the service computes rather than persists.
func TestDeactivatingASupplierModifiesNoOfferingAndNoPreference(t *testing.T) {
	svc, _, offRepo, prefRepo, _ := newService(t)
	ctx := context.Background()
	supplier := createSupplier(t, svc, "ABC Materials")

	offering, err := svc.CreateOffering(ctx, "company_a", "user_1", suppliers.CreateOfferingInput{
		SupplierID: supplier.ID, ProductName: "OPC Cement", Unit: "bag",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetPreferredSupplier(ctx, "company_a", "user_1",
		"material_1", supplier.ID, 0); err != nil {
		t.Fatal(err)
	}

	offeringBefore := offRepo.byID[offering.ID]
	prefBefore := prefRepo.byMaterial["company_a|material_1"]

	retired, err := svc.SetSupplierActive(ctx, "company_a", "user_1", supplier.ID,
		supplier.Revision, false)
	if err != nil {
		t.Fatal(err)
	}
	if retired.Active {
		t.Fatal("the supplier was not retired")
	}

	if offRepo.byID[offering.ID] != offeringBefore {
		t.Error("retiring a supplier modified an offering; retirement must NOT cascade")
	}
	if prefRepo.byMaterial["company_a|material_1"] != prefBefore {
		t.Error("retiring a supplier modified a preference; retirement must NOT cascade")
	}

	// Availability is computed, and is now false.
	view, err := svc.GetOffering(ctx, "company_a", offering.ID)
	if err != nil {
		t.Fatal(err)
	}
	if view.EffectivelyAvailable {
		t.Error("EffectivelyAvailable must be false once the supplier is retired")
	}
	if !view.Offering.Active {
		t.Error("the offering's OWN Active flag must be untouched")
	}
}

// --- Offerings ---

// Acceptance test 86.
func TestCreateOfferingRejectsAnInactiveSupplier(t *testing.T) {
	svc, _, _, _, _ := newService(t)
	ctx := context.Background()
	supplier := createSupplier(t, svc, "ABC Materials")

	if _, err := svc.SetSupplierActive(ctx, "company_a", "user_1", supplier.ID,
		supplier.Revision, false); err != nil {
		t.Fatal(err)
	}

	if _, err := svc.CreateOffering(ctx, "company_a", "user_1", suppliers.CreateOfferingInput{
		SupplierID: supplier.ID, ProductName: "OPC Cement", Unit: "bag",
	}); !errors.Is(err, suppliers.ErrSupplierInactive) {
		t.Fatalf("error = %v, want ErrSupplierInactive", err)
	}
}

// Acceptance test 87: a NON-EXISTENT material is 404. There is deliberately no
// inactive-Material rejection, because M7 has no such concept.
func TestCreateOfferingRejectsANonExistentMaterial(t *testing.T) {
	svc, _, _, _, _ := newService(t)
	ctx := context.Background()
	supplier := createSupplier(t, svc, "ABC Materials")

	missing := "material_zzz"
	if _, err := svc.CreateOffering(ctx, "company_a", "user_1", suppliers.CreateOfferingInput{
		SupplierID: supplier.ID, ProductName: "OPC Cement", Unit: "bag", MaterialID: &missing,
	}); !errors.Is(err, suppliers.ErrMaterialNotFound) {
		t.Fatalf("error = %v, want ErrMaterialNotFound", err)
	}
}

// An offering with no material link at all is valid — the catalog link is
// optional (design spec §4.2).
func TestCreateOfferingWithoutAMaterialLink(t *testing.T) {
	svc, _, _, _, rec := newService(t)
	ctx := context.Background()
	supplier := createSupplier(t, svc, "ABC Materials")

	got, err := svc.CreateOffering(ctx, "company_a", "user_1", suppliers.CreateOfferingInput{
		SupplierID: supplier.ID, ProductName: "Generic Cement", Unit: "bag",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.MaterialID != nil {
		t.Errorf("MaterialID = %v, want nil", got.MaterialID)
	}
	if rec.offeringCreated != 1 {
		t.Errorf("audit offeringCreated = %d, want 1", rec.offeringCreated)
	}
}

// supplierId is a parent filter, not an opaque search term. Returning an empty
// list for a foreign id would disclose that the identifier is valid elsewhere.
func TestListOfferingsRejectsAForeignSupplierFilter(t *testing.T) {
	svc, _, _, _, _ := newService(t)
	supplier := createSupplier(t, svc, "ABC Materials")

	_, err := svc.ListOfferings(context.Background(), "company_b",
		suppliers.OfferingFilter{SupplierID: supplier.ID})
	if !errors.Is(err, suppliers.ErrSupplierNotFound) {
		t.Fatalf("error = %v, want ErrSupplierNotFound", err)
	}
}

// materialId carries the same parent semantics as supplierId and therefore
// must be checked through the tenant-scoped MaterialLookup before listing.
func TestListOfferingsRejectsAForeignMaterialFilter(t *testing.T) {
	svc, _, _, _, _ := newService(t)

	_, err := svc.ListOfferings(context.Background(), "company_b",
		suppliers.OfferingFilter{MaterialID: "material_1"})
	if !errors.Is(err, suppliers.ErrMaterialNotFound) {
		t.Fatalf("error = %v, want ErrMaterialNotFound", err)
	}
}

// Acceptance test 85: SupplierID is immutable after creation.
func TestUpdateOfferingRejectsASupplierChange(t *testing.T) {
	svc, _, _, _, _ := newService(t)
	ctx := context.Background()
	first := createSupplier(t, svc, "ABC Materials")
	second := createSupplier(t, svc, "XYZ Supplies")

	offering, err := svc.CreateOffering(ctx, "company_a", "user_1", suppliers.CreateOfferingInput{
		SupplierID: first.ID, ProductName: "OPC Cement", Unit: "bag",
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := svc.UpdateOffering(ctx, "company_a", "user_1", offering.ID, offering.Revision,
		suppliers.UpdateOfferingInput{SupplierID: &second.ID}); !errors.Is(err,
		suppliers.ErrSupplierIDImmutable) {
		t.Fatalf("error = %v, want ErrSupplierIDImmutable", err)
	}
}

// Passing the SAME supplier id is a no-op, not an error: a client echoing the
// current value back has not asked for a change.
func TestUpdateOfferingAcceptsTheUnchangedSupplierID(t *testing.T) {
	svc, _, _, _, _ := newService(t)
	ctx := context.Background()
	supplier := createSupplier(t, svc, "ABC Materials")

	offering, err := svc.CreateOffering(ctx, "company_a", "user_1", suppliers.CreateOfferingInput{
		SupplierID: supplier.ID, ProductName: "OPC Cement", Unit: "bag",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.UpdateOffering(ctx, "company_a", "user_1", offering.ID, offering.Revision,
		suppliers.UpdateOfferingInput{SupplierID: &supplier.ID}); err != nil {
		t.Fatalf("echoing the current supplier id must not be an error: %v", err)
	}
}

// Acceptance test 89, at the service boundary.
func TestCreateOfferingEnforcesIndicativePriceInvariants(t *testing.T) {
	svc, _, _, _, _ := newService(t)
	ctx := context.Background()
	supplier := createSupplier(t, svc, "ABC Materials")
	asOf := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)

	cases := []struct {
		name  string
		input suppliers.CreateOfferingInput
	}{
		{"price without asOf", suppliers.CreateOfferingInput{
			ProductName: "P", Unit: "bag", IndicativePrice: moneyPtr(2550, "MYR"),
		}},
		{"asOf without price", suppliers.CreateOfferingInput{
			ProductName: "P", Unit: "bag", IndicativePriceAsOf: &asOf,
		}},
		{"zero amount", suppliers.CreateOfferingInput{
			ProductName: "P", Unit: "bag",
			IndicativePrice: moneyPtr(0, "MYR"), IndicativePriceAsOf: &asOf,
		}},
		{"negative amount", suppliers.CreateOfferingInput{
			ProductName: "P", Unit: "bag",
			IndicativePrice: moneyPtr(-100, "MYR"), IndicativePriceAsOf: &asOf,
		}},
		{"price with blank unit", suppliers.CreateOfferingInput{
			ProductName: "P", Unit: "  ",
			IndicativePrice: moneyPtr(2550, "MYR"), IndicativePriceAsOf: &asOf,
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := tc.input
			in.SupplierID = supplier.ID
			if _, err := svc.CreateOffering(ctx, "company_a", "user_1", in); !errors.Is(err,
				suppliers.ErrInvalidIndicativePrice) {
				t.Fatalf("error = %v, want ErrInvalidIndicativePrice", err)
			}
		})
	}
}

// Acceptance test 90: clearing the price clears IndicativePriceAsOf and leaves
// LastVerifiedAt untouched. The two dates mean different things.
func TestClearingTheIndicativePriceClearsAsOfButNotLastVerified(t *testing.T) {
	svc, _, _, _, _ := newService(t)
	ctx := context.Background()
	supplier := createSupplier(t, svc, "ABC Materials")
	asOf := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	verified := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)

	offering, err := svc.CreateOffering(ctx, "company_a", "user_1", suppliers.CreateOfferingInput{
		SupplierID: supplier.ID, ProductName: "OPC Cement", Unit: "bag",
		IndicativePrice: moneyPtr(2550, "MYR"), IndicativePriceAsOf: &asOf,
		LastVerifiedAt: &verified,
	})
	if err != nil {
		t.Fatal(err)
	}

	cleared, err := svc.UpdateOffering(ctx, "company_a", "user_1", offering.ID, offering.Revision,
		suppliers.UpdateOfferingInput{ClearIndicativePrice: true})
	if err != nil {
		t.Fatalf("clearing the price failed: %v", err)
	}

	if cleared.IndicativePrice != nil {
		t.Errorf("IndicativePrice = %+v, want nil", cleared.IndicativePrice)
	}
	if cleared.IndicativePriceAsOf != nil {
		t.Errorf("IndicativePriceAsOf = %v, want nil — it must be cleared consistently",
			cleared.IndicativePriceAsOf)
	}
	if cleared.LastVerifiedAt == nil || !cleared.LastVerifiedAt.Equal(verified) {
		t.Errorf("LastVerifiedAt = %v, want %v untouched — it records when the OFFERING was "+
			"verified, not when the price applied", cleared.LastVerifiedAt, verified)
	}
}

// Acceptance test 91: URLs are validated and stored canonicalized.
func TestCreateOfferingValidatesURLs(t *testing.T) {
	svc, _, _, _, _ := newService(t)
	ctx := context.Background()
	supplier := createSupplier(t, svc, "ABC Materials")

	for _, bad := range []string{
		"javascript:alert(1)",
		"data:text/html,x",
		"file:///etc/passwd",
		"https://user:pass@example.com/x",
		"https://user@example.com/x",
	} {
		t.Run(bad, func(t *testing.T) {
			if _, err := svc.CreateOffering(ctx, "company_a", "user_1",
				suppliers.CreateOfferingInput{
					SupplierID: supplier.ID, ProductName: "P", Unit: "bag", ProductURL: &bad,
				}); !errors.Is(err, suppliers.ErrInvalidURL) {
				t.Fatalf("error = %v, want ErrInvalidURL", err)
			}
		})
	}

	good := "https://example.com/products/cement"
	got, err := svc.CreateOffering(ctx, "company_a", "user_1", suppliers.CreateOfferingInput{
		SupplierID: supplier.ID, ProductName: "P", Unit: "bag", ProductURL: &good,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.ProductURL == nil || *got.ProductURL != good {
		t.Errorf("ProductURL = %v, want the canonical %q", got.ProductURL, good)
	}
}

// --- Effective availability (acceptance test 88) ---

// A linked Material's absence from the catalog does NOT make an offering
// unavailable: M7 has no Material state to consult.
func TestOfferingRemainsAvailableWhenItsMaterialLeavesTheCatalog(t *testing.T) {
	svc, _, offRepo, _, _ := newService(t)
	ctx := context.Background()
	supplier := createSupplier(t, svc, "ABC Materials")

	materialID := "material_1"
	offering, err := svc.CreateOffering(ctx, "company_a", "user_1", suppliers.CreateOfferingInput{
		SupplierID: supplier.ID, ProductName: "OPC Cement", Unit: "bag", MaterialID: &materialID,
	})
	if err != nil {
		t.Fatal(err)
	}

	// The material vanishes from the catalog.
	gone := "material_gone"
	stored := offRepo.byID[offering.ID]
	stored.MaterialID = &gone
	offRepo.byID[offering.ID] = stored

	view, err := svc.GetOffering(ctx, "company_a", offering.ID)
	if err != nil {
		t.Fatalf("an offering whose material left the catalog must stay readable: %v", err)
	}
	if !view.EffectivelyAvailable {
		t.Error("availability is Offering.Active AND Supplier.Active — a missing Material " +
			"must not participate")
	}
	if view.MaterialName != "" {
		t.Errorf("MaterialName = %q, want empty when the lookup reports found=false",
			view.MaterialName)
	}
}

// --- Preference (design spec §4.3) ---

// Acceptance test 94.
func TestSetPreferredSupplierRejectsInactiveSupplierAndMissingMaterial(t *testing.T) {
	svc, _, _, _, _ := newService(t)
	ctx := context.Background()
	supplier := createSupplier(t, svc, "ABC Materials")

	t.Run("missing material", func(t *testing.T) {
		if _, err := svc.SetPreferredSupplier(ctx, "company_a", "user_1",
			"material_zzz", supplier.ID, 0); !errors.Is(err, suppliers.ErrMaterialNotFound) {
			t.Fatalf("error = %v, want ErrMaterialNotFound", err)
		}
	})

	t.Run("inactive supplier", func(t *testing.T) {
		retired, err := svc.SetSupplierActive(ctx, "company_a", "user_1", supplier.ID,
			supplier.Revision, false)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := svc.SetPreferredSupplier(ctx, "company_a", "user_1",
			"material_1", retired.ID, 0); !errors.Is(err, suppliers.ErrSupplierInactive) {
			t.Fatalf("error = %v, want ErrSupplierInactive", err)
		}
	})
}

// Acceptance test 93: a second PUT replaces the first under a Revision guard.
func TestSetPreferredSupplierReplacesUnderRevisionGuard(t *testing.T) {
	svc, _, _, _, rec := newService(t)
	ctx := context.Background()
	first := createSupplier(t, svc, "ABC Materials")
	second := createSupplier(t, svc, "XYZ Supplies")

	created, err := svc.SetPreferredSupplier(ctx, "company_a", "user_1",
		"material_1", first.ID, 0)
	if err != nil {
		t.Fatal(err)
	}

	replaced, err := svc.SetPreferredSupplier(ctx, "company_a", "user_1",
		"material_1", second.ID, created.Revision)
	if err != nil {
		t.Fatalf("replace failed: %v", err)
	}
	if replaced.SupplierID != second.ID {
		t.Errorf("SupplierID = %q, want the replacement", replaced.SupplierID)
	}
	if rec.preferenceSet != 2 {
		t.Errorf("audit preferenceSet = %d, want 2", rec.preferenceSet)
	}

	if _, err := svc.SetPreferredSupplier(ctx, "company_a", "user_1",
		"material_1", first.ID, created.Revision); !errors.Is(err,
		suppliers.ErrRevisionMismatch) {
		t.Fatalf("error = %v, want ErrRevisionMismatch", err)
	}
}

// Acceptance test 95: a preference whose supplier later becomes inactive
// remains readable and is reported unavailable.
func TestPreferenceRemainsReadableWhenItsSupplierIsRetired(t *testing.T) {
	svc, _, _, _, _ := newService(t)
	ctx := context.Background()
	supplier := createSupplier(t, svc, "ABC Materials")

	if _, err := svc.SetPreferredSupplier(ctx, "company_a", "user_1",
		"material_1", supplier.ID, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetSupplierActive(ctx, "company_a", "user_1", supplier.ID,
		supplier.Revision, false); err != nil {
		t.Fatal(err)
	}

	view, err := svc.GetPreferredSupplier(ctx, "company_a", "material_1")
	if err != nil {
		t.Fatalf("the preference must remain readable: %v", err)
	}
	if view.SupplierActive {
		t.Error("SupplierActive must be false")
	}
	if view.SupplierName != "ABC Materials" {
		t.Errorf("SupplierName = %q, want it still reported", view.SupplierName)
	}
}

// Acceptance test 96: DELETE removes the record.
func TestClearPreferredSupplierRemovesTheRecord(t *testing.T) {
	svc, _, _, _, rec := newService(t)
	ctx := context.Background()
	supplier := createSupplier(t, svc, "ABC Materials")

	created, err := svc.SetPreferredSupplier(ctx, "company_a", "user_1",
		"material_1", supplier.ID, 0)
	if err != nil {
		t.Fatal(err)
	}

	if err := svc.ClearPreferredSupplier(ctx, "company_a", "user_1",
		"material_1", created.Revision); err != nil {
		t.Fatalf("clear failed: %v", err)
	}
	if _, err := svc.GetPreferredSupplier(ctx, "company_a", "material_1"); !errors.Is(err,
		suppliers.ErrPreferenceNotFound) {
		t.Fatalf("error = %v, want ErrPreferenceNotFound", err)
	}
	if rec.preferenceCleared != 1 {
		t.Errorf("audit preferenceCleared = %d, want 1", rec.preferenceCleared)
	}
}

// --- Tenant scoping ---

func TestSupplierOperationsAreTenantScoped(t *testing.T) {
	svc, _, _, _, _ := newService(t)
	ctx := context.Background()
	created := createSupplier(t, svc, "ABC Materials")

	if _, err := svc.GetSupplier(ctx, "company_b", created.ID); !errors.Is(err,
		suppliers.ErrSupplierNotFound) {
		t.Errorf("get: error = %v, want ErrSupplierNotFound", err)
	}
	if _, err := svc.UpdateSupplier(ctx, "company_b", "user_1", created.ID, created.Revision,
		suppliers.UpdateSupplierInput{}); !errors.Is(err, suppliers.ErrSupplierNotFound) {
		t.Errorf("update: error = %v, want ErrSupplierNotFound", err)
	}
	if _, err := svc.SetSupplierActive(ctx, "company_b", "user_1", created.ID,
		created.Revision, false); !errors.Is(err, suppliers.ErrSupplierNotFound) {
		t.Errorf("setActive: error = %v, want ErrSupplierNotFound", err)
	}
}

func strPtr(s string) *string { return &s }
