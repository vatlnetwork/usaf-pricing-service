package api

import (
	"context"
	"encoding/json"
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

type pricingStore struct {
	store.VendorPrograms
	programs map[string]*domain.VendorProgram
	errors   map[string]error
	calls    map[string]int
}

func (s *pricingStore) GetByVendor(ctx context.Context, vendor string) (*domain.VendorProgram, error) {
	s.calls[vendor]++
	if err := s.errors[vendor]; err != nil {
		return nil, err
	}
	program, exists := s.programs[vendor]
	if !exists {
		return nil, store.ErrNotFound
	}
	return program, nil
}

func TestDealerPricesMixedResults(t *testing.T) {
	db := &pricingStore{
		programs: map[string]*domain.VendorProgram{"ACME": {
			Vendor:          "ACME",
			DiscountOptions: []domain.DiscountOption{{Name: "half", DiscountPath: []domain.DiscountPathItem{{Type: domain.DiscountPathItemTypePercentage, Amount: 50}}}},
		}},
		errors: map[string]error{"ambiguous": store.ErrAmbiguousVendor, "broken": errors.New("private database details"), "timeout": context.DeadlineExceeded},
		calls:  make(map[string]int),
	}
	handler := NewHandler(db, time.Second, slog.New(slog.NewTextHandler(io.Discard, nil)))
	body := `[
		{"productId":"good","vendor":"ACME","listPrice":24.68,"discountOptions":["half"]},
		{"productId":"missing-option","vendor":"ACME","listPrice":100,"discountOptions":["xxx"]},
		{"productId":"missing-vendor","vendor":"unknown","listPrice":100},
		{"productId":"also-missing-vendor","vendor":"unknown","listPrice":100},
		{"productId":"zero","vendor":"ACME","listPrice":0},
		{"productId":"negative","vendor":"ACME","listPrice":-1},
		{"productId":"no-price","vendor":"ACME"},
		{"productId":"null-price","vendor":"ACME","listPrice":null},
		{"productId":"blank-vendor","vendor":" ","listPrice":10},
		{"productId":"ambiguous","vendor":"ambiguous","listPrice":10},
		{"productId":"broken","vendor":"broken","listPrice":10},
		{"productId":"timeout","vendor":"timeout","listPrice":10}
	]`
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, httptest.NewRequest("POST", "/products/dealer-prices", strings.NewReader(body)))
	if res.Code != 200 {
		t.Fatalf("got %d: %s", res.Code, res.Body.String())
	}
	var results map[string]dealerPriceResult
	if err := json.Unmarshal(res.Body.Bytes(), &results); err != nil {
		t.Fatal(err)
	}
	if len(results) != 12 {
		t.Fatalf("missing product results: %+v", results)
	}
	for _, tt := range []struct {
		id    string
		price float64
		text  string
	}{
		{"good", 12.34, "$12.34"}, {"zero", 0, "$0.00"},
	} {
		got := results[tt.id]
		if got.DealerPrice == nil || *got.DealerPrice != tt.price || got.DealerPriceString != tt.text || got.Reason != "" {
			t.Fatalf("unexpected success for %s: %+v", tt.id, got)
		}
	}
	for id, reason := range map[string]string{
		"missing-option":      "discount option xxx is missing",
		"missing-vendor":      "vendor program for unknown is missing",
		"also-missing-vendor": "vendor program for unknown is missing",
		"negative":            "list price must be finite and nonnegative",
		"no-price":            "listPrice is required",
		"null-price":          "listPrice is required",
		"blank-vendor":        "vendor is required",
		"ambiguous":           "multiple vendor programs found for vendor ambiguous",
		"broken":              "vendor program lookup failed",
		"timeout":             "vendor program lookup timed out",
	} {
		got := results[id]
		if got.DealerPrice != nil || got.DealerPriceString != "Unavailable" || got.Reason != reason {
			t.Fatalf("unexpected failure for %s: %+v", id, got)
		}
	}
	if strings.Contains(res.Body.String(), "private database details") {
		t.Fatal("database error leaked")
	}
	for vendor, calls := range db.calls {
		if calls != 1 {
			t.Fatalf("vendor %s queried %d times in one batch", vendor, calls)
		}
	}
	// Check the wire schema, including explicit nulls rather than omitted fields.
	var raw map[string]map[string]json.RawMessage
	if err := json.Unmarshal(res.Body.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	if len(raw["good"]) != 3 || string(raw["missing-option"]["dealerPrice"]) != "null" {
		t.Fatalf("unexpected response schema: %s", res.Body.String())
	}
}

func TestDealerPricesInvalidBatches(t *testing.T) {
	for _, body := range []string{
		`null`, `{}`, `[null]`, `[{}]`,
		`[{"productId":"same"},{"productId":"same"}]`,
		`[{"productId":"a","unknown":1}]`,
		`[{"productId":"a","listPrice":"bad"}]`,
		`[{"productId":"a","groupName":123,"listPrice":100}]`,
		`[] []`,
	} {
		t.Run(body, func(t *testing.T) {
			handler := NewHandler(failingStore{}, time.Second, slog.New(slog.NewTextHandler(io.Discard, nil)))
			res := httptest.NewRecorder()
			handler.ServeHTTP(res, httptest.NewRequest("POST", "/products/dealer-prices", strings.NewReader(body)))
			if res.Code != 400 {
				t.Fatalf("got %d: %s", res.Code, res.Body.String())
			}
		})
	}
	handler := NewHandler(failingStore{}, time.Second, slog.New(slog.NewTextHandler(io.Discard, nil)))
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, httptest.NewRequest("POST", "/products/dealer-prices", strings.NewReader(`[]`)))
	if res.Code != 200 || strings.TrimSpace(res.Body.String()) != "{}" {
		t.Fatalf("empty batch: got %d: %s", res.Code, res.Body.String())
	}
}
