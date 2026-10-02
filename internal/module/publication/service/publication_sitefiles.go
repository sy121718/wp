package pubservice

// publication_sitefiles.go — 站点静态文件刷新（随激活状态重建 sitemap / robots / feed）。

import (
	"context"
	"go_wp/internal/pipeline"
	"go_wp/internal/seo"

	"go_wp/pkg/i18n"
	"go_wp/pkg/logger"
)

// RefreshSiteFiles 生成/刷新站点级 SEO 产物与自定义 404 页
// （sitemap.xml + robots.txt + feed.xml + 404.html）。
//
// langs 为站点启用语言（默认语言在前），defaultLang 用于 x-default（多语言 P3），
// siteLangMode 为该工程**生效**的语言 URL 方案（off / default_plain / all_prefix）。
// 这三样都由调用方（page 装配层，持有 project 契约）传入 —— publication 不跨模块查语言，
// 也不解析工程设置：方案是**工程级**的（多工程可各不相同），解析入口只有
// pipeline.SiteLangURLModeOf 一处，让本站点文件跟着「第一个配了这个键的工程」走
// 就是多工程数据污染。
// notFoundHTML 同理：站点自定义 404 页内容由调用方传入，publication 不跨模块读 SiteSettings。
func (s *Service) RefreshSiteFiles(ctx context.Context, projectID, baseURL, dir string, langs []string, defaultLang, siteLangMode, notFoundHTML string) (err error) {
	if projectID == "" || dir == "" {
		return nil
	}
	// 自定义 404 页（审计 SEO-013）：与 sitemap 同级写在激活目录根，位置单源在 pipeline。
	// 放在最前面是因为它不依赖已激活路由 —— 站点还没发布过任何页面时，
	// 404 页也该跟着配置走（此时 ListActivePaths 为空，sitemap 仍然能写）。
	if nerr := pipeline.SyncNotFoundPage(dir, notFoundHTML); nerr != nil {
		logger.Scene("publication").With("dir", dir).Error(nerr, "自定义 404 页刷新失败")
		return nerr
	}
	paths, err := s.model.ListActivePaths(ctx, projectID)
	if err != nil {
		return err
	}
	if err = seo.WriteSiteFiles(dir, baseURL, sitemapEntries(baseURL, paths, langs, defaultLang, siteLangMode)); err != nil {
		return err
	}
	// 站点级 feed（审计 SEO-012）：与 sitemap 同一次刷新、同一批条目来源，
	// 见 publication_feed.go。两者是两个独立文件，一个失败不撤销另一个已写入的结果；
	// 但错误照常上抛（调用方只记日志、不回滚已完成的发布）。
	if ferr := s.refreshFeed(ctx, projectID, baseURL, dir, langs, defaultLang, siteLangMode); ferr != nil {
		logger.Scene("publication").With("dir", dir).Error(ferr, "feed.xml 刷新失败")
		return ferr
	}
	return nil
}

// sitemapEntries 已激活路径 → sitemap 条目。
//
// 多语言（开启前缀且 ≥2 语言）时按「逻辑路径」分组：同一逻辑路径的各语言版本
// 互相输出 xhtml:link 互指（含 x-default）。单语言或未开启前缀时输出与 P3 之前一致。
func sitemapEntries(baseURL string, paths, langs []string, defaultLang, siteLangMode string) []seo.SitemapEntry {
	if !i18n.SiteLangURLsSeparated(i18n.SiteLangURLMode(siteLangMode)) || len(langs) < 2 {
		return seo.EntriesFromPaths(baseURL, paths)
	}
	// 语言归属用与构建期完全相同的规则（唯一映射点 pipeline.LangURLRule，
	// 构造在 siteLangRule 一处完成，feed 的条目归属用同一份）：
	// default_plain 下 /about 归属默认语言、/en/about 归属 en-US，两者互为一组。
	rule, _ := siteLangRule(langs, defaultLang, siteLangMode)
	byLogical := map[string]map[string]string{}
	logicalOf := map[string]string{}
	for _, p := range paths {
		lang, logical, ok := rule.Locate(p, langs)
		if !ok {
			continue
		}
		if byLogical[logical] == nil {
			byLogical[logical] = map[string]string{}
		}
		byLogical[logical][lang] = p
		logicalOf[p] = logical
	}
	out := make([]seo.SitemapEntry, 0, len(paths))
	for _, p := range paths {
		entry := seo.EntryForPath(baseURL, p)
		logical, ok := logicalOf[p]
		if !ok {
			out = append(out, entry)
			continue
		}
		group := byLogical[logical]
		if len(group) < 2 {
			out = append(out, entry)
			continue
		}
		for _, l := range langs {
			alt, exists := group[l]
			if !exists {
				continue
			}
			entry.Alternates = append(entry.Alternates, seo.SitemapAlternate{
				Lang: l, Href: seo.JoinURL(baseURL, alt),
			})
		}
		if alt, exists := group[defaultLang]; exists {
			entry.Alternates = append(entry.Alternates, seo.SitemapAlternate{
				Lang: "x-default", Href: seo.JoinURL(baseURL, alt),
			})
		}
		out = append(out, entry)
	}
	return out
}

// 语言归属判定已下沉到 pipeline.LangURLRule.Locate（构建期与 sitemap 同一份规则），
// 见 sitemapEntries：默认语言无前缀方案下，未带任何已知短码前缀的路径归属默认语言。
