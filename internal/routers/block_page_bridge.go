package routers

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

func BlockStalePropagator(pages pagecontract.PageService, projects projectcontract.ProjectService, presentations pipeline.DependencyTarget, presentationRebuilder pipeline.StaleRebuilder) func(context.Context, string) error {
	return func(ctx context.Context, blockID string) error {
		themes, err := projects.ListThemesByBlockID(ctx, blockID)
		if err != nil {
			logger.Scene("block").With("block_id", blockID).Error(err, "反查绑定块的主题失败")
			return err
		}
		pageIDs := make([]string, 0, 8)
		for _, t := range themes {
			ids, merr := pages.MarkStaleForTheme(ctx, t.ID)
			if merr != nil {
				logger.Scene("block").With("block_id", blockID).With("theme_id", t.ID).Error(merr, "标记页面待重建失败")
				return merr
			}
			pageIDs = append(pageIDs, ids...)
		}
		ids, err := pages.MarkStaleForBlock(ctx, blockID)
		if err != nil {
			logger.Scene("block").With("block_id", blockID).Error(err, "反查引用块页面标待重建失败")
			return err
		}
		pageIDs = append(pageIDs, ids...)

		dep := pipeline.BlockKey(blockID)
		instIDs, err := presentations.MarkStaleByDependency(ctx, dep.Kind, dep.Key)
		if err != nil {
			logger.Scene("block").With("block_id", blockID).Error(err, "标记引用块的自动发布实例待重建失败")
			return err
		}

		rebuildStaleAsync("block", pages, pageIDs)
		rebuildStaleAsync("block", presentationRebuilder, instIDs)
		return nil
	}
}

func rebuildStaleAsync(scene string, rebuilder pipeline.StaleRebuilder, ids []string) {
	batch := uniqueIDs(ids)
	if len(batch) == 0 || rebuilder == nil {
		return
	}
	go func() {

		if err := rebuilder.RebuildStale(context.Background(), batch); err != nil {
			logger.Scene(scene).With("count", len(batch)).
				Error(err, "变更后的自动重建失败（相关页面保持待重建，等下次触发）")
		}
	}()
}

func uniqueIDs(ids []string) []string {
	if len(ids) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(ids))
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

func BlockReferenceChecker(blocks blockcontract.BlockService, pages pagecontract.PageService,
	projects projectcontract.ProjectService, presentations presentationcontract.PresentationService,
	templates contenttemplatecontract.ContentTemplateService,
) func(context.Context, string) ([]blockcontract.BlockUsage, error) {
	return func(ctx context.Context, blockID string) ([]blockcontract.BlockUsage, error) {
		var out []blockcontract.BlockUsage

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
