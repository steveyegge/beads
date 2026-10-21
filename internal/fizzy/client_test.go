package fizzy

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func newTestClient(serverURL string) *Client {
	client := NewClient(testToken, serverURL, "/"+testAccount+"/", testBoard)
	client.Retry = RetryConfig{MaxRetries: 2}
	return client
}

func TestClientReadsAllPagesWithTheBoardFilter(t *testing.T) {
	f := newFakeFizzy(t)
	f.addCard(Card{Title: "one"})
	f.addCard(Card{Title: "two"})
	f.addCard(Card{Title: "three", Closed: true})

	cards, err := newTestClient(f.server.URL).FetchCards(context.Background(), IndexAll)
	if err != nil {
		t.Fatal(err)
	}
	if len(cards) != 2 || cards[0].Title != "one" || cards[1].Title != "two" {
		t.Errorf("FetchCards(all) = %+v", cards)
	}
}

func TestClientFetchCardNotFound(t *testing.T) {
	f := newFakeFizzy(t)
	card, err := newTestClient(f.server.URL).FetchCard(context.Background(), 42)
	if err != nil || card != nil {
		t.Errorf("FetchCard(42) = %+v, %v, want nil, nil", card, err)
	}
}

func TestClientRetriesRateLimitAndServerErrors(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch calls.Add(1) {
		case 1:
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
		case 2:
			w.WriteHeader(http.StatusBadGateway)
		default:
			_, _ = w.Write([]byte(`[{"id":"c1","name":"Doing"}]`))
		}
	}))
	defer server.Close()

	columns, err := newTestClient(server.URL).FetchColumns(context.Background())
	if err != nil || len(columns) != 1 || columns[0].Name != "Doing" {
		t.Fatalf("FetchColumns() = %+v, %v", columns, err)
	}
	if calls.Load() != 3 {
		t.Errorf("calls = %d, want 3", calls.Load())
	}
}

func TestClientReturnsRateLimitErrorAfterRetries(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "0")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()

	_, err := newTestClient(server.URL).FetchColumns(context.Background())
	var rateLimit *RateLimitError
	if !errors.As(err, &rateLimit) {
		t.Fatalf("error = %v, want a RateLimitError", err)
	}
}

func TestClientDoesNotRetryClientErrors(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"title":["can't be blank"]}`))
	}))
	defer server.Close()

	err := newTestClient(server.URL).Close(context.Background(), 4)
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusUnprocessableEntity || calls.Load() != 1 {
		t.Fatalf("error = %v, calls = %d", err, calls.Load())
	}
	if !strings.Contains(err.Error(), "/123/cards/4/closure") || strings.Contains(err.Error(), testToken) {
		t.Errorf("error message = %q", err)
	}
}

func TestParseRetryAfter(t *testing.T) {
	headers := http.Header{}
	headers.Set("Retry-After", "7")
	if got := parseRetryAfter(headers); got != 7*time.Second {
		t.Errorf("parseRetryAfter(7) = %v", got)
	}
	headers.Set("Retry-After", "soon")
	if got := parseRetryAfter(headers); got != 0 {
		t.Errorf("parseRetryAfter(soon) = %v", got)
	}
}
