package domain

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"
)

// ProductSelector matches IDs OR groups, then configuration and exclusions.
// Empty positive lists match all products. All matching is exact.
type ProductSelector struct {
	ProductIDs         []string `json:"product_ids,omitempty" bson:"product_ids,omitempty"`
	GroupNames         []string `json:"group_names,omitempty" bson:"group_names,omitempty"`
	Configurations     []string `json:"configurations,omitempty" bson:"configurations,omitempty"`
	ExcludedProductIDs []string `json:"excluded_product_ids,omitempty" bson:"excluded_product_ids,omitempty"`
	ExcludedGroupNames []string `json:"excluded_group_names,omitempty" bson:"excluded_group_names,omitempty"`
}

func (s ProductSelector) Matches(l OrderLine) bool {
	return (len(s.ProductIDs)+len(s.GroupNames) == 0 || slices.Contains(s.ProductIDs, l.ProductID) || slices.Contains(s.GroupNames, l.GroupName)) &&
		(len(s.Configurations) == 0 || slices.Contains(s.Configurations, l.Configuration)) && !slices.Contains(s.ExcludedProductIDs, l.ProductID) && !slices.Contains(s.ExcludedGroupNames, l.GroupName)
}

type ScenarioCondition struct {
	Field    string `json:"field" bson:"field"`
	Operator string `json:"operator" bson:"operator"`
	Value    string `json:"value" bson:"value"`
}
type PriceAdjustment struct {
	Type   string  `json:"type" bson:"type"` // percentage, dollar_amount, multiplier, surcharge
	Amount float64 `json:"amount" bson:"amount"`
}
type BundleComponent struct {
	ProductID     string `json:"product_id" bson:"product_id"`
	Configuration string `json:"configuration,omitempty" bson:"configuration,omitempty"`
	Quantity      int    `json:"quantity" bson:"quantity"`
}

type PricingScenario struct {
	ID                    string              `json:"id" bson:"id"`
	Name                  string              `json:"name" bson:"name"`
	Program               string              `json:"program" bson:"program"`               // regular or special
	Role                  string              `json:"role" bson:"role"`                     // price, adjustment, replacement
	Method                string              `json:"method" bson:"method"`                 // steps, fixed, quote, count, bundle, free, review
	StartingPrice         string              `json:"starting_price" bson:"starting_price"` // list, net, quote, rule
	BaseScenarioID        string              `json:"base_scenario_id,omitempty" bson:"base_scenario_id,omitempty"`
	PriceUnit             string              `json:"price_unit" bson:"price_unit"` // each, case, section, bundle
	Scope                 ProductSelector     `json:"scope" bson:"scope"`
	QualificationScope    *ProductSelector    `json:"qualification_scope,omitempty" bson:"qualification_scope,omitempty"`
	SubtotalBasis         string              `json:"subtotal_basis" bson:"subtotal_basis"` // list, net, quote
	StartsAt              *time.Time          `json:"starts_at,omitempty" bson:"starts_at,omitempty"`
	EndsAt                *time.Time          `json:"ends_at,omitempty" bson:"ends_at,omitempty"`
	Conditions            []ScenarioCondition `json:"conditions" bson:"conditions"`
	Adjustments           []PriceAdjustment   `json:"adjustments" bson:"adjustments"`
	FixedPrice            *float64            `json:"fixed_price,omitempty" bson:"fixed_price,omitempty"`
	PercentPerUnit        float64             `json:"percent_per_unit,omitempty" bson:"percent_per_unit,omitempty"`
	MaximumPercent        float64             `json:"maximum_percent,omitempty" bson:"maximum_percent,omitempty"`
	CountScope            *ProductSelector    `json:"count_scope,omitempty" bson:"count_scope,omitempty"`
	BundleComponents      []BundleComponent   `json:"bundle_components,omitempty" bson:"bundle_components,omitempty"`
	BuyQuantity           int                 `json:"buy_quantity,omitempty" bson:"buy_quantity,omitempty"`
	FreeQuantity          int                 `json:"free_quantity,omitempty" bson:"free_quantity,omitempty"`
	FreeValueLimit        *float64            `json:"free_value_limit,omitempty" bson:"free_value_limit,omitempty"`
	Combination           string              `json:"combination" bson:"combination"` // included_only, compatible, exclusive
	CompatibleScenarioIDs []string            `json:"compatible_scenario_ids,omitempty" bson:"compatible_scenario_ids,omitempty"`
	Priority              int                 `json:"priority" bson:"priority"` // larger wins
	Tier                  int                 `json:"tier" bson:"tier"`         // larger wins
	Approved              bool                `json:"approved" bson:"approved"`
	ApprovalEvidence      string              `json:"approval_evidence,omitempty" bson:"approval_evidence,omitempty"`
	Source                string              `json:"source,omitempty" bson:"source,omitempty"`
	ReviewNote            string              `json:"review_note,omitempty" bson:"review_note,omitempty"`
	PromoCode             string              `json:"promo_code,omitempty" bson:"promo_code,omitempty"`
	Instruction           string              `json:"instruction,omitempty" bson:"instruction,omitempty"`
}

