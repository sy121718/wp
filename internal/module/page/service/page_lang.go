package pageservice

// page_lang.go — 站点产物语言装配（多语言 P2，docs/06-D）。
//
// 语言是构建环境维度：构建、预览、发布都必须显式携带目标语言，产物路径前缀
// 单点经 pipeline.LangPath 计算（禁止各处手拼 "/" + lang + path）。
// 配置开关 i18n.site_lang_prefix（pkg/i18n.SiteLangPrefixEnabled）是决策 D1 的
// 落地闸门：关闭时保持逻辑路径（单语言兼容），开启后全语言带 /{lang}/ 前缀。

import (
	"context"
	"strings"

	pubcontract "go_wp/internal/module/publication/contract"
	"go_wp/internal/pipeline"
	"go_wp/pkg/i18n"
	"go_wp/pkg/logger"
)

// buildLang 解析本次构建语言：请求显式指定优先，否则站点默认语言（i18n.default_lang）。
func buildLang(requested string) string {
	lang := strings.TrimSpace(requested)
	if lang == "" {
		return i18n.GetDefaultLang()
	}
	return lang
}

// sitePath 逻辑访问路径 → 实际访问路径（语言前缀开关见文件头）。
// 关闭：返回规范化逻辑路径；开启：/{lang}/path（语言根映射 /{lang}/index）。
func sitePath(lang, logical string) (string, error) {
	if !i18n.SiteLangPrefixEnabled() {
		return pipeline.NormalizeURL(logical)
	}
	return pipeline.LangPath(lang, logical)
}

// siteRoutePaths 建页/改草稿阶段的路径占用路径：按站点启用语言（project_locales）
// 各一行，默认语言在前（多语言 P3，docs/06-D §14 D10）。
//
// 关闭语言前缀时多语言映射到同一逻辑路径，这里按路径去重（page_routes 主键是
// (project_id, path)，重复插入必然撞唯一键）；语言清单不可读时回退默认语言一种，
// 与 P3 之前的单语言行为完全一致。
func (s *Service) siteRoutePaths(ctx context.Context, projectID, logical string) ([]string, error) {
	entries, err := s.siteRouteEntries(ctx, projectID, logical)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.Path)
	}
	return out, nil
}

// siteRouteEntry 语言 → 该语言下的站点访问路径。
type siteRouteEntry struct {
	Lang string
	Path string
}

// siteRouteEntries 按启用语言（project_locales）计算逻辑路径的各语言站点路径，
// 默认语言在前；关闭前缀时多语言映射到同一路径，按路径去重。
func (s *Service) siteRouteEntries(ctx context.Context, projectID, logical string) ([]siteRouteEntry, error) {
	langs := []string{i18n.GetDefaultLang()}
	if s.project != nil && strings.TrimSpace(projectID) != "" {
		if l, err := s.project.EnabledLangs(ctx, projectID); err == nil && len(l) > 0 {
			langs = l
		}
	}
	seen := map[string]bool{}
	out := make([]siteRouteEntry, 0, len(langs))
	for _, lang := range langs {
		p, err := sitePath(lang, logical)
		if err != nil {
			return nil, err
		}
		if seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, siteRouteEntry{Lang: lang, Path: p})
	}
	return out, nil
}

