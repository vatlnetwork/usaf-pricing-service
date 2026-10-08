package api

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"usaf-pricing-service/domain"
)

func TestDealerPricesQuoteRequests(t *testing.T) {
	db := &pricingStore{programs: map[string]*domain.VendorProgram{
		"quotes":    {Vendor: "quotes", QuoteEnabled: true, ProductOverrides: []domain.ProductOverride{{ProductId: "net", NetPrice: 25}}},
		"discounts": {Vendor: "discounts", DiscountOptions: []domain.DiscountOption{{Name: "half", DiscountPath: []domain.DiscountPathItem{{Type: domain.DiscountPathItemTypePercentage, Amount: 50}}}}},
	}, calls: make(map[string]int)}
	handler := NewHandler(db, time.Second, slog.New(slog.NewTextHandler(io.Discard, nil)))
	body := `[
  {"productId":"quote","vendor":"quotes","quotePrice":42.25,"discountOptions":["missing"]},
  {"productId":"net","vendor":"quotes","quotePrice":42.25,"listPrice":100},
  {"productId":"fallback","vendor":"discounts","quotePrice":42.25,"listPrice":100,"discountOptions":["half"]},
  {"productId":"unsupported","vendor":"discounts","quotePrice":42.25,"listPrice":100},
  {"productId":"unsupported-no-list","vendor":"discounts","quotePrice":42.25},
  {"productId":"fallback-no-list","vendor":"discounts","quotePrice":42.25,"discountOptions":["half"]},
  {"productId":"quote-no-price","vendor":"quotes","listPrice":100,"discountOptions":["quote price"]}
 ]`
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, httptest.NewRequest("POST", "/products/dealer-prices", strings.NewReader(body)))
	if res.Code != 200 {
		t.Fatalf("status %d: %s", res.Code, res.Body.String())
	}
	var results map[string]dealerPriceResult
	if err := json.Unmarshal(res.Body.Bytes(), &results); err != nil {
		t.Fatal(err)
	}
	for id, want := range map[string]float64{"quote": 42.25, "net": 42.25, "fallback": 50} {
		got := results[id]
		if got.DealerPrice == nil || *got.DealerPrice != want || got.Reason != "" {
			t.Fatalf("%s = %+v, want %v", id, got, want)
		}
	}
	if results["quote"].DealerPriceString != "$42.25" {
		t.Fatalf("formatted quote: %+v", results["quote"])
	}
	for id, reason := range map[string]string{
		"unsupported":         "quote pricing is not supported and no discount options were provided",
		"unsupported-no-list": "quote pricing is not supported and no discount options were provided",
		"fallback-no-list":    "listPrice is required",
		"quote-no-price":      "a positive quote price is required for the quote price option",
	} {
		got := results[id]
		if got.DealerPrice != nil || got.DealerPriceString != "Unavailable" || got.Reason != reason {
			t.Fatalf("%s = %+v, want %q", id, got, reason)
		}
	}
}
