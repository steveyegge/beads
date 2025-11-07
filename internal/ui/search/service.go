package search

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/steveyegge/beads/internal/rpc"
	"github.com/steveyegge/beads/internal/types"
)

// ListClient matches the subset of rpc.Client needed for search indexing.
type ListClient interface {
	List(args *rpc.ListArgs) (*rpc.Response, error)
}

// Result describes a single search hit surfaced to the UI.
type Result struct {
	ID        string   `json:"id"`
	Title     string   `json:"title"`
	Status    string   `json:"status"`
	IssueType string   `json:"issue_type"`
	Priority  int      `json:"priority"`
	Labels    []string `json:"labels,omitempty"`
	Snippet   string   `json:"snippet,omitempty"`

	Score     float64   `json:"-"`
	UpdatedAt time.Time `json:"-"`
}

type SortMode string

const (
	SortRelevance SortMode = "relevance"
	SortRecent    SortMode = "recent"
	SortPriority  SortMode = "priority"
)

// ServiceOption configures optional behaviour for the Service.
type ServiceOption func(*Service)

// WithFetchLimit overrides the default number of records fetched per search refresh.
func WithFetchLimit(limit int) ServiceOption {
	return func(s *Service) {
		if limit > 0 {
			s.fetchLimit = limit
		}
	}
}

// WithCacheTTL overrides how long a cached index remains valid.
func WithCacheTTL(ttl time.Duration) ServiceOption {
	return func(s *Service) {
		if ttl >= 0 {
			s.cacheTTL = ttl
		}
	}
}

// WithClock injects a deterministic clock (used for testing).
func WithClock(clock func() time.Time) ServiceOption {
	return func(s *Service) {
		if clock != nil {
			s.now = clock
		}
	}
}

// Service builds and queries an in-memory index backed by the RPC list endpoint.
type Service struct {
	client     ListClient
	fetchLimit int
	cacheTTL   time.Duration
	now        func() time.Time

	mu    sync.RWMutex
	cache map[cacheKey]*cacheEntry
}

type cacheKey struct {
	status    string
	issueType string
	priority  string
	assignee  string
	labels    string
	labelsAny string
	limit     int
}

type cacheEntry struct {
	expires time.Time
	records []record
}

type record struct {
	id               string
	title            string
	status           string
	issueType        string
	priority         int
	labels           []string
	description      string
	lowerID          string
	lowerTitle       string
	lowerDescription string
	lowerLabels      []string
	updatedAt        time.Time
}

// NewService constructs a search service with sensible defaults.
func NewService(client ListClient, opts ...ServiceOption) *Service {
	svc := &Service{
		client:     client,
		fetchLimit: 100,
		cacheTTL:   30 * time.Second,
		now:        time.Now,
		cache:      make(map[cacheKey]*cacheEntry),
	}
	for _, opt := range opts {
		opt(svc)
	}
	return svc
}

// Search executes a fuzzy match against cached metadata, fetching from the RPC list
// endpoint when necessary. The limit applies after scoring has been computed.
func (s *Service) Search(ctx context.Context, rawQuery string, limit int, sortMode SortMode) ([]Result, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	mode := normalizeSortMode(sortMode, rawQuery)

	listArgs, terms := parseQuery(rawQuery)
	if listArgs.Limit == 0 {
		listArgs.Limit = s.fetchLimit
	}

	key := deriveCacheKey(listArgs, s.fetchLimit)
	records, err := s.loadRecords(ctx, key, listArgs)
	if err != nil {
		return nil, err
	}

	if limit <= 0 || limit > len(records) {
		limit = len(records)
	}

	results := make([]Result, 0, len(records))
	for _, rec := range records {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		score, matched := rankRecord(rec, terms)
		if len(terms) > 0 && !matched {
			continue
		}

		result := Result{
			ID:        rec.id,
			Title:     rec.title,
			Status:    rec.status,
			IssueType: rec.issueType,
			Priority:  rec.priority,
			Labels:    append([]string(nil), rec.labels...),
			Snippet:   buildSnippet(rec.description, terms),
			Score:     score,
			UpdatedAt: rec.updatedAt,
		}

		// Ensure results without a textual snippet still have a deterministic fallback.
		if result.Snippet == "" {
			result.Snippet = buildSnippet(rec.title, nil)
		}

		results = append(results, result)
	}

	applySort(results, mode)

	if limit < len(results) {
		results = results[:limit]
	}

	return results, nil
}

func normalizeSortMode(mode SortMode, query string) SortMode {
	raw := strings.TrimSpace(strings.ToLower(string(mode)))
	switch SortMode(raw) {
	case SortRecent, SortPriority, SortRelevance:
		return SortMode(raw)
	}
	if strings.TrimSpace(query) == "" {
		return SortRecent
	}
	return SortRelevance
}

