package fizzy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// linkNextPattern matches the "next" relation in the Link header. Fizzy keeps
// the query filters in the URL of the next page.
var linkNextPattern = regexp.MustCompile(`<([^>]+)>;\s*rel="next"`)

// NewClient creates a client for one account and one board.
func NewClient(token, baseURL, account, boardID string) *Client {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	return &Client{
		Token:      token,
		BaseURL:    strings.TrimRight(baseURL, "/"),
		Account:    strings.Trim(account, "/"),
		BoardID:    boardID,
		HTTPClient: &http.Client{Timeout: DefaultTimeout},
		Retry:      DefaultRetryConfig(),
	}
}

func (c *Client) accountURL(path string) string {
	return c.BaseURL + "/" + c.Account + path
}

func (c *Client) cardURL(number int, suffix string) string {
	return c.accountURL("/cards/" + strconv.Itoa(number) + suffix)
}

// FetchColumns returns the workflow columns of the board, sorted by position.
func (c *Client) FetchColumns(ctx context.Context) ([]Column, error) {
	return fetchAll[Column](ctx, c, c.accountURL("/boards/"+url.PathEscape(c.BoardID)+"/columns"))
}

// FetchCards returns the cards of the board for one index (IndexAll, IndexNotNow or IndexClosed).
func (c *Client) FetchCards(ctx context.Context, index string) ([]Card, error) {
	query := url.Values{"board_ids[]": {c.BoardID}, "indexed_by": {index}}
	return fetchAll[Card](ctx, c, c.accountURL("/cards?"+query.Encode()))
}

