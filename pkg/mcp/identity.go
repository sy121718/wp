package mcp

import "context"

type mcpUserIDKey struct{}

func WithUserID(ctx context.Context, userID int64) context.Context {
	return context.WithValue(ctx, mcpUserIDKey{}, userID)
}

func UserIDFrom(ctx context.Context) int64 {
	if ctx == nil {
		return 0
	}
	if v, ok := ctx.Value(mcpUserIDKey{}).(int64); ok {
		return v
	}
	return 0
}
