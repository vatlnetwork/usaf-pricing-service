package api

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"usaf-pricing-service/domain"
	"usaf-pricing-service/internal/store"
)

// Unimplemented methods panic if a rejected request reaches persistence.
type failingStore struct {
	store.VendorPrograms
	err error
}

func (s failingStore) Get(context.Context, string) (*domain.VendorProgram, error) { return nil, s.err }

func TestInvalidRequestsDoNotReachPersistence(t *testing.T) {
	for _, tt := range []struct {
		name, method, path, body, contentType string
		status                                int
	}{
		{"malformed", "POST", "/vendor-programs", `{`, "application/json", 400},
		{"null", "POST", "/vendor-programs", `null`, "application/json", 400},
		{"unknown field", "POST", "/vendor-programs", `{"vendor":"ACME","id":"client-id"}`, "application/json", 400},
		{"trailing JSON", "POST", "/vendor-programs", `{"vendor":"ACME"} {}`, "application/json", 400},
		{"missing vendor", "POST", "/vendor-programs", `{}`, "application/json", 400},
		{"invalid vendor quote flag", "POST", "/vendor-programs", `{"vendor":"ACME","quote_enabled":"yes"}`, "application/json", 400},
		{"invalid product quote flag", "POST", "/vendor-programs", `{"vendor":"ACME","product_overrides":[{"product_id":"a","quote_enabled":1}]}`, "application/json", 400},
		{"invalid group quote flag", "POST", "/vendor-programs", `{"vendor":"ACME","product_group_overrides":[{"group_name":"group","quote_enabled":1}]}`, "application/json", 400},
		{"missing quote flag", "PUT", "/vendor-programs/id/quote-enabled", `{}`, "application/json", 400},
		{"null quote flag", "PUT", "/vendor-programs/id/quote-enabled", `{"quote_enabled":null}`, "application/json", 400},
		{"invalid quote flag", "PUT", "/vendor-programs/id/quote-enabled", `{"quote_enabled":"true"}`, "application/json", 400},
		{"invalid percentage", "POST", "/vendor-programs", `{"vendor":"ACME","discount_options":[{"name":"base","discount_path":[{"type":"percentage","amount":101}]}]}`, "application/json", 400},
		{"wrong content type", "POST", "/vendor-programs", `{}`, "text/plain", 415},
		{"oversized body", "POST", "/vendor-programs", `{"vendor":"` + strings.Repeat("a", maxBodyBytes) + `"}`, "application/json", 413},
		{"missing options", "PUT", "/vendor-programs/id/discount-options", `{}`, "application/json", 400},
		{"null options", "PUT", "/vendor-programs/id/discount-options", `{"discount_options":null}`, "application/json", 400},
		{"missing overrides", "PATCH", "/vendor-programs/id/product-overrides", `{}`, "application/json", 400},
		{"missing product IDs", "DELETE", "/vendor-programs/id/product-overrides", `{}`, "application/json", 400},
		{"blank product ID", "DELETE", "/vendor-programs/id/product-overrides", `{"product_ids":[" "]}`, "application/json", 400},
		{"blank group on create", "POST", "/vendor-programs", `{"vendor":"ACME","product_group_overrides":[{"group_name":" "}]}`, "application/json", 400},
		{"duplicate groups on create", "POST", "/vendor-programs", `{"vendor":"ACME","product_group_overrides":[{"group_name":"A"},{"group_name":"A"}]}`, "application/json", 400},
		{"duplicate group options on create", "POST", "/vendor-programs", `{"vendor":"ACME","product_group_overrides":[{"group_name":"A","discount_options":[{"name":"same"},{"name":"same"}]}]}`, "application/json", 400},
		{"missing group overrides", "PATCH", "/vendor-programs/id/product-group-overrides", `{}`, "application/json", 400},
		{"null group overrides", "PATCH", "/vendor-programs/id/product-group-overrides", `{"product_group_overrides":null}`, "application/json", 400},
		{"missing group names", "DELETE", "/vendor-programs/id/product-group-overrides", `{}`, "application/json", 400},
		{"null group names", "DELETE", "/vendor-programs/id/product-group-overrides", `{"group_names":null}`, "application/json", 400},
		{"blank group name", "DELETE", "/vendor-programs/id/product-group-overrides", `{"group_names":[" "]}`, "application/json", 400},
		{"invalid expiry on create", "POST", "/vendor-programs", `{"vendor":"ACME","expires_at":"not-a-date"}`, "application/json", 400},
		{"expiry without timezone", "POST", "/vendor-programs", `{"vendor":"ACME","expires_at":"2030-01-01T00:00:00"}`, "application/json", 400},
		{"missing expiry", "PUT", "/vendor-programs/id/expiry", `{}`, "application/json", 400},
		{"invalid expiry", "PUT", "/vendor-programs/id/expiry", `{"expires_at":"invalid"}`, "application/json", 400},
		{"date-only expiry", "PUT", "/vendor-programs/id/expiry", `{"expires_at":"2030-01-01"}`, "application/json", 400},
		{"numeric expiry", "PUT", "/vendor-programs/id/expiry", `{"expires_at":123}`, "application/json", 400},
		{"invalid limit", "GET", "/vendor-programs?limit=0", "", "", 400},
		{"large limit", "GET", "/vendor-programs?limit=1001", "", "", 400},
		{"negative offset", "GET", "/vendor-programs?offset=-1", "", "", 400},
	} {
		t.Run(tt.name, func(t *testing.T) {
			handler := NewHandler(failingStore{}, time.Second, slog.New(slog.NewTextHandler(io.Discard, nil)))
			req := httptest.NewRequest(tt.method, tt.path, strings.NewReader(tt.body))
			req.Header.Set("Content-Type", tt.contentType)
			res := httptest.NewRecorder()
			handler.ServeHTTP(res, req)
			if res.Code != tt.status {
				t.Fatalf("got %d: %s, want %d", res.Code, res.Body.String(), tt.status)
			}
		})
	}
}

func TestRepositoryErrors(t *testing.T) {
	for _, tt := range []struct {
		err    error
		status int
	}{
		{store.ErrInvalidID, 400}, {store.ErrNotFound, 404}, {store.ErrConflict, 409},
		{context.DeadlineExceeded, 504}, {errors.New("private database details"), 500},
	} {
		t.Run(tt.err.Error(), func(t *testing.T) {
			handler := NewHandler(failingStore{err: tt.err}, time.Second, slog.New(slog.NewTextHandler(io.Discard, nil)))
			res := httptest.NewRecorder()
			handler.ServeHTTP(res, httptest.NewRequest("GET", "/vendor-programs/id", nil))
			if res.Code != tt.status {
				t.Fatalf("got %d, want %d", res.Code, tt.status)
			}
			if strings.Contains(res.Body.String(), "private database details") {
				t.Fatal("internal error leaked")
			}
		})
	}
}
