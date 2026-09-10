// Package suppliers owns the company-scoped Supplier Directory, Supplier
// Offerings and the per-Material preferred-Supplier relationship
// (design spec §4; phase1.md §12). Company-wide, never project-scoped.
//
// M7 is supplier-NEUTRAL: this module records who a supplier is and what they
// offer, and contacts no one. Invitations, secure links, supplier portals,
// submitted offers and offer comparison are all M8.
//
// The security guarantee that shapes §4.4: M7 never fetches, resolves, proxies
// or inspects a contractor-supplied URL. There is no HTTP client, no DNS
// lookup and no metadata extraction anywhere in this package, which is what
// makes "no web scraping" structural rather than aspirational — and why there
// is no SSRF surface to defend.
//
// Module boundary (ADR 0002): suppliers imports no other M7 domain module, and
// neither rfqs nor materialrequirements imports it. An indicative price
// therefore has no path into an RFQ.
package suppliers

import (
	"net/url"
	"strings"
	"time"
	"unicode"

	"github.com/shananth/renovation-platform/backend/internal/foundation/money"
)

// MaxURLLength is the §4.4 rule-5 limit, in bytes.
const MaxURLLength = 2048

// Supplier is one commercial directory entity (design spec §4.1).
//
// Name is the ONLY mandatory field: M7 is an internal directory that contacts
// no one, so incomplete records are permitted. M8 decides whether a Supplier
// has a usable invitation contact; M7 does not require an email for future
// issuance.
type Supplier struct {
	ID        string
	CompanyID string

	Name string
	// NameNormalized is the uniqueness key and is NEVER client-supplied — it is
	// always derived by NormalizeSupplierName.
	NameNormalized string

	ContactPerson string
	Email         string
	Phone         string // a trimmed string, never numeric
	Address       string

	MaterialCategories []string
	Notes              string

	// Active is an M7-owned field on an M7-owned record. This is deliberately
	// unlike the Material active state M7 must NOT invent, because
	// materials.Material has no such field and M3 is frozen (design spec §0.3
	// conflict D).
	//
	// Archival is retirement-only: no hard delete and NO cascade. Offerings and
	// preferences are untouched when a Supplier is deactivated; they become
	// merely unavailable (§4.2).
	Active bool

	Revision        int64
	CreatedByUserID string
	CreatedAt       time.Time
	UpdatedAt       time.Time
	SchemaVersion   int
}

// SupplierOffering is a contractor-managed, informational record of something a
// Supplier sells (design spec §4.2).
type SupplierOffering struct {
	ID        string
	CompanyID string

	// SupplierID is IMMUTABLE after creation: moving an offering between
	// suppliers would silently rewrite its provenance.
	SupplierID string
	// MaterialID is an optional catalog link. An offering whose linked Material
	// later leaves the catalog stays readable with its own snapshot fields.
	MaterialID *string

	ProductName string
	Brand       string
	SKU         string
	Description string
	Category    string
	// Unit is required when IndicativePrice is set — a price without a unit
	// cannot be interpreted.
	Unit string

	// Stored canonicalized and validated by ValidateURL. M7 never dereferences
	// either (design spec §4.4).
	ImageURL   *string
	ProductURL *string

	// IndicativePrice is NOT a Supplier Offer. It is a contractor-recorded
	// informational preview: it never enters an RFQ, never appears on an RFQ
	// line, is never copied into a Money field any calculation reads, and is
	// never presented as a supplier quotation. Formal supplier-submitted offers
	// are a separate M8 model in a separate collection (design spec §4.2).
	IndicativePrice     *money.Money
	IndicativePriceAsOf *time.Time
	// LastVerifiedAt is distinct from IndicativePriceAsOf: the latter is when
	// the recorded price applied or was observed, this is when the contractor
	// last verified the offering overall.
	LastVerifiedAt *time.Time

	Active bool

	Revision        int64
	CreatedByUserID string
	CreatedAt       time.Time
	UpdatedAt       time.Time
	SchemaVersion   int
}

// EffectivelyAvailable reports §4.2's computed availability. It is never
// persisted, because it depends on another aggregate whose state can change
// independently.
//
// A linked Material deliberately does NOT participate: M7 has no Material state
// to consult (§0.3 conflict D), so an offering whose Material has left the
// catalog remains available.
func (o SupplierOffering) EffectivelyAvailable(supplierActive bool) bool {
	return o.Active && supplierActive
}

