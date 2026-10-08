package domain

import (
	"errors"
	"fmt"
	"math/big"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
)

type ScenarioEvaluation struct {
	ScenarioID      string            `json:"scenario_id"`
	Name            string            `json:"name"`
	EligibleLineIDs []string          `json:"eligible_line_ids"`
	Rejections      map[string]string `json:"rejections"`
}
type PricedOrderLine struct {
	LineID      string   `json:"line_id"`
	Quantity    int      `json:"quantity"`
	PriceUnit   string   `json:"price_unit"`
	UnitPrice   float64  `json:"unit_price"` // average for allocated bundles/free goods
	Total       float64  `json:"total"`
	ScenarioIDs []string `json:"scenario_ids"`
	Explanation []string `json:"explanation"`
}
type OrderPriceResult struct {
	Available    bool                 `json:"available"`
	Total        *float64             `json:"total"`
	Lines        []PricedOrderLine    `json:"lines"`
	Reasons      []string             `json:"reasons"`
	Evaluations  []ScenarioEvaluation `json:"evaluations"`
	Instructions []string             `json:"instructions"`
	Sources      []string             `json:"sources"`
}
type unitCalculation struct {
	price      *big.Rat
	ids, trace []string
	err        error
}
type orderEvaluator struct {
	program *VendorProgram
	order   Order
	at      time.Time
	rules   map[string]PricingScenario
	cache   map[string]unitCalculation
}

