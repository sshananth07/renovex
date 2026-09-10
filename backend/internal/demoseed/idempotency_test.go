package demoseed_test

import (
	"testing"

	"github.com/shananth/renovation-platform/backend/internal/demoseed"
)

func TestOperationID(t *testing.T) {
	got := demoseed.OperationID("project5", "rfq1:issue")
	want := "renovex-demo:v1:project5:rfq1:issue"
	if got != want {
		t.Fatalf("OperationID() = %q, want %q", got, want)
	}
}

func TestOperationID_DeterministicAcrossCalls(t *testing.T) {
	a := demoseed.OperationID("project5", "supplier1:offer-submit")
	b := demoseed.OperationID("project5", "supplier1:offer-submit")
	if a != b {
		t.Fatalf("OperationID must be deterministic: got %q and %q", a, b)
	}
}

type namedThing struct {
	Name string
}

func TestFindByName_Found(t *testing.T) {
	items := []namedThing{{Name: "Porcelain Floor Tile"}, {Name: "Cement"}}
	got, found := demoseed.FindByName(items, "Cement", func(n namedThing) string { return n.Name })
	if !found {
		t.Fatalf("expected to find Cement")
	}
	if got.Name != "Cement" {
		t.Fatalf("got = %+v", got)
	}
}

func TestFindByName_NotFound(t *testing.T) {
	items := []namedThing{{Name: "Porcelain Floor Tile"}}
	_, found := demoseed.FindByName(items, "Sand", func(n namedThing) string { return n.Name })
	if found {
		t.Fatalf("expected not to find Sand")
	}
}