// Order is a single vendor purchase order. Different configurations of the same
// product may occupy distinct lines. Quantity is measured in each line's unit.
type Order struct {
	At          *time.Time        `json:"at,omitempty"`
	Fulfillment string            `json:"fulfillment,omitempty"`
	Channel     string            `json:"channel,omitempty"`
	DealerClass string            `json:"dealer_class,omitempty"`
	AccountID   string            `json:"account_id,omitempty"`
	OrderUse    string            `json:"order_use,omitempty"`
	Shipment    string            `json:"shipment,omitempty"`
	Attributes  map[string]string `json:"attributes,omitempty"`
	Lines       []OrderLine       `json:"lines"`
	// When provided, every selected scenario must be used. Dependencies need not
	// be selected. This is also how a require_review policy is resolved explicitly.
	SelectedScenarioIDs []string `json:"selected_scenario_ids,omitempty"`
}
type OrderLine struct {
	LineID          string            `json:"line_id"`
	ProductID       string            `json:"product_id"`
	GroupName       string            `json:"group_name,omitempty"`
	Configuration   string            `json:"configuration,omitempty"`
	Quantity        int               `json:"quantity"`
	PriceUnit       string            `json:"price_unit"`
	ListPrice       *float64          `json:"list_price,omitempty"`
	NetPrice        *float64          `json:"net_price,omitempty"`
	QuotePrice      *float64          `json:"quote_price,omitempty"`
	QuoteApproved   bool              `json:"quote_approved,omitempty"`
	Attributes      map[string]string `json:"attributes,omitempty"`
	DiscountOptions []string          `json:"discount_options,omitempty"` // legacy programs only
}

