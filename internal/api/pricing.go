package api

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strings"

	"usaf-pricing-service/domain"
	"usaf-pricing-service/internal/store"
)

type priceProductRequest struct {
	ProductID       string   `json:"productId"`
	Vendor          string   `json:"vendor"`
	ListPrice       *float64 `json:"listPrice"`
	DiscountOptions []string `json:"discountOptions"`
}

type dealerPriceResult struct {
	DealerPrice       *float64 `json:"dealerPrice"`
	DealerPriceString string   `json:"dealerPriceString"`
	Reason            string   `json:"reason"`
}

func unavailablePrice(reason string) dealerPriceResult {
	return dealerPriceResult{DealerPriceString: "Unavailable", Reason: reason}
}

func (a *API) dealerPrices(w http.ResponseWriter, r *http.Request) error {
	products, err := decodeBody[[]priceProductRequest](w, r)
	if err != nil {
		return err
	}
	// A keyed response cannot represent products with missing or duplicate IDs.
	ids := make(map[string]struct{}, len(*products))
	for _, product := range *products {
		if strings.TrimSpace(product.ProductID) == "" {
			return badRequest("every product must have a nonblank productId")
		}
		if _, exists := ids[product.ProductID]; exists {
			return badRequest(fmt.Sprintf("duplicate productId %s", product.ProductID))
		}
		ids[product.ProductID] = struct{}{}
	}
	// Reuse a vendor's snapshot (including lookup failures) within this request.
	type vendorLookup struct {
		program *domain.VendorProgram
		reason  string
	}
	cache := make(map[string]vendorLookup)
	results := make(map[string]dealerPriceResult, len(*products))
	for _, input := range *products {
		if strings.TrimSpace(input.Vendor) == "" {
			results[input.ProductID] = unavailablePrice("vendor is required")
			continue
		}
		if input.ListPrice == nil {
			results[input.ProductID] = unavailablePrice("listPrice is required")
			continue
		}
		if math.IsNaN(*input.ListPrice) || math.IsInf(*input.ListPrice, 0) || *input.ListPrice < 0 {
			results[input.ProductID] = unavailablePrice("list price must be finite and nonnegative")
			continue
		}
		lookup, exists := cache[input.Vendor]
		if !exists {
			lookup.program, err = a.programs.GetByVendor(r.Context(), input.Vendor)
			if err != nil {
				switch {
				case errors.Is(err, store.ErrNotFound):
					lookup.reason = fmt.Sprintf("vendor program for %s is missing", input.Vendor)
				case errors.Is(err, store.ErrAmbiguousVendor):
					lookup.reason = fmt.Sprintf("multiple vendor programs found for vendor %s", input.Vendor)
				case errors.Is(err, context.DeadlineExceeded):
					lookup.reason = "vendor program lookup timed out"
				case errors.Is(err, context.Canceled):
					lookup.reason = "vendor program lookup canceled"
				default:
					lookup.reason = "vendor program lookup failed"
					a.logger.Error("pricing vendor lookup failed", "vendor", input.Vendor, "error", err)
				}
			}
			cache[input.Vendor] = lookup
		}
		if lookup.reason != "" {
			results[input.ProductID] = unavailablePrice(lookup.reason)
			continue
		}
		price, err := lookup.program.CalculateDealerPrice(domain.Product{
			ProductId: input.ProductID, Vendor: input.Vendor, ListPrice: *input.ListPrice, DiscountOptions: input.DiscountOptions,
		})
		if err != nil {
			results[input.ProductID] = unavailablePrice(err.Error())
			continue
		}
		results[input.ProductID] = dealerPriceResult{DealerPrice: &price, DealerPriceString: fmt.Sprintf("$%.2f", price)}
	}
	writeJSON(w, http.StatusOK, results)
	return nil
}
