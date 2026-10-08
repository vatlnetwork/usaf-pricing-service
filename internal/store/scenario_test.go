package store

import (
	"encoding/json"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"usaf-pricing-service/domain"
)

func TestScenarioBSONRoundTrip(t *testing.T) {
	zero := 0.0
	start := time.Date(2026, 10, 8, 18, 0, 0, 0, time.UTC)
	p := domain.VendorProgram{Vendor: "Example"}
	if err := p.UpdateScenarios([]domain.PricingScenario{{ID: "fixed", Name: "Fixed", Method: "fixed", FixedPrice: &zero, Approved: true, StartsAt: &start, Scope: domain.ProductSelector{Configurations: []string{"120V"}, ExcludedGroupNames: []string{"Excluded"}}, Conditions: []domain.ScenarioCondition{{Field: "attribute.member", Operator: "=", Value: "true"}}, Adjustments: []domain.PriceAdjustment{{Type: "surcharge", Amount: 10}}, ApprovalEvidence: "verified"}}, "highest_tier"); err != nil {
		t.Fatal(err)
	}
	before := programDocument{Program: p, Revision: 4}
	data, err := bson.Marshal(before)
	if err != nil {
		t.Fatal(err)
	}
	var after programDocument
	if err = bson.Unmarshal(data, &after); err != nil {
		t.Fatal(err)
	}
	// UpdatedAt is truncated by MongoDB; compare it at storage precision.
	before.Program.UpdatedAt = before.Program.UpdatedAt.UTC().Truncate(time.Millisecond)
	a, _ := json.Marshal(before.Program)
	b, _ := json.Marshal(after.Program)
	if string(a) != string(b) || after.Revision != 4 {
		t.Fatalf("BSON dropped scenario fields:\n%s\n%s", a, b)
	}
}
