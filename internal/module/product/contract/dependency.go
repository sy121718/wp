package productcontract

import "context"

type DependencyInvalidator interface {
	Invalidate(ctx context.Context, kind, key string)
}
