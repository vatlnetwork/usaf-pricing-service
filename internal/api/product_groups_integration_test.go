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

func TestMongoProductGroupOverridesIntegration(t *testing.T) {
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
		"vendor":"Group Vendor",
		"discount_options":[{"name":"Standard","discount_path":[{"type":"percentage","amount":10}]}],
		"product_group_overrides":[
			{"group_name":"Group A","discount_options":[{"name":"Standard","discount_path":[{"type":"percentage","amount":20}]}]},
			{"group_name":"Group B","discount_options":[{"name":"Standard","discount_path":[{"type":"percentage","amount":30}]}]}
		],
		"product_overrides":[{"product_id":"special","discount_options":[{"name":"Standard","discount_path":[{"type":"percentage","amount":40}]}]}]
	}`, 201, &program)
	path := "/vendor-programs/" + program.Id
	var stored domain.VendorProgram
	request("GET", path, "", 200, &stored)
	if !reflect.DeepEqual(stored.ProductGroupOverrides, program.ProductGroupOverrides) || len(stored.ProductGroupOverrides) != 2 {
		t.Fatalf("group round trip failed: %+v", stored)
	}

	checkOption := func(product, query string, amount float64) {
		t.Helper()
		var options []domain.DiscountOption
		request("GET", path+"/products/"+product+"/discount-options"+query, "", 200, &options)
		if len(options) != 1 || options[0].Name != "Standard" || len(options[0].DiscountPath) != 1 || options[0].DiscountPath[0].Amount != amount {
			t.Fatalf("wrong effective option for %s%s: %+v", product, query, options)
		}
	}
	checkOption("special", "?group_name=Group%20A", 40)
	checkOption("ordinary", "?group_name=Group%20A", 20)
	checkOption("ordinary", "?group_name=Group%20B", 30)
	checkOption("ordinary", "?group_name=unknown", 10)
	checkOption("ordinary", "", 10)
	var prices map[string]struct {
		Price  *float64 `json:"dealerPrice"`
		Reason string   `json:"reason"`
	}
	request("POST", "/products/dealer-prices", `[
		{"productId":"special","groupName":"Group A","vendor":"Group Vendor","listPrice":100,"discountOptions":["Standard"]},
		{"productId":"a","groupName":"Group A","vendor":"Group Vendor","listPrice":100,"discountOptions":["Standard"]},
		{"productId":"b","groupName":"Group B","vendor":"Group Vendor","listPrice":100,"discountOptions":["Standard"]},
		{"productId":"other","vendor":"Group Vendor","listPrice":100,"discountOptions":["Standard"]}
	]`, 200, &prices)
	for id, want := range map[string]float64{"special": 60, "a": 80, "b": 70, "other": 90} {
		got := prices[id]
		if got.Price == nil || *got.Price != want || got.Reason != "" {
			t.Fatalf("wrong price for %s: %+v", id, got)
		}
	}
	request("PATCH", path+"/product-group-overrides", `{"product_group_overrides":[
		{"group_name":"Group A","discount_options":[{"name":"Standard","discount_path":[{"type":"percentage","amount":25}]}]},
		{"group_name":"Group C","discount_options":[{"name":"Standard","discount_path":[{"type":"percentage","amount":35}]}]}
	]}`, 200, &program)
	if len(program.ProductGroupOverrides) != 3 {
		t.Fatalf("unexpected upsert: %+v", program)
	}
	checkOption("ordinary", "?group_name=Group%20A", 25)
	checkOption("ordinary", "?group_name=Group%20B", 30)
	checkOption("ordinary", "?group_name=Group%20C", 35)
	// Compare persisted snapshots because BSON timestamps have millisecond precision.
	request("GET", path, "", 200, &program)
	for _, body := range []string{
		`{"product_group_overrides":[{"group_name":"Group A","discount_options":[]},{"group_name":" "}]}`,
		`{"product_group_overrides":[{"group_name":"Group A","discount_options":[{"name":"Standard"},{"name":"Standard"}]}]}`,
	} {
		request("PATCH", path+"/product-group-overrides", body, 400, nil)
		request("GET", path, "", 200, &stored)
		if !reflect.DeepEqual(stored, program) {
			t.Fatal("failed group update was persisted")
		}
	}
	request("PATCH", path+"/product-group-overrides", `{"product_group_overrides":[]}`, 200, nil)
	request("DELETE", path+"/product-group-overrides", `{"group_names":[]}`, 200, nil)
	checkOption("ordinary", "?group_name=Group%20A", 25)
	request("PATCH", path+"/product-group-overrides", `{"product_group_overrides":[{"group_name":"Group A","discount_options":[]}]}`, 200, nil)
	checkOption("ordinary", "?group_name=Group%20A", 10)
	request("DELETE", path+"/product-group-overrides", `{"group_names":["Group A","Group B","unknown"]}`, 200, &program)
	if len(program.ProductGroupOverrides) != 1 || program.ProductGroupOverrides[0].GroupName != "Group C" {
		t.Fatalf("wrong groups deleted: %+v", program)
	}
	checkOption("ordinary", "?group_name=Group%20B", 10)
	checkOption("ordinary", "?group_name=Group%20C", 35)
	checkOption("special", "?group_name=Group%20C", 40)
}
