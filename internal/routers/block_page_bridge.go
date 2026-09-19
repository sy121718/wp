package routers

// block_page_bridge.go — block ↔ page 的装配期桥接。
//
// 原是 dashboard NewHandle 内的两根接线柱（页面回迁后移到装配层）：装配层同时持有
// block 与 page/project 契约，是唯一合法的接线点 —— dashboard 在 page 之后装配
// 打破 block↔page 装配循环的历史职责，由这里接管。
//
// ARCH-02（2026-09）起这里同时是「块引用归属」的合并点：引用判据由各模块自己的
// 只读契约提供（它们才认识自己的表与文档列），本文件只负责把它们拼成一份清单。
// 合并放这里而不是放进 block 模块：block 不能 import 其它模块的 service/model，
// 而做成「端口注入」会把「有哪些引用来源」这条知识散到两处 —— 漏接一类就是静默放行。

import (
	"context"

	blockcontract "go_wp/internal/module/block/contract"
	contenttemplatecontract "go_wp/internal/module/contenttemplate/contract"
	pagecontract "go_wp/internal/module/page/contract"
	presentationcontract "go_wp/internal/module/presentation/contract"
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

// BlockReferenceChecker 块删除 / global→template 切换前的引用检查：合并全部
// **有效源码引用**来源（审计 ARCH-02）。
//
// 五条来源，每条都由拥有该数据的模块自己的只读契约给出（跨模块只经 contract，
// 不允许在装配层直查别人的表）：
//
//  1. 主题槽位绑定      project.ListThemesByBlockID（工程级默认页眉/页脚）
//  2. 页面              page.ListBlockSourceRefs（文档树任意深度 + settings.structure
//     的页眉/页脚/其余槽位）
//  3. 内容模板          contenttemplate.ListBlockSourceRefs（草稿 + 全部历史版本）
//  4. 自动发布实例      presentation.ListBlockSourceRefs（覆盖文档 + 全部文档快照）
//  5. 其它全局块        block.ListBlockSourceRefs（块文档树里的嵌套引用）
//
// 修复前的判据只有 1 与 2 的页面部分：引用只存在于嵌套块、未发布模板、独立文档模式
// 实例时删除一路成功，站点在下一次构建才暴露缺失（审计 ARCH-02 的原问题）。
//
// 为什么不包含「已发布产物引用」：artifact 是不可变的编译产物字节，它的保留由 GC
// 策略决定，不该永久阻断源码删除（任务的验收口径）。产物的影响面提示是另一件事，
// 不在本判据里 —— 先拦源码引用，替换引用操作与产物影响面留给后续。
//
// 任一来源查询失败即整体失败（返回 error）：service 层对检查失败按「仍被引用」处理
// （宁拒勿删），把失败伪装成「没有引用」才是真正的危险。
func BlockReferenceChecker(blocks blockcontract.BlockService, pages pagecontract.PageService,
	projects projectcontract.ProjectService, presentations presentationcontract.PresentationService,
	templates contenttemplatecontract.ContentTemplateService,
) func(context.Context, string) ([]blockcontract.BlockUsage, error) {
	return func(ctx context.Context, blockID string) ([]blockcontract.BlockUsage, error) {
		var out []blockcontract.BlockUsage

		// 1) 主题槽位绑定。
		if projects != nil {
			themes, err := projects.ListThemesByBlockID(ctx, blockID)
			if err != nil {
				logger.Scene("block").With("block_id", blockID).Error(err, "反查绑定块的主题失败")
				return nil, err
			}
			for _, t := range themes {
				out = append(out, blockcontract.BlockUsage{
					Kind: blockcontract.UsageKindThemeSlot, EntityID: t.ID, Label: t.Name,
				})
			}
		} else {
			logger.Scene("block").With("block_id", blockID).Warn("project 契约未装配：主题槽位引用不参与块删除保护")
		}

		// 2)~5) 各模块自己的只读面。契约缺失时**记账**而不是静默跳过 ——
		// 静默跳过正是本次修复要消灭的形态（少查一类 = 放行一次不该放行的删除）。
		type refSource struct {
			name string
			list func() ([]blockcontract.BlockUsage, error)
		}
		sources := make([]refSource, 0, 4)
		if pages != nil {
			sources = append(sources, refSource{"page", func() ([]blockcontract.BlockUsage, error) {
				return pages.ListBlockSourceRefs(ctx, blockID)
			}})
		} else {
			logger.Scene("block").With("block_id", blockID).Warn("page 契约未装配：页面引用不参与块删除保护")
		}
		if templates != nil {
			sources = append(sources, refSource{"contenttemplate", func() ([]blockcontract.BlockUsage, error) {
				return templates.ListBlockSourceRefs(ctx, blockID)
			}})
		} else {
			logger.Scene("block").With("block_id", blockID).Warn("contenttemplate 契约未装配：内容模板引用不参与块删除保护")
		}
		if presentations != nil {
			sources = append(sources, refSource{"presentation", func() ([]blockcontract.BlockUsage, error) {
				return presentations.ListBlockSourceRefs(ctx, blockID)
			}})
		} else {
			logger.Scene("block").With("block_id", blockID).Warn("presentation 契约未装配：自动发布实例引用不参与块删除保护")
		}
		if blocks != nil {
			sources = append(sources, refSource{"block", func() ([]blockcontract.BlockUsage, error) {
				return blocks.ListBlockSourceRefs(ctx, blockID)
			}})
		} else {
			logger.Scene("block").With("block_id", blockID).Warn("block 契约未装配：嵌套块引用不参与块删除保护")
		}
		for _, src := range sources {
			usages, err := src.list()
			if err != nil {
				logger.Scene("block").With("block_id", blockID).With("source", src.name).Error(err, "反查块引用失败")
				return nil, err
			}
			out = append(out, usages...)
		}
		return out, nil
	}
}
