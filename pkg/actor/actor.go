package actor

import "context"

type Actor struct {
	ID int64

	Name string
}

func (a Actor) System() bool { return a.ID == 0 && a.Name == "" }

type ctxKey struct{}

func WithActor(ctx context.Context, a Actor) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, ctxKey{}, a)
}

func From(ctx context.Context) Actor {
	if ctx == nil {
		return Actor{}
	}
	if a, ok := ctx.Value(ctxKey{}).(Actor); ok {
		return a
	}
	return Actor{}
}
