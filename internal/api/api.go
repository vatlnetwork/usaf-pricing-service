package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"

	"usaf-pricing-service/domain"
	"usaf-pricing-service/internal/store"
)

const maxBodyBytes = 1 << 20

type API struct {
	programs store.VendorPrograms
	timeout  time.Duration
	logger   *slog.Logger
}

// NewHandler exposes the API without authentication.
func NewHandler(programs store.VendorPrograms, timeout time.Duration, logger *slog.Logger) http.Handler {
	a := &API{programs: programs, timeout: timeout, logger: logger}
	mux := http.NewServeMux()
	registerUI(mux)
	mux.HandleFunc("POST /products/dealer-prices", a.handle(a.dealerPrices))
	mux.HandleFunc("POST /vendor-programs/preview", a.handle(a.previewOrder))
	mux.HandleFunc("POST /vendor-programs/{id}/price-order", a.handle(a.priceOrder))
	mux.HandleFunc("POST /vendor-programs", a.handle(a.create))
	mux.HandleFunc("GET /vendor-programs", a.handle(a.list))
	mux.HandleFunc("GET /vendor-programs/by-code/{vendorCode}", a.handle(a.getByVendorCode))
	mux.HandleFunc("GET /vendor-programs/{id}", a.handle(a.get))
	mux.HandleFunc("PUT /vendor-programs/{id}", a.handle(a.replace))
	mux.HandleFunc("DELETE /vendor-programs/{id}", a.handle(a.delete))
	mux.HandleFunc("PUT /vendor-programs/{id}/expiry", a.handle(a.updateExpiry))
	mux.HandleFunc("PUT /vendor-programs/{id}/quote-enabled", a.handle(a.updateQuoteEnabled))
	mux.HandleFunc("PUT /vendor-programs/{id}/discount-options", a.handle(a.updateDiscountOptions))
	mux.HandleFunc("PATCH /vendor-programs/{id}/product-overrides", a.handle(a.upsertProductOverrides))
	mux.HandleFunc("DELETE /vendor-programs/{id}/product-overrides", a.handle(a.removeProductOverrides))
	mux.HandleFunc("PATCH /vendor-programs/{id}/product-group-overrides", a.handle(a.upsertProductGroupOverrides))
	mux.HandleFunc("DELETE /vendor-programs/{id}/product-group-overrides", a.handle(a.removeProductGroupOverrides))
	mux.HandleFunc("GET /vendor-programs/{id}/products/{productID}/discount-options", a.handle(a.getProductDiscountOptions))
	return mux
}

type requestError struct {
	status  int
	message string
}

func (e *requestError) Error() string { return e.message }

func badRequest(message string) error {
	return &requestError{status: http.StatusBadRequest, message: message}
}

func (a *API) handle(fn func(http.ResponseWriter, *http.Request) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), a.timeout)
		defer cancel()
		if err := fn(w, r.WithContext(ctx)); err != nil {
			a.writeError(w, r, err)
		}
	}
}