func ratCopy(p *big.Rat) *big.Rat { return new(big.Rat).Set(p) }
func ratInt(n int) *big.Rat       { return big.NewRat(int64(n), 1) }
func money(p *big.Rat) float64    { v, _ := strconv.ParseFloat(p.FloatString(2), 64); return v }
func appendUnique(a []string, values ...string) []string {
	for _, s := range values {
		if s != "" && !slices.Contains(a, s) {
			a = append(a, s)
		}
	}
	return a
}
func lineBasis(l OrderLine, basis string) (*big.Rat, error) {
	var p *float64
	switch basis {
	case "list":
		p = l.ListPrice
	case "net":
		p = l.NetPrice
	case "quote":
		p = l.QuotePrice
		if !l.QuoteApproved {
			return nil, errors.New("quote has not been approved")
		}
	}
	if p == nil {
		return nil, fmt.Errorf("%s price is required for line %s", basis, l.LineID)
	}
	return decimalAmount(*p), nil
}
func (e *orderEvaluator) quantity(selector ProductSelector) int {
	n := 0
	for _, l := range e.order.Lines {
		if selector.Matches(l) {
			n += l.Quantity
		}
	}
	return n
}
func (e *orderEvaluator) subtotal(selector ProductSelector, basis string) (*big.Rat, error) {
	total := new(big.Rat)
	for _, l := range e.order.Lines {
		if selector.Matches(l) {
			p, err := lineBasis(l, basis)
			if err != nil {
				return nil, err
			}
			total.Add(total, new(big.Rat).Mul(p, ratInt(l.Quantity)))
		}
	}
	return total, nil
}
func (e *orderEvaluator) condition(s PricingScenario, c ScenarioCondition, l OrderLine) error {
	selector := s.Scope
	if s.QualificationScope != nil {
		selector = *s.QualificationScope
	}
	var actual string
	var number *big.Rat
	var err error
	present := true
	switch c.Field {
	case "eligible_quantity":
		number = ratInt(e.quantity(selector))
	case "qualifying_quantity":
		if s.CountScope == nil {
			return errors.New("qualifying quantity requires a count scope")
		}
		number = ratInt(e.quantity(*s.CountScope))
	case "line_quantity":
		number = ratInt(l.Quantity)
	case "eligible_subtotal":
		number, err = e.subtotal(selector, s.SubtotalBasis)
	case "order_subtotal":
		number, err = e.subtotal(ProductSelector{}, s.SubtotalBasis)
	case "fulfillment":
		actual = e.order.Fulfillment
	case "channel":
		actual = e.order.Channel
	case "dealer_class":
		actual = e.order.DealerClass
	case "account_id":
		actual = e.order.AccountID
	case "order_use":
		actual = e.order.OrderUse
	case "shipment":
		actual = e.order.Shipment
	case "configuration":
		actual = l.Configuration
	default:
		if strings.HasPrefix(c.Field, "attribute.") {
			actual, present = e.order.Attributes[strings.TrimPrefix(c.Field, "attribute.")]
		}
		if strings.HasPrefix(c.Field, "line_attribute.") {
			actual, present = l.Attributes[strings.TrimPrefix(c.Field, "line_attribute.")]
		}
	}
	if err != nil {
		return err
	}
	if !present || (number == nil && actual == "") {
		return fmt.Errorf("%s is required", c.Field)
	}
	compare := strings.Compare(actual, c.Value)
	if number != nil {
		value, parseErr := strconv.ParseFloat(c.Value, 64)
		if parseErr != nil || !nonnegative(value) {
			return errors.New("invalid numeric condition")
		}
		compare = number.Cmp(decimalAmount(value))
	}
	ok := false
	switch c.Operator {
	case "=":
		ok = compare == 0
	case "!=":
		ok = compare != 0
	case ">":
		ok = compare > 0
	case ">=":
		ok = compare >= 0
	case "<":
		ok = compare < 0
	case "<=":
		ok = compare <= 0
	}
	if !ok {
		return fmt.Errorf("requires %s %s %s", c.Field, c.Operator, c.Value)
	}
	return nil
}
func (e *orderEvaluator) eligibility(s PricingScenario, l OrderLine) error {
	if !s.Approved {
		return fmt.Errorf("scenario needs approval: %s", s.ReviewNote)
	}
	if s.Method == "review" {
		return fmt.Errorf("manual pricing review required: %s", s.ReviewNote)
	}
	if s.StartsAt != nil && e.at.Before(*s.StartsAt) {
		return errors.New("scenario has not started")
	}
	if s.EndsAt != nil && !e.at.Before(*s.EndsAt) {
		return errors.New("scenario has expired")
	}
	if !s.Scope.Matches(l) {
		return errors.New("product or configuration is outside scenario scope")
	}
	if s.Method != "bundle" && s.PriceUnit != l.PriceUnit {
		return errors.New("price unit does not match scenario")
	}
	for _, c := range s.Conditions {
		if err := e.condition(s, c, l); err != nil {
			return err
		}
	}
	return nil
}
func (e *orderEvaluator) unit(s PricingScenario, i int) unitCalculation {
	key := s.ID + "\x00" + strconv.Itoa(i)
	if cached, ok := e.cache[key]; ok {
		return cached
	}
	result := e.calculateUnit(s, i)
	e.cache[key] = result
	return result
}
func (e *orderEvaluator) calculateUnit(s PricingScenario, i int) unitCalculation {
	l := e.order.Lines[i]
	out := unitCalculation{}
	if err := e.eligibility(s, l); err != nil {
		out.err = err
		return out
	}
	var price *big.Rat
	var err error
	switch {
	case s.Method == "fixed":
		price = decimalAmount(*s.FixedPrice)
		out.trace = append(out.trace, fmt.Sprintf("%s: fixed %s price $%s", s.Name, s.PriceUnit, price.FloatString(2)))
	case s.Method == "quote":
		price, err = lineBasis(l, "quote")
	case s.StartingPrice == "rule":
		base := e.unit(e.rules[s.BaseScenarioID], i)
		if base.err != nil {
			out.err = fmt.Errorf("base %s: %w", s.BaseScenarioID, base.err)
			return out
		}
		price = ratCopy(base.price)
		out.ids = append(out.ids, base.ids...)
		out.trace = append(out.trace, base.trace...)
	default:
		price, err = lineBasis(l, s.StartingPrice)
	}
	if err != nil {
		out.err = err
		return out
	}
	if price == nil {
		out.err = errors.New("scenario does not produce a unit price")
		return out
	}
	out.ids = append(out.ids, s.ID)
	if len(out.trace) == 0 {
		out.trace = append(out.trace, fmt.Sprintf("%s: starting price $%s", s.Name, price.FloatString(2)))
	}
	for _, a := range s.Adjustments {
		amount := decimalAmount(a.Amount)
		switch a.Type {
		case "percentage":
			price.Mul(price, new(big.Rat).Quo(new(big.Rat).Sub(ratInt(100), amount), ratInt(100)))
		case "dollar_amount":
			price.Sub(price, amount)
		case "multiplier":
			price.Mul(price, amount)
		case "surcharge":
			price.Add(price, amount)
		}
		if price.Sign() < 0 {
			price.SetInt64(0)
		}
		if price.Cmp(ratInt(1000000000000)) > 0 {
			out.err = errors.New("calculated price exceeds supported maximum")
			return out
		}
		out.trace = append(out.trace, fmt.Sprintf("%s: %s %g → $%s", s.Name, a.Type, a.Amount, price.FloatString(2)))
	}
	if s.Method == "count" {
		count := e.quantity(*s.CountScope)
		pct := new(big.Rat).Mul(decimalAmount(s.PercentPerUnit), ratInt(count))
		cap := decimalAmount(s.MaximumPercent)
		if pct.Cmp(cap) > 0 {
			pct = cap
		}
		price.Mul(price, new(big.Rat).Quo(new(big.Rat).Sub(ratInt(100), pct), ratInt(100)))
		out.trace = append(out.trace, fmt.Sprintf("%s: %d qualifying units; %s%% discount (cap %g%%) → $%s", s.Name, count, pct.FloatString(2), s.MaximumPercent, price.FloatString(2)))
	}
	out.price = price
	return out
}