func applySort(results []Result, mode SortMode) {
	switch mode {
	case SortRecent:
		sort.Slice(results, func(i, j int) bool {
			if !results[i].UpdatedAt.Equal(results[j].UpdatedAt) {
				return results[i].UpdatedAt.After(results[j].UpdatedAt)
			}
			if scoreDiff := results[i].Score - results[j].Score; scoreDiff != 0 {
				return scoreDiff > 0
			}
			return results[i].ID < results[j].ID
		})
	case SortPriority:
		sort.Slice(results, func(i, j int) bool {
			if results[i].Priority != results[j].Priority {
				return results[i].Priority < results[j].Priority
			}
			if scoreDiff := results[i].Score - results[j].Score; scoreDiff != 0 {
				return scoreDiff > 0
			}
			if !results[i].UpdatedAt.Equal(results[j].UpdatedAt) {
				return results[i].UpdatedAt.After(results[j].UpdatedAt)
			}
			return results[i].ID < results[j].ID
		})
	default:
		sort.Slice(results, func(i, j int) bool {
			if scoreDiff := results[i].Score - results[j].Score; scoreDiff != 0 {
				return scoreDiff > 0
			}
			if !results[i].UpdatedAt.Equal(results[j].UpdatedAt) {
				return results[i].UpdatedAt.After(results[j].UpdatedAt)
			}
			return results[i].ID < results[j].ID
		})
	}
}

func (s *Service) loadRecords(ctx context.Context, key cacheKey, args *rpc.ListArgs) ([]record, error) {
	now := s.now()

	s.mu.RLock()
	entry, ok := s.cache[key]
	if ok && (s.cacheTTL == 0 || now.Before(entry.expires)) {
		records := make([]record, len(entry.records))
		copy(records, entry.records)
		s.mu.RUnlock()
		return records, nil
	}
	s.mu.RUnlock()

	resp, err := s.client.List(args)
	if err != nil {
		return nil, fmt.Errorf("list issues: %w", err)
	}
	if resp == nil {
		return nil, fmt.Errorf("list issues: empty response")
	}
	if !resp.Success {
		if resp.Error != "" {
			return nil, fmt.Errorf("list issues: %s", resp.Error)
		}
		return nil, fmt.Errorf("list issues: unknown failure")
	}

	var issues []*types.Issue
	if err := json.Unmarshal(resp.Data, &issues); err != nil {
		return nil, fmt.Errorf("decode issues: %w", err)
	}

	records := make([]record, 0, len(issues))
	for _, issue := range issues {
		if issue == nil {
			continue
		}
		records = append(records, mapIssue(issue))
	}

	s.mu.Lock()
	if s.cache == nil {
		s.cache = make(map[cacheKey]*cacheEntry)
	}
	var expires time.Time
	if s.cacheTTL > 0 {
		expires = now.Add(s.cacheTTL)
	}
	s.cache[key] = &cacheEntry{
		expires: expires,
		records: append([]record(nil), records...),
	}
	s.mu.Unlock()

	return records, nil
}

func mapIssue(issue *types.Issue) record {
	labels := append([]string(nil), issue.Labels...)
	lowerLabels := make([]string, len(labels))
	for i, label := range labels {
		lowerLabels[i] = strings.ToLower(label)
	}
	desc := strings.TrimSpace(issue.Description)
	return record{
		id:               issue.ID,
		title:            issue.Title,
		status:           string(issue.Status),
		issueType:        string(issue.IssueType),
		priority:         issue.Priority,
		labels:           labels,
		description:      desc,
		lowerID:          strings.ToLower(issue.ID),
		lowerTitle:       strings.ToLower(issue.Title),
		lowerDescription: strings.ToLower(desc),
		lowerLabels:      lowerLabels,
		updatedAt:        issue.UpdatedAt,
	}
}

func deriveCacheKey(args *rpc.ListArgs, limit int) cacheKey {
	key := cacheKey{
		status:    strings.ToLower(strings.TrimSpace(args.Status)),
		issueType: strings.ToLower(strings.TrimSpace(args.IssueType)),
		assignee:  strings.ToLower(strings.TrimSpace(args.Assignee)),
		limit:     limit,
	}
	if args.Priority != nil {
		key.priority = fmt.Sprintf("%d", *args.Priority)
	}
	if len(args.Labels) > 0 {
		labels := append([]string(nil), args.Labels...)
		sort.Strings(labels)
		key.labels = strings.Join(labels, "|")
	}
	if len(args.LabelsAny) > 0 {
		labels := append([]string(nil), args.LabelsAny...)
		sort.Strings(labels)
		key.labelsAny = strings.Join(labels, "|")
	}
	return key
}

