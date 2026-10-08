package domain

import (
	"math"
	"reflect"
	"testing"
	"time"
)

func testOverride(productID, optionName string) ProductOverride {
	return ProductOverride{
		ProductId:       productID,
		DiscountOptions: []DiscountOption{{Name: optionName}},
	}
}

func TestUpsertProductOverridesFailurePreservesState(t *testing.T) {
	tests := []struct {
		name    string
		updates []ProductOverride
	}{
		{"invalid item after replacement", []ProductOverride{testOverride("a", "new"), {ProductId: ""}}},
		{"invalid item after append", []ProductOverride{testOverride("b", "new"), {ProductId: ""}}},
		{"duplicate names within override", []ProductOverride{{ProductId: "a", DiscountOptions: []DiscountOption{{Name: "same"}, {Name: "same"}}}}},
		{"duplicate names after valid replacement", []ProductOverride{testOverride("a", "base"), {ProductId: "b", DiscountOptions: []DiscountOption{{Name: "same"}, {Name: "same"}}}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Spare capacity also exposes accidental writes beyond the stored length.
			overrides := make([]ProductOverride, 1, 4)
			overrides[0] = testOverride("a", "old")
			v, err := NewVendorProgram("vendor", []DiscountOption{{Name: "base"}}, overrides)
			if err != nil {
				t.Fatal(err)
			}
			before := *v
			before.ProductOverrides = []ProductOverride{testOverride("a", "old")}
			wantBacking := []ProductOverride{testOverride("a", "old"), {}, {}, {}}
			if err := v.UpsertProductOverrides(tt.updates); err == nil {
				t.Fatal("expected error")
			}
			if !reflect.DeepEqual(*v, before) {
				t.Fatalf("failed upsert changed program: got %+v, want %+v", *v, before)
			}
			if !reflect.DeepEqual(overrides[:cap(overrides)], wantBacking) {
				t.Fatalf("failed upsert mutated original backing array: %+v", overrides[:cap(overrides)])
			}
		})
	}
}

func TestUpsertProductOverridesSuccess(t *testing.T) {
	v, err := NewVendorProgram("vendor", []DiscountOption{{Name: "base"}}, []ProductOverride{testOverride("a", "old"), testOverride("c", "base")})
	if err != nil {
		t.Fatal(err)
	}
	v.UpdatedAt = time.Unix(1, 0)
	createdAt := v.CreatedAt
	updates := []ProductOverride{testOverride("a", "base"), testOverride("b", "base")}
	if err := v.UpsertProductOverrides(updates); err != nil {
		t.Fatal(err)
	}
	want := []ProductOverride{testOverride("a", "base"), testOverride("c", "base"), testOverride("b", "base")}
	if !reflect.DeepEqual(v.ProductOverrides, want) {
		t.Fatalf("got %+v, want %+v", v.ProductOverrides, want)
	}
	if !v.UpdatedAt.After(time.Unix(1, 0)) || !v.CreatedAt.Equal(createdAt) {
		t.Fatal("successful upsert must advance UpdatedAt and preserve CreatedAt")
	}
}

func TestNewVendorProgramRejectsDuplicateProductIDs(t *testing.T) {
	_, err := NewVendorProgram("vendor", nil, []ProductOverride{testOverride("a", "first"), testOverride("a", "second")})
	if err == nil {
		t.Fatal("expected duplicate product ID error")
	}
}

func TestDiscountPathItemValidate(t *testing.T) {
	tests := []struct {
		name    string
		amount  float64
		percent bool
		dollar  bool
	}{
		{"NaN", math.NaN(), false, false},
		{"positive infinity", math.Inf(1), false, false},
		{"negative infinity", math.Inf(-1), false, false},
		{"negative", -1, false, false},
		{"zero", 0, true, true},
		{"fractional", 0.5, true, true},
		{"percentage maximum", 100, true, true},
		{"above percentage maximum", 100.01, false, true},
	}
	for _, tt := range tests {
		for _, kind := range []DiscountPathItemType{DiscountPathItemTypePercentage, DiscountPathItemTypeDollarAmount} {
			t.Run(string(kind)+"/"+tt.name, func(t *testing.T) {
				wantValid := tt.percent
				if kind == DiscountPathItemTypeDollarAmount {
					wantValid = tt.dollar
				}
				err := (DiscountPathItem{Type: kind, Amount: tt.amount}).Validate()
				if (err == nil) != wantValid {
					t.Fatalf("Validate() error = %v, want valid = %v", err, wantValid)
				}
			})
		}
	}
	if err := (DiscountPathItem{Type: "unknown", Amount: 10}).Validate(); err == nil {
		t.Fatal("expected invalid type error")
	}
}

