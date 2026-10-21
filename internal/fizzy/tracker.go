package fizzy

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/steveyegge/beads/internal/config"
	"github.com/steveyegge/beads/internal/tracker"
	"github.com/steveyegge/beads/internal/types"
)

func init() {
	tracker.Register("fizzy", func() tracker.IssueTracker {
		return &Tracker{}
	})
}

// cardRefPattern matches the card part of a ref: .../<account>/cards/<number>
var cardRefPattern = regexp.MustCompile(`/([^/]+)/cards/([1-9]\d*)$`)

// shorthandPattern matches the "fizzy:<number>" shorthand.
var shorthandPattern = regexp.MustCompile(`^fizzy:([1-9]\d*)$`)

// Tracker implements tracker.IssueTracker for one Fizzy board.
type Tracker struct {
	client  *Client
	config  *MappingConfig
	store   tracker.Store
	columns []Column // loaded on first use
}

func (t *Tracker) Name() string         { return "fizzy" }
func (t *Tracker) DisplayName() string  { return "Fizzy" }
func (t *Tracker) ConfigPrefix() string { return "fizzy" }

// Init reads the configuration: fizzy.token, fizzy.url, fizzy.account,
// fizzy.board, fizzy.status_map.<status> and status.custom.
func (t *Tracker) Init(ctx context.Context, store tracker.Store) error {
	t.store = store

	token := t.getConfig(ctx, "fizzy.token", "FIZZY_TOKEN")
	if token == "" {
		return fmt.Errorf("Fizzy token not configured (set fizzy.token or FIZZY_TOKEN)")
	}
	account := t.getConfig(ctx, "fizzy.account", "FIZZY_ACCOUNT")
	if account == "" {
		return fmt.Errorf("Fizzy account not configured (set fizzy.account or FIZZY_ACCOUNT)")
	}
	board := t.getConfig(ctx, "fizzy.board", "FIZZY_BOARD")
	if board == "" {
		return fmt.Errorf("Fizzy board not configured (set fizzy.board or FIZZY_BOARD)")
	}
	t.client = NewClient(token, t.getConfig(ctx, "fizzy.url", "FIZZY_URL"), account, board)

	t.config = DefaultMappingConfig()
	if allConfig, err := store.GetAllConfig(ctx); err == nil {
		const statusPrefix = "fizzy.status_map."
		for key, value := range allConfig {
			if status, ok := strings.CutPrefix(key, statusPrefix); ok && strings.TrimSpace(value) != "" {
				t.config.StatusMap[status] = strings.TrimSpace(value)
			}
		}
		if custom, err := types.ParseCustomStatusConfig(allConfig["status.custom"]); err == nil {
			for _, status := range custom {
				t.config.CustomStatuses[status.Name] = status.Category
			}
		}
	}
	return nil
}

func (t *Tracker) Validate() error {
	if t.client == nil {
		return fmt.Errorf("Fizzy tracker not initialized")
	}
	return nil
}

func (t *Tracker) Close() error { return nil }

// FetchIssues returns the published cards of the board. Fizzy has no filter on
// the update time, so the tracker filters the cards on last_active_at.
func (t *Tracker) FetchIssues(ctx context.Context, opts tracker.FetchOptions) ([]tracker.TrackerIssue, error) {
	if _, err := t.loadColumns(ctx); err != nil {
		return nil, err
	}

	var indexes []string
	switch opts.State {
	case "open":
		indexes = []string{IndexAll, IndexNotNow}
	case "closed":
		indexes = []string{IndexClosed}
	default:
		indexes = []string{IndexAll, IndexNotNow, IndexClosed}
	}

	var result []tracker.TrackerIssue
	seen := map[int]bool{}
	for _, index := range indexes {
		cards, err := t.client.FetchCards(ctx, index)
		if err != nil {
			return nil, err
		}
		for i := range cards {
			card := &cards[i]
			if seen[card.Number] || card.Status == "drafted" {
				continue
			}
			if opts.Since != nil && card.LastActiveAt.Before(*opts.Since) {
				continue
			}
			seen[card.Number] = true
			result = append(result, t.cardToTrackerIssue(card))
			if opts.Limit > 0 && len(result) >= opts.Limit {
				return result, nil
			}
		}
	}
	return result, nil
}

