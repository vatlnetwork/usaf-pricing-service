package domain

import (
	"reflect"
	"testing"
	"time"
)

func testGroupOverride(name string, amount float64) ProductGroupOverride {
	return ProductGroupOverride{GroupName: name, DiscountOptions: []DiscountOption{testDiscountOption("shared", amount)}}
}

func TestNewVendorProgramGroupValidation(t *testing.T) {
	for _, tt := range []struct {
		name      string
		groups    []ProductGroupOverride
		wantError bool
	}{
		{"shared option names across scopes", []ProductGroupOverride{testGroupOverride("A", 20), testGroupOverride("B", 30)}, false},
		{"duplicate groups", []ProductGroupOverride{testGroupOverride("A", 20), testGroupOverride("A", 30)}, true},
		{"blank group", []ProductGroupOverride{testGroupOverride(" ", 20)}, true},
		{"invalid option", []ProductGroupOverride{testGroupOverride("A", -1)}, true},
		{"duplicate options within group", []ProductGroupOverride{{GroupName: "A", DiscountOptions: []DiscountOption{testDiscountOption("shared", 20), testDiscountOption("shared", 30)}}}, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewVendorProgram("vendor", []DiscountOption{testDiscountOption("shared", 10)}, []ProductOverride{testOverride("a", "shared")}, tt.groups)
			if (err != nil) != tt.wantError {
				t.Fatalf("error = %v, want error = %v", err, tt.wantError)
			}
		})
	}
}

func TestGroupOverrideMutations(t *testing.T) {
	groups := []ProductGroupOverride{testGroupOverride("A", 20), testGroupOverride("C", 40)}
	v, err := NewVendorProgram("vendor", []DiscountOption{testDiscountOption("shared", 10)}, []ProductOverride{testOverride("a", "shared")}, groups)
	if err != nil {
		t.Fatal(err)
	}
	groups[0].GroupName = "changed"
	groups[0].DiscountOptions[0].Name = "changed"
	groups[0].DiscountOptions[0].DiscountPath[0].Amount = -1
	if !reflect.DeepEqual(v.ProductGroupOverrides, []ProductGroupOverride{testGroupOverride("A", 20), testGroupOverride("C", 40)}) {
		t.Fatalf("constructor retained input: %+v", v.ProductGroupOverrides)
	}
	v.UpdatedAt = time.Unix(1, 0)
	createdAt := v.CreatedAt
	updates := []ProductGroupOverride{testGroupOverride("A", 25), testGroupOverride("B", 30)}
	if err := v.UpsertProductGroupOverrides(updates); err != nil {
		t.Fatal(err)
	}
	for i := range updates {
		updates[i].GroupName = "changed"
		updates[i].DiscountOptions[0].Name = "changed"
		updates[i].DiscountOptions[0].DiscountPath[0].Amount = -1
	}
	want := []ProductGroupOverride{testGroupOverride("A", 25), testGroupOverride("C", 40), testGroupOverride("B", 30)}
	if !reflect.DeepEqual(v.ProductGroupOverrides, want) {
		t.Fatalf("upsert result = %+v", v.ProductGroupOverrides)
	}
	if !v.UpdatedAt.After(time.Unix(1, 0)) || !v.CreatedAt.Equal(createdAt) {
		t.Fatal("incorrect timestamps")
	}

	for _, updates := range [][]ProductGroupOverride{
		{testGroupOverride("A", 50), testGroupOverride(" ", 20)},
		{testGroupOverride("D", 50), testGroupOverride("B", -1)},
		{testGroupOverride("A", 50), {GroupName: "B", DiscountOptions: []DiscountOption{{Name: "shared"}, {Name: "shared"}}}},
	} {
		before := *v
		before.ProductGroupOverrides = cloneProductGroupOverrides(v.ProductGroupOverrides)
		if err := v.UpsertProductGroupOverrides(updates); err == nil {
			t.Fatal("expected validation error")
		}
		if !reflect.DeepEqual(*v, before) {
			t.Fatalf("failed upsert changed program: %+v", v)
		}
	}
	if err := v.UpdateDiscountOptions([]DiscountOption{testDiscountOption("shared", 15)}); err != nil {
		t.Fatal(err)
	}
	if err := v.UpsertProductOverrides([]ProductOverride{testOverride("a", "shared")}); err != nil {
		t.Fatal(err)
	}
	if err := v.UpsertProductGroupOverrides(nil); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(v.ProductGroupOverrides, want) {
		t.Fatal("empty upsert changed groups")
	}
	if err := v.UpsertProductGroupOverrides([]ProductGroupOverride{{GroupName: "A"}}); err != nil {
		t.Fatal(err)
	}
	if len(v.ProductGroupOverrides[0].DiscountOptions) != 0 {
		t.Fatal("replacement did not clear options")
	}
	v.UpdatedAt = time.Unix(1, 0)
	v.RemoveProductGroupOverrides([]string{"A", "unknown"})
	if !reflect.DeepEqual(v.ProductGroupOverrides, want[1:]) {
		t.Fatalf("incorrect removal: %+v", v.ProductGroupOverrides)
	}
	if !v.UpdatedAt.After(time.Unix(1, 0)) || !v.CreatedAt.Equal(createdAt) {
		t.Fatal("incorrect removal timestamps")
	}
	if len(v.ProductOverrides) != 1 || v.DiscountOptions[0].DiscountPath[0].Amount != 15 {
		t.Fatal("group removal affected other scopes")
	}
}