func testDiscountOption(name string, amount float64) DiscountOption {
	return DiscountOption{
		Name:         name,
		DiscountPath: []DiscountPathItem{{Type: DiscountPathItemTypePercentage, Amount: amount}},
	}
}

func TestNewVendorProgramCopiesInputs(t *testing.T) {
	options := []DiscountOption{testDiscountOption("base", 10)}
	overrides := []ProductOverride{{ProductId: "a", DiscountOptions: []DiscountOption{testDiscountOption("extra", 20)}}}
	v, err := NewVendorProgram("vendor", options, overrides)
	if err != nil {
		t.Fatal(err)
	}
	options[0].DiscountPath[0].Amount = -1
	options[0].Name = "changed"
	overrides[0].DiscountOptions[0].DiscountPath[0].Amount = -1
	overrides[0].DiscountOptions[0].Name = "changed"
	overrides[0].ProductId = "changed"
	overrides[0].DiscountOptions = nil

	wantOptions := []DiscountOption{testDiscountOption("base", 10)}
	wantOverrides := []ProductOverride{{ProductId: "a", DiscountOptions: []DiscountOption{testDiscountOption("extra", 20)}}}
	if !reflect.DeepEqual(v.DiscountOptions, wantOptions) || !reflect.DeepEqual(v.ProductOverrides, wantOverrides) {
		t.Fatalf("input mutation changed program: %+v", v)
	}
}

func TestUpdateDiscountOptionsCopiesInput(t *testing.T) {
	v, err := NewVendorProgram("vendor", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	options := []DiscountOption{testDiscountOption("base", 10)}
	if err := v.UpdateDiscountOptions(options); err != nil {
		t.Fatal(err)
	}
	options[0].DiscountPath[0].Amount = -1
	options[0].Name = "changed"
	want := []DiscountOption{testDiscountOption("base", 10)}
	if !reflect.DeepEqual(v.DiscountOptions, want) {
		t.Fatalf("input mutation changed stored options: %+v", v.DiscountOptions)
	}
}

func TestUpdateDiscountOptionsFailurePreservesState(t *testing.T) {
	v, err := NewVendorProgram("vendor", []DiscountOption{testDiscountOption("base", 10)}, []ProductOverride{testOverride("a", "extra")})
	if err != nil {
		t.Fatal(err)
	}
	updatedAt := v.UpdatedAt
	for _, options := range [][]DiscountOption{
		{testDiscountOption("invalid", -1)},
		{testDiscountOption("extra", 10), testDiscountOption("extra", 20)},
	} {
		if err := v.UpdateDiscountOptions(options); err == nil {
			t.Fatal("expected error")
		}
		if !reflect.DeepEqual(v.DiscountOptions, []DiscountOption{testDiscountOption("base", 10)}) || !v.UpdatedAt.Equal(updatedAt) {
			t.Fatalf("failed update changed program: %+v", v)
		}
	}
}

func TestUpsertProductOverridesCopiesInputs(t *testing.T) {
	v, err := NewVendorProgram("vendor", nil, []ProductOverride{testOverride("a", "old")})
	if err != nil {
		t.Fatal(err)
	}
	updates := []ProductOverride{
		{ProductId: "a", DiscountOptions: []DiscountOption{testDiscountOption("replacement", 10)}},
		{ProductId: "b", DiscountOptions: []DiscountOption{testDiscountOption("addition", 20)}},
	}
	if err := v.UpsertProductOverrides(updates); err != nil {
		t.Fatal(err)
	}
	for i := range updates {
		updates[i].DiscountOptions[0].DiscountPath[0].Amount = -1
		updates[i].DiscountOptions[0].Name = "changed"
		updates[i].ProductId = "changed"
		updates[i].DiscountOptions = nil
	}
	want := []ProductOverride{
		{ProductId: "a", DiscountOptions: []DiscountOption{testDiscountOption("replacement", 10)}},
		{ProductId: "b", DiscountOptions: []DiscountOption{testDiscountOption("addition", 20)}},
	}
	if !reflect.DeepEqual(v.ProductOverrides, want) {
		t.Fatalf("input mutation changed stored overrides: %+v", v.ProductOverrides)
	}
}

func TestGetDiscountOptionsForProductAddsOnlyMatchingOptionsAndReturnsCopies(t *testing.T) {
	v, err := NewVendorProgram("vendor", []DiscountOption{testDiscountOption("base", 10)}, []ProductOverride{
		{ProductId: "a", DiscountOptions: []DiscountOption{testDiscountOption("extra-a", 20)}},
		{ProductId: "b", DiscountOptions: []DiscountOption{testDiscountOption("extra-b", 30)}},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		productID string
		want      []DiscountOption
	}{
		{"a", []DiscountOption{testDiscountOption("base", 10), testDiscountOption("extra-a", 20)}},
		{"b", []DiscountOption{testDiscountOption("base", 10), testDiscountOption("extra-b", 30)}},
		{"without-override", []DiscountOption{testDiscountOption("base", 10)}},
	} {
		t.Run(tt.productID, func(t *testing.T) {
			options := v.GetDiscountOptionsForProduct(tt.productID)
			if !reflect.DeepEqual(options, tt.want) {
				t.Fatalf("got %+v, want %+v", options, tt.want)
			}
			for i := range options {
				options[i].DiscountPath[0].Amount = -1
				options[i].Name = "changed"
			}
			if got := v.GetDiscountOptionsForProduct(tt.productID); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("output mutation changed stored options: %+v", got)
			}
		})
	}
}

func TestNewVendorProgramDiscountOptionNameScopes(t *testing.T) {
	for _, tt := range []struct {
		name      string
		options   []DiscountOption
		overrides []ProductOverride
		wantError bool
	}{
		{"shared across all scopes", []DiscountOption{{Name: "same"}}, []ProductOverride{testOverride("a", "same"), testOverride("b", "same")}, false},
		{"shared between overrides", nil, []ProductOverride{testOverride("a", "same"), testOverride("b", "same")}, false},
		{"duplicate vendor options", []DiscountOption{{Name: "same"}, {Name: "same"}}, nil, true},
		{"duplicate override options", nil, []ProductOverride{{ProductId: "a", DiscountOptions: []DiscountOption{{Name: "same"}, {Name: "same"}}}}, true},
		{"duplicate in later override", nil, []ProductOverride{testOverride("a", "same"), {ProductId: "b", DiscountOptions: []DiscountOption{{Name: "same"}, {Name: "same"}}}}, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewVendorProgram("vendor", tt.options, tt.overrides)
			if (err != nil) != tt.wantError {
				t.Fatalf("error = %v, want error = %v", err, tt.wantError)
			}
		})
	}
}