// FetchIssue returns one card by its number. It returns nil, nil if the card does not exist.
func (t *Tracker) FetchIssue(ctx context.Context, identifier string) (*tracker.TrackerIssue, error) {
	number, err := strconv.Atoi(identifier)
	if err != nil {
		return nil, fmt.Errorf("invalid Fizzy card number %q: %w", identifier, err)
	}
	if _, err := t.loadColumns(ctx); err != nil {
		return nil, err
	}
	card, err := t.client.FetchCard(ctx, number)
	if err != nil || card == nil {
		return nil, err
	}
	ti := t.cardToTrackerIssue(card)
	return &ti, nil
}

// CreateIssue creates a card, then sets its tags and its lane. If a step after
// the creation does not complete, the card stays and the result has a warning.
func (t *Tracker) CreateIssue(ctx context.Context, issue *types.Issue) (*tracker.TrackerIssue, error) {
	lane, err := t.laneForStatus(ctx, issue.Status)
	if err != nil {
		return nil, err
	}
	card, err := t.client.CreateCard(ctx, issue.Title, DescriptionToHTML(issue.Description))
	if err != nil {
		return nil, err
	}

	var warnings []string
	if err := t.applyTagsAndLane(ctx, card, TagsForIssue(issue), lane); err != nil {
		warnings = append(warnings, fmt.Sprintf("Fizzy card %d was created, but its tags or lane were not set: %v", card.Number, err))
	}
	ti, err := t.refreshedTrackerIssue(ctx, card)
	if err != nil {
		return nil, err
	}
	ti.Warnings = append(ti.Warnings, warnings...)
	return ti, nil
}

// UpdateIssue changes the title, the description, the tags and the lane of a card.
// The tags of the card become the tags of the issue.
func (t *Tracker) UpdateIssue(ctx context.Context, externalID string, issue *types.Issue) (*tracker.TrackerIssue, error) {
	number, err := strconv.Atoi(externalID)
	if err != nil {
		return nil, fmt.Errorf("invalid Fizzy card number %q: %w", externalID, err)
	}
	lane, err := t.laneForStatus(ctx, issue.Status)
	if err != nil {
		return nil, err
	}
	card, err := t.client.FetchCard(ctx, number)
	if err != nil {
		return nil, err
	}
	if card == nil {
		return nil, fmt.Errorf("Fizzy card %d not found", number)
	}

	if card.Title != issue.Title || cardText(card) != CanonicalText(issue.Description) {
		if err := t.client.UpdateCard(ctx, number, issue.Title, DescriptionToHTML(issue.Description)); err != nil {
			return nil, err
		}
	}
	if err := t.applyTagsAndLane(ctx, card, TagsForIssue(issue), lane); err != nil {
		return nil, err
	}
	return t.refreshedTrackerIssue(ctx, card)
}

// pushFieldsEqual tells if a card already has the fields that a push of the issue sends.
func (t *Tracker) pushFieldsEqual(ctx context.Context, issue *types.Issue, card *Card) bool {
	if issue == nil || card == nil {
		return false
	}
	lane, err := t.laneForStatus(ctx, issue.Status)
	if err != nil {
		return false
	}
	return card.Title == issue.Title &&
		cardText(card) == CanonicalText(issue.Description) &&
		sameTags(card.Tags, TagsForIssue(issue)) &&
		strings.EqualFold(CardLane(card), lane)
}

// pushContentHash returns the fingerprint of the fields that a push of the issue sends.
func (t *Tracker) pushContentHash(ctx context.Context, issue *types.Issue) string {
	if issue == nil {
		return ""
	}
	lane, err := t.laneForStatus(ctx, issue.Status)
	if err != nil {
		return ""
	}
	return contentHash(issue, lane)
}

// pushTargetScope identifies the board. A change of URL, account or board
// invalidates the stored push hashes.
func (t *Tracker) pushTargetScope() string {
	if t.client == nil {
		return ""
	}
	return strings.ToLower(t.client.BaseURL) + "/" + t.client.Account + "/boards/" + t.client.BoardID
}

// keepLocalFields keeps the beads fields that a card does not hold, or holds
// with less detail, when a pull updates an existing issue:
//   - The assignee, because Fizzy users do not map to beads assignees.
//   - The status, if the card is still in the lane of the local status.
//   - The priority and the type, if the card has no priority or type tag.
//   - The description, if it differs only in line endings or in trailing white space.
//   - The labels, if they differ only in case.
func (t *Tracker) keepLocalFields(ctx context.Context, card *Card, pulled, existing *types.Issue) {
	if card == nil || pulled == nil || existing == nil {
		return
	}
	pulled.Assignee = existing.Assignee

	if lane, err := t.laneForStatus(ctx, existing.Status); err == nil && strings.EqualFold(lane, CardLane(card)) {
		pulled.Status = existing.Status
	}
	priority, issueType, _ := SplitTags(card.Tags)
	if priority < 0 {
		pulled.Priority = existing.Priority
	}
	if issueType == "" {
		pulled.IssueType = existing.IssueType
	}
	if CanonicalText(pulled.Description) == CanonicalText(existing.Description) {
		pulled.Description = existing.Description
	}
	if sameTags(pulled.Labels, existing.Labels) {
		pulled.Labels = existing.Labels
	}
}

