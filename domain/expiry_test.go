package domain

import (
	"errors"
	"testing"
	"time"
)

func TestVendorProgramExpiryBoundary(t *testing.T) {
	expiry := time.Date(2030, 6, 1, 12, 0, 0, 0, time.FixedZone("offset", -6*60*60))
	program := &VendorProgram{ExpiresAt: &expiry}
	for _, tt := range []struct {
		name string
		at   time.Time
		want bool
	}{
		{"before", expiry.Add(-time.Millisecond), false},
		{"at expiry", expiry, true},
		{"after", expiry.Add(time.Millisecond), true},
		{"same instant in UTC", expiry.UTC(), true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := program.IsExpired(tt.at); got != tt.want {
				t.Fatalf("IsExpired = %v, want %v", got, tt.want)
			}
		})
	}
	program.ExpiresAt = nil
	if program.IsExpired(expiry.AddDate(100, 0, 0)) {
		t.Fatal("unset expiry must remain available")
	}
}

func TestUpdateExpiryCopiesAndClears(t *testing.T) {
	program, err := NewVendorProgram("vendor", nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	createdAt := program.CreatedAt
	program.UpdatedAt = time.Unix(1, 0)
	expiry := time.Date(2030, 6, 1, 12, 0, 0, 123456789, time.FixedZone("offset", -6*60*60))
	want := expiry.UTC().Truncate(time.Millisecond)
	program.UpdateExpiry(&expiry)
	expiry = expiry.AddDate(-20, 0, 0)
	if program.ExpiresAt == nil || !program.ExpiresAt.Equal(want) {
		t.Fatalf("expiry = %v, want %v", program.ExpiresAt, want)
	}
	if !program.UpdatedAt.After(time.Unix(1, 0)) || !program.CreatedAt.Equal(createdAt) {
		t.Fatal("incorrect timestamps")
	}
	program.UpdateExpiry(nil)
	if program.ExpiresAt != nil {
		t.Fatal("expiry was not cleared")
	}
}

func TestExpiredProgramUnavailableInDomain(t *testing.T) {
	program, err := NewVendorProgram("vendor", []DiscountOption{testDiscountOption("base", 10)}, []ProductOverride{testOverride("a", "product")}, []ProductGroupOverride{testGroupOverride("group", 20)})
	if err != nil {
		t.Fatal(err)
	}
	past := time.Now().Add(-time.Hour)
	program.UpdateExpiry(&past)
	if options := program.GetDiscountOptionsForProduct("a", "group"); len(options) != 0 {
		t.Fatalf("expired options leaked: %+v", options)
	}
	for _, selected := range [][]string{nil, {"base"}, {"shared"}, {"product"}} {
		_, err := program.CalculateDealerPrice(Product{ProductId: "a", GroupName: "group", Vendor: "vendor", ListPrice: 100, DiscountOptions: selected})
		if !errors.Is(err, ErrVendorProgramExpired) {
			t.Fatalf("selection %v: error = %v", selected, err)
		}
	}
	future := time.Now().Add(time.Hour)
	for _, expiry := range []*time.Time{&future, nil} {
		program.UpdateExpiry(expiry)
		if options := program.GetDiscountOptionsForProduct("a", "group"); len(options) != 3 {
			t.Fatalf("active options = %+v", options)
		}
		price, err := program.CalculateDealerPrice(Product{ProductId: "a", Vendor: "vendor", ListPrice: 100, DiscountOptions: []string{"base"}})
		if err != nil || price != 90 {
			t.Fatalf("active price = %v, %v", price, err)
		}
	}
}