// FetchCard returns one card by its number. It returns nil, nil if the card does not exist.
func (c *Client) FetchCard(ctx context.Context, number int) (*Card, error) {
	var card Card
	if _, err := c.doJSON(ctx, http.MethodGet, c.cardURL(number, ""), nil, &card); err != nil {
		var apiErr *APIError
		if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &card, nil
}

// CreateCard creates a published card on the board. The description is HTML.
func (c *Client) CreateCard(ctx context.Context, title, description string) (*Card, error) {
	var card Card
	body := map[string]any{"card": map[string]any{"title": title, "description": description}}
	if _, err := c.doJSON(ctx, http.MethodPost, c.accountURL("/boards/"+url.PathEscape(c.BoardID)+"/cards"), body, &card); err != nil {
		return nil, err
	}
	return &card, nil
}

// UpdateCard changes the title and the description of a card. The description is HTML.
func (c *Client) UpdateCard(ctx context.Context, number int, title, description string) error {
	body := map[string]any{"card": map[string]any{"title": title, "description": description}}
	_, err := c.doJSON(ctx, http.MethodPut, c.cardURL(number, ""), body, nil)
	return err
}

// Triage moves a card into a workflow column. Fizzy also reopens a closed card
// and resumes a postponed card.
func (c *Client) Triage(ctx context.Context, number int, columnID string) error {
	_, err := c.doJSON(ctx, http.MethodPost, c.cardURL(number, "/triage"), map[string]any{"column_id": columnID}, nil)
	return err
}

// SendToTriage moves a card back to "Maybe?".
func (c *Client) SendToTriage(ctx context.Context, number int) error {
	_, err := c.doJSON(ctx, http.MethodDelete, c.cardURL(number, "/triage"), nil, nil)
	return err
}

// Postpone moves a card to "Not Now".
func (c *Client) Postpone(ctx context.Context, number int) error {
	_, err := c.doJSON(ctx, http.MethodPost, c.cardURL(number, "/not_now"), nil, nil)
	return err
}

// Close moves a card to "Done".
func (c *Client) Close(ctx context.Context, number int) error {
	_, err := c.doJSON(ctx, http.MethodPost, c.cardURL(number, "/closure"), nil, nil)
	return err
}

// Reopen removes a card from "Done".
func (c *Client) Reopen(ctx context.Context, number int) error {
	_, err := c.doJSON(ctx, http.MethodDelete, c.cardURL(number, "/closure"), nil, nil)
	return err
}

// ToggleTag adds the tag to the card, or removes it if the card has it.
// Fizzy creates a tag that does not exist.
func (c *Client) ToggleTag(ctx context.Context, number int, tag string) error {
	_, err := c.doJSON(ctx, http.MethodPost, c.cardURL(number, "/taggings"), map[string]any{"tag_title": tag}, nil)
	return err
}

// fetchAll reads all the pages of a list endpoint.
func fetchAll[T any](ctx context.Context, c *Client, address string) ([]T, error) {
	var items []T
	for page := 0; address != ""; page++ {
		if page >= MaxPages {
			return nil, fmt.Errorf("fizzy: more than %d pages for %s", MaxPages, address)
		}
		var batch []T
		headers, err := c.doJSON(ctx, http.MethodGet, address, nil, &batch)
		if err != nil {
			return nil, err
		}
		items = append(items, batch...)
		address = ""
		if match := linkNextPattern.FindStringSubmatch(headers.Get("Link")); match != nil {
			address = match[1]
		}
	}
	return items, nil
}

// doJSON sends a request, retries the 429 and 5xx responses, and decodes the JSON response into out.
func (c *Client) doJSON(ctx context.Context, method, address string, body, out any) (http.Header, error) {
	var payload []byte
	if body != nil {
		var err error
		if payload, err = json.Marshal(body); err != nil {
			return nil, fmt.Errorf("failed to marshal request body: %w", err)
		}
	}

	var lastErr error
	for attempt := 0; attempt <= c.Retry.MaxRetries; attempt++ {
		var reader io.Reader
		if payload != nil {
			reader = bytes.NewReader(payload)
		}
		req, err := http.NewRequestWithContext(ctx, method, address, reader)
		if err != nil {
			return nil, fmt.Errorf("failed to create request: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+c.Token)
		req.Header.Set("Accept", "application/json")
		if payload != nil {
			req.Header.Set("Content-Type", "application/json")
		}

		resp, err := c.HTTPClient.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("request failed (attempt %d/%d): %w", attempt+1, c.Retry.MaxRetries+1, err)
			if sleepErr := sleep(ctx, c.backoff(attempt)); sleepErr != nil {
				return nil, sleepErr
			}
			continue
		}
		respBody, readErr := io.ReadAll(io.LimitReader(resp.Body, maxResponseSize))
		_ = resp.Body.Close()
		if readErr != nil {
			lastErr = fmt.Errorf("failed to read response (attempt %d/%d): %w", attempt+1, c.Retry.MaxRetries+1, readErr)
			continue
		}

		switch {
		case resp.StatusCode >= 200 && resp.StatusCode < 300:
			if out != nil && len(bytes.TrimSpace(respBody)) > 0 {
				if err := json.Unmarshal(respBody, out); err != nil {
					return nil, fmt.Errorf("failed to decode response of %s %s: %w", method, req.URL.Path, err)
				}
			}
			return resp.Header, nil
		case resp.StatusCode == http.StatusTooManyRequests:
			rateLimit := &RateLimitError{StatusCode: resp.StatusCode, RetryAfter: parseRetryAfter(resp.Header), URL: req.URL.Path}
			lastErr = rateLimit
			if attempt >= c.Retry.MaxRetries {
				return nil, rateLimit
			}
			delay := rateLimit.RetryAfter
			if delay <= 0 {
				delay = c.backoff(attempt)
			}
			if err := sleep(ctx, delay); err != nil {
				return nil, err
			}
		case resp.StatusCode >= 500:
			lastErr = &APIError{StatusCode: resp.StatusCode, Method: method, URL: req.URL.Path, Message: errorMessage(respBody)}
			if err := sleep(ctx, c.backoff(attempt)); err != nil {
				return nil, err
			}
		default:
			return nil, &APIError{StatusCode: resp.StatusCode, Method: method, URL: req.URL.Path, Message: errorMessage(respBody)}
		}
	}
	return nil, fmt.Errorf("max retries (%d) exceeded: %w", c.Retry.MaxRetries+1, lastErr)
}

// backoff returns an exponential delay with jitter.
func (c *Client) backoff(attempt int) time.Duration {
	base := c.Retry.BaseDelay
	if base <= 0 {
		return 0
	}
	if attempt > 30 {
		attempt = 30 // guard against int shift overflow
	}
	delay := base * time.Duration(1<<attempt)
	if c.Retry.MaxBackoff > 0 && delay > c.Retry.MaxBackoff {
		delay = c.Retry.MaxBackoff
	}
	if half := int64(delay / 2); half > 0 {
		delay += time.Duration(rand.Int64N(half)) //nolint:gosec // jitter does not need crypto rand
	}
	return delay
}

func sleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
