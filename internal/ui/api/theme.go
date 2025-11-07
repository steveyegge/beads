package api

import (
	"encoding/json"
	"net/http"
	"strings"
)

// ThemeRequest represents the incoming theme selection request.
type ThemeRequest struct {
	Theme string `json:"theme"`
}

// ThemeResponse represents the response from the theme endpoint.
type ThemeResponse struct {
	Theme   string `json:"theme"`
	Message string `json:"message,omitempty"`
}

// NewThemeHandler creates an HTTP handler for setting the user's theme preference.
// It accepts POST requests with a JSON body containing the theme name,
// sets a cookie for persistence, and returns a success response.
func NewThemeHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req ThemeRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid JSON", http.StatusBadRequest)
			return
		}

		theme := strings.TrimSpace(req.Theme)
		if theme == "" {
			http.Error(w, "Theme name is required", http.StatusBadRequest)
			return
		}

		// Validate theme name (only allow known themes)
		validThemes := map[string]bool{
			"white":  true,
			"orange": true,
			"green":  true, // Future themes
			"blue":   true,
			"black":  true,
		}

		if !validThemes[theme] {
			http.Error(w, "Invalid theme name", http.StatusBadRequest)
			return
		}

		// Set cookie for theme persistence
		// Cookie expires in 1 year
		cookie := &http.Cookie{
			Name:     "beads-theme",
			Value:    theme,
			Path:     "/",
			MaxAge:   365 * 24 * 60 * 60, // 1 year
			HttpOnly: false,              // Allow JavaScript to read for client-side fallback
			SameSite: http.SameSiteStrictMode,
			Secure:   r.TLS != nil, // Only set Secure if using HTTPS
		}
		http.SetCookie(w, cookie)

		// Return success response
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)

		resp := ThemeResponse{
			Theme:   theme,
			Message: "Theme preference saved",
		}
		_ = json.NewEncoder(w).Encode(resp)
	})
}
