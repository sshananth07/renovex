package supplieroffers

import "github.com/shananth/renovation-platform/backend/internal/foundation/quantity"

// supplierOfferQuantityDocument stores the exact decimal as a canonical
// string. shopspring/decimal is never BSON-marshaled directly because that
// representation is not the platform's stable persistence contract.
type supplierOfferQuantityDocument struct {
	Value string `bson:"value"`
	Unit  string `bson:"unit"`
}

func supplierOfferQuantityToDocument(
	value quantity.Quantity,
) supplierOfferQuantityDocument {
	return supplierOfferQuantityDocument{
		Value: value.Value.String(),
		Unit:  value.Unit,
	}
}

func supplierOfferQuantityFromDocument(
	document supplierOfferQuantityDocument,
) (quantity.Quantity, error) {
	return quantity.New(document.Value, document.Unit)
}
