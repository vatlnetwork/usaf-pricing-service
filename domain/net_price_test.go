package domain

import (
	"errors"
	"math"
	"reflect"
	"testing"
	"time"
)

func TestNetPriceOverridePrecedence(t *testing.T) {
	program, err := NewVendorProgram("vendor", []DiscountOption{testDiscountOption("base", 10)}, []ProductOverride{{
		ProductId: "fixed", NetPrice: 42.125,
		// Even invalid or duplicate discount options are ignored for fixed prices.
		DiscountOptions: []DiscountOption{{Name: "ignored"}, {Name: "ignored", DiscountPath: []DiscountPathItem{{Type: "invalid", Amount: -1}}}},
	}}, []ProductGroupOverride{testGroupOverride("group", 20)})
	if err != nil {
		t.Fatal(err)
	}
	want := []DiscountOption{{Name: "net price", NetPrice: 42.125, DiscountPath: []DiscountPathItem{}}}
	options := program.GetDiscountOptionsForProduct("fixed", "group")
	if !reflect.DeepEqual(options, want) {
		t.Fatalf("options = %+v, want %+v", options, want)
	}
	options[0].NetPrice = 1
	if got := program.GetDiscountOptionsForProduct("fixed", "group"); !reflect.DeepEqual(got, want) {
		t.Fatalf("mutating returned options changed net price: %+v", got)
	}
	for _, selected := range [][]string{nil, {}, {"net price"}, {"base", "shared", "ignored"}, {"missing"}, {"net price", "net price", "missing"}} {
		for _, listPrice := range []float64{0, 10, 100} {
			price, err := program.CalculateDealerPrice(Product{ProductId: "fixed", GroupName: "group", Vendor: "vendor", ListPrice: listPrice, DiscountOptions: selected})
			if err != nil || price != 42.125 {
				t.Fatalf("selection %v, list price %v: got (%v, %v), want exact net price", selected, listPrice, price, err)
			}
		}
	}
	if price, err := program.CalculateDealerPrice(Product{ProductId: "ordinary", Vendor: "vendor", ListPrice: 100, DiscountOptions: []string{"base"}}); err != nil || price != 90 {
		t.Fatalf("net override affected another product: %v, %v", price, err)
	}
	// Other updates must not validate ignored duplicate product option names.
	if err := program.UpdateDiscountOptions([]DiscountOption{testDiscountOption("base", 15)}); err != nil {
		t.Fatal(err)
	}
	if err := program.UpsertProductGroupOverrides([]ProductGroupOverride{testGroupOverride("group", 30)}); err != nil {
		t.Fatal(err)
	}
	past := time.Now().Add(-time.Hour)
	program.UpdateExpiry(&past)
	if got := program.GetDiscountOptionsForProduct("fixed", "group"); len(got) != 0 {
		t.Fatalf("expired net price option returned: %+v", got)
	}
	if _, err := program.CalculateDealerPrice(Product{ProductId: "fixed", Vendor: "vendor", ListPrice: 100}); !errors.Is(err, ErrVendorProgramExpired) {
		t.Fatalf("expired net price error = %v", err)
	}
}

func TestNetPriceOverrideUpsertAndClear(t *testing.T) {
	program, err := NewVendorProgram("vendor", []DiscountOption{testDiscountOption("base", 10)}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, netPrice := range []float64{42, 25, 0} {
		if err := program.UpsertProductOverrides([]ProductOverride{{ProductId: "a", NetPrice: netPrice, DiscountOptions: []DiscountOption{testDiscountOption("base", 20)}}}); err != nil {
			t.Fatal(err)
		}
		wantPrice := netPrice
		if netPrice == 0 {
			wantPrice = 80
			if got := program.GetDiscountOptionsForProduct("a", ""); !reflect.DeepEqual(got, []DiscountOption{testDiscountOption("base", 20)}) {
				t.Fatalf("clearing net price did not restore discounts: %+v", got)
			}
		}
		price, err := program.CalculateDealerPrice(Product{ProductId: "a", Vendor: "vendor", ListPrice: 100, DiscountOptions: []string{"base"}})
		if err != nil || price != wantPrice {
			t.Fatalf("net price %v: got (%v, %v), want %v", netPrice, price, err, wantPrice)
		}
	}
}

func TestNetPriceValidation(t *testing.T) {
	for _, price := range []float64{-1, math.NaN(), math.Inf(1), math.Inf(-1)} {
		override := ProductOverride{ProductId: "a", NetPrice: price}
		if _, err := NewVendorProgram("vendor", nil, []ProductOverride{override}, nil); err == nil {
			t.Fatalf("accepted net price %v on create", price)
		}
		program, err := NewVendorProgram("vendor", nil, []ProductOverride{{ProductId: "a", NetPrice: 42}}, nil)
		if err != nil {
			t.Fatal(err)
		}
		before := *program
		if err := program.UpsertProductOverrides([]ProductOverride{{ProductId: "a", NetPrice: 25}, override}); err == nil {
			t.Fatalf("accepted net price %v on update", price)
		}
		if !reflect.DeepEqual(*program, before) {
			t.Fatal("invalid update changed program")
		}
		program.ProductOverrides[0] = override
		if _, err := program.CalculateDealerPrice(Product{ProductId: "a", Vendor: "vendor", ListPrice: 100}); err == nil {
			t.Fatalf("priced invalid stored net price %v", price)
		}
	}
	if err := (ProductOverride{ProductId: " ", NetPrice: 42}).Validate(); err == nil {
		t.Fatal("net price bypassed product ID validation")
	}
}