func TestUpdateDiscountOptionsAllowsOverrideNames(t *testing.T) {
	v, err := NewVendorProgram("vendor", nil, []ProductOverride{testOverride("a", "shared"), testOverride("b", "shared")})
	if err != nil {
		t.Fatal(err)
	}
	options := []DiscountOption{testDiscountOption("shared", 10)}
	if err := v.UpdateDiscountOptions(options); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(v.DiscountOptions, options) {
		t.Fatalf("got %+v, want %+v", v.DiscountOptions, options)
	}
}

func TestGetDiscountOptionsForProductOverridePrecedence(t *testing.T) {
	base := []DiscountOption{testDiscountOption("shared", 10), testDiscountOption("base-only", 5)}
	v, err := NewVendorProgram("vendor", base, []ProductOverride{
		{ProductId: "a", DiscountOptions: []DiscountOption{testDiscountOption("shared", 20), testDiscountOption("extra", 30)}},
		{ProductId: "b", DiscountOptions: []DiscountOption{testDiscountOption("shared", 40), testDiscountOption("extra", 50)}},
		{ProductId: "empty-path", DiscountOptions: []DiscountOption{{Name: "shared"}}},
		{ProductId: "empty-override"},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		productID string
		want      []DiscountOption
	}{
		{"a", []DiscountOption{testDiscountOption("base-only", 5), testDiscountOption("shared", 20), testDiscountOption("extra", 30)}},
		{"b", []DiscountOption{testDiscountOption("base-only", 5), testDiscountOption("shared", 40), testDiscountOption("extra", 50)}},
		{"empty-path", []DiscountOption{testDiscountOption("base-only", 5), {Name: "shared"}}},
		{"empty-override", base},
		{"without-override", base},
	} {
		t.Run(tt.productID, func(t *testing.T) {
			got := v.GetDiscountOptionsForProduct(tt.productID)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("got %+v, want %+v", got, tt.want)
			}
			for i := range got {
				got[i].Name = "changed"
				if len(got[i].DiscountPath) > 0 {
					got[i].DiscountPath[0].Amount = -1
				}
			}
			if got := v.GetDiscountOptionsForProduct(tt.productID); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("output mutation changed stored options: %+v", got)
			}
		})
	}
	v.RemoveProductOverrides([]string{"a"})
	if got := v.GetDiscountOptionsForProduct("a"); !reflect.DeepEqual(got, base) {
		t.Fatalf("removing override did not restore vendor options: %+v", got)
	}
}
