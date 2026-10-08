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

func TestMongoNetPriceOverrideIntegration(t *testing.T) {
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
  "vendor":"Net Vendor",
  "discount_options":[{"name":"base","discount_path":[{"type":"percentage","amount":10}]}],
  "product_group_overrides":[{"group_name":"group","discount_options":[{"name":"group-option"}]}],
  "product_overrides":[{"product_id":"fixed","net_price":42.25,"discount_options":[{"name":"ignored"},{"name":"ignored"}]}]
 }`, 201, &program)
	path := "/vendor-programs/" + program.Id
	checkFixedPrice := func(want float64) {
		t.Helper()
		var stored domain.VendorProgram
		request("GET", path, "", 200, &stored)
		if len(stored.ProductOverrides) != 1 || stored.ProductOverrides[0].NetPrice != want {
			t.Fatalf("net price lost in MongoDB round trip: %+v", stored.ProductOverrides)
		}
		var options []struct {
			Name         string                    `json:"name"`
			NetPrice     float64                   `json:"net_price"`
			DiscountPath []domain.DiscountPathItem `json:"discount_path"`
		}
		request("GET", path+"/products/fixed/discount-options?group_name=group", "", 200, &options)
		if len(options) != 1 || options[0].Name != "net price" || options[0].NetPrice != want || options[0].DiscountPath == nil || len(options[0].DiscountPath) != 0 {
			t.Fatalf("unexpected net price option: %+v", options)
		}
		for _, selected := range []string{`[]`, `["net price"]`, `["base","ignored","missing","missing"]`} {
			var prices map[string]struct {
				Price  *float64 `json:"dealerPrice"`
				Reason string   `json:"reason"`
			}
			request("POST", "/products/dealer-prices", `[{"productId":"fixed","groupName":"group","vendor":"Net Vendor","listPrice":100,"discountOptions":`+selected+`}]`, 200, &prices)
			got := prices["fixed"]
			if got.Price == nil || *got.Price != want || got.Reason != "" {
				t.Fatalf("selection %s: unexpected price %+v", selected, got)
			}
		}
	}
	checkFixedPrice(42.25)
	request("PATCH", path+"/product-overrides", `{"product_overrides":[{"product_id":"fixed","net_price":30.50}]}`, 200, nil)
	checkFixedPrice(30.50)
	request("PATCH", path+"/product-overrides", `{"product_overrides":[{"product_id":"fixed","net_price":20},{"product_id":"invalid","net_price":-1}]}`, 400, nil)
	checkFixedPrice(30.50)
	request("PATCH", path+"/product-overrides", `{"product_overrides":[{"product_id":"fixed","net_price":0}]}`, 200, nil)
	var options []domain.DiscountOption
	request("GET", path+"/products/fixed/discount-options", "", 200, &options)
	if len(options) != 1 || options[0].Name != "base" || options[0].NetPrice != 0 {
		t.Fatalf("clearing net price did not restore vendor option: %+v", options)
	}
}
