package domain

import (
	"encoding/json"
	"math"
	"os"
	"strings"
	"testing"
	"time"
)

func amount(v float64) *float64 { return &v }
func basicRule(id string) PricingScenario {
	return PricingScenario{ID: id, Name: id, Method: "steps", Approved: true}
}
func basicOrder() Order {
	return Order{Lines: []OrderLine{{LineID: "line", ProductID: "p", Quantity: 1, PriceUnit: "each", ListPrice: amount(1000)}}}
}
func scenarioProgram(t *testing.T, policy string, rules ...PricingScenario) *VendorProgram {
	t.Helper()
	v := &VendorProgram{Vendor: "Example"}
	if err := v.UpdateScenarios(rules, policy); err != nil {
		t.Fatal(err)
	}
	return v
}
func assertOrder(t *testing.T, p *VendorProgram, o Order, want *float64) OrderPriceResult {
	t.Helper()
	r, err := p.PriceOrder(o)
	if err != nil {
		t.Fatal(err)
	}
	if want == nil {
		if r.Available || r.Total != nil || len(r.Reasons) == 0 {
			t.Fatalf("expected unavailable, got %+v", r)
		}
	} else {
		if !r.Available || r.Total == nil || math.Abs(*r.Total-*want) > 0.000001 {
			t.Fatalf("want %.2f, got %+v", *want, r)
		}
		sum := 0.0
		for _, l := range r.Lines {
			sum += l.Total
		}
		if math.Abs(sum-*r.Total) > 0.000001 {
			t.Fatalf("line totals %v do not reconcile to %v", sum, *r.Total)
		}
	}
	return r
}
func TestTutorialScenarios(t *testing.T) {
	data, err := os.ReadFile("../internal/api/web/examples.json")
	if err != nil {
		t.Fatal(err)
	}
	var examples []struct {
		ID       string        `json:"id"`
		Program  VendorProgram `json:"program"`
		Order    Order         `json:"order"`
		Expected float64       `json:"expected_total"`
	}
	if err = json.Unmarshal(data, &examples); err != nil {
		t.Fatal(err)
	}
	for _, example := range examples {
		t.Run(example.ID, func(t *testing.T) { assertOrder(t, &example.Program, example.Order, &example.Expected) })
	}
}
func TestScenarioEligibilityAndBoundaries(t *testing.T) {
	base := basicRule("rule")
	base.Adjustments = []PriceAdjustment{{Type: "percentage", Amount: 50}, {Type: "percentage", Amount: 5}, {Type: "percentage", Amount: 2}}
	tests := []struct {
		name   string
		change func(*PricingScenario, *Order)
		price  *float64
	}{
		{"sequential math", func(_ *PricingScenario, _ *Order) {}, amount(465.5)},
		{"pickup", func(s *PricingScenario, o *Order) {
			s.Conditions = []ScenarioCondition{{"fulfillment", "=", "pickup"}}
			o.Fulfillment = "ship"
		}, nil},
		{"missing attribute fails inequality", func(s *PricingScenario, o *Order) {
			s.Conditions = []ScenarioCondition{{"attribute.contract", "!=", "true"}}
		}, nil},
		{"explicit attribute", func(s *PricingScenario, o *Order) {
			s.Conditions = []ScenarioCondition{{"attribute.contract", "!=", "true"}}
			o.Attributes = map[string]string{"contract": "false"}
		}, amount(465.5)},
		{"strict threshold", func(s *PricingScenario, o *Order) {
			s.Conditions = []ScenarioCondition{{"eligible_subtotal", ">", "1000"}}
		}, nil},
		{"inclusive threshold", func(s *PricingScenario, o *Order) {
			s.Conditions = []ScenarioCondition{{"eligible_subtotal", ">=", "1000"}}
		}, amount(465.5)},
		{"upper boundary", func(s *PricingScenario, o *Order) {
			s.Conditions = []ScenarioCondition{{"eligible_subtotal", "<", "1000"}}
		}, nil},
		{"approval", func(s *PricingScenario, o *Order) { s.Approved = false }, nil},
		{"manual review method", func(s *PricingScenario, o *Order) { s.Method = "review"; s.Adjustments = nil }, nil},
		{"exact configuration", func(s *PricingScenario, o *Order) {
			s.Scope.Configurations = []string{"120V"}
			o.Lines[0].Configuration = "240V"
		}, nil},
		{"excluded product", func(s *PricingScenario, o *Order) { s.Scope.ExcludedProductIDs = []string{"p"} }, nil},
		{"unit mismatch", func(s *PricingScenario, o *Order) { s.PriceUnit = "case" }, nil},
		{"zero fixed price", func(s *PricingScenario, o *Order) { s.Method = "fixed"; s.FixedPrice = amount(0) }, amount(0)},
		{"missing net", func(s *PricingScenario, o *Order) { s.StartingPrice = "net" }, nil},
		{"unapproved quote", func(s *PricingScenario, o *Order) { s.Method = "quote"; o.Lines[0].QuotePrice = amount(1000) }, nil},
		{"approved quote plus steps", func(s *PricingScenario, o *Order) {
			s.Method = "quote"
			o.Lines[0].ListPrice = nil
			o.Lines[0].QuotePrice = amount(1000)
			o.Lines[0].QuoteApproved = true
		}, amount(465.5)},
		{"multiplier surcharge", func(s *PricingScenario, o *Order) {
			s.Adjustments = []PriceAdjustment{{"multiplier", 0.48}, {"surcharge", 95}, {"percentage", 5}}
		}, amount(546.25)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := base
			o := basicOrder()
			tt.change(&s, &o)
			p := scenarioProgram(t, "", s)
			assertOrder(t, p, o, tt.price)
		})
	}
}
func TestScenarioExactTimes(t *testing.T) {
	start := time.Date(2026, 10, 8, 18, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	s := basicRule("rule")
	s.StartsAt = &start
	s.EndsAt = &end
	p := scenarioProgram(t, "", s)
	for _, tt := range []struct {
		at   time.Time
		want *float64
	}{{start.Add(-time.Millisecond), nil}, {start, amount(1000)}, {end.Add(-time.Millisecond), amount(1000)}, {end, nil}} {
		o := basicOrder()
		o.At = &tt.at
		assertOrder(t, p, o, tt.want)
	}
	p.ExpiresAt = &start
	o := basicOrder()
	o.At = &start
	assertOrder(t, p, o, nil)
}
func TestScenarioCombinationsAndSelection(t *testing.T) {
	base := basicRule("net")
	base.Method = "fixed"
	base.FixedPrice = amount(100)
	base.Combination = "compatible"
	base.CompatibleScenarioIDs = []string{"pallet", "spec"}
	pallet := basicRule("pallet")
	pallet.Role = "adjustment"
	pallet.StartingPrice = "rule"
	pallet.BaseScenarioID = "net"
	pallet.Adjustments = []PriceAdjustment{{"percentage", 10}}
	pallet.Tier = 1
	pallet.Priority = 2
	spec := basicRule("spec")
	spec.Role = "adjustment"
	spec.StartingPrice = "rule"
	spec.BaseScenarioID = "net"
	spec.Adjustments = []PriceAdjustment{{"percentage", 20}}
	spec.Tier = 2
	spec.Priority = 1
	for _, tt := range []struct {
		policy string
		price  *float64
	}{{"lowest_price", amount(80)}, {"highest_tier", amount(80)}, {"priority", amount(90)}, {"require_review", nil}} {
		p := scenarioProgram(t, tt.policy, base, pallet, spec)
		assertOrder(t, p, basicOrder(), tt.price)
	}
	p := scenarioProgram(t, "require_review", base, pallet, spec)
	o := basicOrder()
	o.SelectedScenarioIDs = []string{"pallet"}
	r := assertOrder(t, p, o, amount(90))
	if strings.Join(r.Lines[0].ScenarioIDs, ",") != "net,pallet" {
		t.Fatal(r)
	}
	o.SelectedScenarioIDs = []string{"pallet", "spec"}
	assertOrder(t, p, o, nil)
	p.Scenarios[1].Approved = false
	o.SelectedScenarioIDs = []string{"pallet"}
	assertOrder(t, p, o, nil)
	base.Combination = "exclusive"
	base.CompatibleScenarioIDs = nil
	if err := p.UpdateScenarios([]PricingScenario{base, pallet}, ""); err == nil {
		t.Fatal("exclusive base accepted adjustment")
	}
}
func TestBundleAlternativesAndFreeGoods(t *testing.T) {
	base := basicRule("regular")
	bundle := basicRule("bundle")
	bundle.Method = "bundle"
	bundle.PriceUnit = "bundle"
	bundle.FixedPrice = amount(150)
	bundle.BundleComponents = []BundleComponent{{ProductID: "p", Quantity: 1}, {ProductID: "accessory", Quantity: 1}}
	o := basicOrder()
	o.Lines[0].ListPrice = amount(100)
	o.Lines = append(o.Lines, OrderLine{LineID: "accessory", ProductID: "accessory", PriceUnit: "each", Quantity: 1, ListPrice: amount(100)})
	p := scenarioProgram(t, "", base, bundle)
	assertOrder(t, p, o, amount(150))
	bundle.FixedPrice = amount(250)
	p = scenarioProgram(t, "", base, bundle)
	assertOrder(t, p, o, amount(200))
	o.SelectedScenarioIDs = []string{"bundle"}
	o.Lines[1].Quantity = 2
	assertOrder(t, p, o, nil)
	free := basicRule("free")
	free.Method = "free"
	free.BuyQuantity = 2
	free.FreeQuantity = 1
	free.FreeValueLimit = amount(20)
	o = basicOrder()
	o.Lines[0].Quantity = 2
	o.Lines[0].ListPrice = amount(100)
	o.Lines = append(o.Lines, OrderLine{LineID: "cheap", ProductID: "p", Quantity: 1, PriceUnit: "each", ListPrice: amount(50)})
	p = scenarioProgram(t, "", free)
	assertOrder(t, p, o, amount(230))
	p.Scenarios[0].FreeValueLimit = nil
	assertOrder(t, p, o, amount(200))
	o.Lines[0].Quantity = 5
	assertOrder(t, p, o, nil)
}
func TestCountCapAndThresholdScope(t *testing.T) {
	s := basicRule("count")
	s.Method = "count"
	s.CountScope = &ProductSelector{GroupNames: []string{"Slicers"}}
	s.PercentPerUnit = 1
	s.MaximumPercent = 10
	s.Scope.GroupNames = []string{"Discounted"}
	s.QualificationScope = &ProductSelector{}
	s.Conditions = []ScenarioCondition{{"eligible_subtotal", ">=", "1200"}}
	base := basicRule("slicer")
	base.Scope.GroupNames = []string{"Slicers"}
	o := basicOrder()
	o.Lines[0].GroupName = "Discounted"
	o.Lines = append(o.Lines, OrderLine{LineID: "slicers", ProductID: "s", GroupName: "Slicers", Quantity: 12, PriceUnit: "each", ListPrice: amount(100)})
	p := scenarioProgram(t, "", s, base)
	assertOrder(t, p, o, amount(2100))
	// Excluded-from-discount lines still count toward the independent threshold.
	o.Lines[1].Quantity = 3
	assertOrder(t, p, o, amount(1270))
	o.Lines[1].Quantity = 1
	assertOrder(t, p, o, nil)
}
func TestScenarioRoundingAndValidation(t *testing.T) {
	s := basicRule("half")
	s.Adjustments = []PriceAdjustment{{"percentage", 50}}
	p := scenarioProgram(t, "", s)
	o := basicOrder()
	o.Lines[0].ListPrice = amount(0.01)
	o.Lines = append(o.Lines, OrderLine{LineID: "two", ProductID: "p", Quantity: 1, PriceUnit: "each", ListPrice: amount(.01)})
	assertOrder(t, p, o, amount(.01))
	original := p.Scenarios[0].ID
	for _, change := range []func(*PricingScenario){func(s *PricingScenario) { s.Method = "unknown" }, func(s *PricingScenario) { s.Adjustments[0].Amount = math.NaN() }, func(s *PricingScenario) { s.Conditions = []ScenarioCondition{{"eligible_quantity", ">=", "NaN"}} }, func(s *PricingScenario) { s.StartingPrice = "rule"; s.BaseScenarioID = s.ID }, func(s *PricingScenario) { s.Conditions = []ScenarioCondition{{"nonsense", "=", "x"}} }} {
		c := basicRule("bad")
		c.Adjustments = []PriceAdjustment{{"percentage", 10}}
		change(&c)
		if err := p.UpdateScenarios([]PricingScenario{c}, ""); err == nil {
			t.Fatal("invalid scenario accepted")
		}
		if p.Scenarios[0].ID != original {
			t.Fatal("failed validation mutated program")
		}
	}
	s.Scope.ProductIDs = []string{"p"}
	p = scenarioProgram(t, "", s)
	s.Scope.ProductIDs[0] = "changed"
	if p.Scenarios[0].Scope.ProductIDs[0] != "p" {
		t.Fatal("rules not deep copied")
	}
	for _, change := range []func(*Order){func(o *Order) { o.Lines[0].Quantity = 0 }, func(o *Order) { o.Lines[0].ListPrice = amount(math.Inf(1)) }, func(o *Order) { o.Lines = append(o.Lines, o.Lines[0]) }, func(o *Order) { o.SelectedScenarioIDs = []string{"unknown"} }} {
		o := basicOrder()
		change(&o)
		if _, err := p.PriceOrder(o); err == nil {
			t.Fatal("invalid order accepted")
		}
	}
}

func TestLegacyOrderAdapter(t *testing.T) {
	p := &VendorProgram{Vendor: "Example", ProductOverrides: []ProductOverride{{ProductId: "p", NetPrice: 0.005}}}
	o := basicOrder()
	o.Lines = append(o.Lines, OrderLine{LineID: "second", ProductID: "p", Quantity: 1, PriceUnit: "each", ListPrice: amount(100)})
	assertOrder(t, p, o, amount(.01))
	expiry := time.Now().Add(-time.Hour)
	p.ExpiresAt = &expiry
	assertOrder(t, p, o, nil)
	beforeExpiry := expiry.Add(-time.Second)
	o.At = &beforeExpiry
	assertOrder(t, p, o, amount(.01))
	o.Lines[0].ListPrice = nil
	assertOrder(t, p, o, nil)
}
