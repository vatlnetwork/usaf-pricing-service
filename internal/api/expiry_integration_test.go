package api_test

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"usaf-pricing-service/domain"
	"usaf-pricing-service/internal/api"
)

func TestMongoVendorProgramExpiryIntegration(t *testing.T) {
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
	past := time.Now().Add(-time.Hour).UTC().Truncate(time.Millisecond)
	future := time.Now().Add(time.Hour).UTC().Truncate(time.Millisecond)
	expiryBody := func(expiry *time.Time) string {
		data, err := json.Marshal(map[string]*time.Time{"expires_at": expiry})
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	create := func(vendor string, expiry *time.Time) domain.VendorProgram {
		t.Helper()
		body := map[string]any{
			"vendor":                  vendor,
			"discount_options":        []domain.DiscountOption{{Name: "base", DiscountPath: []domain.DiscountPathItem{{Type: domain.DiscountPathItemTypePercentage, Amount: 10}}}},
			"product_group_overrides": []domain.ProductGroupOverride{{GroupName: "group", DiscountOptions: []domain.DiscountOption{{Name: "group-option"}}}},
			"product_overrides":       []domain.ProductOverride{{ProductId: "a", DiscountOptions: []domain.DiscountOption{{Name: "product-option"}}}},
		}
		if expiry != nil {
			body["expires_at"] = expiry
		}
		data, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		var program domain.VendorProgram
		request("POST", "/vendor-programs", string(data), 201, &program)
		return program
	}
	price := func(vendor, expectedReason string, want float64) {
		t.Helper()
		products := []domain.Product{
			{ProductId: "a", GroupName: "group", Vendor: vendor, ListPrice: 100, DiscountOptions: []string{"base", "group-option", "product-option"}},
			{ProductId: "no-discounts", Vendor: vendor, ListPrice: 100},
		}
		body, err := json.Marshal(products)
		if err != nil {
			t.Fatal(err)
		}
		var results map[string]struct {
			Price  *float64 `json:"dealerPrice"`
			Text   string   `json:"dealerPriceString"`
			Reason string   `json:"reason"`
		}
		request("POST", "/products/dealer-prices", string(body), 200, &results)
		for _, product := range products {
			got := results[product.ProductId]
			if expectedReason != "" {
				if got.Price != nil || got.Text != "Unavailable" || got.Reason != expectedReason {
					t.Fatalf("unexpected unavailable result: %+v", got)
				}
			} else {
				expected := want
				if product.ProductId == "no-discounts" {
					expected = 100
				}
				if got.Price == nil || *got.Price != expected || got.Reason != "" {
					t.Fatalf("unexpected active result: %+v", got)
				}
			}
		}
	}
	program := create("Expired Vendor", &past)
	path := "/vendor-programs/" + program.Id
	lookup := path + "/products/a/discount-options?group_name=group"
	var stored domain.VendorProgram
	request("GET", path, "", 200, &stored)
	if stored.ExpiresAt == nil || !stored.ExpiresAt.Equal(past) {
		t.Fatalf("expiry did not persist: %v", stored.ExpiresAt)
	}
	var listing []domain.VendorProgram
	request("GET", "/vendor-programs", "", 200, &listing)
	if len(listing) != 1 || listing[0].Id != program.Id {
		t.Fatal("expired program unavailable for management")
	}
	var errorBody map[string]string
	request("GET", lookup, "", 404, &errorBody)
	if errorBody["error"] != domain.ErrVendorProgramExpired.Error() {
		t.Fatalf("unexpected expiry error: %v", errorBody)
	}
	price(program.Vendor, "vendor program for Expired Vendor is missing", 0)

	request("PUT", path+"/expiry", expiryBody(&future), 200, &stored)
	request("GET", path, "", 200, &stored)
	if stored.ExpiresAt == nil || !stored.ExpiresAt.Equal(future) {
		t.Fatal("renewed expiry did not persist")
	}
	var options []domain.DiscountOption
	request("GET", lookup, "", 200, &options)
	if len(options) != 3 {
		t.Fatalf("renewal lost options: %+v", options)
	}
	price(program.Vendor, "", 90)
	before := stored
	request("PUT", path+"/expiry", `{"expires_at":"invalid"}`, 400, nil)
	request("GET", path, "", 200, &stored)
	if !reflect.DeepEqual(stored, before) {
		t.Fatal("invalid expiry mutated program")
	}

	request("PUT", path+"/expiry", expiryBody(&past), 200, nil)
	request("GET", lookup, "", 404, nil)
	request("PUT", path+"/expiry", `{"expires_at":null}`, 200, nil)
	// Decode into a fresh struct to also exercise omitted BSON expiry fields.
	stored = domain.VendorProgram{}
	request("GET", path, "", 200, &stored)
	if stored.ExpiresAt != nil {
		t.Fatal("expiry was not cleared")
	}
	request("GET", lookup, "", 200, nil)
	price(program.Vendor, "", 90)

	// More than two expired programs must be filtered before the ambiguity limit.
	for i := 0; i < 3; i++ {
		create("Shared Vendor", &past)
	}
	create("Shared Vendor", &future)
	price("Shared Vendor", "", 90)
	create("Shared Vendor", nil)
	price("Shared Vendor", "multiple vendor programs found for vendor Shared Vendor", 0)
	legacy := create("Legacy Vendor", nil)
	price(legacy.Vendor, "", 90)
	request("GET", "/vendor-programs/"+legacy.Id+"/products/a/discount-options", "", 200, nil)
}
