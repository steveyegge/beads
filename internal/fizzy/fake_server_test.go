package fizzy

import (
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"net/http/httptest"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	testToken   = "test-token"
	testAccount = "123"
	testBoard   = "board-1"
)

// fakeFizzy is an in-memory Fizzy account with one board. It follows the API
// rules that the tracker depends on: the "all" index has no closed or
// postponed card, triage reopens and resumes a card, and taggings toggle a tag.
type fakeFizzy struct {
	t       *testing.T
	server  *httptest.Server
	mu      sync.Mutex
	columns []Column
	cards   map[int]*Card
	next    int
	clock   time.Time
	calls   []string // mutating calls, as "METHOD /path"
}

func newFakeFizzy(t *testing.T, columns ...string) *fakeFizzy {
	t.Helper()
	f := &fakeFizzy{t: t, cards: map[int]*Card{}, next: 1, clock: time.Now().UTC().Add(-time.Hour)}
	for index, name := range columns {
		f.columns = append(f.columns, Column{ID: fmt.Sprintf("col-%d", index+1), Name: name})
	}
	f.server = httptest.NewServer(http.HandlerFunc(f.handle))
	t.Cleanup(f.server.Close)
	return f
}

// touch moves the clock forward and returns the new time, as the last activity of a change.
func (f *fakeFizzy) touch() time.Time {
	f.clock = f.clock.Add(time.Second)
	return f.clock
}

// addCard adds a card as if a person created it in Fizzy.
func (f *fakeFizzy) addCard(card Card) *Card {
	f.mu.Lock()
	defer f.mu.Unlock()
	card.Number = f.next
	card.ID = fmt.Sprintf("card-%d", f.next)
	if card.Status == "" {
		card.Status = "published"
	}
	card.URL = fmt.Sprintf("%s/%s/cards/%d", f.server.URL, testAccount, f.next)
	if card.LastActiveAt.IsZero() {
		card.LastActiveAt = f.touch()
	}
	card.CreatedAt = card.LastActiveAt
	f.cards[f.next] = &card
	f.next++
	return &card
}

func (f *fakeFizzy) card(number int) *Card {
	f.mu.Lock()
	defer f.mu.Unlock()
	card := *f.cards[number]
	card.Tags = slices.Clone(card.Tags)
	return &card
}

// edit changes a card as a person does in Fizzy, with a last activity in the future.
func (f *fakeFizzy) edit(number int, change func(*Card)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	card := f.cards[number]
	change(card)
	card.LastActiveAt = time.Now().UTC().Add(time.Minute)
}

func (f *fakeFizzy) mutations() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.calls)
}

func (f *fakeFizzy) resetMutations() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = nil
}

var cardPathPattern = regexp.MustCompile(`^/` + testAccount + `/cards/(\d+)(/[a-z_]+)?$`)

func (f *fakeFizzy) handle(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") != "Bearer "+testToken {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if r.Method != http.MethodGet {
		f.calls = append(f.calls, r.Method+" "+r.URL.Path)
	}

	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/"+testAccount+"/boards/"+testBoard+"/columns":
		f.writeJSON(w, http.StatusOK, f.columns)
	case r.Method == http.MethodGet && r.URL.Path == "/"+testAccount+"/cards":
		f.listCards(w, r)
	case r.Method == http.MethodPost && r.URL.Path == "/"+testAccount+"/boards/"+testBoard+"/cards":
		var body struct {
			Card struct{ Title, Description string }
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		number := f.next
		f.next++
		now := f.touch()
		card := &Card{ID: fmt.Sprintf("card-%d", number), Number: number, Title: body.Card.Title, Status: "published",
			Description: plainText(body.Card.Description), DescriptionHTML: richText(body.Card.Description),
			Tags: []string{}, LastActiveAt: now, CreatedAt: now,
			URL: fmt.Sprintf("%s/%s/cards/%d", f.server.URL, testAccount, number)}
		f.cards[number] = card
		f.writeJSON(w, http.StatusCreated, card)
	default:
		match := cardPathPattern.FindStringSubmatch(r.URL.Path)
		if match == nil {
			http.NotFound(w, r)
			return
		}
		number, _ := strconv.Atoi(match[1])
		card := f.cards[number]
		if card == nil {
			http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
			return
		}
		f.cardAction(w, r, card, r.Method+" "+match[2])
	}
}

