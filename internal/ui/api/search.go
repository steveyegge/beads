package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/steveyegge/beads/internal/ui/search"
)

const searchUnavailableDetails = "Search requires an active Beads daemon connection."

// SearchService represents the contract required by the HTTP handler.
type SearchService interface {
	Search(ctx context.Context, query string, limit int, sort search.SortMode) ([]search.Result, error)
}

// NewSearchHandler wires the search service into an HTTP endpoint.
func NewSearchHandler(service SearchService) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		query := strings.TrimSpace(r.URL.Query().Get("q"))
		limit := 20
		if rawLimit := strings.TrimSpace(r.URL.Query().Get("limit")); rawLimit != "" {
			if value, err := strconv.Atoi(rawLimit); err == nil && value > 0 {
				limit = value
			}
		}

		sortParam := strings.TrimSpace(r.URL.Query().Get("sort"))
		results, err := service.Search(r.Context(), query, limit, search.SortMode(sortParam))
		if err != nil {
			if isDaemonUnavailable(err) {
				WriteServiceUnavailable(w, "search unavailable", searchUnavailableDetails)
				return
			}
			http.Error(w, fmt.Sprintf("search failed: %v", err), http.StatusBadGateway)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]any{"results": results}); err != nil {
			http.Error(w, fmt.Sprintf("encode response: %v", err), http.StatusInternalServerError)
			return
		}
	})
}
