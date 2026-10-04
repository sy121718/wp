package navigationservice

// navigation_source.go — 菜单项来源实体解析（契约 SourceResolver 的注入与消费）。
// 解析失败保留记录自身 title/path：构建期不因单个来源实体缺失而整页失败。

import (
	"context"

	"go_wp/pkg/logger"

	navigationcontract "go_wp/internal/module/navigation/contract"
	navigationdto "go_wp/internal/module/navigation/dto"
)

// SetSourceResolver 注入来源实体解析器（启动期装配调用一次，之后只读）。
func (s *Service) SetSourceResolver(r navigationcontract.SourceResolver) { s.sources = r }

// SourceGroups 返回该工程可加入菜单的来源候选（未注入解析器时为空）。
func (s *Service) SourceGroups(ctx context.Context, projectID string) (groups []navigationcontract.SourceGroup, err error) {
	if s.sources == nil {
		return nil, nil
	}
	return s.sources.Candidates(ctx, projectID)
}

// resolveSourceTitles 递归把来源实体的标题/URL 写回菜单树。
// 解析失败保留记录自身值（构建期不因单个来源实体缺失而整页失败）。
func (s *Service) resolveSourceTitles(ctx context.Context, projectID string, nodes []*navigationdto.NavigationNode) {
	if s.sources == nil {
		return
	}
	for _, n := range nodes {
		if n.SourceType != sourceCustom && n.SourceID != nil && *n.SourceID != "" {
			title, url, err := s.sources.ResolveSource(ctx, projectID, n.SourceType, *n.SourceID)
			if err != nil {
				logger.Scene("navigation").
					With("sourceType", n.SourceType).With("sourceId", *n.SourceID).
					Warn("导航来源实体解析失败，回退记录自身标题/链接")
			} else {
				if title != "" {
					n.Title = title
				}
				if url != "" {
					n.Path = url
				}
			}
		}
		s.resolveSourceTitles(ctx, projectID, n.Children)
	}
}