func (a *API) writeError(w http.ResponseWriter, r *http.Request, err error) {
	status, message := http.StatusInternalServerError, "internal server error"
	var reqErr *requestError
	switch {
	case errors.As(err, &reqErr):
		status, message = reqErr.status, reqErr.message
	case errors.Is(err, store.ErrInvalidID):
		status, message = http.StatusBadRequest, err.Error()
	case errors.Is(err, store.ErrNotFound):
		status, message = http.StatusNotFound, err.Error()
	case errors.Is(err, domain.ErrVendorProgramExpired):
		status, message = http.StatusNotFound, err.Error()
	case errors.Is(err, store.ErrConflict), errors.Is(err, store.ErrDuplicateVendorCode):
		status, message = http.StatusConflict, err.Error()
	case errors.Is(err, context.DeadlineExceeded):
		status, message = http.StatusGatewayTimeout, "database operation timed out"
	case errors.Is(err, context.Canceled):
		status, message = http.StatusRequestTimeout, "request canceled"
	default:
		a.logger.Error("API request failed", "method", r.Method, "path", r.URL.Path, "error", err)
	}
	writeJSON(w, status, map[string]string{"error": message})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func decodeBody[T any](w http.ResponseWriter, r *http.Request) (*T, error) {
	if contentType := r.Header.Get("Content-Type"); contentType != "" {
		mediaType, _, err := mime.ParseMediaType(contentType)
		if err != nil || mediaType != "application/json" {
			return nil, &requestError{status: http.StatusUnsupportedMediaType, message: "Content-Type must be application/json"}
		}
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var body *T
	if err := decoder.Decode(&body); err != nil {
		return nil, bodyError(err)
	}
	if body == nil {
		return nil, badRequest("request body must not be null")
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		if err != nil {
			return nil, bodyError(err)
		}
		return nil, badRequest("request body must contain a single JSON value")
	}
	return body, nil
}

func bodyError(err error) error {
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		return &requestError{status: http.StatusRequestEntityTooLarge, message: "request body must not exceed 1 MiB"}
	}
	return badRequest(fmt.Sprintf("invalid JSON request body: %v", err))
}

type programInput struct {
	VendorCode            string                        `json:"vendor_code"`
	Scenarios             []domain.PricingScenario      `json:"scenarios"`
	SelectionPolicy       string                        `json:"selection_policy"`
	Vendor                string                        `json:"vendor"`
	QuoteEnabled          bool                          `json:"quote_enabled"`
	DiscountOptions       []domain.DiscountOption       `json:"discount_options"`
	ProductOverrides      []domain.ProductOverride      `json:"product_overrides"`
	ProductGroupOverrides []domain.ProductGroupOverride `json:"product_group_overrides"`
	ExpiresAt             *time.Time                    `json:"expires_at"`
}

func (a *API) create(w http.ResponseWriter, r *http.Request) error {
	body, err := decodeBody[programInput](w, r)
	if err != nil {
		return err
	}
	program, err := domain.NewVendorProgram(body.Vendor, body.DiscountOptions, body.ProductOverrides, body.ProductGroupOverrides)
	if err != nil {
		return badRequest(err.Error())
	}
	if err := program.UpdateVendorCode(body.VendorCode); err != nil {
		return badRequest(err.Error())
	}
	if err := program.UpdateScenarios(body.Scenarios, body.SelectionPolicy); err != nil {
		return badRequest(err.Error())
	}
	program.UpdateQuoteEnabled(body.QuoteEnabled)
	if body.ExpiresAt != nil {
		program.UpdateExpiry(body.ExpiresAt)
	}
	if err := a.programs.Create(r.Context(), program); err != nil {
		return err
	}
	w.Header().Set("Location", "/vendor-programs/"+program.Id)
	writeJSON(w, http.StatusCreated, program)
	return nil
}

// replace saves a complete editor draft in one persistence operation. The
// optional version also protects against edits made since the form was loaded.
func (a *API) replace(w http.ResponseWriter, r *http.Request) error {
	body, err := decodeBody[struct {
		programInput
		ExpectedUpdatedAt *time.Time `json:"expected_updated_at"`
	}](w, r)
	if err != nil {
		return err
	}
	candidate, err := domain.NewVendorProgram(body.Vendor, body.DiscountOptions, body.ProductOverrides, body.ProductGroupOverrides)
	if err != nil {
		return badRequest(err.Error())
	}
	if err := candidate.UpdateVendorCode(body.VendorCode); err != nil {
		return badRequest(err.Error())
	}
	if err := candidate.UpdateScenarios(body.Scenarios, body.SelectionPolicy); err != nil {
		return badRequest(err.Error())
	}
	candidate.UpdateQuoteEnabled(body.QuoteEnabled)
	candidate.UpdateExpiry(body.ExpiresAt)
	program, err := a.programs.Update(r.Context(), r.PathValue("id"), func(current *domain.VendorProgram) error {
		if body.ExpectedUpdatedAt != nil && !current.UpdatedAt.Truncate(time.Millisecond).Equal(body.ExpectedUpdatedAt.Truncate(time.Millisecond)) {
			return store.ErrConflict
		}
		candidate.Id, candidate.CreatedAt = current.Id, current.CreatedAt
		candidate.UpdatedAt = time.Now().UTC().Truncate(time.Millisecond)
		if !candidate.UpdatedAt.After(current.UpdatedAt) {
			candidate.UpdatedAt = current.UpdatedAt.Truncate(time.Millisecond).Add(time.Millisecond)
		}
		*current = *candidate
		return nil
	})
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, program)
	return nil
}

func (a *API) get(w http.ResponseWriter, r *http.Request) error {
	program, err := a.programs.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, program)
	return nil
}

func (a *API) getByVendorCode(w http.ResponseWriter, r *http.Request) error {
	code := r.PathValue("vendorCode")
	if strings.TrimSpace(code) == "" {
		return badRequest("vendor code must be nonblank")
	}
	program, err := a.programs.GetByVendorCode(r.Context(), code)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, program)
	return nil
}

func (a *API) list(w http.ResponseWriter, r *http.Request) error {
	limit, offset := int64(100), int64(0)
	var err error
	if value := r.URL.Query().Get("limit"); value != "" {
		limit, err = strconv.ParseInt(value, 10, 64)
		if err != nil || limit < 1 || limit > 1000 {
			return badRequest("limit must be between 1 and 1000")
		}
	}
	if value := r.URL.Query().Get("offset"); value != "" {
		offset, err = strconv.ParseInt(value, 10, 64)
		if err != nil || offset < 0 {
			return badRequest("offset must be a nonnegative integer")
		}
	}
	programs, err := a.programs.List(r.Context(), limit, offset)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, programs)
	return nil
}

