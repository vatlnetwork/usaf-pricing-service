package domain

import (
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestQuoteEligibilityAcrossScopes(t *testing.T) {
	for _, tt := range []struct {
		name                   string
		vendor, group, product bool
		productID, groupName   string
		want                   bool
	}{
		{"disabled", false, false, false, "a", "group", false},
		{"vendor", true, false, false, "a", "group", true},
		{"group", false, true, false, "a", "group", true},
		{"product", false, false, true, "a", "", true},
		{"all scopes", true, true, true, "a", "group", true},
		{"other product", false, false, true, "b", "group", false},
		{"other group", false, true, false, "a", "other", false},
		{"empty group", false, true, false, "a", "", false},
		{"exact group match", false, true, false, "a", "Group", false},
		{"exact product match", false, false, true, "A", "group", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			program, err := NewVendorProgram("vendor", []DiscountOption{testDiscountOption("base", 10)},
				[]ProductOverride{{ProductId: "a", QuoteEnabled: tt.product}},
				[]ProductGroupOverride{{GroupName: "group", QuoteEnabled: tt.group}})
			if err != nil {
				t.Fatal(err)
			}
			program.UpdateQuoteEnabled(tt.vendor)
			if got := program.SupportsQuotePricing(tt.productID, tt.groupName); got != tt.want {
				t.Fatalf("supports quotes = %v, want %v", got, tt.want)
			}
			wantOptions := []DiscountOption{testDiscountOption("base", 10)}
			if tt.want {
				wantOptions = append(wantOptions, DiscountOption{Name: "quote price", DiscountPath: []DiscountPathItem{}})
			}
			got := program.GetDiscountOptionsForProduct(tt.productID, tt.groupName)
			if !reflect.DeepEqual(got, wantOptions) {
				t.Fatalf("options = %+v, want %+v", got, wantOptions)
			}
			got[0].DiscountPath[0].Amount = 50
			if !reflect.DeepEqual(program.GetDiscountOptionsForProduct(tt.productID, tt.groupName), wantOptions) {
				t.Fatal("returned options alias program")
			}
			price, err := program.CalculateDealerPrice(Product{ProductId: tt.productID, GroupName: tt.groupName, Vendor: "vendor", ListPrice: 100, QuotePrice: 37.125, DiscountOptions: []string{"base"}})
			wantPrice := 90.0
			if tt.want {
				wantPrice = 37.125
			}
			if err != nil || price != wantPrice {
				t.Fatalf("price = %v, %v; want %v", price, err, wantPrice)
			}
		})
	}
}