// IsExternalRef tells if a ref is a card of the configured Fizzy account, or a "fizzy:<number>" shorthand.
func (t *Tracker) IsExternalRef(ref string) bool {
	if shorthandPattern.MatchString(ref) {
		return true
	}
	if t.client == nil {
		return false
	}
	prefix := strings.ToLower(t.client.BaseURL + "/" + t.client.Account + "/cards/")
	return strings.HasPrefix(strings.ToLower(ref), prefix) && cardRefPattern.MatchString(ref)
}

// ExtractIdentifier returns the card number of a ref.
func (t *Tracker) ExtractIdentifier(ref string) string {
	if match := shorthandPattern.FindStringSubmatch(ref); match != nil {
		return match[1]
	}
	if match := cardRefPattern.FindStringSubmatch(ref); match != nil {
		return match[2]
	}
	return ""
}

// BuildExternalRef returns the URL of the card under the configured base URL.
// The ref does not use the URL that Fizzy sends, because Fizzy builds it from
// its own BASE_URL, and the pull matches refs exactly.
func (t *Tracker) BuildExternalRef(issue *tracker.TrackerIssue) string {
	if t.client == nil || issue.Identifier == "" {
		return "fizzy:" + issue.Identifier
	}
	return t.client.BaseURL + "/" + t.client.Account + "/cards/" + issue.Identifier
}

func (t *Tracker) FieldMapper() tracker.FieldMapper {
	return &fizzyFieldMapper{config: t.config}
}

// loadColumns reads the workflow columns of the board one time.
func (t *Tracker) loadColumns(ctx context.Context) ([]Column, error) {
	if t.columns != nil {
		return t.columns, nil
	}
	columns, err := t.client.FetchColumns(ctx)
	if err != nil {
		return nil, fmt.Errorf("reading the columns of Fizzy board %s: %w", t.client.BoardID, err)
	}
	t.columns = append([]Column{}, columns...)
	return t.columns, nil
}

// laneForStatus returns the lane of a status, and makes sure that a workflow column exists.
func (t *Tracker) laneForStatus(ctx context.Context, status types.Status) (string, error) {
	columns, err := t.loadColumns(ctx)
	if err != nil {
		return "", err
	}
	firstColumn := ""
	if len(columns) > 0 {
		firstColumn = columns[0].Name
	}
	lane := t.config.LaneForStatus(status, firstColumn)
	if !IsFixedLane(lane) && t.columnID(lane) == "" {
		return "", fmt.Errorf("Fizzy board %s has no column %q (fizzy.status_map.%s)", t.client.BoardID, lane, status)
	}
	return lane, nil
}

func (t *Tracker) columnID(name string) string {
	for _, column := range t.columns {
		if strings.EqualFold(column.Name, name) {
			return column.ID
		}
	}
	return ""
}

// applyTagsAndLane makes the tags of the card equal to tags, then moves the card to the lane.
func (t *Tracker) applyTagsAndLane(ctx context.Context, card *Card, tags []string, lane string) error {
	current := map[string]bool{}
	for _, tag := range card.Tags {
		current[NormalizeTag(tag)] = true
	}
	desired := map[string]bool{}
	var errs []error
	for _, tag := range tags {
		desired[tag] = true
		if !current[tag] {
			errs = append(errs, t.client.ToggleTag(ctx, card.Number, tag))
		}
	}
	for tag := range current {
		if !desired[tag] {
			errs = append(errs, t.client.ToggleTag(ctx, card.Number, tag))
		}
	}
	errs = append(errs, t.moveCard(ctx, card, lane))
	return errors.Join(errs...)
}