func (f *fakeFizzy) listCards(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("board_ids[]") != testBoard {
		f.t.Errorf("card list without the board filter: %s", r.URL)
	}
	index := r.URL.Query().Get("indexed_by")
	var cards []*Card
	for _, card := range f.cards {
		switch index {
		case IndexClosed:
			if card.Closed {
				cards = append(cards, card)
			}
		case IndexNotNow:
			if card.Postponed && !card.Closed {
				cards = append(cards, card)
			}
		default:
			if !card.Closed && !card.Postponed {
				cards = append(cards, card)
			}
		}
	}
	slices.SortFunc(cards, func(a, b *Card) int { return a.Number - b.Number })
	// One card for each page, so that the tests use the pagination.
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	if page < len(cards) {
		next := *r.URL
		query := next.Query()
		query.Set("page", strconv.Itoa(page+1))
		next.RawQuery = query.Encode()
		w.Header().Set("Link", fmt.Sprintf(`<%s%s>; rel="next"`, f.server.URL, next.RequestURI()))
	}
	if page > len(cards) {
		f.writeJSON(w, http.StatusOK, []*Card{})
		return
	}
	f.writeJSON(w, http.StatusOK, cards[page-1:page])
}

func (f *fakeFizzy) cardAction(w http.ResponseWriter, r *http.Request, card *Card, action string) {
	switch action {
	case "GET ":
		f.writeJSON(w, http.StatusOK, card)
		return
	case "PUT ":
		var body struct {
			Card struct{ Title, Description string }
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		card.Title = body.Card.Title
		card.Description = plainText(body.Card.Description)
		card.DescriptionHTML = richText(body.Card.Description)
		card.LastActiveAt = f.touch()
		f.writeJSON(w, http.StatusOK, card)
		return
	case "POST /taggings":
		var body struct {
			TagTitle string `json:"tag_title"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		tag := strings.ToLower(strings.TrimLeft(body.TagTitle, "#")) // the rule of Fizzy for tag titles
		if index := slices.Index(card.Tags, tag); index >= 0 {
			card.Tags = slices.Delete(card.Tags, index, index+1)
		} else {
			card.Tags = append(card.Tags, tag)
		}
	case "POST /triage":
		var body struct {
			ColumnID string `json:"column_id"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		column := f.column(body.ColumnID)
		if column == nil {
			http.Error(w, `{"error":"no column"}`, http.StatusUnprocessableEntity)
			return
		}
		card.Column, card.Closed, card.Postponed = column, false, false
	case "DELETE /triage":
		card.Column, card.Postponed = nil, false
	case "POST /not_now":
		card.Column, card.Postponed = nil, true
	case "POST /closure":
		card.Column, card.Closed, card.Postponed = nil, true, false
	case "DELETE /closure":
		card.Closed = false
	default:
		http.NotFound(w, r)
		return
	}
	card.LastActiveAt = f.touch()
	w.WriteHeader(http.StatusNoContent)
}

func (f *fakeFizzy) column(id string) *Column {
	for index := range f.columns {
		if f.columns[index].ID == id {
			column := f.columns[index]
			return &column
		}
	}
	return nil
}

func (f *fakeFizzy) writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		f.t.Errorf("encode response: %v", err)
	}
}

// plainText converts HTML to plain text as Fizzy does for "description": the
// paragraphs are joined, so the blank lines are lost.
func plainText(description string) string {
	text := strings.NewReplacer("<p><br></p>", "", "<br>", "\n", "</p><p>", "\n", "<p>", "", "</p>", "").Replace(description)
	return html.UnescapeString(text)
}

// richText wraps HTML as Fizzy does for "description_html", with the white
// space and the comments of the view templates.
func richText(description string) string {
	if description == "" {
		return ""
	}
	return "<!-- BEGIN app/views/layouts/action_text/contents/_content.html.erb -->" +
		"<div class=\"action-text-content\">\n  " + description + "\n</div>\n<!-- END -->"
}
