// Package procurementlimits owns the pure, cross-module bounds for external
// procurement requests. Domain modules still decide which fields are required;
// this package makes the approved byte, rune, count and horizon rules identical.
package procurementlimits

import (
	"errors"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	MaxJSONBodyBytes = 1 << 20
	MaxIDBytes       = 128

	MaxInvitationsPerChain = 100
	MaxLines               = 100
	MaxChargeGroups        = 25
	MaxIDsPerRequest       = 100
	DefaultPageSize        = 25
	MaxPageSize            = 100
	DefaultBatchSize       = 25
	MaxBatchSize           = 100
	MaxErrorDetails        = 10

	MaxDisplayTextRunes = 200
	MaxSKUTextRunes     = 128
	MaxNoteRunes        = 500
	MaxLongTextRunes    = 2000
)

var (
	ErrInvalidID       = errors.New("procurementlimits: invalid id")
	ErrTextTooLong     = errors.New("procurementlimits: text too long")
	ErrCollectionLimit = errors.New("procurementlimits: collection limit exceeded")
	ErrDuplicateID     = errors.New("procurementlimits: duplicate id")
	ErrInvalidPageSize = errors.New("procurementlimits: invalid page size")
	ErrInvalidBatch    = errors.New("procurementlimits: invalid batch size")
	ErrInvalidDate     = errors.New("procurementlimits: date outside approved horizon")
)

// ValidateID accepts canonical printable ASCII only. Byte and rune counts are
// therefore identical, which bounds index keys and audit/log identities.
func ValidateID(value string) error {
	if len(value) < 1 || len(value) > MaxIDBytes {
		return ErrInvalidID
	}
	for index := 0; index < len(value); index++ {
		if value[index] < 0x21 || value[index] > 0x7e {
			return ErrInvalidID
		}
	}
	return nil
}

// TrimText applies the shared whitespace normalization before counting Unicode
// code points. Multi-byte user text therefore receives the same human-visible
// limit as ASCII text.
func TrimText(value string, maximumRunes int) (string, error) {
	trimmed := strings.TrimSpace(value)
	if maximumRunes < 0 || utf8.RuneCountInString(trimmed) > maximumRunes {
		return "", ErrTextTooLong
	}
	return trimmed, nil
}

func ValidateCount(count, maximum int) error {
	if count < 0 || count > maximum {
		return ErrCollectionLimit
	}
	return nil
}

func ValidateUniqueIDs(ids []string) error {
	if err := ValidateCount(len(ids), MaxIDsPerRequest); err != nil {
		return err
	}
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		if err := ValidateID(id); err != nil {
			return err
		}
		if _, duplicate := seen[id]; duplicate {
			return ErrDuplicateID
		}
		seen[id] = struct{}{}
	}
	return nil
}

func PageSize(supplied int) (int, error) {
	if supplied == 0 {
		return DefaultPageSize, nil
	}
	if supplied < 1 || supplied > MaxPageSize {
		return 0, ErrInvalidPageSize
	}
	return supplied, nil
}

func BatchSize(supplied int) (int, error) {
	if supplied == 0 {
		return DefaultBatchSize, nil
	}
	if supplied < 1 || supplied > MaxBatchSize {
		return 0, ErrInvalidBatch
	}
	return supplied, nil
}

func validateAfterWithin(start, candidate time.Time, maximum time.Duration) error {
	if start.IsZero() || candidate.IsZero() || !candidate.After(start) ||
		candidate.After(start.Add(maximum)) {
		return ErrInvalidDate
	}
	return nil
}

func ValidateResponseDeadline(issuedAt, deadline time.Time) error {
	return validateAfterWithin(issuedAt, deadline, 180*24*time.Hour)
}

func ValidateInvitationExpiry(now, expiresAt time.Time) error {
	return validateAfterWithin(now, expiresAt, 180*24*time.Hour)
}

func ValidateOfferValidity(submittedAt, validUntil time.Time) error {
	return validateAfterWithin(submittedAt, validUntil, 366*24*time.Hour)
}

func ValidateRequiredByDate(responseDeadline time.Time, requiredBy *time.Time) error {
	if requiredBy == nil {
		return nil
	}
	if responseDeadline.IsZero() || requiredBy.Before(responseDeadline) ||
		requiredBy.After(responseDeadline.AddDate(5, 0, 0)) {
		return ErrInvalidDate
	}
	return nil
}