type priceOffer struct {
	rule           PricingScenario
	indexes        []int
	amounts        []*big.Rat
	calculations   []unitCalculation
	total          *big.Rat
	tier, priority int64
}

func (e *orderEvaluator) unitOffer(s PricingScenario, i int, c unitCalculation) priceOffer {
	amount := new(big.Rat).Mul(c.price, ratInt(e.order.Lines[i].Quantity))
	quantity := int64(e.order.Lines[i].Quantity)
	return priceOffer{rule: s, indexes: []int{i}, amounts: []*big.Rat{amount}, calculations: []unitCalculation{c}, total: ratCopy(amount), tier: int64(s.Tier) * quantity, priority: int64(s.Priority) * quantity}
}
func (e *orderEvaluator) groupOffer(s PricingScenario) (priceOffer, error) {
	offer := priceOffer{rule: s, total: new(big.Rat)}
	quantities := make([]int, len(s.BundleComponents))
	quantity := 0
	for i, l := range e.order.Lines {
		matches := s.Scope.Matches(l)
		component := -1
		if s.Method == "bundle" {
			matches = false
			for j, c := range s.BundleComponents {
				if l.ProductID == c.ProductID && l.Configuration == c.Configuration {
					matches = true
					component = j
					break
				}
			}
		}
		if !matches {
			continue
		}
		if err := e.eligibility(s, l); err != nil {
			return offer, fmt.Errorf("line %s: %w", l.LineID, err)
		}
		if component >= 0 {
			if l.PriceUnit != "each" {
				return offer, errors.New("bundle components must use each units")
			}
			quantities[component] += l.Quantity
		}
		quantity += l.Quantity
		offer.indexes = append(offer.indexes, i)
		c := unitCalculation{ids: []string{s.ID}}
		if s.Method == "free" {
			c = e.unit(s, i)
			if c.err != nil {
				return offer, c.err
			}
		} else {
			c.price = ratInt(1)
			c.trace = []string{fmt.Sprintf("%s: complete bundle $%.2f; allocated by component quantity", s.Name, *s.FixedPrice)}
		}
		amount := new(big.Rat).Mul(c.price, ratInt(l.Quantity))
		offer.amounts = append(offer.amounts, amount)
		offer.calculations = append(offer.calculations, c)
		offer.total.Add(offer.total, amount)
	}
	if len(offer.indexes) == 0 {
		return offer, errors.New("no eligible products")
	}
	if s.Method == "bundle" {
		for j, c := range s.BundleComponents {
			if quantities[j] != c.Quantity {
				return offer, fmt.Errorf("bundle requires exactly %d of %s (%s); found %d", c.Quantity, c.ProductID, c.Configuration, quantities[j])
			}
		}
		total := decimalAmount(*s.FixedPrice)
		for j, amount := range offer.amounts {
			offer.amounts[j] = new(big.Rat).Mul(new(big.Rat).Quo(amount, offer.total), total)
		}
		offer.total = total
	} else {
		if quantity != s.BuyQuantity+s.FreeQuantity {
			return offer, fmt.Errorf("promotion requires exactly %d paid + %d free units; found %d", s.BuyQuantity, s.FreeQuantity, quantity)
		}
		// Award the cheapest eligible units first, with an optional per-unit cap.
		positions := make([]int, len(offer.indexes))
		for j := range positions {
			positions[j] = j
		}
		sort.SliceStable(positions, func(a, b int) bool {
			return offer.calculations[positions[a]].price.Cmp(offer.calculations[positions[b]].price) < 0
		})
		remaining := s.FreeQuantity
		for _, j := range positions {
			if remaining == 0 {
				break
			}
			n := min(remaining, e.order.Lines[offer.indexes[j]].Quantity)
			credit := ratCopy(offer.calculations[j].price)
			if s.FreeValueLimit != nil && credit.Cmp(decimalAmount(*s.FreeValueLimit)) > 0 {
				credit = decimalAmount(*s.FreeValueLimit)
			}
			credit.Mul(credit, ratInt(n))
			offer.amounts[j].Sub(offer.amounts[j], credit)
			offer.total.Sub(offer.total, credit)
			offer.calculations[j].trace = append(offer.calculations[j].trace, fmt.Sprintf("%s: %d free units, credit $%s", s.Name, n, credit.FloatString(2)))
			remaining -= n
		}
	}
	offer.tier = int64(s.Tier) * int64(quantity)
	offer.priority = int64(s.Priority) * int64(quantity)
	return offer, nil
}