func nonnegative(n float64) bool             { return !math.IsNaN(n) && !math.IsInf(n, 0) && n >= 0 && n <= 1e12 }
func oneOf(v string, allowed ...string) bool { return slices.Contains(allowed, v) }
func stringsValid(v []string) bool {
	seen := map[string]bool{}
	for _, s := range v {
		if strings.TrimSpace(s) == "" || seen[s] {
			return false
		}
		seen[s] = true
	}
	return true
}
func (s ProductSelector) validate() error {
	for _, a := range [][]string{s.ProductIDs, s.GroupNames, s.Configurations, s.ExcludedProductIDs, s.ExcludedGroupNames} {
		if !stringsValid(a) {
			return errors.New("selector values must be nonblank and unique")
		}
	}
	return nil
}
func numericCondition(field string) bool {
	return oneOf(field, "eligible_subtotal", "eligible_quantity", "qualifying_quantity", "order_subtotal", "line_quantity")
}
func (c ScenarioCondition) validate() error {
	if !oneOf(c.Operator, "=", "!=", ">", ">=", "<", "<=") {
		return errors.New("invalid condition operator")
	}
	if numericCondition(c.Field) {
		v, err := strconv.ParseFloat(c.Value, 64)
		if err != nil || !nonnegative(v) {
			return errors.New("numeric condition requires a finite nonnegative value")
		}
		return nil
	}
	if !oneOf(c.Field, "fulfillment", "channel", "dealer_class", "account_id", "order_use", "shipment", "configuration") && !(strings.HasPrefix(c.Field, "attribute.") && len(c.Field) > 10) && !(strings.HasPrefix(c.Field, "line_attribute.") && len(c.Field) > 15) {
		return fmt.Errorf("unsupported condition field %s", c.Field)
	}
	if !oneOf(c.Operator, "=", "!=") {
		return errors.New("text conditions support only = and !=")
	}
	return nil
}
func (s *PricingScenario) defaults() {
	if s.Program == "" {
		s.Program = "regular"
	}
	if s.Role == "" {
		s.Role = "price"
	}
	if s.Method == "" {
		s.Method = "steps"
	}
	if s.StartingPrice == "" {
		s.StartingPrice = "list"
	}
	if s.PriceUnit == "" {
		s.PriceUnit = "each"
	}
	if s.SubtotalBasis == "" {
		s.SubtotalBasis = "list"
	}
	if s.Combination == "" {
		s.Combination = "included_only"
	}
	for _, p := range []**time.Time{&s.StartsAt, &s.EndsAt} {
		if *p != nil {
			v := (*p).UTC().Truncate(time.Millisecond)
			*p = &v
		}
	}
}
func (s PricingScenario) validate() error {
	if strings.TrimSpace(s.ID) == "" || strings.TrimSpace(s.Name) == "" {
		return errors.New("scenario id and name are required")
	}
	if !oneOf(s.Program, "regular", "special") || !oneOf(s.Role, "price", "adjustment", "replacement") || !oneOf(s.Method, "steps", "fixed", "quote", "count", "bundle", "free", "review") || !oneOf(s.StartingPrice, "list", "net", "quote", "rule") || !oneOf(s.PriceUnit, "each", "case", "section", "bundle") || !oneOf(s.SubtotalBasis, "list", "net", "quote") || !oneOf(s.Combination, "included_only", "compatible", "exclusive") {
		return errors.New("invalid scenario program, role, method, starting price, price unit, subtotal basis, or combination")
	}
	if s.Priority < 0 || s.Tier < 0 || s.Priority > 1000000 || s.Tier > 1000000 {
		return errors.New("priority and tier must be between 0 and 1000000")
	}
	if s.StartsAt != nil && s.EndsAt != nil && !s.StartsAt.Before(*s.EndsAt) {
		return errors.New("scenario end must be after start")
	}
	if s.Role == "adjustment" && s.StartingPrice != "rule" {
		return errors.New("additional adjustments must reference a base scenario")
	}
	if (s.StartingPrice == "rule") != (s.BaseScenarioID != "") {
		return errors.New("rule starting price requires a base scenario ID; other starting prices cannot reference a rule")
	}
	if s.StartingPrice == "rule" && oneOf(s.Method, "fixed", "bundle", "quote", "review") {
		return errors.New("this method cannot use a base scenario")
	}
	for _, selector := range []*ProductSelector{&s.Scope, s.QualificationScope, s.CountScope} {
		if selector != nil {
			if err := selector.validate(); err != nil {
				return err
			}
		}
	}
	if !stringsValid(s.CompatibleScenarioIDs) {
		return errors.New("compatible scenario IDs must be nonblank and unique")
	}
	if s.Combination != "compatible" && len(s.CompatibleScenarioIDs) > 0 {
		return errors.New("compatible scenario IDs require compatible combination")
	}
	if len(s.Conditions) > 50 || len(s.Adjustments) > 50 {
		return errors.New("maximum 50 conditions and adjustments per scenario")
	}
	for _, c := range s.Conditions {
		if err := c.validate(); err != nil {
			return err
		}
	}
	for _, a := range s.Adjustments {
		if !oneOf(a.Type, "percentage", "dollar_amount", "multiplier", "surcharge") || !nonnegative(a.Amount) || (a.Type == "percentage" && a.Amount > 100) {
			return errors.New("invalid adjustment type or amount")
		}
	}
	for _, n := range []*float64{s.FixedPrice, s.FreeValueLimit} {
		if n != nil && !nonnegative(*n) {
			return errors.New("price must be finite, nonnegative, and at most 1 trillion")
		}
	}
	if oneOf(s.Method, "fixed", "bundle") && s.FixedPrice == nil {
		return errors.New("fixed and bundle methods require fixed_price")
	}
	if !oneOf(s.Method, "fixed", "bundle") && s.FixedPrice != nil {
		return errors.New("fixed_price is only supported for fixed and bundle methods")
	}
	if s.Method == "count" {
		if s.CountScope == nil || !nonnegative(s.PercentPerUnit) || s.PercentPerUnit > 100 || !nonnegative(s.MaximumPercent) || s.MaximumPercent > 100 {
			return errors.New("count method requires count_scope and percentages between 0 and 100")
		}
	} else if s.CountScope != nil || s.PercentPerUnit != 0 || s.MaximumPercent != 0 {
		return errors.New("count fields require count method")
	}
	if s.Method == "bundle" {
		if s.PriceUnit != "bundle" || len(s.BundleComponents) == 0 || len(s.BundleComponents) > 100 {
			return errors.New("bundle requires bundle price unit and 1–100 exact components")
		}
		seen := map[string]bool{}
		for _, c := range s.BundleComponents {
			key := c.ProductID + "\x00" + c.Configuration
			if strings.TrimSpace(c.ProductID) == "" || c.Quantity < 1 || c.Quantity > 1000000 || seen[key] {
				return errors.New("bundle components require unique product/configuration pairs and positive quantities")
			}
			seen[key] = true
		}
	} else if s.PriceUnit == "bundle" || len(s.BundleComponents) > 0 {
		return errors.New("bundle unit and components require bundle method")
	}
	if s.Method == "free" {
		if s.BuyQuantity < 1 || s.FreeQuantity < 1 || s.BuyQuantity+s.FreeQuantity > 1000000 {
			return errors.New("free method requires positive paid and free quantities, at most 1000000 total")
		}
	} else if s.BuyQuantity != 0 || s.FreeQuantity != 0 || s.FreeValueLimit != nil {
		return errors.New("free-goods fields require free method")
	}
	if oneOf(s.Method, "bundle", "free", "review") && len(s.Adjustments) > 0 {
		return errors.New("bundle, free, and review methods cannot contain adjustment steps; use a base rule for free goods")
	}
	return nil
}

