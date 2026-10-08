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
		{"replacement conflicts with base", []ProductOverride{testOverride("a", "base")}},
		{"append conflicts with base", []ProductOverride{testOverride("b", "base")}},
		{"duplicate names within override", []ProductOverride{{ProductId: "a", DiscountOptions: []DiscountOption{{Name: "same"}, {Name: "same"}}}}},
		{"duplicate names between overrides", []ProductOverride{testOverride("a", "same"), testOverride("b", "same")}},
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
	v, err := NewVendorProgram("vendor", nil, []ProductOverride{testOverride("a", "old"), testOverride("c", "untouched")})
	if err != nil {
		t.Fatal(err)
	}
	v.UpdatedAt = time.Unix(1, 0)
	createdAt := v.CreatedAt
	updates := []ProductOverride{testOverride("a", "new"), testOverride("b", "added")}
	if err := v.UpsertProductOverrides(updates); err != nil {
		t.Fatal(err)
	}
	want := []ProductOverride{testOverride("a", "new"), testOverride("c", "untouched"), testOverride("b", "added")}
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
		{testDiscountOption("extra", 10)},
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
