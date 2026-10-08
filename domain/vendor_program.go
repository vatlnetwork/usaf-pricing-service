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
	Id                    string                 `json:"id" bson:"-"`
	Vendor                string                 `json:"vendor" bson:"vendor"` // name of the vendor
	DiscountOptions       []DiscountOption       `json:"discount_options" bson:"discount_options"`
	ProductOverrides      []ProductOverride      `json:"product_overrides" bson:"product_overrides"`
	ProductGroupOverrides []ProductGroupOverride `json:"product_group_overrides" bson:"product_group_overrides"`
	ExpiresAt             *time.Time             `json:"expires_at" bson:"expires_at,omitempty"`
	CreatedAt             time.Time              `json:"created_at" bson:"created_at"`
	UpdatedAt             time.Time              `json:"updated_at" bson:"updated_at"`
}

var ErrVendorProgramExpired = errors.New("vendor program has expired")

// IsExpired reports whether the expiry instant has been reached. A nil expiry
// preserves availability for programs created before expiry was supported.
func (v *VendorProgram) IsExpired(at time.Time) bool {
	return v.ExpiresAt != nil && !at.Before(*v.ExpiresAt)
}

// UpdateExpiry sets or clears the expiry, copying the input and using MongoDB's
// millisecond precision so availability is consistent before and after storage.
func (v *VendorProgram) UpdateExpiry(expiresAt *time.Time) {
	v.ExpiresAt = nil
	if expiresAt != nil {
		expiry := expiresAt.UTC().Truncate(time.Millisecond)
		v.ExpiresAt = &expiry
	}
	v.UpdatedAt = time.Now()
}

type DiscountOption struct {
	Name         string             `json:"name" bson:"name"`
	DiscountPath []DiscountPathItem `json:"discount_path" bson:"discount_path"`
	// NetPrice is populated for the effective option from a net price override.
	NetPrice float64 `json:"net_price,omitempty" bson:"net_price,omitempty"`
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
	NetPrice        float64          `json:"net_price,omitempty" bson:"net_price,omitempty"` // zero means no fixed price
	DiscountOptions []DiscountOption `json:"discount_options" bson:"discount_options"`
}

func (p ProductOverride) Validate() error {
	if strings.TrimSpace(p.ProductId) == "" {
		return errors.New("product id is required")
	}
	if err := validateNetPrice(p.NetPrice); err != nil {
		return err
	}
	if p.NetPrice > 0 {
		return nil
	}

	for _, o := range p.DiscountOptions {
		err := o.Validate()
		if err != nil {
			return err
		}
	}

	return validateDiscountOptionNames(p.DiscountOptions)
}

func validateNetPrice(price float64) error {
	if math.IsNaN(price) || math.IsInf(price, 0) || price < 0 {
		return errors.New("net price must be finite and nonnegative")
	}
	return nil
}

type ProductGroupOverride struct {
	GroupName       string           `json:"group_name" bson:"group_name"`
	DiscountOptions []DiscountOption `json:"discount_options" bson:"discount_options"`
}

func (p ProductGroupOverride) Validate() error {
	if strings.TrimSpace(p.GroupName) == "" {
		return errors.New("product group name is required")
	}

	for _, o := range p.DiscountOptions {
		err := o.Validate()
		if err != nil {
			return err
		}
	}

	return validateDiscountOptionNames(p.DiscountOptions)
}

