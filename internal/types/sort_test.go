package types

import "testing"

func TestParseIssueSortOrder(t *testing.T) {
	opts := ParseIssueSortOrder("updated-desc,title-asc,priority-desc")
	if len(opts) != 3 {
		t.Fatalf("expected 3 options, got %d", len(opts))
	}
	if opts[0].Field != SortFieldUpdated || opts[0].Direction != SortDesc {
		t.Fatalf("unexpected first option %+v", opts[0])
	}
	if opts[1].Field != SortFieldTitle || opts[1].Direction != SortAsc {
		t.Fatalf("unexpected second option %+v", opts[1])
	}
	if opts[2].Field != SortFieldPriority || opts[2].Direction != SortDesc {
		t.Fatalf("unexpected third option %+v", opts[2])
	}
}

func TestParseIssueSortOrderSkipsInvalid(t *testing.T) {
	opts := ParseIssueSortOrder("unknown-desc,updated-ascending,,title-desc")
	if len(opts) != 2 {
		t.Fatalf("expected 2 valid options, got %d", len(opts))
	}
	if opts[0].Field != SortFieldUpdated || opts[0].Direction != SortAsc {
		t.Fatalf("unexpected updated option %+v", opts[0])
	}
	if opts[1].Field != SortFieldTitle || opts[1].Direction != SortDesc {
		t.Fatalf("unexpected title option %+v", opts[1])
	}
}

func TestEncodeIssueSortOrder(t *testing.T) {
	order := EncodeIssueSortOrder([]IssueSortOption{
		{Field: SortFieldUpdated, Direction: SortDesc},
		{Field: SortFieldTitle, Direction: SortAsc},
	})
	if order != "updated-desc,title-asc" {
		t.Fatalf("unexpected encoded order %q", order)
	}
}

func TestDefaultIssueSortOptions(t *testing.T) {
	defaults := DefaultIssueSortOptions()
	if len(defaults) != 2 {
		t.Fatalf("expected 2 defaults, got %d", len(defaults))
	}
	if defaults[0].Field != SortFieldPriority || defaults[0].Direction != SortAsc {
		t.Fatalf("unexpected primary default %+v", defaults[0])
	}
	if defaults[1].Field != SortFieldUpdated || defaults[1].Direction != SortDesc {
		t.Fatalf("unexpected fallback default %+v", defaults[1])
	}
}
