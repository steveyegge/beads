// Package fizzy provides a client and a tracker for Fizzy, the kanban tracker of 37signals.
//
// API reference: https://github.com/basecamp/fizzy/tree/main/docs/api
package fizzy

import (
	"net/http"
	"time"
)

const (
	// DefaultBaseURL is the hosted Fizzy service.
	DefaultBaseURL = "https://app.fizzy.do"

	// DefaultTimeout is the timeout of one HTTP request.
	DefaultTimeout = 30 * time.Second

	// MaxPages guards against a pagination loop.
	MaxPages = 1000

	// maxResponseSize limits the size of a response body.
	maxResponseSize = 50 * 1024 * 1024
)

// Card indexes of GET /:account/cards. The "all" index does not contain the
// closed cards and the cards in "Not Now".
const (
	IndexAll    = "all"
	IndexNotNow = "not_now"
	IndexClosed = "closed"
)

// Client calls the Fizzy API for one account and one board.
type Client struct {
	Token      string // Personal access token ("Read + Write" for push)
	BaseURL    string // For example "https://app.fizzy.do"
	Account    string // Account slug, for example "897362094"
	BoardID    string
	HTTPClient *http.Client
	Retry      RetryConfig
}

// Column is a workflow column of a board.
type Column struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Card is a Fizzy card. Column is nil when the card is in "Maybe?", "Not Now" or "Done".
type Card struct {
	ID              string    `json:"id"`
	Number          int       `json:"number"`
	Title           string    `json:"title"`
	Status          string    `json:"status"`      // "published" or "drafted"
	Description     string    `json:"description"` // plain text, without blank lines
	DescriptionHTML string    `json:"description_html"`
	Tags            []string  `json:"tags"`
	Closed          bool      `json:"closed"`
	Postponed       bool      `json:"postponed"`
	LastActiveAt    time.Time `json:"last_active_at"`
	CreatedAt       time.Time `json:"created_at"`
	URL             string    `json:"url"`
	Column          *Column   `json:"column"`
}
