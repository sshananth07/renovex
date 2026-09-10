package supplieroffers

import (
	"errors"
	"testing"
)

func TestOfferVersionRepositoryValidationRejectsOversizedAggregate(t *testing.T) {
	version := SupplierOfferVersion{Lines: make([]SupplierOfferLine, 101)}
	if err := validateOfferVersionPersistence(version); !errors.Is(err, ErrInputLimitExceeded) {
		t.Errorf("line validation error = %v, want ErrInputLimitExceeded", err)
	}
	version.Lines = nil
	version.ChargeGroups = make([]ConditionalChargeGroup, 26)
	if err := validateOfferVersionPersistence(version); !errors.Is(err, ErrInputLimitExceeded) {
		t.Errorf("group validation error = %v, want ErrInputLimitExceeded", err)
	}
}
