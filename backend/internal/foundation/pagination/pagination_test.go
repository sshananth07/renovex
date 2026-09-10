package pagination_test

import (
	"testing"

	"github.com/shananth/renovation-platform/backend/internal/foundation/pagination"
)

func TestParseRequestDefaultsPageTo1(t *testing.T) {
	req, err := pagination.ParseRequest(0, 0, "", "", "", nil, "createdAt", pagination.OrderDesc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if req.Page != 1 {
		t.Errorf("Page = %d, want 1", req.Page)
	}
}

func TestParseRequestDefaultsPageSizeTo25(t *testing.T) {
	req, err := pagination.ParseRequest(0, 0, "", "", "", nil, "createdAt", pagination.OrderDesc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if req.PageSize != 25 {
		t.Errorf("PageSize = %d, want 25", req.PageSize)
	}
}

func TestParseRequestRejectsPageBelow1(t *testing.T) {
	if _, err := pagination.ParseRequest(0, 25, "", "", "", nil, "createdAt", pagination.OrderDesc); err != nil {
		t.Fatalf("page=0 should use the default, got error: %v", err)
	}
	if _, err := pagination.ParseRequest(-1, 25, "", "", "", nil, "createdAt", pagination.OrderDesc); err == nil {
		t.Fatal("expected an error for page=-1")
	}
}

func TestParseRequestRejectsPageSizeBelow1(t *testing.T) {
	if _, err := pagination.ParseRequest(1, -1, "", "", "", nil, "createdAt", pagination.OrderDesc); err == nil {
		t.Fatal("expected an error for pageSize=-1")
	}
}

func TestParseRequestRejectsPageSizeAbove100(t *testing.T) {
	if _, err := pagination.ParseRequest(1, 101, "", "", "", nil, "createdAt", pagination.OrderDesc); err == nil {
		t.Fatal("expected an error for pageSize=101")
	}
}

func TestParseRequestAcceptsPageSizeAt100(t *testing.T) {
	req, err := pagination.ParseRequest(1, 100, "", "", "", nil, "createdAt", pagination.OrderDesc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if req.PageSize != 100 {
		t.Errorf("PageSize = %d, want 100", req.PageSize)
	}
}

func TestParseRequestTrimsSearch(t *testing.T) {
	req, err := pagination.ParseRequest(1, 25, "  hello  ", "", "", nil, "createdAt", pagination.OrderDesc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if req.Search != "hello" {
		t.Errorf("Search = %q, want %q", req.Search, "hello")
	}
}

func TestParseRequestRejectsSearchOver200Runes(t *testing.T) {
	long := make([]byte, 201)
	for i := range long {
		long[i] = 'a'
	}
	if _, err := pagination.ParseRequest(1, 25, string(long), "", "", nil, "createdAt", pagination.OrderDesc); err == nil {
		t.Fatal("expected an error for search over 200 runes")
	}
}

func TestParseRequestAcceptsSearchAt200Runes(t *testing.T) {
	exact := make([]byte, 200)
	for i := range exact {
		exact[i] = 'a'
	}
	req, err := pagination.ParseRequest(1, 25, string(exact), "", "", nil, "createdAt", pagination.OrderDesc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(req.Search) != 200 {
		t.Errorf("Search length = %d, want 200", len(req.Search))
	}
}

func TestParseRequestUsesDefaultSortAndOrderWhenBothAbsent(t *testing.T) {
	req, err := pagination.ParseRequest(1, 25, "", "", "", []string{"createdAt", "name"}, "createdAt", pagination.OrderDesc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if req.Sort != "createdAt" || req.Order != pagination.OrderDesc {
		t.Errorf("Sort/Order = %q/%q, want createdAt/desc", req.Sort, req.Order)
	}
}

func TestParseRequestDefaultsOrderToAscWhenSortSuppliedWithoutOrder(t *testing.T) {
	req, err := pagination.ParseRequest(1, 25, "", "name", "", []string{"createdAt", "name"}, "createdAt", pagination.OrderDesc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if req.Sort != "name" {
		t.Errorf("Sort = %q, want name", req.Sort)
	}
	if req.Order != pagination.OrderAsc {
		t.Errorf("Order = %q, want asc", req.Order)
	}
}

func TestParseRequestAppliesSuppliedOrderToDefaultFieldWhenSortAbsent(t *testing.T) {
	req, err := pagination.ParseRequest(1, 25, "", "", "asc", []string{"createdAt", "name"}, "createdAt", pagination.OrderDesc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if req.Sort != "createdAt" {
		t.Errorf("Sort = %q, want createdAt (the default field)", req.Sort)
	}
	if req.Order != pagination.OrderAsc {
		t.Errorf("Order = %q, want asc", req.Order)
	}
}

func TestParseRequestRejectsUnsupportedSortField(t *testing.T) {
	if _, err := pagination.ParseRequest(1, 25, "", "notAllowed", "", []string{"createdAt", "name"}, "createdAt", pagination.OrderDesc); err == nil {
		t.Fatal("expected an error for a sort field outside the allowlist")
	}
}

func TestParseRequestRejectsInvalidOrderValue(t *testing.T) {
	if _, err := pagination.ParseRequest(1, 25, "", "name", "sideways", []string{"createdAt", "name"}, "createdAt", pagination.OrderDesc); err == nil {
		t.Fatal("expected an error for an order value other than asc/desc")
	}
}

func TestOffsetCalculation(t *testing.T) {
	cases := []struct {
		page, pageSize, wantOffset int
	}{
		{1, 25, 0},
		{2, 25, 25},
		{3, 10, 20},
		{1, 100, 0},
	}
	for _, c := range cases {
		req, err := pagination.ParseRequest(c.page, c.pageSize, "", "", "", nil, "createdAt", pagination.OrderDesc)
		if err != nil {
			t.Fatalf("page=%d pageSize=%d: unexpected error: %v", c.page, c.pageSize, err)
		}
		if got := req.Offset(); got != c.wantOffset {
			t.Errorf("page=%d pageSize=%d: Offset() = %d, want %d", c.page, c.pageSize, got, c.wantOffset)
		}
	}
}

func TestNewResponseBuildsCanonicalShape(t *testing.T) {
	req, err := pagination.ParseRequest(2, 10, "", "", "", nil, "createdAt", pagination.OrderDesc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	items := []string{"a", "b"}
	resp := pagination.NewResponse(items, req, 42)

	if resp.Page != 2 {
		t.Errorf("Page = %d, want 2", resp.Page)
	}
	if resp.PageSize != 10 {
		t.Errorf("PageSize = %d, want 10", resp.PageSize)
	}
	if resp.Total != 42 {
		t.Errorf("Total = %d, want 42", resp.Total)
	}
	if len(resp.Items) != 2 {
		t.Errorf("len(Items) = %d, want 2", len(resp.Items))
	}
}

func TestStableSortAppendsIDTiebreakerInSameDirection(t *testing.T) {
	req, err := pagination.ParseRequest(1, 25, "", "name", "asc", []string{"createdAt", "name"}, "createdAt", pagination.OrderDesc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	sortSpec := req.MongoSort()
	if len(sortSpec) != 2 {
		t.Fatalf("expected 2 sort keys (primary + tiebreaker), got %d: %v", len(sortSpec), sortSpec)
	}
	if sortSpec[0].Key != "name" || sortSpec[0].Value != 1 {
		t.Errorf("primary sort = %+v, want {name, 1}", sortSpec[0])
	}
	if sortSpec[1].Key != "_id" || sortSpec[1].Value != 1 {
		t.Errorf("tiebreaker sort = %+v, want {_id, 1} (same direction as primary)", sortSpec[1])
	}
}

func TestStableSortTiebreakerMatchesDescendingDirection(t *testing.T) {
	req, err := pagination.ParseRequest(1, 25, "", "", "", nil, "createdAt", pagination.OrderDesc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	sortSpec := req.MongoSort()
	if sortSpec[0].Value != -1 {
		t.Errorf("primary sort value = %v, want -1 (desc)", sortSpec[0].Value)
	}
	if sortSpec[1].Key != "_id" || sortSpec[1].Value != -1 {
		t.Errorf("tiebreaker sort = %+v, want {_id, -1}", sortSpec[1])
	}
}

func TestSearchRegexEscapesLiteralCharacters(t *testing.T) {
	req, err := pagination.ParseRequest(1, 25, "a.b*c(d)", "", "", nil, "createdAt", pagination.OrderDesc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	pattern := req.SearchRegexPattern()
	// The pattern must not treat '.', '*', '(', ')' as regex metacharacters —
	// it should only ever match the literal substring "a.b*c(d)".
	if pattern == "" {
		t.Fatal("expected a non-empty escaped pattern")
	}
	if pattern == req.Search {
		t.Fatal("expected the pattern to differ from the raw search (metacharacters must be escaped)")
	}
}

func TestSearchRegexPatternEmptyWhenSearchAbsent(t *testing.T) {
	req, err := pagination.ParseRequest(1, 25, "", "", "", nil, "createdAt", pagination.OrderDesc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if req.SearchRegexPattern() != "" {
		t.Fatalf("expected an empty pattern when no search was supplied, got %q", req.SearchRegexPattern())
	}
}
