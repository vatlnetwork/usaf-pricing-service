package domain

import (
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"
)

type VendorProgram struct {
	Id               string            `json:"id" bson:"-"`
	Vendor           string            `json:"vendor" bson:"vendor"` // name of the vendor
	DiscountOptions  []DiscountOption  `json:"discount_options" bson:"discount_options"`
	ProductOverrides []ProductOverride `json:"product_overrides" bson:"product_overrides"`
	CreatedAt        time.Time         `json:"created_at" bson:"created_at"`
	UpdatedAt        time.Time         `json:"updated_at" bson:"updated_at"`
}

type DiscountOption struct {
	Name         string             `json:"name" bson:"name"`
	DiscountPath []DiscountPathItem `json:"discount_path" bson:"discount_path"`
}

func (d DiscountOption) Validate() error {
	if strings.TrimSpace(d.Name) == "" {
		return errors.New("name is required")
	}

	for _, d := range d.DiscountPath {
		err := d.Validate()
		if err != nil {
			return err
		}
	}

	return nil
}

type DiscountPathItem struct {
	Type   DiscountPathItemType `json:"type" bson:"type"`
	Amount float64              `json:"amount" bson:"amount"`
}

func (d DiscountPathItem) Validate() error {
	err := d.Type.Validate()
	if err != nil {
		return err
	}

	if math.IsNaN(d.Amount) || math.IsInf(d.Amount, 0) {
		return errors.New("discount amount must be finite")
	}

	if d.Type == DiscountPathItemTypePercentage {
		if d.Amount < 0 || d.Amount > 100 {
			return errors.New("discount amount must be between 0 and 100 inclusive")
		}
	}

	if d.Type == DiscountPathItemTypeDollarAmount {
		if d.Amount < 0 {
			return errors.New("discount amount must be greater than or equal to zero")
		}
	}

	return nil
}

type DiscountPathItemType string

const (
	DiscountPathItemTypePercentage   DiscountPathItemType = "percentage"
	DiscountPathItemTypeDollarAmount DiscountPathItemType = "dollar_amount"
)

func (d DiscountPathItemType) Validate() error {
	switch d {
	case DiscountPathItemTypePercentage,
		DiscountPathItemTypeDollarAmount:
		// ok
		return nil
	default:
		return errors.New("invalid discount path item type")
	}
}

type ProductOverride struct {
	ProductId       string           `json:"product_id" bson:"product_id"`
	DiscountOptions []DiscountOption `json:"discount_options" bson:"discount_options"`
}

func (p ProductOverride) Validate() error {
	if strings.TrimSpace(p.ProductId) == "" {
		return errors.New("product id is required")
	}

	for _, o := range p.DiscountOptions {
		err := o.Validate()
		if err != nil {
			return err
		}
	}

	return nil
}

func NewVendorProgram(
	vendor string,
	discountOptions []DiscountOption,
	productOverrides []ProductOverride,
) (*VendorProgram, error) {
	if strings.TrimSpace(vendor) == "" {
		return nil, errors.New("vendor name is required")
	}

	optionNames := []string{}

	for _, o := range discountOptions {
		err := o.Validate()
		if err != nil {
			return nil, err
		}
		if slices.Contains(optionNames, o.Name) {
			return nil, fmt.Errorf("cannot have duplicate discount option names. found duplicate: %v", o.Name)
		}
		optionNames = append(optionNames, o.Name)
	}

	productIds := []string{}

	for _, o := range productOverrides {
		err := o.Validate()
		if err != nil {
			return nil, err
		}
		if slices.Contains(productIds, o.ProductId) {
			return nil, fmt.Errorf("cannot have duplicate product ids in product overrides. found duplicate: %v", o.ProductId)
		}
		productIds = append(productIds, o.ProductId)
		for _, opt := range o.DiscountOptions {
			if slices.Contains(optionNames, opt.Name) {
				return nil, fmt.Errorf("cannot have duplicate discount option names. found duplicate: %v", opt.Name)
			}
			optionNames = append(optionNames, opt.Name)
		}
	}

	return &VendorProgram{
		Id:               "", // id will be set by the database layer
		Vendor:           vendor,
		DiscountOptions:  cloneDiscountOptions(discountOptions),
		ProductOverrides: cloneProductOverrides(productOverrides),
		CreatedAt:        time.Now(),
		UpdatedAt:        time.Now(),
	}, nil
}

