package suppliers

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/shananth/renovation-platform/backend/internal/foundation/money"
	"github.com/shananth/renovation-platform/backend/internal/identity"
)

const timeLayout = "2006-01-02T15:04:05Z07:00"

// --- DTOs ---

type moneyDTO struct {
	Amount   int64  `json:"amount"`
	Currency string `json:"currency"`
}

type supplierDTO struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// NameNormalized is exposed read-only so a client can predict a collision
	// before submitting. It is never accepted as input — the server always
	// derives it (design spec §4.1).
	NameNormalized string `json:"nameNormalized"`

	ContactPerson string `json:"contactPerson,omitempty"`
	Email         string `json:"email,omitempty"`
	Phone         string `json:"phone,omitempty"`
	Address       string `json:"address,omitempty"`

	MaterialCategories []string `json:"materialCategories"`
	Notes              string   `json:"notes,omitempty"`

	Active bool `json:"active"`

	Revision  int64  `json:"revision"`
	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`
}

type supplierOutput struct {
	Body supplierDTO
}

type offeringDTO struct {
	ID          string `json:"id"`
	SupplierID  string `json:"supplierId"`
	MaterialID  string `json:"materialId,omitempty"`
	ProductName string `json:"productName"`
	Brand       string `json:"brand,omitempty"`
	SKU         string `json:"sku,omitempty"`
	Description string `json:"description,omitempty"`
	Category    string `json:"category,omitempty"`
	Unit        string `json:"unit,omitempty"`

	ImageURL   string `json:"imageUrl,omitempty"`
	ProductURL string `json:"productUrl,omitempty"`

	// An INDICATIVE price: a contractor-recorded informational preview, never a
	// Supplier Offer. It never enters an RFQ and is never presented as a
	// supplier quotation (design spec §4.2).
	IndicativePrice     *moneyDTO `json:"indicativePrice,omitempty"`
	IndicativePriceAsOf string    `json:"indicativePriceAsOf,omitempty"`
	LastVerifiedAt      string    `json:"lastVerifiedAt,omitempty"`

	Active bool `json:"active"`

	// Computed, never persisted: Offering.Active AND Supplier.Active.
	EffectivelyAvailable bool   `json:"effectivelyAvailable"`
	SupplierName         string `json:"supplierName,omitempty"`
	SupplierActive       bool   `json:"supplierActive"`
	MaterialName         string `json:"materialName,omitempty"`

	Revision  int64  `json:"revision"`
	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`
}

type offeringOutput struct {
	Body offeringDTO
}

