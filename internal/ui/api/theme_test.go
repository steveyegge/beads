package api_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/steveyegge/beads/internal/ui/api"
)

func TestThemeHandlerRejectsNonPost(t *testing.T) {
	handler := api.NewThemeHandler()

	req := httptest.NewRequest(http.MethodGet, "/api/theme", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected status 405, got %d", rec.Code)
	}
}

func TestThemeHandlerRejectsInvalidJSON(t *testing.T) {
	handler := api.NewThemeHandler()

	req := httptest.NewRequest(http.MethodPost, "/api/theme", bytes.NewBufferString("invalid json"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", rec.Code)
	}
}

func TestThemeHandlerRejectsEmptyTheme(t *testing.T) {
	handler := api.NewThemeHandler()

	payload := map[string]string{"theme": ""}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/api/theme", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", rec.Code)
	}
}

func TestThemeHandlerRejectsInvalidTheme(t *testing.T) {
	handler := api.NewThemeHandler()

	payload := map[string]string{"theme": "invalid-theme"}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/api/theme", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", rec.Code)
	}
}

func TestThemeHandlerSetsOrangeTheme(t *testing.T) {
	handler := api.NewThemeHandler()

	payload := map[string]string{"theme": "orange"}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/api/theme", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	// Check cookie is set
	cookies := rec.Result().Cookies()
	var themeCookie *http.Cookie
	for _, c := range cookies {
		if c.Name == "beads-theme" {
			themeCookie = c
			break
		}
	}

	if themeCookie == nil {
		t.Fatal("expected beads-theme cookie to be set")
	}

	if themeCookie.Value != "orange" {
		t.Fatalf("expected cookie value 'orange', got %q", themeCookie.Value)
	}

	if themeCookie.MaxAge <= 0 {
		t.Fatal("expected cookie MaxAge to be positive (persistent)")
	}

	// Check response
	var resp map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp["theme"] != "orange" {
		t.Fatalf("expected response theme 'orange', got %v", resp["theme"])
	}
}

func TestThemeHandlerSetsWhiteTheme(t *testing.T) {
	handler := api.NewThemeHandler()

	payload := map[string]string{"theme": "white"}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/api/theme", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	// Check cookie
	cookies := rec.Result().Cookies()
	var themeCookie *http.Cookie
	for _, c := range cookies {
		if c.Name == "beads-theme" {
			themeCookie = c
			break
		}
	}

	if themeCookie == nil {
		t.Fatal("expected beads-theme cookie to be set")
	}

	if themeCookie.Value != "white" {
		t.Fatalf("expected cookie value 'white', got %q", themeCookie.Value)
	}
}