func TestProductGroupDiscountPrecedence(t *testing.T) {
	v, err := NewVendorProgram("vendor", []DiscountOption{testDiscountOption("shared", 10), testDiscountOption("vendor-only", 5)}, []ProductOverride{
		{ProductId: "special", DiscountOptions: []DiscountOption{testDiscountOption("shared", 40), testDiscountOption("group-only", 50)}},
		{ProductId: "disabled", DiscountOptions: []DiscountOption{{Name: "shared"}}},
	}, []ProductGroupOverride{
		{GroupName: "Group A", DiscountOptions: []DiscountOption{testDiscountOption("shared", 20), testDiscountOption("group-only", 30)}},
		testGroupOverride("Group B", 60),
		{GroupName: "Disabled", DiscountOptions: []DiscountOption{{Name: "shared"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		name, product, group string
		want                 []DiscountOption
		price                float64
	}{
		{"product over group and vendor", "special", "Group A", []DiscountOption{testDiscountOption("vendor-only", 5), testDiscountOption("shared", 40), testDiscountOption("group-only", 50)}, 60},
		{"group over vendor", "ordinary", "Group A", []DiscountOption{testDiscountOption("vendor-only", 5), testDiscountOption("shared", 20), testDiscountOption("group-only", 30)}, 80},
		{"separate group", "ordinary", "Group B", []DiscountOption{testDiscountOption("vendor-only", 5), testDiscountOption("shared", 60)}, 40},
		{"missing group", "ordinary", "unknown", v.DiscountOptions, 90},
		{"no group", "ordinary", "", v.DiscountOptions, 90},
		{"exact group match", "ordinary", "group a", v.DiscountOptions, 90},
		{"whitespace matches exactly", "ordinary", "Group A ", v.DiscountOptions, 90},
		{"empty group path", "ordinary", "Disabled", []DiscountOption{testDiscountOption("vendor-only", 5), {Name: "shared"}}, 100},
		{"empty product path", "disabled", "Group B", []DiscountOption{testDiscountOption("vendor-only", 5), {Name: "shared"}}, 100},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := v.GetDiscountOptionsForProduct(tt.product, tt.group)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("got %+v, want %+v", got, tt.want)
			}
			price, err := v.CalculateDealerPrice(Product{ProductId: tt.product, GroupName: tt.group, Vendor: "vendor", ListPrice: 100, DiscountOptions: []string{"shared"}})
			if err != nil || price != tt.price {
				t.Fatalf("price = %v, %v; want %v", price, err, tt.price)
			}
			for i := range got {
				got[i].Name = "changed"
				if len(got[i].DiscountPath) > 0 {
					got[i].DiscountPath[0].Amount = -1
				}
			}
			if got := v.GetDiscountOptionsForProduct(tt.product, tt.group); !reflect.DeepEqual(got, tt.want) {
				t.Fatal("lookup retained mutable references")
			}
		})
	}
	v.RemoveProductOverrides([]string{"special"})
	if price, err := v.CalculateDealerPrice(Product{ProductId: "special", GroupName: "Group A", Vendor: "vendor", ListPrice: 100, DiscountOptions: []string{"shared"}}); err != nil || price != 80 {
		t.Fatalf("group fallback: %v, %v", price, err)
	}
	v.RemoveProductGroupOverrides([]string{"Group A"})
	if price, err := v.CalculateDealerPrice(Product{ProductId: "special", GroupName: "Group A", Vendor: "vendor", ListPrice: 100, DiscountOptions: []string{"shared"}}); err != nil || price != 90 {
		t.Fatalf("vendor fallback: %v, %v", price, err)
	}
}
