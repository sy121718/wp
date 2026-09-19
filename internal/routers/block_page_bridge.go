package routers

// block_page_bridge.go — block ↔ page 的装配期桥接。
//
// 原是 dashboard NewHandle 内的两根接线柱（页面回迁后移到装配层）：装配层同时持有
// block 与 page/project 契约，是唯一合法的接线点 —— dashboard 在 page 之后装配
// 打破 block↔page 装配循环的历史职责，由这里接管。

import (
	"context"

	pagecontract "go_wp/internal/module/page/contract"
	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/internal/pipeline"
	"go_wp/pkg/logger"
)

// BlockStalePropagator 全局块内容变更/删除后的 stale 传播，覆盖三条引用路径：
//  1. 页眉/页脚槽位绑定该块的主题（project 契约 ListThemesByBlockID）→ 逐主题 MarkStaleForTheme；
//  2. core.globalref 引用（页面文档树内 "blockId" 节点）→ MarkStaleForBlock；
//  3. 页面级 settings.structure 页眉/页脚自选覆盖 → MarkStaleForBlock。
//
// 自动发布实例按真实构建依赖反查（含嵌套全局块）。
// 路径 2/3 由 page 契约按 blockID 反查；重叠命中同一页面时 stale=true 幂等，无妨。
func BlockStalePropagator(pages pagecontract.PageService, projects projectcontract.ProjectService, presentations pipeline.DependencyTarget) func(context.Context, string) error {
	return func(ctx context.Context, blockID string) error {
		themes, err := projects.ListThemesByBlockID(ctx, blockID)
		if err != nil {
			logger.Scene("block").With("block_id", blockID).Error(err, "反查绑定块的主题失败")
			return err
		}
		for _, t := range themes {
			if err := pages.MarkStaleForTheme(ctx, t.ID); err != nil {
				logger.Scene("block").With("block_id", blockID).With("theme_id", t.ID).Error(err, "标记页面待重建失败")
				return err
			}
		}
		if err := pages.MarkStaleForBlock(ctx, blockID); err != nil {
			logger.Scene("block").With("block_id", blockID).Error(err, "反查引用块页面标待重建失败")
			return err
		}
		dep := pipeline.BlockKey(blockID)
		if _, err := presentations.MarkStaleByDependency(ctx, dep.Kind, dep.Key); err != nil {
			logger.Scene("block").With("block_id", blockID).Error(err, "标记引用块的自动发布实例待重建失败")
			return err
		}
		return nil
	}
}

// BlockReferenceChecker 块删除前的引用检查：主题槽位绑定该块，或存在引用页面。
func BlockReferenceChecker(pages pagecontract.PageService, projects projectcontract.ProjectService) func(context.Context, string) (bool, error) {
	return func(ctx context.Context, blockID string) (bool, error) {
		themes, err := projects.ListThemesByBlockID(ctx, blockID)
		if err != nil {
			logger.Scene("block").With("block_id", blockID).Error(err, "反查绑定块的主题失败")
			return false, err
		}
		if len(themes) > 0 {
			return true, nil
		}
		count, err := pages.CountBlockReference(ctx, blockID)
		if err != nil {
			logger.Scene("block").With("block_id", blockID).Error(err, "统计引用块页面失败")
			return false, err
		}
		return count > 0, nil
	}
}
