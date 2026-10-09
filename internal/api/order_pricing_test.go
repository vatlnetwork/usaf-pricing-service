package api

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"usaf-pricing-service/domain"
)

func (s *editorStore) Create(_ context.Context, p *domain.VendorProgram) error {
	s.calls++
	p.Id = "0123456789abcdef01234567"
	s.program = *p
	return nil
}
func (s *editorStore) Get(_ context.Context, _ string) (*domain.VendorProgram, error) {
	s.calls++
	p := s.program
	return &p, nil
}
func TestScenarioHTTPWorkflow(t *testing.T) {
	db := &editorStore{}
	handler := NewHandler(db, time.Second, slog.New(slog.NewTextHandler(io.Discard, nil)))
	call := func(method, path, body string, status int) []byte {
		t.Helper()
		res := httptest.NewRecorder()
		handler.ServeHTTP(res, httptest.NewRequest(method, path, strings.NewReader(body)))
		if res.Code != status {
			t.Fatalf("%s %s: %d %s", method, path, res.Code, res.Body.String())
		}
		return res.Body.Bytes()
	}
	program := `{"vendor":"Atosa","vendor_code":"001","scenarios":[{"id":"pickup","name":"Pickup","approved":true,"adjustments":[{"type":"percentage","amount":50},{"type":"percentage","amount":5},{"type":"percentage","amount":2}],"conditions":[{"field":"fulfillment","operator":"=","value":"pickup"}]}]}`
	order := `{"fulfillment":"pickup","lines":[{"line_id":"a","product_id":"product","quantity":1,"price_unit":"each","list_price":1000}]}`
	call("POST", "/vendor-programs", program, 201)
	if db.program.VendorCode != "001" || len(db.program.Scenarios) != 1 || db.program.SelectionPolicy != "lowest_price" {
		t.Fatal("scenarios not saved")
	}
	for _, path := range []string{"/vendor-programs/0123456789abcdef01234567/price-order", "/vendor-programs/preview"} {
		body := order
		before := db.calls
		if strings.HasSuffix(path, "preview") {
			body = `{"program":` + program + `,"order":` + order + `}`
		}
		data := call("POST", path, body, 200)
		var result domain.OrderPriceResult
		if err := json.Unmarshal(data, &result); err != nil {
			t.Fatal(err)
		}
		if !result.Available || *result.Total != 465.5 {
			t.Fatalf("unexpected result %s", data)
		}
		if strings.HasSuffix(path, "preview") && db.calls != before {
			t.Fatal("preview touched persistence")
		}
	}
	shipped := strings.Replace(order, "pickup", "ship", 1)
	data := call("POST", "/vendor-programs/0123456789abcdef01234567/price-order", shipped, 200)
	if !strings.Contains(string(data), `"available":false`) || !strings.Contains(string(data), "requires fulfillment = pickup") {
		t.Fatalf("missing eligibility explanation %s", data)
	}
	before := db.calls
	call("POST", "/vendor-programs/preview", `{"program":`+program+`,"order":`+strings.Replace(order, `"quantity":1`, `"quantity":0`, 1)+`}`, 400)
	call("POST", "/vendor-programs/preview", `{"program":`+strings.Replace(program, `"vendor_code":"001"`, `"vendor_code":"  "`, 1)+`,"order":`+order+`}`, 400)
	call("PUT", "/vendor-programs/0123456789abcdef01234567", strings.Replace(program, `"vendor_code":"001"`, `"vendor_code":"  "`, 1), 400)
	call("POST", "/vendor-programs", strings.Replace(program, `"percentage"`, `"invalid"`, 1), 400)
	call("POST", "/vendor-programs/preview", `{"program":`+program+`,"order":{"unknown":1}}`, 400)
	if db.calls != before {
		t.Fatal("invalid request touched persistence")
	}
	call("PUT", "/vendor-programs/0123456789abcdef01234567", strings.Replace(program, "Atosa", "Renamed", 1), 200)
	if db.program.VendorCode != "001" || db.program.Vendor != "Renamed" || len(db.program.Scenarios) != 1 {
		t.Fatal("replacement lost scenarios")
	}
}
func TestScenarioAssets(t *testing.T) {
	handler := NewHandler(failingStore{}, time.Second, slog.New(slog.NewTextHandler(io.Discard, nil)))
	for _, path := range []string{"/test", "/assets/test.js", "/assets/scenarios.js", "/assets/scenarios.css", "/assets/examples.json"} {
		r := httptest.NewRecorder()
		handler.ServeHTTP(r, httptest.NewRequest(http.MethodGet, path, nil))
		if r.Code != 200 || r.Body.Len() == 0 {
			t.Fatalf("asset %s unavailable: %d", path, r.Code)
		}
		if !strings.Contains(r.Header().Get("Content-Security-Policy"), "script-src 'self'") {
			t.Fatal("asset lost CSP")
		}
	}
}