// MaterialSupplierPreference records at most one preferred Supplier per
// Material (design spec §4.3).
//
// It is ADVISORY ONLY: it never automatically adds a Supplier to an RFQ and
// never creates an invitation. Suggestion ranking is deferred (§20).
type MaterialSupplierPreference struct {
	ID         string
	CompanyID  string
	MaterialID string
	SupplierID string

	Revision      int64
	CreatedAt     time.Time
	UpdatedAt     time.Time
	SchemaVersion int
}

// NormalizeSupplierName derives the uniqueness key: trim, collapse consecutive
// Unicode whitespace to a single space, Unicode-aware lowercase (design spec
// §4.1).
//
// Uniqueness on this key is NOT partial on Active, so a retired supplier still
// blocks the name. That prevents retirement followed by accidental duplication:
// one Supplier record is one commercial entity, and distinct branches use
// distinguishable names.
func NormalizeSupplierName(name string) (string, error) {
	fields := strings.FieldsFunc(name, unicode.IsSpace)
	normalized := strings.ToLower(strings.Join(fields, " "))
	if normalized == "" {
		return "", ErrSupplierNameRequired
	}
	return normalized, nil
}

// NormalizeMaterialCategories trims each entry, drops empties, removes
// case-insensitive duplicates and preserves a deterministic first-seen order
// (design spec §4.1).
//
// First-seen order rather than sorted: the contractor's own ordering carries
// intent, and re-sorting would silently rewrite it on every save.
func NormalizeMaterialCategories(categories []string) []string {
	seen := make(map[string]struct{}, len(categories))
	out := make([]string, 0, len(categories))
	for _, c := range categories {
		trimmed := strings.TrimSpace(c)
		if trimmed == "" {
			continue
		}
		key := strings.ToLower(trimmed)
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, trimmed)
	}
	return out
}

// ValidateIndicativePrice enforces the §4.2 invariants:
//
//	price == nil  =>  asOf MUST be nil
//	price != nil  =>  amount > 0, currency non-empty, asOf REQUIRED, unit non-empty
//
// Zero never means "unknown" — nil already expresses that — so a zero amount is
// rejected rather than stored as an ambiguous sentinel.
func ValidateIndicativePrice(price *money.Money, asOf *time.Time, unit string) error {
	if price == nil {
		if asOf != nil {
			return ErrInvalidIndicativePrice
		}
		return nil
	}
	if price.Amount <= 0 {
		return ErrInvalidIndicativePrice
	}
	if strings.TrimSpace(price.Currency) == "" {
		return ErrInvalidIndicativePrice
	}
	if asOf == nil {
		return ErrInvalidIndicativePrice
	}
	if strings.TrimSpace(unit) == "" {
		return ErrInvalidIndicativePrice
	}
	return nil
}

// ValidateURL applies the §4.4 rules and returns the parsed CANONICAL form,
// which is what gets stored — not the original text.
//
// An empty input means "no URL supplied" and is not an error; the caller stores
// nil.
//
// M7 never fetches, resolves, proxies or inspects the result. Because no code
// path dereferences a contractor-supplied URL there is no SSRF surface, which
// is precisely why private and loopback hosts are NOT rejected: doing so would
// imply a resolution step that does not exist.
func ValidateURL(raw string) (string, error) {
	if raw == "" {
		return "", nil
	}
	if len(raw) > MaxURLLength {
		return "", ErrInvalidURL
	}

	u, err := url.Parse(raw)
	if err != nil {
		return "", ErrInvalidURL
	}
	// Exactly http or https — this rejects javascript:, data:, file:, ftp: and
	// every other scheme, including the empty scheme of a relative reference.
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", ErrInvalidURL
	}
	if u.Host == "" {
		return "", ErrInvalidURL
	}
	// Embedded credentials are rejected in BOTH forms (user:pass@host and
	// user@host). A stored URL is rendered in the contractor UI and handed to
	// M8, so an embedded secret would be persisted in plaintext, logged and
	// displayed; and user@host is a host-spoofing vector, where a reader sees
	// a trusted name while the browser resolves a different host.
	if u.User != nil {
		return "", ErrInvalidURL
	}

	canonical := u.String()
	if len(canonical) > MaxURLLength {
		return "", ErrInvalidURL
	}
	return canonical, nil
}
