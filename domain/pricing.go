package domain

import (
	"errors"
	"fmt"
	"math"
	"math/big"
	"strconv"
	"strings"
)

// CalculateDealerPrice applies selected options in product order and each
// option's path in path order. All selected options must exist, even if an
// earlier discount would reduce the price to zero. Only the final price is
// rounded, to the nearest cent (half cents round up).
func (v *VendorProgram) CalculateDealerPrice(product Product) (float64, error) {
	if strings.TrimSpace(product.ProductId) == "" {
		return 0, errors.New("product id is required")
	}
	if strings.TrimSpace(product.Vendor) == "" {
		return 0, errors.New("vendor is required")
	}
	if product.Vendor != v.Vendor {
		return 0, errors.New("product vendor does not match vendor program")
	}
	if math.IsNaN(product.ListPrice) || math.IsInf(product.ListPrice, 0) || product.ListPrice < 0 {
		return 0, errors.New("list price must be finite and nonnegative")
	}
	available := make(map[string]DiscountOption)
	for _, option := range v.GetDiscountOptionsForProduct(product.ProductId) {
		if _, exists := available[option.Name]; exists {
			return 0, fmt.Errorf("discount option %s is ambiguous", option.Name)
		}
		available[option.Name] = option
	}
	selected := make([]DiscountOption, 0, len(product.DiscountOptions))
	for _, name := range product.DiscountOptions {
		option, exists := available[name]
		if !exists {
			return 0, fmt.Errorf("discount option %s is missing", name)
		}
		if err := option.Validate(); err != nil {
			return 0, fmt.Errorf("discount option %s is invalid: %w", name, err)
		}
		selected = append(selected, option)
	}

	// Decimal rational arithmetic avoids binary-float drift at half-cent boundaries.
	price := decimalAmount(product.ListPrice)
	hundred := big.NewRat(100, 1)
	for _, option := range selected {
		for _, item := range option.DiscountPath {
			if price.Sign() <= 0 {
				return 0, nil
			}
			amount := decimalAmount(item.Amount)
			switch item.Type {
			case DiscountPathItemTypePercentage:
				factor := new(big.Rat).Sub(hundred, amount)
				factor.Quo(factor, hundred)
				price.Mul(price, factor)
			case DiscountPathItemTypeDollarAmount:
				price.Sub(price, amount)
			}
		}
	}
	if price.Sign() <= 0 {
		return 0, nil
	}
	return strconv.ParseFloat(price.FloatString(2), 64)
}

// Call only with finite values. FormatFloat recovers their decimal representation.
func decimalAmount(value float64) *big.Rat {
	amount, _ := new(big.Rat).SetString(strconv.FormatFloat(value, 'f', -1, 64))
	return amount
}
