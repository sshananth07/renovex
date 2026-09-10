// Package pagination owns the canonical page/pageSize/search/sort/order
// value object shared by every paginated F1 list endpoint. It knows nothing
// about any domain module's fields, filters, or Mongo collection — each
// module supplies its own sort allowlist and default (field, order) pair,
// and owns its own repository query construction. This package only
// standardizes parsing, validation, and the two mechanical pieces every
// module would otherwise duplicate: a stable Mongo sort specification and a
// regex-safe search pattern.
package pagination

import (
	"errors"
	"regexp"
	"strings"
	"unicode/utf8"

	"go.mongodb.org/mongo-driver/v2/bson"
)

const (
	DefaultPage     = 1
	DefaultPageSize = 25
	MaxPageSize     = 100
	MaxSearchRunes  = 200
)

// Order is a validated sort direction.
type Order string

const (
	OrderAsc  Order = "asc"
	OrderDesc Order = "desc"
)

var (
	ErrInvalidPage       = errors.New("pagination: page must be >= 1")
	ErrInvalidPageSize   = errors.New("pagination: pageSize must be between 1 and 100")
	ErrSearchTooLong     = errors.New("pagination: search must be at most 200 characters")
	ErrUnsupportedSort   = errors.New("pagination: sort field is not supported by this endpoint")
	ErrInvalidOrderValue = errors.New("pagination: order must be \"asc\" or \"desc\"")
)

// Request is a parsed, validated pagination request, ready to drive a
// repository query. Callers construct it via ParseRequest — there is no
// exported way to build one with unvalidated fields.
type Request struct {
	Page     int
	PageSize int
	Search   string
	Sort     string
	Order    Order
}

// ParseRequest validates and normalizes raw pagination inputs.
//
//   - page: 0 means "use the default" (1); negative is an error.
//   - pageSize: 0 means "use the default" (25); outside [1,100] is an error.
//   - rawSearch: trimmed, then bounded to MaxSearchRunes (Unicode-aware).
//   - rawSort/rawOrder: "" for either falls back per the rules below.
//     allowedSorts is the endpoint's sort allowlist; a non-empty rawSort
//     outside it is an error. defaultSort/defaultOrder is the endpoint's
//     own default (field, direction) pair, used when sort and/or order are
//     absent.
//
// Rules (matching the F0.5 plan exactly):
//   - sort and order both absent: use (defaultSort, defaultOrder).
//   - sort supplied, order absent: use (rawSort, asc).
//   - order supplied, sort absent: use (defaultSort, rawOrder).
//   - both supplied: use (rawSort, rawOrder), validating rawSort against
//     allowedSorts and rawOrder against {"asc","desc"}.
func ParseRequest(
	page, pageSize int,
	rawSearch, rawSort, rawOrder string,
	allowedSorts []string,
	defaultSort string, defaultOrder Order,
) (Request, error) {
	if page == 0 {
		page = DefaultPage
	}
	if page < 1 {
		return Request{}, ErrInvalidPage
	}

	if pageSize == 0 {
		pageSize = DefaultPageSize
	}
	if pageSize < 1 || pageSize > MaxPageSize {
		return Request{}, ErrInvalidPageSize
	}

	search := strings.TrimSpace(rawSearch)
	if utf8.RuneCountInString(search) > MaxSearchRunes {
		return Request{}, ErrSearchTooLong
	}

	sort := defaultSort
	order := defaultOrder
	switch {
	case rawSort != "" && rawOrder != "":
		sort = rawSort
		parsedOrder, err := parseOrder(rawOrder)
		if err != nil {
			return Request{}, err
		}
		order = parsedOrder
	case rawSort != "":
		sort = rawSort
		order = OrderAsc
	case rawOrder != "":
		parsedOrder, err := parseOrder(rawOrder)
		if err != nil {
			return Request{}, err
		}
		order = parsedOrder
	}

	if rawSort != "" && !allowed(sort, allowedSorts) {
		return Request{}, ErrUnsupportedSort
	}

	return Request{Page: page, PageSize: pageSize, Search: search, Sort: sort, Order: order}, nil
}

func parseOrder(raw string) (Order, error) {
	switch Order(raw) {
	case OrderAsc, OrderDesc:
		return Order(raw), nil
	default:
		return "", ErrInvalidOrderValue
	}
}

func allowed(sort string, allowedSorts []string) bool {
	for _, s := range allowedSorts {
		if s == sort {
			return true
		}
	}
	return false
}

// Offset returns the number of items to skip for this Request's page.
func (r Request) Offset() int {
	return (r.Page - 1) * r.PageSize
}

// mongoDirection converts Order to Mongo's ±1 sort direction.
func (o Order) mongoDirection() int {
	if o == OrderAsc {
		return 1
	}
	return -1
}

// MongoSort returns a stable two-key Mongo sort specification: the
// requested field, followed by "_id" as a tie-breaker in the SAME
// direction, so rows with equal primary-sort values still produce a total,
// repeatable order across pages.
func (r Request) MongoSort() bson.D {
	direction := r.Order.mongoDirection()
	return bson.D{
		{Key: r.Sort, Value: direction},
		{Key: "_id", Value: direction},
	}
}

// searchMetacharacters are every rune with special meaning in Mongo's regex
// (PCRE-derived) dialect. Escaping them turns Request.Search into a literal
// substring match — a caller-supplied "search" can never be interpreted as
// caller-supplied regex.
var searchMetacharacterPattern = regexp.MustCompile(`[.^$|()\[\]{}*+?\\]`)

// SearchRegexPattern returns Request.Search with every regex metacharacter
// escaped, suitable for a Mongo $regex filter performing literal
// case-insensitive substring matching. Returns "" when no search was
// supplied, so callers can skip adding a $regex filter entirely.
func (r Request) SearchRegexPattern() string {
	if r.Search == "" {
		return ""
	}
	return searchMetacharacterPattern.ReplaceAllStringFunc(r.Search, func(m string) string {
		return `\` + m
	})
}

// Response is the canonical paginated list response envelope every F1 list
// endpoint returns: {items, page, pageSize, total}.
type Response[T any] struct {
	Items    []T `json:"items"`
	Page     int `json:"page"`
	PageSize int `json:"pageSize"`
	Total    int `json:"total"`
}

// NewResponse builds the canonical envelope from a page of items, the
// Request that produced them, and the total matching-document count (from a
// CountDocuments call using the SAME filter as the Find call, per the plan's
// "filter and count predicates are identical" requirement — that identity
// must be enforced by each module's repository method, not by this package).
func NewResponse[T any](items []T, req Request, total int) Response[T] {
	return Response[T]{Items: items, Page: req.Page, PageSize: req.PageSize, Total: total}
}