// renameReservedAllLangs 按启用语言逐语言迁移路径占用（建页/改草稿/改 URL）。
//
// targetLang 非空（改某语言 URL）时：该语言允许迁移本页 active 行（发布时
// reserved 被原地升级为 active，改 URL 的 DB 同步依赖这一迁移），其他语言
// **只迁移 reserved 行**——否则会把别的语言的激活行改到新路径，线上路由丢失。
// targetLang 为空（改草稿路径）时所有语言同等对待。
//
// 任一语言失败即把已迁移的迁回（尽力而为）并返回该错误：路由表与草稿路径必须同源。
func (s *Service) renameReservedAllLangs(ctx context.Context, projectID, pageID, oldLogical, newLogical, targetLang string) error {
	if s.routes == nil {
		return nil
	}
	oldEntries, err := s.siteRouteEntries(ctx, projectID, oldLogical)
	if err != nil {
		return ErrInvalidPath
	}
	newEntries, err := s.siteRouteEntries(ctx, projectID, newLogical)
	if err != nil {
		return ErrInvalidPath
	}
	if len(oldEntries) != len(newEntries) {
		// 语言清单在迁移中途变化（极罕见）：不做半途改名，交由调用方重试。
		return ErrInvalidPath
	}
	done := 0
	for i := range oldEntries {
		oldPath, newPath := oldEntries[i].Path, newEntries[i].Path
		if oldPath == newPath {
			done = i + 1
			continue
		}
		onlyReserved := targetLang != "" && oldEntries[i].Lang != targetLang
		if rerr := s.routes.RenameReserved(ctx, &pubcontract.RenameReservedReq{
			ProjectID: projectID, PageID: pageID, OldPath: oldPath, NewPath: newPath,
			OnlyReserved: onlyReserved,
		}); rerr != nil {
			for j := 0; j < done; j++ {
				if oldEntries[j].Path == newEntries[j].Path {
					continue
				}
				if rberr := s.routes.RenameReserved(ctx, &pubcontract.RenameReservedReq{
					ProjectID: projectID, PageID: pageID,
					OldPath: newEntries[j].Path, NewPath: oldEntries[j].Path,
					OnlyReserved: targetLang != "" && oldEntries[j].Lang != targetLang,
				}); rberr != nil {
					logger.Scene("page").With("pageId", pageID).Error(rberr, "保留路由回迁失败")
				}
			}
			return rerr
		}
		done = i + 1
	}
	return nil
}

// enabledLangsOf 站点启用语言（默认语言在前；清单不可读时回退默认语言一种）。
func (s *Service) enabledLangsOf(ctx context.Context, projectID string) []string {
	if s.project != nil && strings.TrimSpace(projectID) != "" {
		if langs, err := s.project.EnabledLangs(ctx, projectID); err == nil && len(langs) > 0 {
			return langs
		}
	}
	return []string{i18n.GetDefaultLang()}
}

// defaultLocaleOf 站点默认语言（清单 is_default，缺失回退 i18n.default_lang）。
func (s *Service) defaultLocaleOf(ctx context.Context, projectID string) string {
	if s.project != nil && strings.TrimSpace(projectID) != "" {
		if d, err := s.project.DefaultLocale(ctx, projectID); err == nil && d != "" {
			return d
		}
	}
	return i18n.GetDefaultLang()
}

// highlightPath 导航「当前项」高亮用的访问路径：与导航项 URL 同源（带前缀）。
// 路径非法或空时返回空串（不标记当前项，绝不让高亮逻辑影响构建主链）。
func highlightPath(lang, logical string) string {
	if strings.TrimSpace(logical) == "" {
		return ""
	}
	p, err := sitePath(lang, logical)
	if err != nil {
		return ""
	}
	return p
}

// localizeMenuURL 导航项 URL 本地化：站内绝对路径（以 / 开头）加语言前缀，
// 外链（http/https///mailto/tel/#）与相对路径原样保留。
//
// 必要性：导航 URL 来自 navigation 表，存的是站点逻辑路径；多语言开启前缀后
// 产物里的菜单链接必须指向 /{lang}/path，否则访客点菜单必然 404。
func localizeMenuURL(lang, raw string) string {
	u := strings.TrimSpace(raw)
	if u == "" || !strings.HasPrefix(u, "/") || strings.HasPrefix(u, "//") {
		return raw
	}
	p, err := sitePath(lang, u)
	if err != nil {
		return raw
	}
	return p
}

// MarkStaleForI18n 把全部页面标记为待重建（文案词条变更后调用）。
//
// 与 Manifest 的 i18n 依赖条目（DependencyKind=i18n）配套：
// 依赖条目负责「产物字节与词条 revision 的对应关系」，本方法负责「变更后重新排队」。
// 调用方：后台 i18n CRUD（docs/06-D §14 D7，尚未实现）或运维脚本。
func (s *Service) MarkStaleForI18n(ctx context.Context) error {
	return s.model.MarkStaleForI18n(ctx)
}

// buildDependencies 构建期依赖（pipeline.DependencyProvider 实现）。
// 组件固定文案由构建期取词注入 HTML 字节，因此产物依赖文案词条资源版本号：
// 改文案 → revision 变化 → 依赖比对不等 → 触发重建（docs/06-D §10.4）。
func (s *Service) buildDependencies(_ context.Context, _ pipeline.BuildInput) []pipeline.Dependency {
	return []pipeline.Dependency{pipeline.I18NDependency(i18n.Revision())}
}