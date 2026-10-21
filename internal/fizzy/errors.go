package fizzy

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// APIError is a response of the Fizzy API with a status code that is not 2xx.
type APIError struct {
	StatusCode int
	Method     string
	URL        string
	Message    string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("fizzy API error: %s %s: status %d: %s", e.Method, e.URL, e.StatusCode, e.Message)
}

// RateLimitError is a 429 response of the Fizzy API.
type RateLimitError struct {
	StatusCode int
	RetryAfter time.Duration // 0 if Retry-After was not present
	URL        string
}

// RateLimitRetryAfter implements tracker.RateLimitedError.
func (e *RateLimitError) RateLimitRetryAfter() time.Duration {
	return e.RetryAfter
}

func (e *RateLimitError) Error() string {
	if e.RetryAfter > 0 {
		return fmt.Sprintf("fizzy rate limit (status %d): retry-after=%s", e.StatusCode, e.RetryAfter)
	}
	return fmt.Sprintf("fizzy rate limit (status %d)", e.StatusCode)
}

// RetryConfig controls the retries of the Fizzy client.
type RetryConfig struct {
	MaxRetries int
	BaseDelay  time.Duration
	MaxBackoff time.Duration
}

// DefaultRetryConfig returns the retry settings of the client.
func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		MaxRetries: 3,
		BaseDelay:  time.Second,
		MaxBackoff: time.Minute,
	}
}

// parseRetryAfter reads a Retry-After header in seconds or in HTTP date format.
func parseRetryAfter(headers http.Header) time.Duration {
	value := strings.TrimSpace(headers.Get("Retry-After"))
	if value == "" {
		return 0
	}
	if seconds, err := strconv.Atoi(value); err == nil && seconds >= 0 {
		return time.Duration(seconds) * time.Second
	}
	if date, err := http.ParseTime(value); err == nil {
		if d := time.Until(date); d > 0 {
			return d
		}
	}
	return 0
}

// errorMessage keeps the start of an error body. Rails can send a full HTML page.
func errorMessage(body []byte) string {
	text := strings.Join(strings.Fields(string(body)), " ")
	if len(text) > 200 {
		text = text[:200] + "…"
	}
	return text
}
