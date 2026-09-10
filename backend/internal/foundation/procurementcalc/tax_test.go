package procurementcalc

import (
	"errors"
	"strings"
	"testing"

	"github.com/shananth/renovation-platform/backend/internal/foundation/money"
)

func TestCalculateTaxNotApplicableReturnsZero(t *testing.T) {
	result, err := CalculateTax(TaxCalculationInput{
		Currency: "MYR",
		Tax: SupplierOfferTax{
			Mode: TaxModeNotApplicable,
		},
		QuotedLines: []TaxableQuotedLine{
			{RFQLineID: "line-1", Subtotal: money.New(12_345, "MYR")},
		},
	})
	if err != nil {
		t.Fatalf("CalculateTax returned an unexpected error: %v", err)
	}
	if result.Total != money.New(0, "MYR") {
		t.Fatalf("tax total = %+v, want zero MYR", result.Total)
	}
	if len(result.Lines) != 0 {
		t.Fatalf("not_applicable returned %d line taxes, want none", len(result.Lines))
	}
}

func TestCalculateTaxNotApplicableRejectsCrossModeFields(t *testing.T) {
	rate := money.RateBPS(600)
	tests := []struct {
		name  string
		input TaxCalculationInput
	}{
		{
			name: "offer-level record",
			input: TaxCalculationInput{
				Currency: "MYR",
				Tax: SupplierOfferTax{
					Mode: TaxModeNotApplicable,
					OfferLevel: &QuotedOfferTax{
						TaxType:   TaxTypeOther,
						TaxAmount: money.New(100, "MYR"),
						BasisNote: "quoted lump-sum tax",
					},
				},
			},
		},
		{
			name: "line-level record",
			input: TaxCalculationInput{
				Currency: "MYR",
				Tax:      SupplierOfferTax{Mode: TaxModeNotApplicable},
				QuotedLines: []TaxableQuotedLine{{
					RFQLineID: "line-1",
					Subtotal:  money.New(10_000, "MYR"),
					Tax: &QuotedLineTax{
						TaxType:            TaxTypeServiceTax,
						RateBPS:            &rate,
						RegistrationNumber: "SST-123",
					},
				}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := CalculateTax(tt.input)
			if !errors.Is(err, ErrTaxFieldsNotAllowed) {
				t.Fatalf("CalculateTax error = %v, want ErrTaxFieldsNotAllowed", err)
			}
		})
	}
}

func TestCalculateTaxLineLevelSumsIndividuallyRoundedAmounts(t *testing.T) {
	rate := money.RateBPS(5000)
	lineTax := func() *QuotedLineTax {
		return &QuotedLineTax{
			TaxType:            TaxTypeSalesTax,
			RateBPS:            &rate,
			RegistrationNumber: "SST-123",
		}
	}

	result, err := CalculateTax(TaxCalculationInput{
		Currency: "MYR",
		Tax:      SupplierOfferTax{Mode: TaxModeLineLevel},
		QuotedLines: []TaxableQuotedLine{
			{RFQLineID: "line-1", Subtotal: money.New(1, "MYR"), Tax: lineTax()},
			{RFQLineID: "line-2", Subtotal: money.New(1, "MYR"), Tax: lineTax()},
		},
	})
	if err != nil {
		t.Fatalf("CalculateTax returned an unexpected error: %v", err)
	}
	if len(result.Lines) != 2 {
		t.Fatalf("line tax count = %d, want 2", len(result.Lines))
	}
	for i, line := range result.Lines {
		if line.Amount != money.New(1, "MYR") {
			t.Fatalf("line tax %d = %+v, want one MYR cent", i, line.Amount)
		}
	}
	if result.Total != money.New(2, "MYR") {
		t.Fatalf("tax total = %+v, want two MYR cents", result.Total)
	}
}

func TestCalculateTaxLineLevelValidatesEachRecord(t *testing.T) {
	validRate := money.RateBPS(600)
	zeroRate := money.RateBPS(0)
	tooHighRate := money.RateBPS(10_001)

	tests := []struct {
		name string
		line TaxableQuotedLine
		want error
	}{
		{
			name: "tax record required",
			line: TaxableQuotedLine{RFQLineID: "line-1", Subtotal: money.New(100, "MYR")},
			want: ErrLineTaxRequired,
		},
		{
			name: "exempt forbids rate",
			line: TaxableQuotedLine{
				RFQLineID: "line-1",
				Subtotal:  money.New(100, "MYR"),
				Tax: &QuotedLineTax{
					TaxType: TaxTypeServiceTax,
					Exempt:  true,
					RateBPS: &validRate,
				},
			},
			want: ErrTaxRateNotAllowed,
		},
		{
			name: "non-exempt requires rate",
			line: TaxableQuotedLine{
				RFQLineID: "line-1",
				Subtotal:  money.New(100, "MYR"),
				Tax: &QuotedLineTax{
					TaxType:            TaxTypeServiceTax,
					RegistrationNumber: "SST-123",
				},
			},
			want: ErrTaxRateRequired,
		},
		{
			name: "zero rate rejected",
			line: TaxableQuotedLine{
				RFQLineID: "line-1",
				Subtotal:  money.New(100, "MYR"),
				Tax: &QuotedLineTax{
					TaxType:            TaxTypeServiceTax,
					RateBPS:            &zeroRate,
					RegistrationNumber: "SST-123",
				},
			},
			want: ErrInvalidTaxRate,
		},
		{
			name: "rate over one hundred percent rejected",
			line: TaxableQuotedLine{
				RFQLineID: "line-1",
				Subtotal:  money.New(100, "MYR"),
				Tax: &QuotedLineTax{
					TaxType:            TaxTypeServiceTax,
					RateBPS:            &tooHighRate,
					RegistrationNumber: "SST-123",
				},
			},
			want: ErrInvalidTaxRate,
		},
		{
			name: "service tax requires registration",
			line: TaxableQuotedLine{
				RFQLineID: "line-1",
				Subtotal:  money.New(100, "MYR"),
				Tax:       &QuotedLineTax{TaxType: TaxTypeServiceTax, RateBPS: &validRate},
			},
			want: ErrTaxRegistrationRequired,
		},
		{
			name: "sales tax requires registration",
			line: TaxableQuotedLine{
				RFQLineID: "line-1",
				Subtotal:  money.New(100, "MYR"),
				Tax:       &QuotedLineTax{TaxType: TaxTypeSalesTax, RateBPS: &validRate},
			},
			want: ErrTaxRegistrationRequired,
		},
		{
			name: "other tax requires basis note",
			line: TaxableQuotedLine{
				RFQLineID: "line-1",
				Subtotal:  money.New(100, "MYR"),
				Tax:       &QuotedLineTax{TaxType: TaxTypeOther, RateBPS: &validRate},
			},
			want: ErrTaxBasisNoteRequired,
		},
		{
			name: "unknown tax type rejected",
			line: TaxableQuotedLine{
				RFQLineID: "line-1",
				Subtotal:  money.New(100, "MYR"),
				Tax:       &QuotedLineTax{TaxType: "vat", RateBPS: &validRate},
			},
			want: ErrInvalidTaxType,
		},
		{
			name: "foreign currency rejected",
			line: TaxableQuotedLine{
				RFQLineID: "line-1",
				Subtotal:  money.New(100, "USD"),
				Tax: &QuotedLineTax{
					TaxType:            TaxTypeServiceTax,
					RateBPS:            &validRate,
					RegistrationNumber: "SST-123",
				},
			},
			want: ErrTaxCurrencyMismatch,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := CalculateTax(TaxCalculationInput{
				Currency:    "MYR",
				Tax:         SupplierOfferTax{Mode: TaxModeLineLevel},
				QuotedLines: []TaxableQuotedLine{tt.line},
			})
			if !errors.Is(err, tt.want) {
				t.Fatalf("CalculateTax error = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestCalculateTaxLineLevelExemptRecordReturnsZero(t *testing.T) {
	result, err := CalculateTax(TaxCalculationInput{
		Currency: "MYR",
		Tax:      SupplierOfferTax{Mode: TaxModeLineLevel},
		QuotedLines: []TaxableQuotedLine{{
			RFQLineID: "line-1",
			Subtotal:  money.New(10_000, "MYR"),
			Tax: &QuotedLineTax{
				TaxType: TaxTypeServiceTax,
				Exempt:  true,
			},
		}},
	})
	if err != nil {
		t.Fatalf("CalculateTax returned an unexpected error: %v", err)
	}
	if len(result.Lines) != 1 || result.Lines[0].Amount != money.New(0, "MYR") {
		t.Fatalf("exempt line taxes = %+v, want one zero-MYR record", result.Lines)
	}
	if result.Total != money.New(0, "MYR") {
		t.Fatalf("tax total = %+v, want zero MYR", result.Total)
	}
}

func TestCalculateTaxOfferLevelPreservesQuotedAmount(t *testing.T) {
	result, err := CalculateTax(TaxCalculationInput{
		Currency: "MYR",
		Tax: SupplierOfferTax{
			Mode: TaxModeOfferLevel,
			OfferLevel: &QuotedOfferTax{
				TaxType:            TaxTypeServiceTax,
				TaxAmount:          money.New(12_345, "MYR"),
				BasisNote:          "Supplier-quoted tax for the complete offer",
				RegistrationNumber: "SST-123",
			},
		},
		QuotedLines: []TaxableQuotedLine{
			{RFQLineID: "line-1", Subtotal: money.New(200_000, "MYR")},
		},
	})
	if err != nil {
		t.Fatalf("CalculateTax returned an unexpected error: %v", err)
	}
	if result.Total != money.New(12_345, "MYR") {
		t.Fatalf("tax total = %+v, want the exact quoted MYR 123.45", result.Total)
	}
	if len(result.Lines) != 0 {
		t.Fatalf("offer_level returned %d line taxes, want none", len(result.Lines))
	}
}

func TestCalculateTaxOfferLevelValidatesRecord(t *testing.T) {
	valid := func() *QuotedOfferTax {
		return &QuotedOfferTax{
			TaxType:            TaxTypeServiceTax,
			TaxAmount:          money.New(100, "MYR"),
			BasisNote:          "complete-offer quoted tax",
			RegistrationNumber: "SST-123",
		}
	}

	tests := []struct {
		name string
		tax  *QuotedOfferTax
		line *QuotedLineTax
		want error
	}{
		{name: "record required", want: ErrOfferTaxRequired},
		{
			name: "zero amount rejected",
			tax: &QuotedOfferTax{
				TaxType: TaxTypeOther, TaxAmount: money.New(0, "MYR"), BasisNote: "basis",
			},
			want: ErrInvalidTaxAmount,
		},
		{
			name: "negative amount rejected",
			tax: &QuotedOfferTax{
				TaxType: TaxTypeOther, TaxAmount: money.New(-1, "MYR"), BasisNote: "basis",
			},
			want: ErrInvalidTaxAmount,
		},
		{
			name: "foreign currency rejected",
			tax: &QuotedOfferTax{
				TaxType: TaxTypeOther, TaxAmount: money.New(100, "USD"), BasisNote: "basis",
			},
			want: ErrTaxCurrencyMismatch,
		},
		{
			name: "basis note required",
			tax: &QuotedOfferTax{
				TaxType: TaxTypeOther, TaxAmount: money.New(100, "MYR"),
			},
			want: ErrTaxBasisNoteRequired,
		},
		{
			name: "service tax requires registration",
			tax: &QuotedOfferTax{
				TaxType: TaxTypeServiceTax, TaxAmount: money.New(100, "MYR"), BasisNote: "basis",
			},
			want: ErrTaxRegistrationRequired,
		},
		{
			name: "sales tax requires registration",
			tax: &QuotedOfferTax{
				TaxType: TaxTypeSalesTax, TaxAmount: money.New(100, "MYR"), BasisNote: "basis",
			},
			want: ErrTaxRegistrationRequired,
		},
		{
			name: "unknown type rejected",
			tax: &QuotedOfferTax{
				TaxType: "vat", TaxAmount: money.New(100, "MYR"), BasisNote: "basis",
			},
			want: ErrInvalidTaxType,
		},
		{
			name: "line tax forbidden",
			tax:  valid(),
			line: &QuotedLineTax{TaxType: TaxTypeOther, Exempt: true, BasisNote: "basis"},
			want: ErrTaxFieldsNotAllowed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := CalculateTax(TaxCalculationInput{
				Currency: "MYR",
				Tax: SupplierOfferTax{
					Mode:       TaxModeOfferLevel,
					OfferLevel: tt.tax,
				},
				QuotedLines: []TaxableQuotedLine{{
					RFQLineID: "line-1",
					Subtotal:  money.New(1_000, "MYR"),
					Tax:       tt.line,
				}},
			})
			if !errors.Is(err, tt.want) {
				t.Fatalf("CalculateTax error = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestCalculateTaxOfferLevelOtherAllowsMissingRegistration(t *testing.T) {
	_, err := CalculateTax(TaxCalculationInput{
		Currency: "MYR",
		Tax: SupplierOfferTax{
			Mode: TaxModeOfferLevel,
			OfferLevel: &QuotedOfferTax{
				TaxType:   TaxTypeOther,
				TaxAmount: money.New(100, "MYR"),
				BasisNote: "Supplier-defined local levy",
			},
		},
	})
	if err != nil {
		t.Fatalf("CalculateTax returned an unexpected error: %v", err)
	}
}

func TestCalculateTaxRejectsUnsupportedCurrencyAndOversizedText(t *testing.T) {
	rate := money.RateBPS(600)
	tests := []struct {
		name  string
		input TaxCalculationInput
		want  error
	}{
		{
			name: "unsupported rfq currency",
			input: TaxCalculationInput{
				Currency: "USD",
				Tax:      SupplierOfferTax{Mode: TaxModeNotApplicable},
			},
			want: ErrUnsupportedCurrency,
		},
		{
			name: "line registration number too long",
			input: TaxCalculationInput{
				Currency: "MYR",
				Tax:      SupplierOfferTax{Mode: TaxModeLineLevel},
				QuotedLines: []TaxableQuotedLine{{
					RFQLineID: "line-1",
					Subtotal:  money.New(100, "MYR"),
					Tax: &QuotedLineTax{
						TaxType:            TaxTypeServiceTax,
						RateBPS:            &rate,
						RegistrationNumber: strings.Repeat("r", MaxTaxRegistrationNumberLength+1),
					},
				}},
			},
			want: ErrTaxTextTooLong,
		},
		{
			name: "line basis note too long",
			input: TaxCalculationInput{
				Currency: "MYR",
				Tax:      SupplierOfferTax{Mode: TaxModeLineLevel},
				QuotedLines: []TaxableQuotedLine{{
					RFQLineID: "line-1",
					Subtotal:  money.New(100, "MYR"),
					Tax: &QuotedLineTax{
						TaxType:   TaxTypeOther,
						RateBPS:   &rate,
						BasisNote: strings.Repeat("b", MaxTaxBasisNoteLength+1),
					},
				}},
			},
			want: ErrTaxTextTooLong,
		},
		{
			name: "offer basis note too long",
			input: TaxCalculationInput{
				Currency: "MYR",
				Tax: SupplierOfferTax{
					Mode: TaxModeOfferLevel,
					OfferLevel: &QuotedOfferTax{
						TaxType:   TaxTypeOther,
						TaxAmount: money.New(100, "MYR"),
						BasisNote: strings.Repeat("b", MaxTaxBasisNoteLength+1),
					},
				},
			},
			want: ErrTaxTextTooLong,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := CalculateTax(tt.input)
			if !errors.Is(err, tt.want) {
				t.Fatalf("CalculateTax error = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestValidateTaxSelectionRequiresCompleteOfferForOfferLevel(t *testing.T) {
	quoted := []string{"line-1", "line-2"}
	tests := []struct {
		name     string
		mode     TaxMode
		selected []string
		wantErr  bool
	}{
		{name: "offer level none selected", mode: TaxModeOfferLevel},
		{
			name: "offer level all selected", mode: TaxModeOfferLevel,
			selected: []string{"line-1", "line-2"},
		},
		{
			name: "offer level partial rejected", mode: TaxModeOfferLevel,
			selected: []string{"line-1"}, wantErr: true,
		},
		{
			name: "line level partial allowed", mode: TaxModeLineLevel,
			selected: []string{"line-1"},
		},
		{
			name: "not applicable partial allowed", mode: TaxModeNotApplicable,
			selected: []string{"line-1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateTaxSelection(tt.mode, quoted, tt.selected)
			if tt.wantErr && !errors.Is(err, ErrOfferLevelTaxRequiresCompleteSelection) {
				t.Fatalf("ValidateTaxSelection error = %v, want complete-selection error", err)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("ValidateTaxSelection returned an unexpected error: %v", err)
			}
		})
	}
}

func TestCalculateTaxRejectsInvalidQuotedLineSet(t *testing.T) {
	rate := money.RateBPS(600)
	validTax := &QuotedLineTax{
		TaxType:            TaxTypeServiceTax,
		RateBPS:            &rate,
		RegistrationNumber: "SST-123",
	}
	tests := []struct {
		name  string
		lines []TaxableQuotedLine
	}{
		{
			name: "blank line id",
			lines: []TaxableQuotedLine{{
				Subtotal: money.New(100, "MYR"),
				Tax:      validTax,
			}},
		},
		{
			name: "duplicate line id",
			lines: []TaxableQuotedLine{
				{RFQLineID: "line-1", Subtotal: money.New(100, "MYR"), Tax: validTax},
				{RFQLineID: "line-1", Subtotal: money.New(100, "MYR"), Tax: validTax},
			},
		},
		{
			name: "zero subtotal",
			lines: []TaxableQuotedLine{{
				RFQLineID: "line-1",
				Subtotal:  money.New(0, "MYR"),
				Tax:       validTax,
			}},
		},
		{
			name: "negative subtotal",
			lines: []TaxableQuotedLine{{
				RFQLineID: "line-1",
				Subtotal:  money.New(-1, "MYR"),
				Tax:       validTax,
			}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := CalculateTax(TaxCalculationInput{
				Currency:    "MYR",
				Tax:         SupplierOfferTax{Mode: TaxModeLineLevel},
				QuotedLines: tt.lines,
			})
			if !errors.Is(err, ErrInvalidLineTax) {
				t.Fatalf("CalculateTax error = %v, want ErrInvalidLineTax", err)
			}
		})
	}
}
