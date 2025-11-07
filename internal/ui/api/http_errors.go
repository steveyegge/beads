package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// jsonErrorResponse encodes a structured error payload for UI clients.
type jsonErrorResponse struct {
	Error   string `json:"error"`
	Details string `json:"details,omitempty"`
}

// WriteServiceUnavailable emits a structured 503 response with a short retry window.
func WriteServiceUnavailable(w http.ResponseWriter, message, details string) {
	if w.Header().Get("Retry-After") == "" {
		w.Header().Set("Retry-After", strconv.Itoa(int((5 * time.Second).Seconds())))
	}
	WriteJSONError(w, http.StatusServiceUnavailable, message, details)
}

// WriteJSONError writes an error response encoded as JSON with the given status.
func WriteJSONError(w http.ResponseWriter, status int, message, details string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")

	payload := jsonErrorResponse{
		Error: strings.TrimSpace(message),
	}
	if detail := strings.TrimSpace(details); detail != "" {
		payload.Details = detail
	}

	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
