package rfqissuance

import (
	"errors"
	"testing"
)

func TestIssuedVersionRepositoryValidationRejectsOversizedAggregate(t *testing.T) {
	version := IssuedRFQVersion{Lines: make([]IssuedRFQLine, 101)}
	if err := validateIssuedVersionPersistence(version); !errors.Is(err, ErrInputLimitExceeded) {
		t.Errorf("repository validation error = %v, want ErrInputLimitExceeded", err)
	}
}
