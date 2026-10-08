package api_test

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"usaf-pricing-service/domain"
	"usaf-pricing-service/internal/api"
)

func TestMongoQuotePricingIntegration(t *testing.T) {
	db := integrationStore(t)
	handler := api.NewHandler(db, 5*time.Second, slog.New(slog.NewTextHandler(io.Discard, nil)))
	request := func(method, path, body string, status int, result any) {
		t.Helper()
		res := httptest.NewRecorder()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		handler.ServeHTTP(res, req)
		if res.Code != status {
			t.Fatalf("%s %s: %d %s, want %d", method, path, res.Code, res.Body.String(), status)
		}
		if result != nil {
			if err := json.Unmarshal(res.Body.Bytes(), result); err != nil {
				t.Fatal(err)
			}
		}
	}
	var program domain.VendorProgram
	request("POST", "/vendor-programs", `{
  "vendor":"Quotes","quote_enabled":true,
  "discount_options":[{"name":"base","discount_path":[{"type":"percentage","amount":10}]}],
  "product_overrides":[{"product_id":"special","quote_enabled":true}],
  "product_group_overrides":[{"group_name":"group","quote_enabled":true}]
 }`, 201, &program)
	path := "/vendor-programs/" + program.Id
	checkStored := func(vendor, group, product bool) {
		t.Helper()
		var stored domain.VendorProgram
		request("GET", path, "", 200, &stored)
		if stored.QuoteEnabled != vendor || len(stored.ProductGroupOverrides) != 1 || stored.ProductGroupOverrides[0].QuoteEnabled != group || len(stored.ProductOverrides) != 1 || stored.ProductOverrides[0].QuoteEnabled != product {
			t.Fatalf("quote flags lost: %+v", stored)
		}
	}
	checkProduct := func(product, group string, wantQuote bool) {
		t.Helper()
		var options []domain.DiscountOption
		request("GET", path+"/products/"+product+"/discount-options?group_name="+group, "", 200, &options)
		quoteCount := 0
		for _, option := range options {
			if option.Name == "quote price" {
				quoteCount++
			}
		}
		if (wantQuote && quoteCount != 1) || (!wantQuote && quoteCount != 0) {
			t.Fatalf("quote options for %s/%s: %+v", product, group, options)
		}
		var prices map[string]struct {
			Price  *float64 `json:"dealerPrice"`
			Reason string   `json:"reason"`
		}
		request("POST", "/products/dealer-prices", `[{"productId":"`+product+`","groupName":"`+group+`","vendor":"Quotes","quotePrice":42.25}]`, 200, &prices)
		got := prices[product]
		if wantQuote {
			if got.Price == nil || *got.Price != 42.25 || got.Reason != "" {
				t.Fatalf("quote result: %+v", got)
			}
		} else if got.Price != nil || got.Reason != "quote pricing is not supported and no discount options were provided" {
			t.Fatalf("unsupported quote: %+v", got)
		}
	}
	checkStored(true, true, true)
	checkProduct("ordinary", "", true)
	checkProduct("special", "group", true)
	request("PUT", path+"/quote-enabled", `{"quote_enabled":false}`, 200, &program)
	checkStored(false, true, true)
	checkProduct("ordinary", "", false)
	checkProduct("ordinary", "group", true)
	checkProduct("special", "", true)
	request("PATCH", path+"/product-overrides", `{"product_overrides":[{"product_id":"special","quote_enabled":false}]}`, 200, nil)
	request("PATCH", path+"/product-group-overrides", `{"product_group_overrides":[{"group_name":"group","quote_enabled":false}]}`, 200, nil)
	checkStored(false, false, false)
	checkProduct("special", "group", false)
	request("PUT", path+"/quote-enabled", `{"quote_enabled":true}`, 200, nil)
	checkStored(true, false, false)
	checkProduct("special", "group", true)
	// Upserts can enable quote pricing on existing overrides, too.
	request("PATCH", path+"/product-overrides", `{"product_overrides":[{"product_id":"special","quote_enabled":true}]}`, 200, nil)
	request("PATCH", path+"/product-group-overrides", `{"product_group_overrides":[{"group_name":"group","quote_enabled":true}]}`, 200, nil)
	checkStored(true, true, true)
}
