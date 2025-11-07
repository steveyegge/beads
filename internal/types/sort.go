package types

import "strings"

// DefaultIssueSortOptions returns the default ordering for issue queries:
// priority ascending with updated timestamp fallback.
func DefaultIssueSortOptions() []IssueSortOption {
	return []IssueSortOption{
		{Field: SortFieldPriority, Direction: SortAsc},
		{Field: SortFieldUpdated, Direction: SortDesc},
	}
}

// ParseIssueSortOrder converts a comma-delimited string (e.g. "number-desc,title-asc")
// into a slice of IssueSortOption values. Unrecognised fields or directions are skipped.
func ParseIssueSortOrder(raw string) []IssueSortOption {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}

	parts := strings.Split(raw, ",")
	options := make([]IssueSortOption, 0, len(parts))
	seen := make(map[IssueSortField]bool)

	for _, part := range parts {
		token := strings.TrimSpace(part)
		if token == "" {
			continue
		}

		field, dir := splitSortToken(token)
		if field == "" || dir == "" {
			continue
		}

		sortField := mapSortField(field)
		if sortField == "" {
			continue
		}

		direction := mapSortDirection(dir)
		if direction == "" {
			continue
		}

		if seen[sortField] {
			continue
		}
		seen[sortField] = true

		options = append(options, IssueSortOption{
			Field:     sortField,
			Direction: direction,
		})
	}

	return options
}

// EncodeIssueSortOrder converts a slice of IssueSortOption values into a canonical
// string representation suitable for query parameters.
func EncodeIssueSortOrder(options []IssueSortOption) string {
	if len(options) == 0 {
		return ""
	}

	tokens := make([]string, 0, len(options))
	for _, opt := range options {
		field := encodeSortField(opt.Field)
		dir := encodeSortDirection(opt.Direction)
		if field == "" || dir == "" {
			continue
		}
		tokens = append(tokens, field+"-"+dir)
	}
	return strings.Join(tokens, ",")
}

func splitSortToken(token string) (string, string) {
	if idx := strings.IndexAny(token, ":-"); idx >= 0 {
		left := strings.TrimSpace(token[:idx])
		right := strings.TrimSpace(token[idx+1:])
		return strings.ToLower(left), strings.ToLower(right)
	}
	token = strings.ToLower(token)
	switch token {
	case "updatedasc":
		return "updated", "asc"
	case "updateddesc":
		return "updated", "desc"
	case "createdasc":
		return "created", "asc"
	case "createddesc":
		return "created", "desc"
	case "priorityasc":
		return "priority", "asc"
	case "prioritydesc":
		return "priority", "desc"
	case "titleasc":
		return "title", "asc"
	case "titledesc":
		return "title", "desc"
	default:
		return "", ""
	}
}

func mapSortField(raw string) IssueSortField {
	switch strings.ToLower(raw) {
	case "updated", "updated_at":
		return SortFieldUpdated
	case "created", "created_at":
		return SortFieldCreated
	case "priority":
		return SortFieldPriority
	case "title":
		return SortFieldTitle
	default:
		return ""
	}
}

func mapSortDirection(raw string) SortDirection {
	switch strings.ToLower(raw) {
	case "asc", "ascending":
		return SortAsc
	case "desc", "descending":
		return SortDesc
	default:
		return ""
	}
}

func encodeSortField(field IssueSortField) string {
	switch field {
	case SortFieldUpdated:
		return "updated"
	case SortFieldCreated:
		return "created"
	case SortFieldPriority:
		return "priority"
	case SortFieldTitle:
		return "title"
	default:
		return ""
	}
}

func encodeSortDirection(dir SortDirection) string {
	switch dir {
	case SortAsc:
		return "asc"
	case SortDesc:
		return "desc"
	default:
		return ""
	}
}
