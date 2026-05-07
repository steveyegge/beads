package proxy

import "context"

type DatabaseProxy interface {
	Start(ctx context.Context) error
}
