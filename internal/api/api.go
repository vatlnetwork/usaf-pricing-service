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
	mux.HandleFunc("POST /products/dealer-prices", a.handle(a.dealerPrices))
	mux.HandleFunc("POST /vendor-programs", a.handle(a.create))
	mux.HandleFunc("GET /vendor-programs", a.handle(a.list))
	mux.HandleFunc("GET /vendor-programs/{id}", a.handle(a.get))
	mux.HandleFunc("DELETE /vendor-programs/{id}", a.handle(a.delete))
	mux.HandleFunc("PUT /vendor-programs/{id}/discount-options", a.handle(a.updateDiscountOptions))
	mux.HandleFunc("PATCH /vendor-programs/{id}/product-overrides", a.handle(a.upsertProductOverrides))
	mux.HandleFunc("DELETE /vendor-programs/{id}/product-overrides", a.handle(a.removeProductOverrides))
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
	case errors.Is(err, store.ErrConflict):
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

func (a *API) create(w http.ResponseWriter, r *http.Request) error {
	body, err := decodeBody[struct {
		Vendor           string                   `json:"vendor"`
		DiscountOptions  []domain.DiscountOption  `json:"discount_options"`
		ProductOverrides []domain.ProductOverride `json:"product_overrides"`
	}](w, r)
	if err != nil {
		return err
	}
	program, err := domain.NewVendorProgram(body.Vendor, body.DiscountOptions, body.ProductOverrides)
	if err != nil {
		return badRequest(err.Error())
	}
	if err := a.programs.Create(r.Context(), program); err != nil {
		return err
	}
	w.Header().Set("Location", "/vendor-programs/"+program.Id)
	writeJSON(w, http.StatusCreated, program)
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

func (a *API) getProductDiscountOptions(w http.ResponseWriter, r *http.Request) error {
	program, err := a.programs.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, program.GetDiscountOptionsForProduct(r.PathValue("productID")))
	return nil
}
