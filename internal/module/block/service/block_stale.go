package blockservice

// block_stale.go — 全局块内容变更/删除后的 stale 传播。
// 传播器由编排层注入；未注入时只记日志，不阻断块本身的写入。

import (
	"context"
	"errors"

	blockmodel "go_wp/internal/module/block/model"
	"go_wp/pkg/logger"
)

// SetStalePropagator 注入全局块 stale 传播器（编排层在 page 装配后绑定，
// 打破「block 先于 page 装配」的顺序约束）。未注入时内容变更不传播。
func (s *Service) SetStalePropagator(p func(ctx context.Context, blockID string) error) {
	s.propagate = p
}

// propagateStale 块内容变更/删除后触发引用方 stale 传播。
// reuse_mode=template 的块不传播（docs/02-D §9）：插入时已复制 AST，页面持有独立副本，
// 源块修改不影响任何页面。传播器未注入或传播失败只记日志，不阻断保存/删除主流程。
func (s *Service) propagateStale(ctx context.Context, blockID, reuseMode string) {
	if reuseMode == blockmodel.ReuseTemplate {
		return // 一次性复制的片段不传播 stale
	}
	if s.propagate == nil {
		logger.Scene("block").With("block_id", blockID).Error(
			errors.New("stale 传播器未注入"),
			"全局块变更未传播 stale，请检查 dashboard 装配是否调用 RequireWiring",
		)
		return
	}
	if err := s.propagate(ctx, blockID); err != nil {
		logger.Scene("block").With("block_id", blockID).Error(err, "全局块 stale 传播失败")
	}
}