type offerPlan struct {
	offers         []int
	total          *big.Rat
	tier, priority int64
	ways           int
}

func betterPlan(a, b offerPlan, policy string) bool {
	if b.total == nil {
		return true
	}
	if policy == "highest_tier" && a.tier != b.tier {
		return a.tier > b.tier
	}
	if policy == "priority" && a.priority != b.priority {
		return a.priority > b.priority
	}
	return a.total.Cmp(b.total) < 0
}

// Exact-cover search keeps bundle/free offers atomic. Memoization makes ordinary
// independent unit pricing linear in the number of candidates. Complex overlap
// is bounded and returns review rather than an arbitrary partial result.
func selectOffers(offers []priceOffer, n int, policy string, selected []string) (offerPlan, error) {
	byLine := make([][]int, n)
	for i, o := range offers {
		for _, line := range o.indexes {
			byLine[line] = append(byLine[line], i)
		}
	}
	for i, a := range byLine {
		if len(a) == 0 {
			return offerPlan{}, fmt.Errorf("no eligible pricing scenario for line %d", i+1)
		}
	}
	covered := make([]byte, n)
	used := make([]int, len(selected))
	memo := map[string]offerPlan{}
	states := 0
	var search func() (offerPlan, error)
	search = func() (offerPlan, error) {
		key := string(covered)
		for _, u := range used {
			if u > 0 {
				key += "1"
			} else {
				key += "0"
			}
		}
		if p, ok := memo[key]; ok {
			return p, nil
		}
		states++
		if states > 20000 {
			return offerPlan{}, errors.New("too many overlapping pricing alternatives; select scenarios for review")
		}
		first := -1
		for i, v := range covered {
			if v == 0 {
				first = i
				break
			}
		}
		if first < 0 {
			for _, u := range used {
				if u == 0 {
					return offerPlan{}, nil
				}
			}
			return offerPlan{total: new(big.Rat), ways: 1}, nil
		}
		best := offerPlan{}
		ways := 0
		for _, oi := range byLine[first] {
			o := offers[oi]
			overlap := false
			for _, i := range o.indexes {
				if covered[i] != 0 {
					overlap = true
					break
				}
			}
			if overlap {
				continue
			}
			for _, i := range o.indexes {
				covered[i] = 1
			}
			si := slices.Index(selected, o.rule.ID)
			if si >= 0 {
				used[si]++
			}
			tail, err := search()
			for _, i := range o.indexes {
				covered[i] = 0
			}
			if si >= 0 {
				used[si]--
			}
			if err != nil {
				return offerPlan{}, err
			}
			if tail.total == nil {
				continue
			}
			p := offerPlan{offers: append([]int{oi}, tail.offers...), total: new(big.Rat).Add(o.total, tail.total), tier: o.tier + tail.tier, priority: o.priority + tail.priority}
			ways = min(2, ways+tail.ways)
			if betterPlan(p, best, policy) {
				best = p
			}
		}
		best.ways = ways
		memo[key] = best
		return best, nil
	}
	p, err := search()
	if err != nil {
		return p, err
	}
	if p.total == nil {
		return p, errors.New("selected scenarios cannot cover the order without overlap")
	}
	if policy == "require_review" && len(selected) == 0 && p.ways > 1 {
		return p, errors.New("multiple scenarios qualify; select the approved scenarios to price this order")
	}
	return p, nil
}

