package domain

import (
	"math"
	"strings"
	"testing"
)

func TestCalculateDealerPrice(t *testing.T) {
	program := &VendorProgram{
		Vendor: "ACME",
		DiscountOptions: []DiscountOption{
			{Name: "ten-percent", DiscountPath: []DiscountPathItem{{Type: DiscountPathItemTypePercentage, Amount: 10}}},
			{Name: "ten-dollars", DiscountPath: []DiscountPathItem{{Type: DiscountPathItemTypeDollarAmount, Amount: 10}}},
			{Name: "half", DiscountPath: []DiscountPathItem{{Type: DiscountPathItemTypePercentage, Amount: 50}}},
			{Name: "free", DiscountPath: []DiscountPathItem{{Type: DiscountPathItemTypePercentage, Amount: 100}}},
			{Name: "path", DiscountPath: []DiscountPathItem{
				{Type: DiscountPathItemTypePercentage, Amount: 10},
				{Type: DiscountPathItemTypeDollarAmount, Amount: 5},
			}},
			{Name: "zero", DiscountPath: []DiscountPathItem{{Type: DiscountPathItemTypePercentage, Amount: 0}}},
			{Name: "empty"},
			{Name: "invalid", DiscountPath: []DiscountPathItem{{Type: DiscountPathItemTypePercentage, Amount: math.NaN()}}},
		},
		ProductOverrides: []ProductOverride{
			{ProductId: "overridden", DiscountOptions: []DiscountOption{testDiscountOption("path", 20)}},
			{ProductId: "other-override", DiscountOptions: []DiscountOption{testDiscountOption("path", 30)}},
			{ProductId: "disabled", DiscountOptions: []DiscountOption{{Name: "path"}}},
			{ProductId: "special", DiscountOptions: []DiscountOption{{Name: "extra", DiscountPath: []DiscountPathItem{{Type: DiscountPathItemTypePercentage, Amount: 50}}}}},
		},
	}
	for _, tt := range []struct {
		name      string
		productID string
		price     float64
		options   []string
		want      float64
		errText   string
	}{
		{"override replaces entire vendor path", "overridden", 100, []string{"path"}, 80, ""},
		{"separate product override", "other-override", 100, []string{"path"}, 70, ""},
		{"vendor path without override", "a", 100, []string{"path"}, 85, ""},
		{"empty override path replaces vendor path", "disabled", 100, []string{"path"}, 100, ""},
		{"override and inherited option", "overridden", 100, []string{"path", "ten-dollars"}, 70, ""},
		{"repeated override selection", "overridden", 100, []string{"path", "path"}, 64, ""},
		{"percentage", "a", 100, []string{"ten-percent"}, 90, ""},
		{"dollar", "a", 100, []string{"ten-dollars"}, 90, ""},
		{"percentage then dollar", "a", 100, []string{"ten-percent", "ten-dollars"}, 80, ""},
		{"dollar then percentage", "a", 100, []string{"ten-dollars", "ten-percent"}, 81, ""},
		{"path and product option", "special", 100, []string{"path", "extra"}, 42.5, ""},
		{"no selections", "a", 100, nil, 100, ""},
		{"empty path", "a", 100, []string{"empty"}, 100, ""},
		{"zero discount", "a", 100, []string{"zero"}, 100, ""},
		{"zero starting price", "a", 0, []string{"ten-dollars"}, 0, ""},
		{"exact zero", "a", 10, []string{"ten-dollars", "ten-percent"}, 0, ""},
		{"clamp below zero", "a", 5, []string{"ten-dollars", "half"}, 0, ""},
		{"100 percent", "a", 100, []string{"free", "ten-dollars"}, 0, ""},
		{"round only final result", "a", 0.05, []string{"half", "half"}, 0.01, ""},
		{"half cent", "a", 1.005, nil, 1.01, ""},
		{"half cent after subtraction", "a", 11.005, []string{"ten-dollars"}, 1.01, ""},
		{"below half cent", "a", 1.004, nil, 1, ""},
		{"round to zero", "a", 0.004, nil, 0, ""},
		{"missing option", "a", 100, []string{"missing"}, 0, "discount option missing is missing"},
		{"other product override", "a", 100, []string{"extra"}, 0, "discount option extra is missing"},
		{"missing after free option", "a", 100, []string{"free", "missing"}, 0, "discount option missing is missing"},
		{"missing at initial zero", "a", 0, []string{"missing"}, 0, "discount option missing is missing"},
		{"invalid stored option", "a", 100, []string{"invalid"}, 0, "discount option invalid is invalid"},
		{"negative price", "a", -1, nil, 0, "list price must be finite and nonnegative"},
		{"nonfinite price", "a", math.Inf(1), nil, 0, "list price must be finite and nonnegative"},
		{"NaN price", "a", math.NaN(), nil, 0, "list price must be finite and nonnegative"},
		{"missing product ID", "", 100, nil, 0, "product id is required"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := program.CalculateDealerPrice(Product{ProductId: tt.productID, Vendor: "ACME", ListPrice: tt.price, DiscountOptions: tt.options})
			if tt.errText != "" {
				if err == nil || !strings.Contains(err.Error(), tt.errText) {
					t.Fatalf("got error %v, want %q", err, tt.errText)
				}
				return
			}
			if err != nil || got != tt.want {
				t.Fatalf("got (%v, %v), want (%v, nil)", got, err, tt.want)
			}
		})
	}
	for _, vendor := range []string{"", "OTHER"} {
		if _, err := program.CalculateDealerPrice(Product{ProductId: "a", Vendor: vendor, ListPrice: 100}); err == nil {
			t.Fatalf("accepted invalid vendor %q", vendor)
		}
	}
}
