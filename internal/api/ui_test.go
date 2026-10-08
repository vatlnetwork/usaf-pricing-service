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

func TestUIRoutes(t *testing.T) {
	handler := NewHandler(failingStore{}, time.Second, slog.New(slog.NewTextHandler(io.Discard, nil)))
	for _, tt := range []struct {
		method, path, contentType, contains string
		status                              int
	}{
		{"GET", "/", "text/html", "Vendor-wide options", 200},
		{"HEAD", "/", "text/html", "", 200},
		{"GET", "/assets/app.css", "text/css", ".product-scope", 200},
		{"GET", "/assets/app.js", "text/javascript", "expected_updated_at", 200},
		{"GET", "/unknown", "text/plain", "404", 404},
		{"POST", "/", "text/plain", "Method Not Allowed", 405},
	} {
		t.Run(tt.method+tt.path, func(t *testing.T) {
			res := httptest.NewRecorder()
			handler.ServeHTTP(res, httptest.NewRequest(tt.method, tt.path, nil))
			if res.Code != tt.status || !strings.HasPrefix(res.Header().Get("Content-Type"), tt.contentType) || !strings.Contains(res.Body.String(), tt.contains) {
				t.Fatalf("unexpected response: %d %s %s", res.Code, res.Header(), res.Body.String())
			}
			if tt.method == "HEAD" && res.Body.Len() != 0 {
				t.Fatal("HEAD returned a body")
			}
		})
	}
}

type editorStore struct {
	store.VendorPrograms
	program domain.VendorProgram
	calls   int
}

func (s *editorStore) Update(_ context.Context, _ string, mutate func(*domain.VendorProgram) error) (*domain.VendorProgram, error) {
	s.calls++
	candidate := s.program
	if err := mutate(&candidate); err != nil {
		return nil, err
	}
	s.program = candidate
	return &candidate, nil
}

func TestReplaceProgram(t *testing.T) {
	original := domain.VendorProgram{Id: "original-id", Vendor: "Original", CreatedAt: time.Now().Add(-time.Hour), UpdatedAt: time.Now().Add(-time.Minute).Truncate(time.Millisecond), ProductOverrides: []domain.ProductOverride{{ProductId: "old"}}}
	db := &editorStore{program: original}
	handler := NewHandler(db, time.Second, slog.New(slog.NewTextHandler(io.Discard, nil)))
	request := func(body string, status int) domain.VendorProgram {
		t.Helper()
		res := httptest.NewRecorder()
		handler.ServeHTTP(res, httptest.NewRequest("PUT", "/vendor-programs/original-id", strings.NewReader(body)))
		if res.Code != status {
			t.Fatalf("got %d %s, want %d", res.Code, res.Body.String(), status)
		}
		var result domain.VendorProgram
		if status == 200 {
			if err := json.Unmarshal(res.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
		}
		return result
	}
	// Responses from existing endpoints can include sub-millisecond precision,
	// while MongoDB persists millisecond timestamps.
	version := original.UpdatedAt.Add(123 * time.Microsecond).Format(time.RFC3339Nano)
	body := `{"vendor":"Renamed","quote_enabled":true,"expires_at":"2030-01-01T00:00:00Z","discount_options":[{"name":"Base","discount_path":[{"type":"percentage","amount":10}]}],"product_group_overrides":[{"group_name":"Kitchen","discount_options":[{"name":"Base","discount_path":[{"type":"percentage","amount":20}]}]}],"product_overrides":[{"product_id":"new","net_price":42,"quote_enabled":true}],"expected_updated_at":"` + version + `"}`
	saved := request(body, 200)
	if saved.Id != original.Id || !saved.CreatedAt.Equal(original.CreatedAt) || !saved.UpdatedAt.After(original.UpdatedAt) || saved.Vendor != "Renamed" || !saved.QuoteEnabled || saved.ExpiresAt == nil || len(saved.DiscountOptions) != 1 || len(saved.ProductGroupOverrides) != 1 || len(saved.ProductOverrides) != 1 || saved.ProductOverrides[0].ProductId != "new" || saved.ProductOverrides[0].NetPrice != 42 {
		t.Fatalf("incorrect replacement: %+v", saved)
	}
	request(body, 409)
	if !db.program.UpdatedAt.Equal(saved.UpdatedAt) {
		t.Fatal("conflict changed program")
	}
	calls := db.calls
	for _, invalid := range []string{`{}`, `{"vendor":" "}`, `{"vendor":"Valid","discount_options":[{"name":"bad","discount_path":[{"type":"percentage","amount":101}]}]}`, `{"vendor":"Valid","product_overrides":[{"product_id":"duplicate"},{"product_id":"duplicate"}]}`, `{"vendor":"Valid","product_group_overrides":[{"group_name":"duplicate"},{"group_name":"duplicate"}]}`, `{"vendor":"Valid","id":"injected"}`, `{"vendor":"Valid","expected_updated_at":"bad"}`} {
		request(invalid, 400)
	}
	if db.calls != calls {
		t.Fatal("invalid replacement reached persistence")
	}
	cleared := request(`{"vendor":"Cleared","discount_options":[],"product_overrides":[],"product_group_overrides":[],"expires_at":null}`, 200)
	if len(cleared.DiscountOptions)+len(cleared.ProductOverrides)+len(cleared.ProductGroupOverrides) != 0 || cleared.ExpiresAt != nil || cleared.QuoteEnabled {
		t.Fatalf("fields not cleared: %+v", cleared)
	}
}
