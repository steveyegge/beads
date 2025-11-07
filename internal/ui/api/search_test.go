package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/steveyegge/beads/internal/ui/search"
)

type recordingSearchService struct {
	t         *testing.T
	results   []search.Result
	err       error
	calls     int
	lastQuery string
	lastLimit int
	lastSort  search.SortMode
}

func (s *recordingSearchService) Search(_ context.Context, query string, limit int, sort search.SortMode) ([]search.Result, error) {
	s.calls++
	s.lastQuery = query
	s.lastLimit = limit
	s.lastSort = sort
	return s.results, s.err
}

func TestNewSearchHandlerRejectsNonGET(t *testing.T) {
	t.Parallel()

	svc := &recordingSearchService{}
	handler := NewSearchHandler(svc)

	req := httptest.NewRequest(http.MethodPost, "/api/search?q=test", nil)
	res := httptest.NewRecorder()

	handler.ServeHTTP(res, req)

	if res.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", res.Code, http.StatusMethodNotAllowed)
	}
	if svc.calls != 0 {
		t.Fatalf("expected search service not to be called, got %d", svc.calls)
	}
}

func TestNewSearchHandlerSuccess(t *testing.T) {
	t.Parallel()

	expectedResults := []search.Result{
		{ID: "ui-1", Title: "First"},
		{ID: "ui-2", Title: "Second"},
	}
	svc := &recordingSearchService{results: expectedResults}
	handler := NewSearchHandler(svc)

	query := url.Values{}
	query.Set("q", "  backlog  ")
	query.Set("limit", "7")
	query.Set("sort", "recent")
	req := httptest.NewRequest(http.MethodGet, "/api/search?"+query.Encode(), nil)
	res := httptest.NewRecorder()

	handler.ServeHTTP(res, req)

	if svc.calls != 1 {
		t.Fatalf("search service called %d times, want 1", svc.calls)
	}
	if svc.lastQuery != "backlog" {
		t.Fatalf("query = %q, want %q", svc.lastQuery, "backlog")
	}
	if svc.lastLimit != 7 {
		t.Fatalf("limit = %d, want 7", svc.lastLimit)
	}
	if svc.lastSort != search.SortMode("recent") {
		t.Fatalf("sort = %q, want recent", svc.lastSort)
	}

	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", res.Code, http.StatusOK)
	}
	if got := res.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("content-type = %q, want application/json", got)
	}

	var payload struct {
		Results []search.Result `json:"results"`
	}
	if err := json.NewDecoder(res.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(payload.Results) != len(expectedResults) {
		t.Fatalf("results length = %d, want %d", len(payload.Results), len(expectedResults))
	}
	for i, result := range expectedResults {
		if !reflect.DeepEqual(payload.Results[i], result) {
			t.Fatalf("result[%d] = %+v, want %+v", i, payload.Results[i], result)
		}
	}
}

func TestNewSearchHandlerErrorAndDefaultLimit(t *testing.T) {
	t.Parallel()

	svc := &recordingSearchService{err: errors.New("boom")}
	handler := NewSearchHandler(svc)

	query := url.Values{}
	query.Set("q", "  bad ")
	query.Set("limit", "-5")
	query.Set("sort", "relevance")
	req := httptest.NewRequest(http.MethodGet, "/api/search?"+query.Encode(), nil)
	res := httptest.NewRecorder()

	handler.ServeHTTP(res, req)

	if svc.calls != 1 {
		t.Fatalf("search service called %d times, want 1", svc.calls)
	}
	if svc.lastLimit != 20 {
		t.Fatalf("limit = %d, want default 20", svc.lastLimit)
	}
	if svc.lastQuery != "bad" {
		t.Fatalf("query = %q, want %q", svc.lastQuery, "bad")
	}
	if svc.lastSort != search.SortMode("relevance") {
		t.Fatalf("sort = %q, want relevance", svc.lastSort)
	}

	if res.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want %d", res.Code, http.StatusBadGateway)
	}
	body := res.Body.String()
	if !strings.Contains(body, "search failed: boom") {
		t.Fatalf("response body %q missing error message", body)
	}
}
