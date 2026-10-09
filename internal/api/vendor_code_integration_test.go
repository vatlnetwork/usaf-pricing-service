package api_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"usaf-pricing-service/domain"
	"usaf-pricing-service/internal/api"
	"usaf-pricing-service/internal/store"
)

func TestMongoVendorCodeLifecycle(t *testing.T) {
	db := integrationStore(t)
	handler := api.NewHandler(db, 5*time.Second, slog.New(slog.NewTextHandler(io.Discard, nil)))
	call := func(method, path, body string, status int) domain.VendorProgram {
		t.Helper()
		res := httptest.NewRecorder()
		handler.ServeHTTP(res, httptest.NewRequest(method, path, strings.NewReader(body)))
		if res.Code != status {
			t.Fatalf("%s %s: %d %s; want %d", method, path, res.Code, res.Body.String(), status)
		}
		var p domain.VendorProgram
		if status == 200 || status == 201 {
			if err := json.Unmarshal(res.Body.Bytes(), &p); err != nil {
				t.Fatal(err)
			}
		}
		return p
	}
	first := call("POST", "/vendor-programs", `{"vendor":"Canonical","vendor_code":"001/A"}`, 201)
	second := call("POST", "/vendor-programs", `{"vendor":"Other"}`, 201)
	call("POST", "/vendor-programs", `{"vendor":"Legacy"}`, 201)
	call("POST", "/vendor-programs", `{"vendor":"Empty","vendor_code":""}`, 201)
	call("POST", "/vendor-programs", `{"vendor":"Blank","vendor_code":"  "}`, 400)
	call("POST", "/vendor-programs", `{"vendor":"Numeric","vendor_code":1}`, 400)
	call("POST", "/vendor-programs", `{"vendor":"Duplicate","vendor_code":"001/A"}`, 409)
	call("PUT", "/vendor-programs/"+second.Id, `{"vendor":"Other","vendor_code":"001/A"}`, 409)
	if p := call("GET", "/vendor-programs/"+second.Id, "", 200); p.VendorCode != "" || p.Vendor != "Other" {
		t.Fatal("failed duplicate update changed stored program")
	}
	if p := call("GET", "/vendor-programs/by-code/001%2FA", "", 200); p.Id != first.Id || p.VendorCode != "001/A" {
		t.Fatal("encoded code lookup did not return saved program")
	}
	call("GET", "/vendor-programs/by-code/001%2Fa", "", 404)
	call("GET", "/vendor-programs/by-code/%20", "", 400)
	// Updating the same program with its current code must succeed.
	call("PUT", "/vendor-programs/"+first.Id, `{"vendor":"Renamed","vendor_code":"001/A"}`, 200)
	if p := call("PUT", "/vendor-programs/"+first.Id+"/quote-enabled", `{"quote_enabled":true}`, 200); p.VendorCode != "001/A" {
		t.Fatal("focused update lost vendor code")
	}
	call("PUT", "/vendor-programs/"+first.Id+"/expiry", `{"expires_at":"2000-01-01T00:00:00Z"}`, 200)
	call("GET", "/vendor-programs/by-code/001%2FA", "", 404)
	call("POST", "/vendor-programs", `{"vendor":"Expired duplicate","vendor_code":"001/A"}`, 409)
	if p := call("GET", "/vendor-programs/"+first.Id, "", 200); p.VendorCode != "001/A" {
		t.Fatal("expired program lost code")
	}
	call("PUT", "/vendor-programs/"+first.Id+"/expiry", `{"expires_at":null}`, 200)
	call("GET", "/vendor-programs/by-code/001%2FA", "", 200)
	call("PUT", "/vendor-programs/"+first.Id, `{"vendor":"Renamed","vendor_code":"NEW"}`, 200)
	call("GET", "/vendor-programs/by-code/001%2FA", "", 404)
	call("GET", "/vendor-programs/by-code/NEW", "", 200)
	call("PUT", "/vendor-programs/"+second.Id, `{"vendor":"Other","vendor_code":"001/A"}`, 200)
	call("PUT", "/vendor-programs/"+first.Id, `{"vendor":"Renamed"}`, 200)
	call("GET", "/vendor-programs/by-code/NEW", "", 404)
	call("POST", "/vendor-programs", `{"vendor":"Reused","vendor_code":"NEW"}`, 201)
}

func TestMongoConcurrentVendorCodeCreates(t *testing.T) {
	db := integrationStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	start := make(chan struct{})
	results := make(chan error, 8)
	for range 8 {
		go func() {
			<-start
			results <- db.Create(ctx, &domain.VendorProgram{Vendor: "Concurrent", VendorCode: "UNIQUE"})
		}()
	}
	close(start)
	winners := 0
	for range 8 {
		err := <-results
		if err == nil {
			winners++
		} else if !errors.Is(err, store.ErrDuplicateVendorCode) {
			t.Errorf("unexpected write error: %v", err)
		}
	}
	if winners != 1 {
		t.Fatalf("got %d successful creates for one code; want 1", winners)
	}
}
