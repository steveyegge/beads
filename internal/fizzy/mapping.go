package fizzy

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/steveyegge/beads/internal/types"
)

// Names of the fixed lanes of a Fizzy board. A value of fizzy.status_map.<status>
// is one of these names or the name of a workflow column.
const (
	LaneMaybe  = "Maybe?"
	LaneNotNow = "Not Now"
	LaneDone   = "Done"
)

// TypeTagPrefix marks the tag that holds the issue type, as the scoped
// "type::" labels of the GitHub and GitLab trackers.
const TypeTagPrefix = "type::"

// priorityTagPattern matches the priority tags "p0" to "p4".
var priorityTagPattern = regexp.MustCompile(`^p([0-4])$`)

// builtInStatusOrder gives the order of the reverse status lookup. If more
// than one status maps to a lane, a pull gives the first one.
var builtInStatusOrder = []types.Status{
	types.StatusOpen,
	types.StatusInProgress,
	types.StatusBlocked,
	types.StatusDeferred,
	types.StatusClosed,
	types.StatusPinned,
	types.StatusHooked,
}

// MappingConfig controls how beads fields map to Fizzy cards.
type MappingConfig struct {
	// StatusMap maps a beads status to a lane name (fizzy.status_map.<status>).
	StatusMap map[string]string
	// CustomStatuses gives the category of each custom status (status.custom).
	CustomStatuses map[string]types.StatusCategory
}

// DefaultMappingConfig returns a mapping without status map and without custom statuses.
func DefaultMappingConfig() *MappingConfig {
	return &MappingConfig{
		StatusMap:      map[string]string{},
		CustomStatuses: map[string]types.StatusCategory{},
	}
}

// category returns the category of a status. A status without a known category is active.
func (m *MappingConfig) category(status types.Status) types.StatusCategory {
	switch status {
	case types.StatusOpen:
		return types.CategoryActive
	case types.StatusInProgress, types.StatusBlocked, types.StatusHooked:
		return types.CategoryWIP
	case types.StatusDeferred, types.StatusPinned:
		return types.CategoryFrozen
	case types.StatusClosed:
		return types.CategoryDone
	}
	if category, ok := m.CustomStatuses[string(status)]; ok && category != types.CategoryUnspecified {
		return category
	}
	return types.CategoryActive
}

// mappedLane returns the lane of fizzy.status_map.<status>, if the key exists.
func (m *MappingConfig) mappedLane(status types.Status) (string, bool) {
	lane, ok := m.StatusMap[string(status)]
	lane = strings.TrimSpace(lane)
	return lane, ok && lane != ""
}

// LaneForStatus returns the lane of a beads status. firstColumn is the name of
// the first workflow column of the board, or "" if the board has no column.
//
// Without a status map, a done status goes to "Done", a frozen status goes to
// "Not Now", a wip status goes to the first workflow column and the other
// statuses go to "Maybe?".
func (m *MappingConfig) LaneForStatus(status types.Status, firstColumn string) string {
	if lane, ok := m.mappedLane(status); ok {
		return lane
	}
	switch m.category(status) {
	case types.CategoryDone:
		return LaneDone
	case types.CategoryFrozen:
		return LaneNotNow
	case types.CategoryWIP:
		if firstColumn != "" {
			return firstColumn
		}
	}
	return LaneMaybe
}

// StatusForLane returns the beads status of a lane: the first status in
// builtInStatusOrder (then the custom statuses, sorted) that the status map
// links to the lane. Without a match, "Done" gives closed, "Not Now" gives
// deferred, "Maybe?" gives open and a workflow column gives in_progress.
func (m *MappingConfig) StatusForLane(lane string) types.Status {
	for _, status := range m.statusOrder() {
		if mapped, ok := m.mappedLane(status); ok && strings.EqualFold(mapped, lane) {
			return status
		}
	}
	switch {
	case strings.EqualFold(lane, LaneDone):
		return types.StatusClosed
	case strings.EqualFold(lane, LaneNotNow):
		return types.StatusDeferred
	case strings.EqualFold(lane, LaneMaybe):
		return types.StatusOpen
	}
	return types.StatusInProgress
}

