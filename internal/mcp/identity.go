package mcp

// identity.go — 把「谁在调用」带进工具的执行体。
//
// 执行体只拿得到一个 context 和一个参数结构体，而「操作人」这类字段**不能从
// 参数里读**：让请求方能填操作人，等于把审计留痕变成可伪造的字段（模型会照着
// 用户说的名字往里填，而它会填错）。所以它由执行器从请求上下文注入 ——
// 与后台 handler 从会话取操作人是同一条口径。
//
// 用独立类型作 key（而不是字符串）：字符串 key 在多个包之间会撞名，
// 而 context 的值一旦被别的包覆盖就是静默的错人 —— 这类错误在审计里看不出来。

import "context"

type mcpUserIDKey struct{}

// WithUserID 把调用者 id 放进 context（由 Runner 在执行前注入）。
func WithUserID(ctx context.Context, userID int64) context.Context {
	return context.WithValue(ctx, mcpUserIDKey{}, userID)
}

// UserIDFrom 取调用者 id；没有时返回 0。
//
// 返回 0 而不是报错：零值在调用方那里天然表示「没有操作人」，工具可以据此
// 决定拒执行还是按系统操作处理。返回错误会逼每个工具写一遍相同的判空。
func UserIDFrom(ctx context.Context) int64 {
	if ctx == nil {
		return 0
	}
	if v, ok := ctx.Value(mcpUserIDKey{}).(int64); ok {
		return v
	}
	return 0
}
