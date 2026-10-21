package fizzy

import (
	"fmt"

	"github.com/steveyegge/beads/internal/tracker"
	"github.com/steveyegge/beads/internal/types"
)

// fizzyFieldMapper implements tracker.FieldMapper for Fizzy cards.
type fizzyFieldMapper struct {
	config *MappingConfig
}

// PriorityToBeads reads a priority tag ("p0" to "p4"). Without a tag, the priority is 2.
func (m *fizzyFieldMapper) PriorityToBeads(trackerPriority interface{}) int {
	if tag, ok := trackerPriority.(string); ok {
		if match := priorityTagPattern.FindStringSubmatch(NormalizeTag(tag)); match != nil {
			return int(match[1][0] - '0')
		}
	}
	return 2
}

func (m *fizzyFieldMapper) PriorityToTracker(beadsPriority int) interface{} {
	return PriorityTag(beadsPriority)
}

// StatusToBeads maps a lane name to a beads status.
func (m *fizzyFieldMapper) StatusToBeads(trackerState interface{}) types.Status {
	if lane, ok := trackerState.(string); ok {
		return m.config.StatusForLane(lane)
	}
	return types.StatusOpen
}

// StatusToTracker maps a beads status to a lane name. A wip status without a
// status map gives "Maybe?" here, because the mapper does not know the columns.
func (m *fizzyFieldMapper) StatusToTracker(beadsStatus types.Status) interface{} {
	return m.config.LaneForStatus(beadsStatus, "")
}

func (m *fizzyFieldMapper) TypeToBeads(trackerType interface{}) types.IssueType {
	if name, ok := trackerType.(string); ok && name != "" {
		return types.IssueType(name)
	}
	return types.TypeTask
}

func (m *fizzyFieldMapper) TypeToTracker(beadsType types.IssueType) interface{} {
	return TypeTag(beadsType)
}

// IssueToBeads converts a card to a beads issue. It returns nil if the tracker issue has no card.
func (m *fizzyFieldMapper) IssueToBeads(ti *tracker.TrackerIssue) *tracker.IssueConversion {
	card, ok := ti.Raw.(*Card)
	if !ok || card == nil {
		return nil
	}
	priority, issueType, labels := SplitTags(card.Tags)
	if priority < 0 {
		priority = 2
	}
	if issueType == "" {
		issueType = types.TypeTask
	}
	issue := &types.Issue{
		Title:        card.Title,
		Description:  cardText(card),
		Priority:     priority,
		IssueType:    issueType,
		Status:       m.config.StatusForLane(CardLane(card)),
		Labels:       labels,
		SourceSystem: fmt.Sprintf("fizzy:%d", card.Number),
		CreatedAt:    card.CreatedAt,
		UpdatedAt:    card.LastActiveAt,
	}
	if card.Closed {
		closedAt := card.LastActiveAt
		issue.ClosedAt = &closedAt
	}
	return &tracker.IssueConversion{Issue: issue}
}

func (m *fizzyFieldMapper) IssueToTracker(issue *types.Issue) map[string]interface{} {
	return map[string]interface{}{
		"title":       issue.Title,
		"description": DescriptionToHTML(issue.Description),
		"tags":        TagsForIssue(issue),
		"lane":        m.config.LaneForStatus(issue.Status, ""),
	}
}