func TestQuotePricePrecedenceAndFallback(t *testing.T) {
	for _, tt := range []struct {
		name             string
		enabled          bool
		quote, net, list float64
		selected         []string
		want             float64
		errText          string
	}{
		{name: "quote without selections", enabled: true, quote: 42.125, want: 42.125},
		{name: "quote ignores unknown and repeated options", enabled: true, quote: 42.125, list: 100, selected: []string{"missing", "base", "base"}, want: 42.125},
		{name: "quote beats net price", enabled: true, quote: 42.125, net: 25, list: 100, want: 42.125},
		{name: "quote does not depend on list price", enabled: true, quote: 42, list: -1, want: 42},
		{name: "unsupported quote uses discounts", quote: 42, list: 100, selected: []string{"base"}, want: 90},
		{name: "unsupported quote without selections", quote: 42, list: 100, errText: "quote pricing is not supported and no discount options were provided"},
		{name: "unsupported quote without selections even with net", quote: 42, net: 25, list: 100, errText: "quote pricing is not supported and no discount options were provided"},
		{name: "unsupported quote validates selected options", quote: 42, list: 100, selected: []string{"missing"}, errText: "discount option missing is missing"},
		{name: "zero quote uses discounts", enabled: true, list: 100, selected: []string{"base"}, want: 90},
		{name: "negative quote uses discounts", enabled: true, quote: -1, list: 100, selected: []string{"base"}, want: 90},
		{name: "no quote keeps net price", enabled: true, net: 25, list: 100, selected: []string{"missing"}, want: 25},
		{name: "no quote keeps list price", enabled: true, list: 100, want: 100},
		{name: "quote option needs a price", enabled: true, list: 100, selected: []string{"quote price"}, errText: "a positive quote price is required"},
		{name: "quote option with price", enabled: true, quote: 42, list: 100, selected: []string{"quote price"}, want: 42},
		{name: "NaN quote", enabled: true, quote: math.NaN(), list: 100, errText: "quote price must be finite"},
		{name: "infinite quote", enabled: true, quote: math.Inf(1), list: 100, errText: "quote price must be finite"},
		{name: "negative infinite quote", quote: math.Inf(-1), list: 100, errText: "quote price must be finite"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			program := &VendorProgram{Vendor: "vendor", QuoteEnabled: tt.enabled, DiscountOptions: []DiscountOption{testDiscountOption("base", 10)}, ProductOverrides: []ProductOverride{{ProductId: "a", NetPrice: tt.net}}}
			price, err := program.CalculateDealerPrice(Product{ProductId: "a", Vendor: "vendor", ListPrice: tt.list, QuotePrice: tt.quote, DiscountOptions: tt.selected})
			if tt.errText != "" {
				if err == nil || !strings.Contains(err.Error(), tt.errText) {
					t.Fatalf("error = %v, want %q", err, tt.errText)
				}
			} else if err != nil || price != tt.want {
				t.Fatalf("price = %v, %v; want %v", price, err, tt.want)
			}
		})
	}
}

func TestQuoteOptionsAndExpiry(t *testing.T) {
	program := &VendorProgram{Vendor: "vendor", QuoteEnabled: true, DiscountOptions: []DiscountOption{{Name: "quote price", DiscountPath: []DiscountPathItem{{Type: DiscountPathItemTypePercentage, Amount: 50}}}}, ProductOverrides: []ProductOverride{{ProductId: "fixed", NetPrice: 25, QuoteEnabled: true}}}
	want := []DiscountOption{{Name: "net price", NetPrice: 25, DiscountPath: []DiscountPathItem{}}, {Name: "quote price", DiscountPath: []DiscountPathItem{}}}
	if got := program.GetDiscountOptionsForProduct("fixed", ""); !reflect.DeepEqual(got, want) {
		t.Fatalf("net and quote options = %+v", got)
	}
	if got := program.GetDiscountOptionsForProduct("ordinary", ""); len(got) != 1 || got[0].Name != "quote price" || len(got[0].DiscountPath) != 0 {
		t.Fatalf("quote option collision: %+v", got)
	}
	past := time.Now().Add(-time.Hour)
	program.UpdateExpiry(&past)
	if got := program.GetDiscountOptionsForProduct("fixed", ""); len(got) != 0 {
		t.Fatalf("expired options: %+v", got)
	}
	if _, err := program.CalculateDealerPrice(Product{ProductId: "fixed", Vendor: "vendor", QuotePrice: 42}); !errors.Is(err, ErrVendorProgramExpired) {
		t.Fatalf("expired quote: %v", err)
	}
	program.UpdateExpiry(nil)
	for _, product := range []Product{{ProductId: "", Vendor: "vendor", QuotePrice: 42}, {ProductId: "a", Vendor: "", QuotePrice: 42}, {ProductId: "a", Vendor: "other", QuotePrice: 42}} {
		if _, err := program.CalculateDealerPrice(product); err == nil {
			t.Fatalf("quote bypassed identity validation: %+v", product)
		}
	}
	program.UpdatedAt = time.Unix(1, 0)
	program.UpdateQuoteEnabled(false)
	if program.QuoteEnabled || !program.UpdatedAt.After(time.Unix(1, 0)) {
		t.Fatal("quote setting or timestamp not updated")
	}
}
