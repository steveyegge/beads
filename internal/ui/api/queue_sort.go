package api

import (
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/steveyegge/beads/internal/types"
)

// sortClosedQueueIssues orders issues for the closed queue by most recently closed.
func sortClosedQueueIssues(issues []*types.Issue) {
	if len(issues) == 0 {
		return
	}
	sort.SliceStable(issues, func(i, j int) bool {
		return closedQueueLess(issues[i], issues[j])
	})
}

func closedQueueLess(a, b *types.Issue) bool {
	if a == nil {
		return false
	}
	if b == nil {
		return true
	}

	timeA := closedSortTimestamp(a)
	timeB := closedSortTimestamp(b)
	if !timeA.Equal(timeB) {
		return timeA.After(timeB)
	}

	numA, hasNumA := extractNumericSuffix(a.ID)
	numB, hasNumB := extractNumericSuffix(b.ID)

	if hasNumA && hasNumB && numA != numB {
		return numA > numB
	}
	if hasNumA != hasNumB {
		return hasNumA
	}
	return strings.Compare(a.ID, b.ID) > 0
}

func closedSortTimestamp(issue *types.Issue) time.Time {
	if issue == nil {
		return time.Time{}
	}
	if issue.ClosedAt != nil && !issue.ClosedAt.IsZero() {
		return issue.ClosedAt.UTC()
	}
	if !issue.UpdatedAt.IsZero() {
		return issue.UpdatedAt.UTC()
	}
	return time.Time{}
}

func extractNumericSuffix(id string) (int, bool) {
	id = strings.TrimSpace(id)
	if id == "" {
		return 0, false
	}
	pos := len(id)
	for pos > 0 {
		r := id[pos-1]
		if r < '0' || r > '9' {
			break
		}
		pos--
	}
	if pos == len(id) {
		return 0, false
	}
	num, err := strconv.Atoi(id[pos:])
	if err != nil {
		return 0, false
	}
	return num, true
}
