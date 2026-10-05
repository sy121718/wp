package aiservice

// ai_tool_idempotency.go — 把幂等台账接到 mcp 层的端口上。
//
// 为什么 adapter 在 service 而不在 model：`mcp.IdempotencyStore` 的返回类型是
// `mcp.Result`，而 model 层不该认识工具层（那是它的上层）。service 在这里的角色
// 是**装配方**——它同时看得见 model 与 mcp，转换就在这里发生一次。

import (
	"context"

	"go_wp/internal/mcp"
	aimodel "go_wp/internal/module/ai/model"
)

// ToolIdempotencyStore 实现 mcp.IdempotencyStore。
type ToolIdempotencyStore struct {
	m *aimodel.ToolIdempotencyModel
}

// NewToolIdempotencyStore 构造。
func NewToolIdempotencyStore(m *aimodel.ToolIdempotencyModel) *ToolIdempotencyStore {
	return &ToolIdempotencyStore{m: m}
}

// Lookup 取同一次意图上次的结果。
func (s *ToolIdempotencyStore) Lookup(ctx context.Context, tool, key string) (mcp.Result, bool, error) {
	if s == nil || s.m == nil {
		// 没接台账时**不阻断**写操作：接入缺失是装配问题，而把它变成
		// 「所有写工具都不可用」会让排查方向指向工具本身。
		// 代价是这段时间没有幂等保护 —— 这是有意的取舍，见装配处的断言。
		return mcp.Result{}, false, nil
	}
	row, ok, err := s.m.Lookup(ctx, tool, key)
	if err != nil || !ok {
		return mcp.Result{}, false, err
	}
	return mcp.Result{Text: row.ResultText}, true, nil
}

// Save 记下这次成功的结果。
func (s *ToolIdempotencyStore) Save(ctx context.Context, tool, key string, res mcp.Result) error {
	if s == nil || s.m == nil {
		return nil
	}
	return s.m.Save(ctx, tool, key, res.Text)
}