// UpdateScenarios validates and copies the entire rule graph before mutation.
func (v *VendorProgram) UpdateScenarios(scenarios []PricingScenario, policy string) error {
	if policy == "" {
		policy = "lowest_price"
	}
	if !oneOf(policy, "lowest_price", "highest_tier", "priority", "require_review") {
		return errors.New("invalid selection policy")
	}
	if len(scenarios) > 2000 {
		return errors.New("maximum 2000 scenarios per program")
	}
	data, err := json.Marshal(scenarios)
	if err != nil {
		return err
	}
	var copy []PricingScenario
	if err = json.Unmarshal(data, &copy); err != nil {
		return err
	}
	byID := map[string]*PricingScenario{}
	for i := range copy {
		s := &copy[i]
		s.defaults()
		if err = s.validate(); err != nil {
			return fmt.Errorf("scenario %q: %w", s.ID, err)
		}
		if byID[s.ID] != nil {
			return fmt.Errorf("duplicate scenario id %s", s.ID)
		}
		byID[s.ID] = s
	}
	for _, s := range copy {
		for _, id := range s.CompatibleScenarioIDs {
			if byID[id] == nil || id == s.ID {
				return fmt.Errorf("scenario %s has unknown or self compatibility reference %s", s.ID, id)
			}
		}
		visited := map[string]bool{s.ID: true}
		current := s
		for current.BaseScenarioID != "" {
			base := byID[current.BaseScenarioID]
			if base == nil {
				return fmt.Errorf("scenario %s has unknown base %s", current.ID, current.BaseScenarioID)
			}
			if visited[base.ID] {
				return errors.New("scenario base references form a cycle")
			}
			visited[base.ID] = true
			if len(visited) > 16 {
				return errors.New("maximum 16 base rules in a chain")
			}
			if oneOf(base.Method, "bundle", "free", "review") || base.PriceUnit != current.PriceUnit {
				return errors.New("base rules must produce a matching unit price")
			}
			// Both sides explicitly authorize the combination. Every ancestor also
			// authorizes the child, preventing an indirect bypass of exclusivity.
			if base.Combination != "compatible" || !slices.Contains(base.CompatibleScenarioIDs, s.ID) {
				return fmt.Errorf("base scenario %s does not permit combination with %s", base.ID, s.ID)
			}
			current = *base
		}
	}
	v.Scenarios = copy
	v.SelectionPolicy = policy
	v.UpdatedAt = time.Now()
	return nil
}
func (o Order) Validate() error {
	if len(o.Lines) == 0 || len(o.Lines) > 100 {
		return errors.New("order requires 1–100 lines")
	}
	ids := map[string]bool{}
	for _, l := range o.Lines {
		if strings.TrimSpace(l.LineID) == "" || strings.TrimSpace(l.ProductID) == "" || ids[l.LineID] {
			return errors.New("line IDs must be nonblank and unique; product IDs are required")
		}
		ids[l.LineID] = true
		if l.Quantity < 1 || l.Quantity > 1000000 {
			return errors.New("line quantity must be between 1 and 1000000")
		}
		if !oneOf(l.PriceUnit, "each", "case", "section") {
			return errors.New("line price_unit must be each, case, or section")
		}
		for _, p := range []*float64{l.ListPrice, l.NetPrice, l.QuotePrice} {
			if p != nil && !nonnegative(*p) {
				return errors.New("line prices must be finite, nonnegative, and at most 1 trillion")
			}
		}
	}
	if !stringsValid(o.SelectedScenarioIDs) {
		return errors.New("selected scenario IDs must be nonblank and unique")
	}
	return nil
}
