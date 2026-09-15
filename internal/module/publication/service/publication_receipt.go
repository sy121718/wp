package pubservice

// publication_receipt.go — 两段式回执（pending 回执的扫描与回滚）。

import (
	"context"
	"errors"
	"strings"
	"time"

	pubenums "go_wp/internal/module/publication/enums"
	pubmodel "go_wp/internal/module/publication/model"

	"gorm.io/gorm"
)

// RollbackReceipts 启动恢复：全部 pending 回执标记 rolled_back，返回处理数量。
func (s *Service) RollbackReceipts(ctx context.Context) (count int64, err error) {
	now := time.Now().UTC()
	result := s.model.ReceiptDB(ctx).
		Where("receipt_state = ?", pubmodel.ReceiptPending).
		Updates(map[string]any{"receipt_state": pubmodel.ReceiptRolledBack, "completed_at": now})
	return result.RowsAffected, result.Error
}

func receiptAction(action, fallback string) string {
	if strings.TrimSpace(action) == "" {
		return fallback
	}
	return action
}

// errRouteOccupied 事务内占位冲突哨兵，外层映射为 pubenums.ErrRouteOccupied。
var errRouteOccupied = errors.New(pubenums.ErrRouteOccupied)

// receiptPayload 回执数据结构化序列化（替代手工拼接 JSON，避免特殊字符生成非法 jsonb）。
type receiptPayload struct {
	To string `json:"to,omitempty"`
}

func markReceipt(tx *gorm.DB, id int64, state string, now time.Time) error {
	return tx.Model(&pubmodel.ReceiptEntity{}).
		Where("id = ?", id).
		Updates(map[string]any{"receipt_state": state, "completed_at": now}).Error
}