func (a *API) delete(w http.ResponseWriter, r *http.Request) error {
	if err := a.programs.Delete(r.Context(), r.PathValue("id")); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (a *API) update(w http.ResponseWriter, r *http.Request, mutate func(*domain.VendorProgram) error) error {
	program, err := a.programs.Update(r.Context(), r.PathValue("id"), func(program *domain.VendorProgram) error {
		if err := mutate(program); err != nil {
			return badRequest(err.Error())
		}
		return nil
	})
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, program)
	return nil
}

func (a *API) updateExpiry(w http.ResponseWriter, r *http.Request) error {
	body, err := decodeBody[struct {
		ExpiresAt json.RawMessage `json:"expires_at"`
	}](w, r)
	if err != nil {
		return err
	}
	if len(body.ExpiresAt) == 0 {
		return badRequest("expires_at is required; use null to clear the expiry")
	}
	var expiresAt *time.Time
	if err := json.Unmarshal(body.ExpiresAt, &expiresAt); err != nil {
		return badRequest("expires_at must be an RFC 3339 timestamp with a timezone, or null")
	}
	return a.update(w, r, func(program *domain.VendorProgram) error {
		program.UpdateExpiry(expiresAt)
		return nil
	})
}

func (a *API) updateQuoteEnabled(w http.ResponseWriter, r *http.Request) error {
	body, err := decodeBody[struct {
		QuoteEnabled *bool `json:"quote_enabled"`
	}](w, r)
	if err != nil {
		return err
	}
	if body.QuoteEnabled == nil {
		return badRequest("quote_enabled is required and must be a boolean")
	}
	return a.update(w, r, func(program *domain.VendorProgram) error {
		program.UpdateQuoteEnabled(*body.QuoteEnabled)
		return nil
	})
}

func (a *API) updateDiscountOptions(w http.ResponseWriter, r *http.Request) error {
	body, err := decodeBody[struct {
		DiscountOptions *[]domain.DiscountOption `json:"discount_options"`
	}](w, r)
	if err != nil {
		return err
	}
	if body.DiscountOptions == nil {
		return badRequest("discount_options is required and must be an array")
	}
	return a.update(w, r, func(program *domain.VendorProgram) error {
		return program.UpdateDiscountOptions(*body.DiscountOptions)
	})
}

func (a *API) upsertProductOverrides(w http.ResponseWriter, r *http.Request) error {
	body, err := decodeBody[struct {
		ProductOverrides *[]domain.ProductOverride `json:"product_overrides"`
	}](w, r)
	if err != nil {
		return err
	}
	if body.ProductOverrides == nil {
		return badRequest("product_overrides is required and must be an array")
	}
	return a.update(w, r, func(program *domain.VendorProgram) error {
		return program.UpsertProductOverrides(*body.ProductOverrides)
	})
}

func (a *API) removeProductOverrides(w http.ResponseWriter, r *http.Request) error {
	body, err := decodeBody[struct {
		ProductIDs *[]string `json:"product_ids"`
	}](w, r)
	if err != nil {
		return err
	}
	if body.ProductIDs == nil {
		return badRequest("product_ids is required and must be an array")
	}
	for _, id := range *body.ProductIDs {
		if strings.TrimSpace(id) == "" {
			return badRequest("product_ids must not contain blank IDs")
		}
	}
	return a.update(w, r, func(program *domain.VendorProgram) error {
		program.RemoveProductOverrides(*body.ProductIDs)
		return nil
	})
}

func (a *API) upsertProductGroupOverrides(w http.ResponseWriter, r *http.Request) error {
	body, err := decodeBody[struct {
		ProductGroupOverrides *[]domain.ProductGroupOverride `json:"product_group_overrides"`
	}](w, r)
	if err != nil {
		return err
	}
	if body.ProductGroupOverrides == nil {
		return badRequest("product_group_overrides is required and must be an array")
	}
	return a.update(w, r, func(program *domain.VendorProgram) error {
		return program.UpsertProductGroupOverrides(*body.ProductGroupOverrides)
	})
}

func (a *API) removeProductGroupOverrides(w http.ResponseWriter, r *http.Request) error {
	body, err := decodeBody[struct {
		GroupNames *[]string `json:"group_names"`
	}](w, r)
	if err != nil {
		return err
	}
	if body.GroupNames == nil {
		return badRequest("group_names is required and must be an array")
	}
	for _, name := range *body.GroupNames {
		if strings.TrimSpace(name) == "" {
			return badRequest("group_names must not contain blank names")
		}
	}
	return a.update(w, r, func(program *domain.VendorProgram) error {
		program.RemoveProductGroupOverrides(*body.GroupNames)
		return nil
	})
}

func (a *API) getProductDiscountOptions(w http.ResponseWriter, r *http.Request) error {
	program, err := a.programs.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		return err
	}
	if program.IsExpired(time.Now()) {
		return domain.ErrVendorProgramExpired
	}
	writeJSON(w, http.StatusOK, program.GetDiscountOptionsForProduct(r.PathValue("productID"), r.URL.Query().Get("group_name")))
	return nil
}
