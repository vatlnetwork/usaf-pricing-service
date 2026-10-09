package api

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"usaf-pricing-service/domain"
	"usaf-pricing-service/internal/store"
)

type codePricingStore struct {
	pricingStore
	codeCalls map[string]int
}

func (s *codePricingStore) GetByVendorCode(_ context.Context, code string) (*domain.VendorProgram, error) {
	s.codeCalls[code]++
	if code != "001" {
		return nil, store.ErrNotFound
	}
	return &domain.VendorProgram{Vendor: "Canonical name", VendorCode: code}, nil
}

func TestDealerPricesVendorCodes(t *testing.T) {
	db := &codePricingStore{pricingStore: pricingStore{
		programs: map[string]*domain.VendorProgram{"001": {Vendor: "001", ProductOverrides: []domain.ProductOverride{{ProductId: "name", NetPrice: 42}}}},
		calls:    make(map[string]int),
	}, codeCalls: make(map[string]int)}
	handler := NewHandler(db, time.Second, slog.New(slog.NewTextHandler(io.Discard, nil)))
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, httptest.NewRequest("POST", "/products/dealer-prices", strings.NewReader(`[
		{"productId":"code","vendorCode":"001","listPrice":100},
		{"productId":"different-name","vendorCode":"001","vendor":"Other name","listPrice":100},
		{"productId":"name","vendor":"001","listPrice":100},
		{"productId":"unknown","vendorCode":"missing","vendor":"001","listPrice":100},
		{"productId":"unknown-again","vendorCode":"missing","listPrice":100},
		{"productId":"blank","vendorCode":"  ","vendor":"001","listPrice":100}
	]`)))
	if res.Code != 200 {
		t.Fatalf("%d: %s", res.Code, res.Body.String())
	}
	var results map[string]dealerPriceResult
	if err := json.Unmarshal(res.Body.Bytes(), &results); err != nil {
		t.Fatal(err)
	}
	for id, want := range map[string]float64{"code": 100, "different-name": 100, "name": 42} {
		if r := results[id]; r.DealerPrice == nil || *r.DealerPrice != want {
			t.Errorf("%s: %+v", id, r)
		}
	}
	for _, id := range []string{"unknown", "unknown-again", "blank"} {
		if r := results[id]; r.DealerPrice != nil || r.Reason == "" {
			t.Errorf("%s should be unavailable: %+v", id, r)
		}
	}
	if db.calls["001"] != 1 || db.codeCalls["001"] != 1 || db.codeCalls["missing"] != 1 || len(db.codeCalls) != 2 {
		t.Fatalf("name and code caches must be separate: %+v %+v", db.calls, db.codeCalls)
	}
}