// Allocate the rounded grand total by largest remainder, so displayed line
// totals always sum to the returned total (including bundles and fractional cents).
func allocateCents(amounts []*big.Rat, total *big.Rat) []float64 {
	cents := make([]int64, len(amounts))
	fractions := make([]*big.Rat, len(amounts))
	sum := int64(0)
	for i, a := range amounts {
		x := new(big.Rat).Mul(a, ratInt(100))
		whole := new(big.Int).Quo(x.Num(), x.Denom())
		cents[i] = whole.Int64()
		sum += cents[i]
		fractions[i] = new(big.Rat).Sub(x, new(big.Rat).SetInt(whole))
	}
	target := new(big.Rat).Mul(total, ratInt(100))
	target.Add(target, big.NewRat(1, 2))
	rounded := new(big.Int).Quo(target.Num(), target.Denom()).Int64()
	indexes := make([]int, len(amounts))
	for i := range indexes {
		indexes[i] = i
	}
	sort.SliceStable(indexes, func(i, j int) bool { return fractions[indexes[i]].Cmp(fractions[indexes[j]]) > 0 })
	for n := int64(0); n < rounded-sum; n++ {
		cents[indexes[int(n)%len(indexes)]]++
	}
	out := make([]float64, len(amounts))
	for i, c := range cents {
		out[i] = float64(c) / 100
	}
	return out
}
func (v *VendorProgram) PriceOrder(order Order) (OrderPriceResult, error) {
	result := OrderPriceResult{Lines: []PricedOrderLine{}, Reasons: []string{}, Evaluations: []ScenarioEvaluation{}, Instructions: []string{}, Sources: []string{}}
	if err := order.Validate(); err != nil {
		return result, err
	}
	at := time.Now()
	if order.At != nil {
		at = *order.At
	}
	if v.IsExpired(at) {
		result.Reasons = append(result.Reasons, "vendor program has expired at the order date")
		return result, nil
	}
	if len(v.Scenarios) == 0 {
		return v.priceLegacyOrder(order, result)
	}
	// Validate stored rules as well as newly submitted drafts before evaluating.
	validated := *v
	if err := validated.UpdateScenarios(v.Scenarios, v.SelectionPolicy); err != nil {
		return result, fmt.Errorf("invalid pricing scenarios: %w", err)
	}
	e := orderEvaluator{program: &validated, order: order, at: at, rules: map[string]PricingScenario{}, cache: map[string]unitCalculation{}}
	for _, s := range validated.Scenarios {
		e.rules[s.ID] = s
	}
	for _, id := range order.SelectedScenarioIDs {
		if _, ok := e.rules[id]; !ok {
			return result, fmt.Errorf("unknown selected scenario %s", id)
		}
	}
	offers := []priceOffer{}
	for _, s := range validated.Scenarios {
		evaluation := ScenarioEvaluation{ScenarioID: s.ID, Name: s.Name, EligibleLineIDs: []string{}, Rejections: map[string]string{}}
		candidate := len(order.SelectedScenarioIDs) == 0 || slices.Contains(order.SelectedScenarioIDs, s.ID)
		if oneOf(s.Method, "bundle", "free") {
			offer, err := e.groupOffer(s)
			if err != nil {
				evaluation.Rejections["order"] = err.Error()
			} else {
				for _, i := range offer.indexes {
					evaluation.EligibleLineIDs = append(evaluation.EligibleLineIDs, order.Lines[i].LineID)
				}
				if candidate {
					offers = append(offers, offer)
				}
			}
		} else {
			for i, l := range order.Lines {
				c := e.unit(s, i)
				if c.err != nil {
					evaluation.Rejections[l.LineID] = c.err.Error()
				} else {
					evaluation.EligibleLineIDs = append(evaluation.EligibleLineIDs, l.LineID)
					if candidate {
						offers = append(offers, e.unitOffer(s, i, c))
					}
				}
			}
		}
		result.Evaluations = append(result.Evaluations, evaluation)
	}
	plan, err := selectOffers(offers, len(order.Lines), validated.SelectionPolicy, order.SelectedScenarioIDs)
	if err != nil {
		result.Reasons = append(result.Reasons, err.Error())
		return result, nil
	}
	if plan.total.Cmp(ratInt(1000000000000)) > 0 {
		result.Reasons = append(result.Reasons, "order total exceeds supported maximum")
		return result, nil
	}
	amounts := make([]*big.Rat, len(order.Lines))
	result.Lines = make([]PricedOrderLine, len(order.Lines))
	for _, oi := range plan.offers {
		o := offers[oi]
		for j, i := range o.indexes {
			l := order.Lines[i]
			c := o.calculations[j]
			amounts[i] = o.amounts[j]
			result.Lines[i] = PricedOrderLine{LineID: l.LineID, Quantity: l.Quantity, PriceUnit: l.PriceUnit, ScenarioIDs: c.ids, Explanation: c.trace}
			for _, id := range c.ids {
				s := e.rules[id]
				result.Instructions = appendUnique(result.Instructions, s.Instruction)
				if s.PromoCode != "" {
					result.Instructions = appendUnique(result.Instructions, "Use promo code "+s.PromoCode)
				}
				result.Sources = appendUnique(result.Sources, s.Source, s.ApprovalEvidence)
			}
		}
	}
	totals := allocateCents(amounts, plan.total)
	for i := range result.Lines {
		result.Lines[i].Total = totals[i]
		result.Lines[i].UnitPrice = money(new(big.Rat).Quo(amounts[i], ratInt(order.Lines[i].Quantity)))
	}
	total := money(plan.total)
	result.Total = &total
	result.Available = true
	return result, nil
}
func (v *VendorProgram) priceLegacyOrder(order Order, result OrderPriceResult) (OrderPriceResult, error) {
	if len(order.SelectedScenarioIDs) > 0 {
		return result, errors.New("this program has no scenarios")
	}
	total := new(big.Rat)
	copy := *v
	copy.ExpiresAt = nil // caller already checked the requested order instant
	amounts := []*big.Rat{}
	for _, l := range order.Lines {
		if l.PriceUnit != "each" {
			result.Reasons = append(result.Reasons, "legacy programs only support each units")
			continue
		}
		list := 0.0
		if l.ListPrice != nil {
			list = *l.ListPrice
		} else if !(l.QuotePrice != nil && l.QuoteApproved && *l.QuotePrice > 0 && v.SupportsQuotePricing(l.ProductID, l.GroupName)) {
			result.Reasons = append(result.Reasons, l.LineID+": list price is required")
			continue
		}
		quote := 0.0
		if l.QuotePrice != nil && l.QuoteApproved {
			quote = *l.QuotePrice
		}
		price, err := copy.CalculateDealerPrice(Product{ProductId: l.ProductID, GroupName: l.GroupName, Vendor: v.Vendor, ListPrice: list, QuotePrice: quote, DiscountOptions: l.DiscountOptions})
		if err != nil {
			result.Reasons = append(result.Reasons, l.LineID+": "+err.Error())
			continue
		}
		amount := new(big.Rat).Mul(decimalAmount(price), ratInt(l.Quantity))
		total.Add(total, amount)
		amounts = append(amounts, amount)
		result.Lines = append(result.Lines, PricedOrderLine{LineID: l.LineID, Quantity: l.Quantity, PriceUnit: l.PriceUnit, UnitPrice: price, Total: money(amount), ScenarioIDs: []string{}, Explanation: []string{"Existing vendor/group/product discount precedence"}})
	}
	if len(result.Reasons) > 0 {
		result.Lines = []PricedOrderLine{}
		return result, nil
	}
	if total.Cmp(ratInt(1000000000000)) > 0 {
		result.Lines = []PricedOrderLine{}
		result.Reasons = append(result.Reasons, "order total exceeds supported maximum")
		return result, nil
	}
	amount := money(total)
	lineTotals := allocateCents(amounts, total)
	for i := range result.Lines {
		result.Lines[i].Total = lineTotals[i]
	}
	result.Available = true
	result.Total = &amount
	return result, nil
}
