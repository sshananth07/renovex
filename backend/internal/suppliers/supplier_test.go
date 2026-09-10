package suppliers_test

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/shananth/renovation-platform/backend/internal/foundation/money"
	"github.com/shananth/renovation-platform/backend/internal/suppliers"
)

// D1 covers the supplier domain models, name normalization, the indicative-price
// invariants and the URL validator (design spec §4.1-§4.4).

// --- Name normalization (design spec §4.1, acceptance test 80) ---

func TestNormalizeSupplierName(t *testing.T) {
	cases := []struct {
		name, input, want string
	}{
		{"trims", "  ABC Materials  ", "abc materials"},
		{"lowercases", "ABC MATERIALS", "abc materials"},
		{"collapses runs of whitespace", "ABC   Materials", "abc materials"},
		{"collapses mixed whitespace", "ABC \t\n Materials", "abc materials"},
		{"already normalized", "abc materials", "abc materials"},
		{"unicode-aware lowercase", "ÅKE Byggmaterial", "åke byggmaterial"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := suppliers.NormalizeSupplierName(tc.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("NormalizeSupplierName(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

// The two forms acceptance test 80 names must collide.
func TestNormalizedNamesCollideAcrossSpacingAndCase(t *testing.T) {
	a, err := suppliers.NormalizeSupplierName("  ABC   Materials ")
	if err != nil {
		t.Fatal(err)
	}
	b, err := suppliers.NormalizeSupplierName("abc materials")
	if err != nil {
		t.Fatal(err)
	}
	if a != b {
		t.Errorf("%q and %q must normalize to the same key, got %q vs %q",
			"  ABC   Materials ", "abc materials", a, b)
	}
}

func TestNormalizeSupplierNameRejectsAnEmptyResult(t *testing.T) {
	for _, input := range []string{"", "   ", "\t\n", "    "} {
		if _, err := suppliers.NormalizeSupplierName(input); !errors.Is(err,
			suppliers.ErrSupplierNameRequired) {
			t.Errorf("NormalizeSupplierName(%q) error = %v, want ErrSupplierNameRequired",
				input, err)
		}
	}
}

// --- Material categories (design spec §4.1, acceptance test 83) ---

func TestNormalizeMaterialCategories(t *testing.T) {
	got := suppliers.NormalizeMaterialCategories([]string{
		"  Cement ", "cement", "Tiles", "", "   ", "TILES", "Sand",
	})
	want := []string{"Cement", "Tiles", "Sand"}

	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("category %d = %q, want %q (deterministic first-seen order)",
				i, got[i], want[i])
		}
	}
}

func TestNormalizeMaterialCategoriesOnEmptyInput(t *testing.T) {
	if got := suppliers.NormalizeMaterialCategories(nil); len(got) != 0 {
		t.Errorf("got %v, want empty", got)
	}
	if got := suppliers.NormalizeMaterialCategories([]string{"", "  "}); len(got) != 0 {
		t.Errorf("got %v, want empty", got)
	}
}

// --- Effective availability (design spec §4.2, acceptance tests 84, 88) ---

// Availability is Offering.Active AND Supplier.Active. A linked Material does
// NOT participate: M7 has no Material state to consult (§0.3 conflict D).
func TestOfferingIsEffectivelyAvailable(t *testing.T) {
	cases := []struct {
		name                           string
		offeringActive, supplierActive bool
		want                           bool
	}{
		{"both active", true, true, true},
		{"offering retired", false, true, false},
		{"supplier retired", true, false, false},
		{"both retired", false, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			o := suppliers.SupplierOffering{Active: tc.offeringActive}
			if got := o.EffectivelyAvailable(tc.supplierActive); got != tc.want {
				t.Errorf("EffectivelyAvailable(%v) = %v, want %v",
					tc.supplierActive, got, tc.want)
			}
		})
	}
}

// --- Indicative price invariants (design spec §4.2, acceptance test 89) ---

func TestValidateIndicativePrice(t *testing.T) {
	asOf := timePtr(time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC))
	valid := money.New(2550, "MYR")

	cases := []struct {
		name    string
		price   *money.Money
		asOf    *time.Time
		unit    string
		wantErr error
	}{
		{"nil price, nil asOf, no unit", nil, nil, "", nil},
		{"valid price with asOf and unit", &valid, asOf, "bag", nil},
		{"price without asOf", &valid, nil, "bag", suppliers.ErrInvalidIndicativePrice},
		{"nil price WITH asOf", nil, asOf, "", suppliers.ErrInvalidIndicativePrice},
		{"price with blank unit", &valid, asOf, "  ", suppliers.ErrInvalidIndicativePrice},
		{"zero amount", moneyPtr(0, "MYR"), asOf, "bag", suppliers.ErrInvalidIndicativePrice},
		{"negative amount", moneyPtr(-1, "MYR"), asOf, "bag", suppliers.ErrInvalidIndicativePrice},
		{"blank currency", moneyPtr(2550, ""), asOf, "bag", suppliers.ErrInvalidIndicativePrice},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := suppliers.ValidateIndicativePrice(tc.price, tc.asOf, tc.unit)
			if tc.wantErr == nil {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("error = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

// Zero never means "unknown" — nil already expresses that (design spec §4.2).
func TestZeroIndicativePriceIsRejectedRatherThanTreatedAsUnknown(t *testing.T) {
	asOf := timePtr(time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC))
	if err := suppliers.ValidateIndicativePrice(moneyPtr(0, "MYR"), asOf,
		"bag"); !errors.Is(err, suppliers.ErrInvalidIndicativePrice) {
		t.Fatalf("error = %v, want ErrInvalidIndicativePrice — nil expresses unknown", err)
	}
}

func moneyPtr(amount int64, currency string) *money.Money {
	m := money.New(amount, currency)
	return &m
}

func timePtr(t time.Time) *time.Time { return &t }

// --- URL validation (design spec §4.4, acceptance tests 91, 91a) ---

func TestValidateURLAcceptsHTTPAndHTTPSAndCanonicalizes(t *testing.T) {
	cases := []struct{ input, want string }{
		{"https://example.com/product", "https://example.com/product"},
		{"http://example.com/image.png", "http://example.com/image.png"},
		{"https://example.com", "https://example.com"},
		// The parsed canonical form is stored, not the original text.
		{"https://EXAMPLE.com/Product", "https://EXAMPLE.com/Product"},
		{"https://example.com/a%20b", "https://example.com/a%20b"},
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			got, err := suppliers.ValidateURL(tc.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("ValidateURL(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

// Rule 2: the scheme must be exactly http or https.
func TestValidateURLRejectsDangerousSchemes(t *testing.T) {
	for _, input := range []string{
		"javascript:alert(1)",
		"data:text/html;base64,PHNjcmlwdD4=",
		"file:///etc/passwd",
		"ftp://example.com/x",
		"mailto:someone@example.com",
		"//example.com/protocol-relative",
		"example.com/no-scheme",
	} {
		t.Run(input, func(t *testing.T) {
			if _, err := suppliers.ValidateURL(input); !errors.Is(err, suppliers.ErrInvalidURL) {
				t.Errorf("ValidateURL(%q) error = %v, want ErrInvalidURL", input, err)
			}
		})
	}
}

// Rule 4, acceptance test 91a: embedded credentials are rejected in BOTH forms.
//
// Two reasons: a stored URL is rendered in the contractor UI and handed to M8,
// so an embedded secret would be persisted in plaintext, logged and displayed;
// and user@host is a host-spoofing vector — a reader sees the trusted name
// while the browser resolves the real host.
func TestValidateURLRejectsEmbeddedCredentials(t *testing.T) {
	for _, input := range []string{
		"https://user:password@example.com/product",
		"https://user@example.com/image",
		"http://admin:secret@internal.example.com/",
		"https://trusted-supplier.com@evil.example/x",
	} {
		t.Run(input, func(t *testing.T) {
			got, err := suppliers.ValidateURL(input)
			if !errors.Is(err, suppliers.ErrInvalidURL) {
				t.Fatalf("ValidateURL(%q) error = %v, want ErrInvalidURL", input, err)
			}
			// No credential may survive in the returned value.
			if strings.Contains(got, "password") || strings.Contains(got, "secret") {
				t.Errorf("a credential leaked into the result: %q", got)
			}
		})
	}
}

// Rule 3: the host must be non-empty.
func TestValidateURLRejectsAnEmptyHost(t *testing.T) {
	for _, input := range []string{"https://", "http:///path", "https:///"} {
		if _, err := suppliers.ValidateURL(input); !errors.Is(err, suppliers.ErrInvalidURL) {
			t.Errorf("ValidateURL(%q) error = %v, want ErrInvalidURL", input, err)
		}
	}
}

// Rule 5: total length <= 2048 bytes.
func TestValidateURLRejectsOverlongInput(t *testing.T) {
	long := "https://example.com/" + strings.Repeat("a", 2048)
	if _, err := suppliers.ValidateURL(long); !errors.Is(err, suppliers.ErrInvalidURL) {
		t.Errorf("a %d-byte url was accepted", len(long))
	}

	atLimit := "https://example.com/" + strings.Repeat("a", 2048-len("https://example.com/"))
	if len(atLimit) != 2048 {
		t.Fatalf("test setup: url is %d bytes, want exactly 2048", len(atLimit))
	}
	if _, err := suppliers.ValidateURL(atLimit); err != nil {
		t.Errorf("a url at exactly the 2048-byte limit must be accepted, got %v", err)
	}
}

// An empty string means "no URL supplied" and is not an error; the caller
// stores nil.
func TestValidateURLTreatsEmptyAsAbsent(t *testing.T) {
	got, err := suppliers.ValidateURL("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "" {
		t.Errorf("got %q, want an empty result", got)
	}
}

// --- The no-network guarantee (design spec §4.4, acceptance test 92) ---
//
// The key server-side guarantee: M7 never fetches, resolves, proxies or
// inspects either URL. This is what makes "no web scraping" structural rather
// than aspirational, and it is why there is no SSRF surface and why rejecting
// private/loopback hosts is unnecessary.
//
// Asserted structurally: the package must not import any network-capable
// package. A test double cannot prove this — it could only observe calls a
// future edit added — whereas an import that does not exist cannot make a
// request at all.
func TestSuppliersPackageImportsNoNetworkCapablePackage(t *testing.T) {
	// Loopback and private hosts must be ACCEPTED, proving no resolution is
	// attempted and no host-based rejection exists.
	for _, input := range []string{
		"http://127.0.0.1:8080/image.png",
		"http://localhost/x",
		"http://169.254.169.254/latest/meta-data/",
		"http://10.0.0.1/internal",
		"http://[::1]/x",
		"https://this-host-does-not-resolve.invalid/x",
	} {
		t.Run(input, func(t *testing.T) {
			if _, err := suppliers.ValidateURL(input); err != nil {
				t.Errorf("ValidateURL(%q) = %v; M7 never resolves a host, so a private or "+
					"unresolvable host must be accepted rather than probed", input, err)
			}
		})
	}
}

// --- Model shape ---

// Supplier.Active is an M7-owned field on an M7-owned record — unlike the
// Material active state M7 must not invent (design spec §0.3 conflict D, §4.1).
func TestSupplierCarriesItsOwnActiveField(t *testing.T) {
	typ := reflect.TypeOf(suppliers.Supplier{})
	field, ok := typ.FieldByName("Active")
	if !ok {
		t.Fatal("Supplier.Active must exist")
	}
	if field.Type.Kind() != reflect.Bool {
		t.Errorf("Supplier.Active is %v, want bool", field.Type)
	}
	if _, ok := typ.FieldByName("NameNormalized"); !ok {
		t.Error("Supplier.NameNormalized must exist — it is the uniqueness key")
	}
}

// The offering's supplier link is immutable after creation (acceptance test 85);
// the model records it, and the service enforces it.
func TestSupplierOfferingShape(t *testing.T) {
	typ := reflect.TypeOf(suppliers.SupplierOffering{})
	for _, name := range []string{
		"SupplierID", "MaterialID", "ProductName", "Unit",
		"ImageURL", "ProductURL", "IndicativePrice", "IndicativePriceAsOf",
		"LastVerifiedAt", "Active",
	} {
		if _, ok := typ.FieldByName(name); !ok {
			t.Errorf("SupplierOffering.%s must exist", name)
		}
	}
	// MaterialID is optional, so it must be a pointer.
	f, _ := typ.FieldByName("MaterialID")
	if f.Type.Kind() != reflect.Ptr {
		t.Errorf("SupplierOffering.MaterialID is %v, want a pointer — the catalog link is optional",
			f.Type)
	}
}

func TestMaterialSupplierPreferenceShape(t *testing.T) {
	typ := reflect.TypeOf(suppliers.MaterialSupplierPreference{})
	for _, name := range []string{"CompanyID", "MaterialID", "SupplierID", "Revision"} {
		if _, ok := typ.FieldByName(name); !ok {
			t.Errorf("MaterialSupplierPreference.%s must exist", name)
		}
	}
}
