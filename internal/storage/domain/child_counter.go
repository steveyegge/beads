package domain

import "context"

type ChildCounterSQLRepository interface {
	NextChildID(ctx context.Context, parentID string) (string, error)
}