type preferenceDTO struct {
	MaterialID   string `json:"materialId"`
	MaterialName string `json:"materialName,omitempty"`
	SupplierID   string `json:"supplierId"`
	SupplierName string `json:"supplierName,omitempty"`
	// A preference whose supplier is retired stays readable and is reported
	// unavailable (design spec §4.3).
	SupplierActive bool `json:"supplierActive"`

	Revision  int64  `json:"revision"`
	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`
}

type preferenceOutput struct {
	Body preferenceDTO
}

// --- request inputs ---

type createSupplierInput struct {
	Body struct {
		Name               string   `json:"name" required:"true" minLength:"1"`
		ContactPerson      string   `json:"contactPerson,omitempty"`
		Email              string   `json:"email,omitempty"`
		Phone              string   `json:"phone,omitempty"`
		Address            string   `json:"address,omitempty"`
		MaterialCategories []string `json:"materialCategories,omitempty"`
		Notes              string   `json:"notes,omitempty"`
	}
}

type listSuppliersInput struct {
	Query    string `query:"q"`
	Category string `query:"category"`
	Active   string `query:"active"`
}

type listSuppliersOutput struct {
	Body struct {
		Suppliers []supplierDTO `json:"suppliers"`
	}
}

type getByIDInput struct {
	ID string `path:"id"`
}

type patchSupplierInput struct {
	ID   string `path:"id"`
	Body struct {
		ExpectedRevision int64 `json:"expectedRevision" required:"true"`

		Name               *string   `json:"name,omitempty"`
		ContactPerson      *string   `json:"contactPerson,omitempty"`
		Email              *string   `json:"email,omitempty"`
		Phone              *string   `json:"phone,omitempty"`
		Address            *string   `json:"address,omitempty"`
		MaterialCategories *[]string `json:"materialCategories,omitempty"`
		Notes              *string   `json:"notes,omitempty"`
	}
}

type setActiveInput struct {
	ID   string `path:"id"`
	Body struct {
		ExpectedRevision int64 `json:"expectedRevision" required:"true"`
		Active           bool  `json:"active" required:"true"`
	}
}

type createOfferingInput struct {
	Body struct {
		SupplierID  string `json:"supplierId" required:"true" minLength:"1"`
		MaterialID  string `json:"materialId,omitempty"`
		ProductName string `json:"productName" required:"true" minLength:"1"`
		Brand       string `json:"brand,omitempty"`
		SKU         string `json:"sku,omitempty"`
		Description string `json:"description,omitempty"`
		Category    string `json:"category,omitempty"`
		Unit        string `json:"unit,omitempty"`

		ImageURL   string `json:"imageUrl,omitempty"`
		ProductURL string `json:"productUrl,omitempty"`

		IndicativePriceAmount   *int64 `json:"indicativePriceAmount,omitempty"`
		IndicativePriceCurrency string `json:"indicativePriceCurrency,omitempty"`
		IndicativePriceAsOf     string `json:"indicativePriceAsOf,omitempty"`
		LastVerifiedAt          string `json:"lastVerifiedAt,omitempty"`
	}
}

// Resolve reports exhaustive request-level errors for the indicative-price
// cross-field invariant mirrored from ValidateIndicativePrice (§4.2):
// price present => positive amount, non-blank currency, non-blank unit, and
// a required as-of date; price absent => as-of must also be absent. This is
// defense-in-depth ahead of the identical service-level check — it exists so
// the client gets precise field-scoped errors before the handler even runs,
// not to replace the authoritative check.
func (i *createOfferingInput) Resolve(ctx huma.Context) []error {
	var errs []error
	if i.Body.IndicativePriceAmount == nil {
		if i.Body.IndicativePriceAsOf != "" {
			errs = append(errs, &huma.ErrorDetail{
				Message:  "price as-of date must not be set when no indicative price is provided",
				Location: "body.indicativePriceAsOf",
				Value:    i.Body.IndicativePriceAsOf,
			})
		}
		return errs
	}
	if *i.Body.IndicativePriceAmount <= 0 {
		errs = append(errs, &huma.ErrorDetail{
			Message:  "price must be greater than zero",
			Location: "body.indicativePriceAmount",
			Value:    *i.Body.IndicativePriceAmount,
		})
	}
	if i.Body.IndicativePriceCurrency == "" {
		errs = append(errs, &huma.ErrorDetail{
			Message:  "currency is required when an indicative price is provided",
			Location: "body.indicativePriceCurrency",
			Value:    i.Body.IndicativePriceCurrency,
		})
	}
	if i.Body.Unit == "" {
		errs = append(errs, &huma.ErrorDetail{
			Message:  "unit is required when an indicative price is provided",
			Location: "body.unit",
			Value:    i.Body.Unit,
		})
	}
	if i.Body.IndicativePriceAsOf == "" {
		errs = append(errs, &huma.ErrorDetail{
			Message:  "price as-of date is required when an indicative price is provided",
			Location: "body.indicativePriceAsOf",
			Value:    i.Body.IndicativePriceAsOf,
		})
	}
	return errs
}

var _ huma.Resolver = (*createOfferingInput)(nil)

type listOfferingsInput struct {
	SupplierID string `query:"supplierId"`
	MaterialID string `query:"materialId"`
	Active     string `query:"active"`
}

type listOfferingsOutput struct {
	Body struct {
		Offerings []offeringDTO `json:"offerings"`
	}
}

type patchOfferingInput struct {
	ID   string `path:"id"`
	Body struct {
		ExpectedRevision int64 `json:"expectedRevision" required:"true"`

		// Present so a mismatch can be REJECTED rather than silently ignored;
		// §4.2 makes it immutable.
		SupplierID *string `json:"supplierId,omitempty"`

		MaterialID      *string `json:"materialId,omitempty"`
		ClearMaterialID bool    `json:"clearMaterialId,omitempty"`

		ProductName *string `json:"productName,omitempty"`
		Brand       *string `json:"brand,omitempty"`
		SKU         *string `json:"sku,omitempty"`
		Description *string `json:"description,omitempty"`
		Category    *string `json:"category,omitempty"`
		Unit        *string `json:"unit,omitempty"`

		ImageURL        *string `json:"imageUrl,omitempty"`
		ClearImageURL   bool    `json:"clearImageUrl,omitempty"`
		ProductURL      *string `json:"productUrl,omitempty"`
		ClearProductURL bool    `json:"clearProductUrl,omitempty"`

		IndicativePriceAmount   *int64  `json:"indicativePriceAmount,omitempty"`
		IndicativePriceCurrency string  `json:"indicativePriceCurrency,omitempty"`
		IndicativePriceAsOf     *string `json:"indicativePriceAsOf,omitempty"`
		ClearIndicativePrice    bool    `json:"clearIndicativePrice,omitempty"`

		LastVerifiedAt      *string `json:"lastVerifiedAt,omitempty"`
		ClearLastVerifiedAt bool    `json:"clearLastVerifiedAt,omitempty"`
	}
}

type getPreferenceInput struct {
	MaterialID string `path:"materialId"`
}

type putPreferenceInput struct {
	MaterialID string `path:"materialId"`
	Body       struct {
		SupplierID       string `json:"supplierId" required:"true" minLength:"1"`
		ExpectedRevision int64  `json:"expectedRevision"`
	}
}

type deletePreferenceInput struct {
	MaterialID string `path:"materialId"`
	Body       struct {
		ExpectedRevision int64 `json:"expectedRevision" required:"true"`
	}
}

type noContentOutput struct {
	Status int
}

// RegisterHandlers registers the thirteen supplier routes of §13.3.
//
// The three material-scoped preference routes are registered HERE even though
// the path is material-scoped. That is a URL design choice, not an ownership
// claim: the preference belongs to suppliers, and internal/materials is not
// modified (design spec §13.3).
func RegisterHandlers(api huma.API, svc *Service) {
	huma.Register(api, huma.Operation{
		OperationID: "suppliers-create",
		Method:      http.MethodPost,
		Path:        "/suppliers",
		Summary:     "Create a Supplier directory entry; only name is required",
	}, func(ctx context.Context, input *createSupplierInput) (*supplierOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		created, err := svc.CreateSupplier(ctx, principal.CompanyID, principal.UserID,
			CreateSupplierInput{
				Name: input.Body.Name, ContactPerson: input.Body.ContactPerson,
				Email: input.Body.Email, Phone: input.Body.Phone, Address: input.Body.Address,
				MaterialCategories: input.Body.MaterialCategories, Notes: input.Body.Notes,
			})
		if err != nil {
			return nil, mapSuppliersError(err)
		}
		return &supplierOutput{Body: toSupplierDTO(created)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "suppliers-list",
		Method:      http.MethodGet,
		Path:        "/suppliers",
		Summary:     "Search Suppliers by name and filter by category or active state",
	}, func(ctx context.Context, input *listSuppliersInput) (*listSuppliersOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		active, err := parseOptionalBool(input.Active)
		if err != nil {
			return nil, huma.Error422UnprocessableEntity("active must be true or false")
		}

		found, err := svc.ListSuppliers(ctx, principal.CompanyID, SupplierFilter{
			Query: input.Query, Category: input.Category, Active: active,
		})
		if err != nil {
			return nil, mapSuppliersError(err)
		}
		out := &listSuppliersOutput{}
		out.Body.Suppliers = make([]supplierDTO, 0, len(found))
		for _, s := range found {
			out.Body.Suppliers = append(out.Body.Suppliers, toSupplierDTO(s))
		}
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "suppliers-get",
		Method:      http.MethodGet,
		Path:        "/suppliers/{id}",
		Summary:     "Get one Supplier",
	}, func(ctx context.Context, input *getByIDInput) (*supplierOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		found, err := svc.GetSupplier(ctx, principal.CompanyID, input.ID)
		if err != nil {
			return nil, mapSuppliersError(err)
		}
		return &supplierOutput{Body: toSupplierDTO(found)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "suppliers-update",
		Method:      http.MethodPatch,
		Path:        "/suppliers/{id}",
		Summary:     "Edit a Supplier under a Revision guard",
	}, func(ctx context.Context, input *patchSupplierInput) (*supplierOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		updated, err := svc.UpdateSupplier(ctx, principal.CompanyID, principal.UserID,
			input.ID, input.Body.ExpectedRevision, UpdateSupplierInput{
				Name: input.Body.Name, ContactPerson: input.Body.ContactPerson,
				Email: input.Body.Email, Phone: input.Body.Phone, Address: input.Body.Address,
				MaterialCategories: input.Body.MaterialCategories, Notes: input.Body.Notes,
			})
		if err != nil {
			return nil, mapSuppliersError(err)
		}
		return &supplierOutput{Body: toSupplierDTO(updated)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "suppliers-set-active",
		Method:      http.MethodPost,
		Path:        "/suppliers/{id}/active",
		Summary:     "Retire or reactivate a Supplier; no cascade to offerings or preferences",
	}, func(ctx context.Context, input *setActiveInput) (*supplierOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		updated, err := svc.SetSupplierActive(ctx, principal.CompanyID, principal.UserID,
			input.ID, input.Body.ExpectedRevision, input.Body.Active)
		if err != nil {
			return nil, mapSuppliersError(err)
		}
		return &supplierOutput{Body: toSupplierDTO(updated)}, nil
	})

	// --- Offerings ---

	huma.Register(api, huma.Operation{
		OperationID: "supplier-offerings-create",
		Method:      http.MethodPost,
		Path:        "/supplier-offerings",
		Summary:     "Record something an active Supplier sells",
	}, func(ctx context.Context, input *createOfferingInput) (*offeringOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}

		asOf, err := parseOptionalTime(input.Body.IndicativePriceAsOf)
		if err != nil {
			return nil, huma.Error422UnprocessableEntity("indicativePriceAsOf must be RFC3339")
		}
		verified, err := parseOptionalTime(input.Body.LastVerifiedAt)
		if err != nil {
			return nil, huma.Error422UnprocessableEntity("lastVerifiedAt must be RFC3339")
		}
		price, err := buildOptionalMoney(input.Body.IndicativePriceAmount,
			input.Body.IndicativePriceCurrency)
		if err != nil {
			return nil, mapSuppliersError(err)
		}

		created, err := svc.CreateOffering(ctx, principal.CompanyID, principal.UserID,
			CreateOfferingInput{
				SupplierID: input.Body.SupplierID, MaterialID: optionalString(input.Body.MaterialID),
				ProductName: input.Body.ProductName, Brand: input.Body.Brand,
				SKU: input.Body.SKU, Description: input.Body.Description,
				Category: input.Body.Category, Unit: input.Body.Unit,
				ImageURL:        optionalString(input.Body.ImageURL),
				ProductURL:      optionalString(input.Body.ProductURL),
				IndicativePrice: price, IndicativePriceAsOf: asOf, LastVerifiedAt: verified,
			})
		if err != nil {
			return nil, mapSuppliersError(err)
		}

		view, err := svc.GetOffering(ctx, principal.CompanyID, created.ID)
		if err != nil {
			return nil, mapSuppliersError(err)
		}
		return &offeringOutput{Body: toOfferingDTO(view)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "supplier-offerings-list",
		Method:      http.MethodGet,
		Path:        "/supplier-offerings",
		Summary:     "List Supplier Offerings with computed effective availability",
	}, func(ctx context.Context, input *listOfferingsInput) (*listOfferingsOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		active, err := parseOptionalBool(input.Active)
		if err != nil {
			return nil, huma.Error422UnprocessableEntity("active must be true or false")
		}

		views, err := svc.ListOfferings(ctx, principal.CompanyID, OfferingFilter{
			SupplierID: input.SupplierID, MaterialID: input.MaterialID, Active: active,
		})
		if err != nil {
			return nil, mapSuppliersError(err)
		}
		out := &listOfferingsOutput{}
		out.Body.Offerings = make([]offeringDTO, 0, len(views))
		for _, v := range views {
			out.Body.Offerings = append(out.Body.Offerings, toOfferingDTO(v))
		}
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "supplier-offerings-get",
		Method:      http.MethodGet,
		Path:        "/supplier-offerings/{id}",
		Summary:     "Get one Supplier Offering",
	}, func(ctx context.Context, input *getByIDInput) (*offeringOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		view, err := svc.GetOffering(ctx, principal.CompanyID, input.ID)
		if err != nil {
			return nil, mapSuppliersError(err)
		}
		return &offeringOutput{Body: toOfferingDTO(view)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "supplier-offerings-update",
		Method:      http.MethodPatch,
		Path:        "/supplier-offerings/{id}",
		Summary:     "Edit a Supplier Offering; supplierId is immutable",
	}, func(ctx context.Context, input *patchOfferingInput) (*offeringOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}

		update := UpdateOfferingInput{
			SupplierID:           input.Body.SupplierID,
			MaterialID:           input.Body.MaterialID,
			ClearMaterialID:      input.Body.ClearMaterialID,
			ProductName:          input.Body.ProductName,
			Brand:                input.Body.Brand,
			SKU:                  input.Body.SKU,
			Description:          input.Body.Description,
			Category:             input.Body.Category,
			Unit:                 input.Body.Unit,
			ImageURL:             input.Body.ImageURL,
			ClearImageURL:        input.Body.ClearImageURL,
			ProductURL:           input.Body.ProductURL,
			ClearProductURL:      input.Body.ClearProductURL,
			ClearIndicativePrice: input.Body.ClearIndicativePrice,
			ClearLastVerifiedAt:  input.Body.ClearLastVerifiedAt,
		}

		if input.Body.ClearIndicativePrice && input.Body.IndicativePriceAmount != nil {
			return nil, huma.Error422UnprocessableEntity(
				"indicativePriceAmount and clearIndicativePrice are mutually exclusive")
		}
		price, err := buildOptionalMoney(input.Body.IndicativePriceAmount,
			input.Body.IndicativePriceCurrency)
		if err != nil {
			return nil, mapSuppliersError(err)
		}
		update.IndicativePrice = price

		if input.Body.IndicativePriceAsOf != nil {
			asOf, err := parseOptionalTime(*input.Body.IndicativePriceAsOf)
			if err != nil {
				return nil, huma.Error422UnprocessableEntity("indicativePriceAsOf must be RFC3339")
			}
			update.IndicativePriceAsOf = asOf
		}
		if input.Body.LastVerifiedAt != nil {
			verified, err := parseOptionalTime(*input.Body.LastVerifiedAt)
			if err != nil {
				return nil, huma.Error422UnprocessableEntity("lastVerifiedAt must be RFC3339")
			}
			update.LastVerifiedAt = verified
		}

		updated, err := svc.UpdateOffering(ctx, principal.CompanyID, principal.UserID,
			input.ID, input.Body.ExpectedRevision, update)
		if err != nil {
			return nil, mapSuppliersError(err)
		}
		view, err := svc.GetOffering(ctx, principal.CompanyID, updated.ID)
		if err != nil {
			return nil, mapSuppliersError(err)
		}
		return &offeringOutput{Body: toOfferingDTO(view)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "supplier-offerings-set-active",
		Method:      http.MethodPost,
		Path:        "/supplier-offerings/{id}/active",
		Summary:     "Retire or reactivate a Supplier Offering",
	}, func(ctx context.Context, input *setActiveInput) (*offeringOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		updated, err := svc.SetOfferingActive(ctx, principal.CompanyID, principal.UserID,
			input.ID, input.Body.ExpectedRevision, input.Body.Active)
		if err != nil {
			return nil, mapSuppliersError(err)
		}
		view, err := svc.GetOffering(ctx, principal.CompanyID, updated.ID)
		if err != nil {
			return nil, mapSuppliersError(err)
		}
		return &offeringOutput{Body: toOfferingDTO(view)}, nil
	})

	// --- Preference (material-scoped paths, suppliers-owned) ---

	huma.Register(api, huma.Operation{
		OperationID: "materials-preferred-supplier-get",
		Method:      http.MethodGet,
		Path:        "/materials/{materialId}/preferred-supplier",
		Summary:     "Read the preferred Supplier for a Material",
	}, func(ctx context.Context, input *getPreferenceInput) (*preferenceOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		view, err := svc.GetPreferredSupplier(ctx, principal.CompanyID, input.MaterialID)
		if err != nil {
			return nil, mapSuppliersError(err)
		}
		return &preferenceOutput{Body: toPreferenceDTO(view)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "materials-preferred-supplier-set",
		Method:      http.MethodPut,
		Path:        "/materials/{materialId}/preferred-supplier",
		Summary:     "Revision-guarded create-or-replace of a Material's preferred Supplier",
	}, func(ctx context.Context, input *putPreferenceInput) (*preferenceOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		if _, err := svc.SetPreferredSupplier(ctx, principal.CompanyID, principal.UserID,
			input.MaterialID, input.Body.SupplierID, input.Body.ExpectedRevision); err != nil {
			return nil, mapSuppliersError(err)
		}
		view, err := svc.GetPreferredSupplier(ctx, principal.CompanyID, input.MaterialID)
		if err != nil {
			return nil, mapSuppliersError(err)
		}
		return &preferenceOutput{Body: toPreferenceDTO(view)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID:   "materials-preferred-supplier-clear",
		Method:        http.MethodDelete,
		Path:          "/materials/{materialId}/preferred-supplier",
		Summary:       "Clear a Material's preferred Supplier; the Material is untouched",
		DefaultStatus: http.StatusNoContent,
	}, func(ctx context.Context, input *deletePreferenceInput) (*noContentOutput, error) {
		principal, ok := identity.PrincipalFromContext(ctx)
		if !ok {
			return nil, huma.Error401Unauthorized("authentication required")
		}
		if err := svc.ClearPreferredSupplier(ctx, principal.CompanyID, principal.UserID,
			input.MaterialID, input.Body.ExpectedRevision); err != nil {
			return nil, mapSuppliersError(err)
		}
		return &noContentOutput{Status: http.StatusNoContent}, nil
	})
}

// mapSuppliersError implements the §13.5 status table.
//
// ErrUnclassifiedDuplicateKey is deliberately absent: a duplicate this package
// could not positively identify must not be given an invented client-facing
// meaning, so it falls through to 500 (design spec §12.4).
func mapSuppliersError(err error) error {
	switch {
	// --- 404 ---
	case errors.Is(err, ErrSupplierNotFound):
		return huma.Error404NotFound("supplier not found")
	case errors.Is(err, ErrSupplierOfferingNotFound):
		return huma.Error404NotFound("supplier offering not found")
	case errors.Is(err, ErrPreferenceNotFound):
		return huma.Error404NotFound("no preferred supplier is set for that material")
	case errors.Is(err, ErrMaterialNotFound):
		return huma.Error404NotFound("material not found")

	// --- 422 ---
	case errors.Is(err, ErrSupplierNameRequired):
		return huma.Error422UnprocessableEntity("supplier name is required")
	case errors.Is(err, ErrProductNameRequired):
		return huma.Error422UnprocessableEntity("productName is required")
	case errors.Is(err, ErrInvalidEmail):
		return huma.Error422UnprocessableEntity("the supplied email address is not valid")
	case errors.Is(err, ErrInvalidURL):
		return huma.Error422UnprocessableEntity(
			"url must be an http or https address without embedded credentials")
	case errors.Is(err, ErrInvalidIndicativePrice):
		return huma.Error422UnprocessableEntity(
			"an indicative price needs a positive amount, a currency, an as-of date and a unit")
	case errors.Is(err, ErrSupplierIDImmutable):
		return huma.Error422UnprocessableEntity("supplierId is immutable after creation")
	case errors.Is(err, ErrSupplierInactive):
		return huma.Error422UnprocessableEntity("that supplier is inactive")

	// --- 409 ---
	case errors.Is(err, ErrSupplierNameTaken):
		return huma.Error409Conflict(
			"a supplier with that name already exists; reactivate or rename the existing record")
	case errors.Is(err, ErrPreferenceAlreadyExists):
		return huma.Error409Conflict("a preferred supplier already exists for that material")
	case errors.Is(err, ErrRevisionMismatch):
		return huma.Error409Conflict("record changed since it was read")

	default:
		return err
	}
}

// --- projections ---

func toSupplierDTO(s Supplier) supplierDTO {
	categories := s.MaterialCategories
	if categories == nil {
		categories = []string{}
	}
	return supplierDTO{
		ID: s.ID, Name: s.Name, NameNormalized: s.NameNormalized,
		ContactPerson: s.ContactPerson, Email: s.Email,
		Phone: s.Phone, Address: s.Address,
		MaterialCategories: categories, Notes: s.Notes,
		Active:    s.Active,
		Revision:  s.Revision,
		CreatedAt: s.CreatedAt.Format(timeLayout),
		UpdatedAt: s.UpdatedAt.Format(timeLayout),
	}
}

func toOfferingDTO(v OfferingView) offeringDTO {
	o := v.Offering
	dto := offeringDTO{
		ID: o.ID, SupplierID: o.SupplierID,
		ProductName: o.ProductName, Brand: o.Brand, SKU: o.SKU,
		Description: o.Description, Category: o.Category, Unit: o.Unit,
		Active: o.Active,

		EffectivelyAvailable: v.EffectivelyAvailable,
		SupplierName:         v.SupplierName,
		SupplierActive:       v.SupplierActive,
		MaterialName:         v.MaterialName,

		Revision:  o.Revision,
		CreatedAt: o.CreatedAt.Format(timeLayout),
		UpdatedAt: o.UpdatedAt.Format(timeLayout),
	}
	if o.MaterialID != nil {
		dto.MaterialID = *o.MaterialID
	}
	if o.ImageURL != nil {
		dto.ImageURL = *o.ImageURL
	}
	if o.ProductURL != nil {
		dto.ProductURL = *o.ProductURL
	}
	if o.IndicativePrice != nil {
		dto.IndicativePrice = &moneyDTO{
			Amount: o.IndicativePrice.Amount, Currency: o.IndicativePrice.Currency,
		}
	}
	if o.IndicativePriceAsOf != nil {
		dto.IndicativePriceAsOf = o.IndicativePriceAsOf.Format(timeLayout)
	}
	if o.LastVerifiedAt != nil {
		dto.LastVerifiedAt = o.LastVerifiedAt.Format(timeLayout)
	}
	return dto
}

func toPreferenceDTO(v PreferenceView) preferenceDTO {
	return preferenceDTO{
		MaterialID: v.Preference.MaterialID, MaterialName: v.MaterialName,
		SupplierID: v.Preference.SupplierID, SupplierName: v.SupplierName,
		SupplierActive: v.SupplierActive,
		Revision:       v.Preference.Revision,
		CreatedAt:      v.Preference.CreatedAt.Format(timeLayout),
		UpdatedAt:      v.Preference.UpdatedAt.Format(timeLayout),
	}
}

// --- input helpers ---

// buildOptionalMoney assembles a Money from the amount/currency pair. A nil
// amount means absent; an amount without a currency is invalid rather than
// silently defaulted, because guessing a currency would produce a wrong price.
func buildOptionalMoney(amount *int64, currency string) (*money.Money, error) {
	if amount == nil {
		return nil, nil
	}
	if currency == "" {
		return nil, ErrInvalidIndicativePrice
	}
	m := money.New(*amount, currency)
	return &m, nil
}

func optionalString(v string) *string {
	if v == "" {
		return nil
	}
	return &v
}

// parseOptionalBool maps "" to nil so an omitted filter means "either", which a
// plain bool could not express.
func parseOptionalBool(v string) (*bool, error) {
	switch v {
	case "":
		return nil, nil
	case "true":
		t := true
		return &t, nil
	case "false":
		f := false
		return &f, nil
	default:
		return nil, errors.New("suppliers: not a boolean")
	}
}

// parseOptionalTime treats "" as absent rather than as a parse error.
func parseOptionalTime(v string) (*time.Time, error) {
	if v == "" {
		return nil, nil
	}
	parsed, err := time.Parse(timeLayout, v)
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}
