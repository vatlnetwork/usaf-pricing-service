package api_test

import (
	"context"
	"encoding/json"
	"testing"

	"usaf-pricing-service/domain"
)

func TestMongoScenarioPersistenceIntegration(t *testing.T) {
	db := integrationStore(t)
	ctx := context.Background()
	p := &domain.VendorProgram{Vendor: "Scenario integration"}
	price := 6647.0
	if err := p.UpdateScenarios([]domain.PricingScenario{{ID: "bundle", Name: "Bundle", Method: "bundle", PriceUnit: "bundle", Approved: true, FixedPrice: &price, BundleComponents: []domain.BundleComponent{{ProductID: "p", Quantity: 5}}, Scope: domain.ProductSelector{ProductIDs: []string{"p"}}, Conditions: []domain.ScenarioCondition{{Field: "fulfillment", Operator: "=", Value: "pickup"}}}}, "lowest_price"); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(ctx, p); err != nil {
		t.Fatal(err)
	}
	read, err := db.Get(ctx, p.Id)
	if err != nil {
		t.Fatal(err)
	}
	want, _ := json.Marshal(p.Scenarios)
	got, _ := json.Marshal(read.Scenarios)
	if string(want) != string(got) || read.SelectionPolicy != p.SelectionPolicy {
		t.Fatalf("scenario BSON roundtrip: got %s, want %s", got, want)
	}
	result, err := read.PriceOrder(domain.Order{Fulfillment: "pickup", Lines: []domain.OrderLine{{LineID: "a", ProductID: "p", PriceUnit: "each", Quantity: 5}}})
	if err != nil || !result.Available || *result.Total != 6647 {
		t.Fatalf("stored pricing failed: %+v %v", result, err)
	}
	_, err = db.Update(ctx, p.Id, func(v *domain.VendorProgram) error { return v.UpdateScenarios(nil, "require_review") })
	if err != nil {
		t.Fatal(err)
	}
	read, err = db.Get(ctx, p.Id)
	if err != nil || len(read.Scenarios) != 0 || read.SelectionPolicy != "require_review" {
		t.Fatal("scenario replacement did not persist")
	}
}