// moveCard puts a card in a lane. Triage also reopens a closed card and resumes a postponed card.
func (t *Tracker) moveCard(ctx context.Context, card *Card, lane string) error {
	if strings.EqualFold(CardLane(card), lane) {
		return nil
	}
	switch {
	case strings.EqualFold(lane, LaneDone):
		return t.client.Close(ctx, card.Number)
	case strings.EqualFold(lane, LaneNotNow):
		if card.Closed {
			if err := t.client.Reopen(ctx, card.Number); err != nil {
				return err
			}
		}
		return t.client.Postpone(ctx, card.Number)
	case strings.EqualFold(lane, LaneMaybe):
		if card.Closed {
			if err := t.client.Reopen(ctx, card.Number); err != nil {
				return err
			}
		}
		return t.client.SendToTriage(ctx, card.Number)
	}
	return t.client.Triage(ctx, card.Number, t.columnID(lane))
}

func (t *Tracker) refreshedTrackerIssue(ctx context.Context, card *Card) (*tracker.TrackerIssue, error) {
	refreshed, err := t.client.FetchCard(ctx, card.Number)
	if err != nil {
		return nil, err
	}
	if refreshed == nil {
		refreshed = card
	}
	ti := t.cardToTrackerIssue(refreshed)
	return &ti, nil
}

func (t *Tracker) cardToTrackerIssue(card *Card) tracker.TrackerIssue {
	priority, issueType, labels := SplitTags(card.Tags)
	if priority < 0 {
		priority = 2
	}
	ti := tracker.TrackerIssue{
		ID:          card.ID,
		Identifier:  strconv.Itoa(card.Number),
		URL:         card.URL,
		Title:       card.Title,
		Description: cardText(card),
		Priority:    priority,
		State:       CardLane(card),
		Type:        string(issueType),
		Labels:      labels,
		CreatedAt:   card.CreatedAt,
		UpdatedAt:   card.LastActiveAt,
		Raw:         card,
	}
	if card.Closed {
		completed := card.LastActiveAt
		ti.CompletedAt = &completed
	}
	return ti
}

// getConfig reads a config value from storage, then from the environment.
// The token is a yaml-only key: it stays in config.yaml, out of the Dolt database.
func (t *Tracker) getConfig(ctx context.Context, key, envVar string) string {
	if config.IsYamlOnlyKey(key) {
		if value := config.GetString(key); value != "" {
			return value
		}
		return os.Getenv(envVar)
	}
	if value, err := t.store.GetConfig(ctx, key); err == nil && value != "" {
		return value
	}
	return os.Getenv(envVar)
}

// ValidateBaseURL refuses a URL without a host, and http:// for a host that is not local.
func ValidateBaseURL(raw string) error {
	if raw == "" {
		return nil
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return fmt.Errorf("fizzy.url %q is not a valid URL", raw)
	}
	switch u.Scheme {
	case "https":
		return nil
	case "http":
		host := u.Hostname()
		if host == "localhost" || host == "127.0.0.1" || host == "::1" || strings.HasSuffix(host, ".localhost") {
			return nil
		}
		return fmt.Errorf("fizzy.url %q must use https (http is accepted only for localhost)", raw)
	}
	return fmt.Errorf("fizzy.url %q must use https", raw)
}

// PushHooks returns the push hooks of the sync engine. ContentEqual and
// ContentHash let the engine skip a card that already has the fields of the issue.
func (t *Tracker) PushHooks(ctx context.Context) *tracker.PushHooks {
	return &tracker.PushHooks{
		ContentEqual: func(local *types.Issue, remote *tracker.TrackerIssue) bool {
			if remote == nil {
				return false
			}
			card, ok := remote.Raw.(*Card)
			return ok && t.pushFieldsEqual(ctx, local, card)
		},
		ContentHash: func(local *types.Issue) string {
			return t.pushContentHash(ctx, local)
		},
		TargetScope: t.pushTargetScope,
	}
}

// PullHooks returns the pull hooks of the sync engine. generateID gives an ID
// to an issue that a pull creates. AfterConvert keeps the local fields that a
// card does not hold (see keepLocalFields).
func (t *Tracker) PullHooks(generateID func(context.Context, *types.Issue) error) *tracker.PullHooks {
	return &tracker.PullHooks{
		GenerateID: generateID,
		AfterConvert: func(ctx context.Context, extIssue *tracker.TrackerIssue, conv *tracker.IssueConversion, _ string, existing *types.Issue, _ tracker.SyncOptions) error {
			if existing == nil || conv == nil || extIssue == nil {
				return nil
			}
			if card, ok := extIssue.Raw.(*Card); ok {
				t.keepLocalFields(ctx, card, conv.Issue, existing)
			}
			return nil
		},
	}
}
