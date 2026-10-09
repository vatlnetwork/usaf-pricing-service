package domain

import "testing"

func TestVendorCodePricingIdentity(t *testing.T) {
	p := &VendorProgram{Vendor: "Canonical name", VendorCode: "001"}
	for _, tt := range []struct {
		name, code string
		wantError  bool
	}{
		{"", "001", false},
		{"Different name", "001", false},
		{"Canonical name", "", false},
		{"Different name", "", true},
		{"Canonical name", "002", true},
		{"Canonical name", "  ", true},
		{"Canonical name", "001 ", true},
	} {
		price, err := p.CalculateDealerPrice(Product{ProductId: "p", Vendor: tt.name, VendorCode: tt.code, ListPrice: 100})
		if (err != nil) != tt.wantError || (err == nil && price != 100) {
			t.Errorf("name %q code %q: price=%v err=%v", tt.name, tt.code, price, err)
		}
	}
}

func TestUpdateVendorCode(t *testing.T) {
	p := &VendorProgram{VendorCode: "original"}
	if err := p.UpdateVendorCode(" \t"); err == nil || p.VendorCode != "original" {
		t.Fatal("invalid code must leave the original code intact")
	}
	for _, code := range []string{"001", "AbC", ""} {
		if err := p.UpdateVendorCode(code); err != nil || p.VendorCode != code {
			t.Fatalf("code %q: %v", code, err)
		}
	}
}
