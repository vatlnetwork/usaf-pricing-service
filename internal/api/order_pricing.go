package api

import (
	"net/http"
	"usaf-pricing-service/domain"
)

func (a *API) priceOrder(w http.ResponseWriter, r *http.Request) error {
	order, err := decodeBody[domain.Order](w, r)
	if err != nil {
		return err
	}
	if err = order.Validate(); err != nil {
		return badRequest(err.Error())
	}
	program, err := a.programs.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		return err
	}
	result, err := program.PriceOrder(*order)
	if err != nil {
		return badRequest(err.Error())
	}
	writeJSON(w, http.StatusOK, result)
	return nil
}
func (a *API) previewOrder(w http.ResponseWriter, r *http.Request) error {
	body, err := decodeBody[struct {
		Program programInput `json:"program"`
		Order   domain.Order `json:"order"`
	}](w, r)
	if err != nil {
		return err
	}
	program, err := domain.NewVendorProgram(body.Program.Vendor, body.Program.DiscountOptions, body.Program.ProductOverrides, body.Program.ProductGroupOverrides)
	if err != nil {
		return badRequest(err.Error())
	}
	if err = program.UpdateScenarios(body.Program.Scenarios, body.Program.SelectionPolicy); err != nil {
		return badRequest(err.Error())
	}
	program.UpdateExpiry(body.Program.ExpiresAt)
	program.UpdateQuoteEnabled(body.Program.QuoteEnabled)
	result, err := program.PriceOrder(body.Order)
	if err != nil {
		return badRequest(err.Error())
	}
	writeJSON(w, http.StatusOK, result)
	return nil
}