func parseQuery(raw string) (*rpc.ListArgs, []string) {
	args := &rpc.ListArgs{}
	var terms []string

	for _, token := range tokenizeQuery(raw) {
		if token == "" {
			continue
		}
		lower := strings.ToLower(token)

		switch {
		case strings.HasPrefix(lower, "is:"):
			if value := strings.TrimPrefix(lower, "is:"); value != "" {
				switch value {
				case "open", "ready":
					if args.Status == "" {
						args.Status = string(types.StatusOpen)
					}
				case "in_progress":
					args.Status = string(types.StatusInProgress)
				case "blocked":
					args.Status = string(types.StatusBlocked)
				case "closed", "done":
					args.Status = string(types.StatusClosed)
				}
			}
		case strings.HasPrefix(lower, "status:"):
			args.Status = strings.TrimPrefix(lower, "status:")
		case strings.HasPrefix(lower, "label:"):
			label := trimQuotes(token[len("label:"):])
			if label != "" {
				args.Labels = append(args.Labels, label)
			}
		case strings.HasPrefix(lower, "labels:"):
			label := trimQuotes(token[len("labels:"):])
			if label != "" {
				args.Labels = append(args.Labels, label)
			}
		case strings.HasPrefix(lower, "labels_any:"):
			label := trimQuotes(token[len("labels_any:"):])
			if label != "" {
				args.LabelsAny = append(args.LabelsAny, label)
			}
		case strings.HasPrefix(lower, "type:"):
			args.IssueType = strings.TrimPrefix(lower, "type:")
		case strings.HasPrefix(lower, "assignee:"):
			args.Assignee = trimQuotes(token[len("assignee:"):])
		case strings.HasPrefix(lower, "priority:"):
			if priority, err := parsePriority(strings.TrimPrefix(lower, "priority:")); err == nil {
				args.Priority = &priority
			}
		case strings.HasPrefix(lower, "queue:"):
			if args.Status == "" {
				switch strings.TrimPrefix(lower, "queue:") {
				case "ready":
					args.Status = string(types.StatusOpen)
				case "in_progress":
					args.Status = string(types.StatusInProgress)
				case "blocked":
					args.Status = string(types.StatusBlocked)
				case "recent":
					// Leave status unset; rely on updated ordering.
				}
			}
		default:
			if term := normalizeTerm(token); term != "" {
				terms = append(terms, term)
			}
		}
	}

	return args, terms
}

func tokenizeQuery(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}

	var tokens []string
	var buf strings.Builder
	var quote rune

	flush := func() {
		if buf.Len() == 0 {
			return
		}
		tokens = append(tokens, buf.String())
		buf.Reset()
	}

	for _, r := range raw {
		switch {
		case quote != 0:
			buf.WriteRune(r)
			if r == quote {
				quote = 0
			}
		case r == '"' || r == '\'':
			quote = r
			buf.WriteRune(r)
		case unicode.IsSpace(r):
			flush()
		default:
			buf.WriteRune(r)
		}
	}

	flush()
	return tokens
}

func trimQuotes(value string) string {
	value = strings.TrimSpace(value)
	value = strings.Trim(value, "\"'")
	return value
}

func parsePriority(raw string) (int, error) {
	switch raw {
	case "0", "1", "2", "3", "4":
		return int(raw[0] - '0'), nil
	}
	return 0, fmt.Errorf("invalid priority %q", raw)
}

func normalizeTerm(token string) string {
	normalized := strings.ToLower(strings.Trim(token, "\"'"))
	return normalized
}

func rankRecord(rec record, terms []string) (float64, bool) {
	if len(terms) == 0 {
		// Pure filter queries return all results with neutral score.
		return 0, true
	}

	var score float64
	var matched bool

	for _, term := range terms {
		if term == "" {
			continue
		}
		if sc, ok := fieldScore(term, rec.lowerID, 6); ok {
			score += sc
			matched = true
		}
		if sc, ok := fieldScore(term, rec.lowerTitle, 5); ok {
			score += sc
			matched = true
		}
		for _, label := range rec.lowerLabels {
			if sc, ok := fieldScore(term, label, 3); ok {
				score += sc
				matched = true
				break
			}
		}
		if sc, ok := fieldScore(term, rec.lowerDescription, 1); ok {
			score += sc
			matched = true
		}
	}

	return score, matched
}

func fieldScore(term, field string, weight float64) (float64, bool) {
	if field == "" || term == "" {
		return 0, false
	}

	switch {
	case field == term:
		return weight * 5, true
	case strings.HasPrefix(field, term):
		return weight * 4, true
	case strings.Contains(field, term):
		return weight * 3, true
	case isSubsequence(term, field):
		return weight * 2, true
	default:
		return 0, false
	}
}

func isSubsequence(term, field string) bool {
	if term == "" {
		return true
	}
	ri := 0
	runesTerm := []rune(term)
	for _, r := range field {
		if ri < len(runesTerm) && r == runesTerm[ri] {
			ri++
		}
		if ri == len(runesTerm) {
			return true
		}
	}
	return ri == len(runesTerm)
}

func buildSnippet(text string, terms []string) string {
	clean := strings.TrimSpace(text)
	if clean == "" {
		return ""
	}

	lower := strings.ToLower(clean)
	for _, term := range terms {
		if term == "" {
			continue
		}
		if idx := strings.Index(lower, term); idx >= 0 {
			start := idx - 40
			if start < 0 {
				start = 0
			}
			end := idx + len(term) + 40
			if end > len(clean) {
				end = len(clean)
			}
			snippet := strings.TrimSpace(clean[start:end])
			if start > 0 {
				snippet = "…" + snippet
			}
			if end < len(clean) {
				snippet = snippet + "…"
			}
			return snippet
		}
	}

	if len(clean) > 120 {
		return strings.TrimSpace(clean[:120]) + "…"
	}
	return clean
}