func (m *MappingConfig) statusOrder() []types.Status {
	order := slices.Clone(builtInStatusOrder)
	var custom []string
	for name := range m.StatusMap {
		if !slices.Contains(order, types.Status(name)) {
			custom = append(custom, name)
		}
	}
	sort.Strings(custom)
	for _, name := range custom {
		order = append(order, types.Status(name))
	}
	return order
}

// IsFixedLane tells if a lane is "Maybe?", "Not Now" or "Done".
func IsFixedLane(lane string) bool {
	return strings.EqualFold(lane, LaneMaybe) || strings.EqualFold(lane, LaneNotNow) || strings.EqualFold(lane, LaneDone)
}

// CardLane returns the lane of a card.
func CardLane(card *Card) string {
	switch {
	case card.Closed:
		return LaneDone
	case card.Postponed:
		return LaneNotNow
	case card.Column != nil:
		return card.Column.Name
	}
	return LaneMaybe
}

// NormalizeTag applies the rules of Fizzy to a tag title: lowercase, without a leading "#".
func NormalizeTag(tag string) string {
	return strings.ToLower(strings.TrimLeft(strings.TrimSpace(tag), "#"))
}

// PriorityTag returns the tag of a beads priority, from "p0" to "p4".
func PriorityTag(priority int) string {
	return fmt.Sprintf("p%d", min(max(priority, 0), 4))
}

// TypeTag returns the tag of a beads issue type, for example "type::bug".
func TypeTag(issueType types.IssueType) string {
	return TypeTagPrefix + strings.ToLower(string(issueType))
}

// TagsForIssue returns the tags of the card of an issue: the labels, the
// priority tag and the type tag, sorted and without duplicates.
func TagsForIssue(issue *types.Issue) []string {
	tags := []string{PriorityTag(issue.Priority)}
	if issue.IssueType != "" {
		tags = append(tags, TypeTag(issue.IssueType))
	}
	for _, label := range issue.Labels {
		tags = append(tags, NormalizeTag(label))
	}
	return sortedUnique(tags)
}

// SplitTags reads the tags of a card. It returns the priority (-1 if no
// priority tag), the issue type ("" if no type tag) and the other tags as
// labels. If a card has more than one priority or type tag, the first tag in
// sorted order wins, and the other ones are ignored.
func SplitTags(tags []string) (priority int, issueType types.IssueType, labels []string) {
	priority = -1
	for _, tag := range sortedUnique(normalizeTags(tags)) {
		if match := priorityTagPattern.FindStringSubmatch(tag); match != nil {
			if priority < 0 {
				priority = int(match[1][0] - '0')
			}
			continue
		}
		if name, ok := strings.CutPrefix(tag, TypeTagPrefix); ok {
			if issueType == "" && name != "" {
				issueType = types.IssueType(name)
			}
			continue
		}
		labels = append(labels, tag)
	}
	return priority, issueType, labels
}

// contentHash returns a fingerprint of the fields that a push sends: the
// title, the description, the tags and the lane.
func contentHash(issue *types.Issue, lane string) string {
	h := sha256.New()
	for _, part := range []string{issue.Title, CanonicalText(issue.Description), strings.Join(TagsForIssue(issue), "\x00"), strings.ToLower(lane)} {
		_, _ = h.Write([]byte(part))
		_, _ = h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

// sameTags tells if two tag lists contain the same tags, after normalization.
func sameTags(a, b []string) bool {
	return slices.Equal(sortedUnique(normalizeTags(a)), sortedUnique(normalizeTags(b)))
}

func normalizeTags(tags []string) []string {
	normalized := make([]string, 0, len(tags))
	for _, tag := range tags {
		normalized = append(normalized, NormalizeTag(tag))
	}
	return normalized
}

func sortedUnique(values []string) []string {
	values = slices.DeleteFunc(slices.Clone(values), func(value string) bool { return value == "" })
	slices.Sort(values)
	return slices.Compact(values)
}
