package pubservice

// publication_receipt.go — 两段式回执（pending 回执的扫描与回滚）。

import (
	"context"
	"strings"
	"time"

	pubmodel "go_wp/internal/module/publication/model"
)

// RollbackReceipts 启动恢复：全部 pending 回执标记 rolled_back，返回处理数量。
func (s *Service) RollbackReceipts(ctx context.Context) (count int64, err error) {
	return s.model.RollbackPendingReceipts(ctx, time.Now().UTC())
}

func receiptAction(action, fallback string) string {
	if strings.TrimSpace(action) == "" {
		return fallback
	}
	return action
}

// errRouteOccupied 事务内占位冲突哨兵，外层映射为 pubenums.ErrRouteOccupied。
// 本体在 model 侧（pubmodel.ErrRouteOccupied）：路由/回执的 …Tx 具名方法在唯一键
// 冲突时归一返回它，service 这边只做「哨兵 → 用户文案」的映射，两侧共用同一哨兵，
// errors.Is 的匹配不会因为哨兵分居两包而失效。
var errRouteOccupied = pubmodel.ErrRouteOccupied

// receiptPayload 回执数据结构化序列化（替代手工拼接 JSON，避免特殊字符生成非法 jsonb）。
type receiptPayload struct {
	To string `json:"to,omitempty"`
}
