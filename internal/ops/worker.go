package ops

import "context"

type Worker interface {
	Name() string
	Start(ctx context.Context) error
}