func (v *VendorProgram) UpdateDiscountOptions(discountOptions []DiscountOption) error {
	for _, opt := range discountOptions {
		err := opt.Validate()
		if err != nil {
			return err
		}
	}

	candidate := *v
	candidate.DiscountOptions = cloneDiscountOptions(discountOptions)

	err := candidate.validateDiscountOptionUniqueness()
	if err != nil {
		return err
	}

	v.DiscountOptions = candidate.DiscountOptions
	v.UpdatedAt = time.Now()

	return nil
}

func (v *VendorProgram) UpsertProductOverrides(productOverrides []ProductOverride) error {
	// Stage changes in a separate slice so failures leave the program unchanged.
	candidate := *v
	candidate.ProductOverrides = slices.Clone(v.ProductOverrides)

	for _, o := range productOverrides {
		err := o.Validate()
		if err != nil {
			return err
		}

		o.DiscountOptions = cloneDiscountOptions(o.DiscountOptions)
		exists := false

		for i, existing := range candidate.ProductOverrides {
			if existing.ProductId == o.ProductId {
				candidate.ProductOverrides[i] = o
				exists = true
				break
			}
		}

		if !exists {
			candidate.ProductOverrides = append(candidate.ProductOverrides, o)
		}
	}

	err := candidate.validateDiscountOptionUniqueness()
	if err != nil {
		return err
	}

	v.ProductOverrides = candidate.ProductOverrides
	v.UpdatedAt = time.Now()

	return nil
}

func (v *VendorProgram) validateDiscountOptionUniqueness() error {
	optionNames := []string{}

	for _, opt := range v.DiscountOptions {
		if slices.Contains(optionNames, opt.Name) {
			return fmt.Errorf("cannot have duplicate discount option names. found duplicate: %v", opt.Name)
		}
		optionNames = append(optionNames, opt.Name)
	}

	for _, o := range v.ProductOverrides {
		for _, opt := range o.DiscountOptions {
			if slices.Contains(optionNames, opt.Name) {
				return fmt.Errorf("cannot have duplicate discount option name. found duplicate: %v", opt.Name)
			}
			optionNames = append(optionNames, opt.Name)
		}
	}

	return nil
}

func (v *VendorProgram) RemoveProductOverrides(productIds []string) {
	newOverrides := []ProductOverride{}

	for _, o := range v.ProductOverrides {
		if slices.Contains(productIds, o.ProductId) {
			continue
		} else {
			newOverrides = append(newOverrides, o)
		}
	}

	v.ProductOverrides = newOverrides
	v.UpdatedAt = time.Now()
}

// GetDiscountOptionsForProduct returns independent copies of all vendor options,
// followed by any additional options for the specified product.
func (v *VendorProgram) GetDiscountOptionsForProduct(productId string) []DiscountOption {
	options := []DiscountOption{}

	options = append(options, v.DiscountOptions...)

	for _, override := range v.ProductOverrides {
		if override.ProductId == productId {
			options = append(options, override.DiscountOptions...)
		}
	}

	return cloneDiscountOptions(options)
}

func cloneDiscountOptions(options []DiscountOption) []DiscountOption {
	cloned := slices.Clone(options)
	for i := range cloned {
		cloned[i].DiscountPath = slices.Clone(cloned[i].DiscountPath)
	}
	return cloned
}

func cloneProductOverrides(overrides []ProductOverride) []ProductOverride {
	cloned := slices.Clone(overrides)
	for i := range cloned {
		cloned[i].DiscountOptions = cloneDiscountOptions(cloned[i].DiscountOptions)
	}
	return cloned
}
