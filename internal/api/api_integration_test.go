package api_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"usaf-pricing-service/domain"
	"usaf-pricing-service/internal/api"
	"usaf-pricing-service/internal/config"
	"usaf-pricing-service/internal/store"
)

func integrationStore(t *testing.T) *store.MongoDB {
	t.Helper()
	uri := os.Getenv("MONGODB_TEST_URI")
	if uri == "" {
		t.Skip("set MONGODB_TEST_URI to run MongoDB integration tests")
	}
	cfg := config.MongoDB{URI: uri, Database: "usaf_pricing_test_" + bson.NewObjectID().Hex(), Collection: "vendor_programs", ConnectTimeoutSeconds: 5, OperationTimeoutSeconds: 5}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	db, err := store.OpenMongoDB(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		client, err := mongo.Connect(options.Client().ApplyURI(uri))
		if err != nil {
			t.Error(err)
		} else {
			if err := client.Database(cfg.Database).Drop(ctx); err != nil {
				t.Error(err)
			}
			if err := client.Disconnect(ctx); err != nil {
				t.Error(err)
			}
		}
		if err := db.Close(ctx); err != nil {
			t.Error(err)
		}
	})
	return db
}

func TestMongoAPIIntegration(t *testing.T) {
	db := integrationStore(t)
	server := httptest.NewServer(api.NewHandler(db, 5*time.Second, slog.New(slog.NewTextHandler(io.Discard, nil))))
	defer server.Close()
	request := func(method, path, body string, status int) []byte {
		t.Helper()
		req, err := http.NewRequest(method, server.URL+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		res, err := server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		data, err := io.ReadAll(res.Body)
		if err != nil {
			t.Fatal(err)
		}
		if res.StatusCode != status {
			t.Fatalf("%s %s: got %d %s, want %d", method, path, res.StatusCode, data, status)
		}
		if status == 201 && res.Header.Get("Location") == "" {
			t.Fatal("missing Location header")
		}
		return data
	}
	decodeProgram := func(data []byte) domain.VendorProgram {
		t.Helper()
		var program domain.VendorProgram
		if err := json.Unmarshal(data, &program); err != nil {
			t.Fatal(err)
		}
		return program
	}
	created := decodeProgram(request("POST", "/vendor-programs", `{
		"vendor":"ACME",
		"discount_options":[{"name":"base","discount_path":[{"type":"percentage","amount":10}]}],
		"product_overrides":[{"product_id":"a","discount_options":[{"name":"extra-a","discount_path":[{"type":"dollar_amount","amount":5}]},{"name":"base","discount_path":[{"type":"percentage","amount":20}]}]}]
	}`, 201))
	if len(created.Id) != 24 || created.CreatedAt.IsZero() || created.UpdatedAt.IsZero() {
		t.Fatalf("invalid generated fields: %+v", created)
	}
	path := "/vendor-programs/" + created.Id
	got := decodeProgram(request("GET", path, "", 200))
	if got.Id != created.Id || got.Vendor != "ACME" || got.DiscountOptions[0].DiscountPath[0].Amount != 10 || got.ProductOverrides[0].ProductId != "a" {
		t.Fatalf("MongoDB round trip lost fields: %+v", got)
	}
	var list []domain.VendorProgram
	if err := json.Unmarshal(request("GET", "/vendor-programs?limit=1&offset=0", "", 200), &list); err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Id != created.Id {
		t.Fatalf("unexpected list: %+v", list)
	}
	if data := request("GET", "/vendor-programs?offset=1", "", 200); strings.TrimSpace(string(data)) != "[]" {
		t.Fatalf("expected empty page, got %s", data)
	}

	updated := decodeProgram(request("PUT", path+"/discount-options", `{"discount_options":[{"name":"base-new","discount_path":[{"type":"percentage","amount":15}]}]}`, 200))
	if updated.DiscountOptions[0].Name != "base-new" || len(updated.ProductOverrides) != 1 {
		t.Fatalf("unexpected options update: %+v", updated)
	}
	updated = decodeProgram(request("PATCH", path+"/product-overrides", `{"product_overrides":[
		{"product_id":"a","discount_options":[{"name":"extra-a-new","discount_path":[]},{"name":"base-new","discount_path":[{"type":"percentage","amount":25}]}]},
		{"product_id":"b","discount_options":[{"name":"extra-b","discount_path":[]},{"name":"base-new","discount_path":[{"type":"percentage","amount":35}]}]}
	]}`, 200))
	if len(updated.ProductOverrides) != 2 || updated.ProductOverrides[0].DiscountOptions[0].Name != "extra-a-new" {
		t.Fatalf("unexpected upsert: %+v", updated)
	}
	for _, product := range []struct {
		id     string
		names  []string
		amount float64
	}{
		{"a", []string{"extra-a-new", "base-new"}, 25}, {"b", []string{"extra-b", "base-new"}, 35}, {"other", []string{"base-new"}, 15},
	} {
		var discounts []domain.DiscountOption
		if err := json.Unmarshal(request("GET", path+"/products/"+product.id+"/discount-options", "", 200), &discounts); err != nil {
			t.Fatal(err)
		}
		if len(discounts) != len(product.names) {
			t.Fatalf("wrong discounts for %s: %+v", product.id, discounts)
		}
		for i, name := range product.names {
			if discounts[i].Name != name {
				t.Fatalf("wrong option: got %s, want %s", discounts[i].Name, name)
			}
			if name == "base-new" && (len(discounts[i].DiscountPath) != 1 || discounts[i].DiscountPath[0].Amount != product.amount) {
				t.Fatalf("wrong effective option for %s: %+v", product.id, discounts[i])
			}
		}
	}
	request("PATCH", path+"/product-overrides", `{"product_overrides":[{"product_id":"a","discount_options":[{"name":"base-new"},{"name":"base-new"}]}]}`, 400)
	got = decodeProgram(request("GET", path, "", 200))
	if got.ProductOverrides[0].DiscountOptions[0].Name != "extra-a-new" {
		t.Fatal("failed update was persisted")
	}
	updated = decodeProgram(request("DELETE", path+"/product-overrides", `{"product_ids":["a"]}`, 200))
	if len(updated.ProductOverrides) != 1 || updated.ProductOverrides[0].ProductId != "b" {
		t.Fatalf("wrong overrides removed: %+v", updated)
	}
	request("PUT", path+"/discount-options", `{"discount_options":[]}`, 200)
	got = decodeProgram(request("GET", path, "", 200))
	if len(got.DiscountOptions) != 0 || len(got.ProductOverrides) != 1 {
		t.Fatal("empty array did not clear vendor options independently")
	}
	request("GET", "/vendor-programs/not-an-id", "", 400)
	request("GET", "/vendor-programs/"+bson.NewObjectID().Hex(), "", 404)
	request("DELETE", path, "", 204)
	request("GET", path, "", 404)
	request("DELETE", path, "", 404)
}

func TestMongoUpdateConflictAndRollbackIntegration(t *testing.T) {
	db := integrationStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	program, err := domain.NewVendorProgram("ACME", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Create(ctx, program); err != nil {
		t.Fatal(err)
	}
	// The outer update reads first; the inner update commits before it can write.
	_, err = db.Update(ctx, program.Id, func(p *domain.VendorProgram) error {
		_, err := db.Update(ctx, program.Id, func(current *domain.VendorProgram) error {
			return current.UpdateDiscountOptions([]domain.DiscountOption{{Name: "winner"}})
		})
		if err != nil {
			return err
		}
		return p.UpdateDiscountOptions([]domain.DiscountOption{{Name: "stale"}})
	})
	if !errors.Is(err, store.ErrConflict) {
		t.Fatalf("got %v, want conflict", err)
	}
	rejected := errors.New("reject mutation")
	_, err = db.Update(ctx, program.Id, func(p *domain.VendorProgram) error {
		p.DiscountOptions = nil
		return rejected
	})
	if !errors.Is(err, rejected) {
		t.Fatalf("got %v, want rejected mutation", err)
	}
	got, err := db.Get(ctx, program.Id)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.DiscountOptions) != 1 || got.DiscountOptions[0].Name != "winner" {
		t.Fatalf("committed update was lost: %+v", got)
	}
}

func TestMongoPricingIntegration(t *testing.T) {
	db := integrationStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	program, err := domain.NewVendorProgram("ACME", []domain.DiscountOption{
		{Name: "standard", DiscountPath: []domain.DiscountPathItem{
			{Type: domain.DiscountPathItemTypePercentage, Amount: 10},
			{Type: domain.DiscountPathItemTypeDollarAmount, Amount: 5},
		}},
	}, []domain.ProductOverride{
		{ProductId: "overridden", DiscountOptions: []domain.DiscountOption{{Name: "standard", DiscountPath: []domain.DiscountPathItem{{Type: domain.DiscountPathItemTypePercentage, Amount: 20}}}}},
		{ProductId: "disabled", DiscountOptions: []domain.DiscountOption{{Name: "standard"}}},
		{ProductId: "a", DiscountOptions: []domain.DiscountOption{{Name: "extra", DiscountPath: []domain.DiscountPathItem{{Type: domain.DiscountPathItemTypePercentage, Amount: 50}}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Create(ctx, program); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(api.NewHandler(db, 5*time.Second, slog.New(slog.NewTextHandler(io.Discard, nil))))
	defer server.Close()
	type result struct {
		Price  *float64 `json:"dealerPrice"`
		Text   string   `json:"dealerPriceString"`
		Reason string   `json:"reason"`
	}
	price := func(body string) map[string]result {
		t.Helper()
		res, err := server.Client().Post(server.URL+"/products/dealer-prices", "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		if res.StatusCode != http.StatusOK {
			t.Fatalf("pricing status = %d", res.StatusCode)
		}
		var results map[string]result
		if err := json.NewDecoder(res.Body).Decode(&results); err != nil {
			t.Fatal(err)
		}
		return results
	}
	results := price(`[
		{"productId":"overridden","vendor":"ACME","listPrice":100,"discountOptions":["standard"]},
		{"productId":"disabled","vendor":"ACME","listPrice":100,"discountOptions":["standard"]},
		{"productId":"a","vendor":"ACME","listPrice":100,"discountOptions":["standard","extra"]},
		{"productId":"b","vendor":"ACME","listPrice":100,"discountOptions":["extra"]},
		{"productId":"c","vendor":"ACME","listPrice":100,"discountOptions":["standard"]},
		{"productId":"zero","vendor":"ACME","listPrice":4,"discountOptions":["standard"]},
		{"productId":"unknown","vendor":"unknown","listPrice":100},
		{"productId":"case","vendor":"acme","listPrice":100}
	]`)
	for id, want := range map[string]float64{"a": 42.5, "c": 85, "zero": 0, "overridden": 80, "disabled": 100} {
		got := results[id]
		if got.Price == nil || *got.Price != want || got.Reason != "" {
			t.Fatalf("%s: got %+v, want price %v", id, got, want)
		}
	}
	for id, reason := range map[string]string{
		"b":       "discount option extra is missing",
		"unknown": "vendor program for unknown is missing",
		"case":    "vendor program for acme is missing",
	} {
		got := results[id]
		if got.Price != nil || got.Text != "Unavailable" || got.Reason != reason {
			t.Fatalf("%s: got %+v, want %s", id, got, reason)
		}
	}
	// Creating a second program must produce an ambiguity error, not an arbitrary price.
	duplicate, err := domain.NewVendorProgram("ACME", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Create(ctx, duplicate); err != nil {
		t.Fatal(err)
	}
	got := price(`[{"productId":"a","vendor":"ACME","listPrice":100}]`)["a"]
	if got.Price != nil || got.Reason != "multiple vendor programs found for vendor ACME" {
		t.Fatalf("ambiguous vendor: %+v", got)
	}
}