func NewVendorProgram(
	vendor string,
	discountOptions []DiscountOption,
	productOverrides []ProductOverride,
	productGroupOverrides []ProductGroupOverride,
) (*VendorProgram, error) {
	if strings.TrimSpace(vendor) == "" {
		return nil, errors.New("vendor name is required")
	}

	for _, o := range discountOptions {
		err := o.Validate()
		if err != nil {
			return nil, err
		}
	}
	if err := validateDiscountOptionNames(discountOptions); err != nil {
		return nil, err
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
	}

	groupNames := make(map[string]struct{}, len(productGroupOverrides))
	for _, override := range productGroupOverrides {
		if err := override.Validate(); err != nil {
			return nil, err
		}
		if _, exists := groupNames[override.GroupName]; exists {
			return nil, fmt.Errorf("cannot have duplicate group names in product group overrides. found duplicate: %v", override.GroupName)
		}
		groupNames[override.GroupName] = struct{}{}
	}

	return &VendorProgram{
		Id:                    "", // id will be set by the database layer
		Vendor:                vendor,
		DiscountOptions:       cloneDiscountOptions(discountOptions),
		ProductOverrides:      cloneProductOverrides(productOverrides),
		ProductGroupOverrides: cloneProductGroupOverrides(productGroupOverrides),
		CreatedAt:             time.Now(),
		UpdatedAt:             time.Now(),
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

func (v *VendorProgram) UpsertProductGroupOverrides(productGroupOverrides []ProductGroupOverride) error {
	// Stage changes in a separate slice so failures leave the program unchanged.
	candidate := *v
	candidate.ProductGroupOverrides = slices.Clone(v.ProductGroupOverrides)

	for _, o := range productGroupOverrides {
		err := o.Validate()
		if err != nil {
			return err
		}

		o.DiscountOptions = cloneDiscountOptions(o.DiscountOptions)
		exists := false

		for i, existing := range candidate.ProductGroupOverrides {
			if existing.GroupName == o.GroupName {
				candidate.ProductGroupOverrides[i] = o
				exists = true
				break
			}
		}

		if !exists {
			candidate.ProductGroupOverrides = append(candidate.ProductGroupOverrides, o)
		}
	}

	err := candidate.validateDiscountOptionUniqueness()
	if err != nil {
		return err
	}

	v.ProductGroupOverrides = candidate.ProductGroupOverrides
	v.UpdatedAt = time.Now()

	return nil
}

func (v *VendorProgram) validateDiscountOptionUniqueness() error {
	if err := validateDiscountOptionNames(v.DiscountOptions); err != nil {
		return err
	}

	for _, o := range v.ProductOverrides {
		if o.NetPrice > 0 {
			continue
		}
		if err := validateDiscountOptionNames(o.DiscountOptions); err != nil {
			return err
		}
	}

	for _, o := range v.ProductGroupOverrides {
		if err := validateDiscountOptionNames(o.DiscountOptions); err != nil {
			return err
		}
	}

	return nil
}

func validateDiscountOptionNames(options []DiscountOption) error {
	names := make(map[string]struct{}, len(options))
	for _, option := range options {
		if _, exists := names[option.Name]; exists {
			return fmt.Errorf("cannot have duplicate discount option names. found duplicate: %v", option.Name)
		}
		names[option.Name] = struct{}{}
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

func (v *VendorProgram) RemoveProductGroupOverrides(groupNames []string) {
	newOverrides := []ProductGroupOverride{}

	for _, o := range v.ProductGroupOverrides {
		if slices.Contains(groupNames, o.GroupName) {
			continue
		} else {
			newOverrides = append(newOverrides, o)
		}
	}

	v.ProductGroupOverrides = newOverrides
	v.UpdatedAt = time.Now()
}

// GetDiscountOptionsForProduct returns independent copies of effective options
// in vendor, group, then product order. Higher-priority scopes replace entire
// options with matching names. An empty group name skips group overrides.
// A positive product net price replaces all options with one fixed-price option.
// Expired programs return no options.
func (v *VendorProgram) GetDiscountOptionsForProduct(productId, groupName string) []DiscountOption {
	if v.IsExpired(time.Now()) {
		return []DiscountOption{}
	}
	if netPrice := v.netPriceForProduct(productId); netPrice > 0 {
		return []DiscountOption{{Name: "net price", NetPrice: netPrice, DiscountPath: []DiscountPathItem{}}}
	}
	productOptions := []DiscountOption{}
	for _, override := range v.ProductOverrides {
		if override.ProductId == productId {
			productOptions = append(productOptions, override.DiscountOptions...)
		}
	}
	groupOptions := []DiscountOption{}
	if groupName != "" {
		for _, override := range v.ProductGroupOverrides {
			if override.GroupName == groupName {
				groupOptions = append(groupOptions, override.DiscountOptions...)
			}
		}
	}
	options := mergeDiscountOptions(v.DiscountOptions, groupOptions)
	return cloneDiscountOptions(mergeDiscountOptions(options, productOptions))
}

func (v *VendorProgram) netPriceForProduct(productId string) float64 {
	for _, override := range v.ProductOverrides {
		if override.ProductId == productId {
			return override.NetPrice
		}
	}
	return 0
}

// Preserve duplicates within a scope so pricing can still reject ambiguous
// stored data; only matching names from the lower-priority scope are removed.
func mergeDiscountOptions(base, overrides []DiscountOption) []DiscountOption {
	overriddenNames := make(map[string]struct{}, len(overrides))
	for _, option := range overrides {
		overriddenNames[option.Name] = struct{}{}
	}
	options := make([]DiscountOption, 0, len(base)+len(overrides))
	for _, option := range base {
		if _, overridden := overriddenNames[option.Name]; !overridden {
			options = append(options, option)
		}
	}
	return append(options, overrides...)
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

func cloneProductGroupOverrides(overrides []ProductGroupOverride) []ProductGroupOverride {
	cloned := slices.Clone(overrides)
	for i := range cloned {
		cloned[i].DiscountOptions = cloneDiscountOptions(cloned[i].DiscountOptions)
	}
	return cloned
}
