package pipeline

// site_lang.go — 站点语言 URL 与 hreflang 装配（page / presentation 共用，EDT-003）。

import (
	"context"
	"os"
	"strings"

	"go_wp/internal/builder"
	"go_wp/internal/builder/core"
	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/internal/seo"
	"go_wp/pkg/i18n"
	"go_wp/pkg/logger"
)

// SiteRouteEntry 语言 → 该语言下的站点访问路径。
type SiteRouteEntry struct {
	Lang string
	Path string
}

// LangURLRuleForProject 构造站点语言 URL 规则（与 page.langURLRuleOf 同源）。
func LangURLRuleForProject(ctx context.Context, project projectcontract.ProjectService, projectID string) LangURLRule {
	return NewLangURLRule(
		i18n.SiteLangURLsSeparated(),
		i18n.SiteLangURLPrefixDefault(),
		DefaultLocale(ctx, project, projectID),
		i18n.URLCodeOverrides(),
	)
}

// EnabledLangs 站点启用语言（默认语言在前；清单不可读时回退默认语言一种）。
func EnabledLangs(ctx context.Context, project projectcontract.ProjectService, projectID string) []string {
	if project != nil && strings.TrimSpace(projectID) != "" {
		langs, err := project.EnabledLangs(ctx, projectID)
		if err == nil && len(langs) > 0 {
			return langs
		}
		if err != nil {
			logger.Scene("build").With("projectId", projectID).
				Error(err, "启用语言清单读取失败，已降级为默认语言单语言构建")
		}
	}
	return []string{i18n.GetDefaultLang()}
}

// DefaultLocale 站点默认语言（清单 is_default，缺失回退 i18n.default_lang）。
func DefaultLocale(ctx context.Context, project projectcontract.ProjectService, projectID string) string {
	if project != nil && strings.TrimSpace(projectID) != "" {
		if d, err := project.DefaultLocale(ctx, projectID); err == nil && d != "" {
			return d
		}
	}
	return i18n.GetDefaultLang()
}

// SitePath 逻辑访问路径 → 实际访问路径。
func SitePath(rule LangURLRule, lang, logical string) (string, error) {
	return rule.Path(lang, logical)
}

// LocalizeMenuURL 导航项 URL 本地化（与 page.localizeMenuURL 同源）。
func LocalizeMenuURL(ctx context.Context, project projectcontract.ProjectService, projectID, lang, raw string) string {
	u := strings.TrimSpace(raw)
	if u == "" || !strings.HasPrefix(u, "/") || strings.HasPrefix(u, "//") {
		return raw
	}
	u = seo.CanonicalPublicPath(u)
	rule := LangURLRuleForProject(ctx, project, projectID)
	langs := EnabledLangs(ctx, project, projectID)
	if _, logical, ok := rule.Locate(u, langs); ok {
		u = logical
	}
	p, err := SitePath(rule, lang, u)
	if err != nil {
		return raw
	}
	return seo.CanonicalPublicPath(p)
}

// SiteRouteEntries 按启用语言计算逻辑路径的各语言站点路径（默认语言在前）。
func SiteRouteEntries(ctx context.Context, project projectcontract.ProjectService, projectID, logical string) ([]SiteRouteEntry, error) {
	langs := EnabledLangs(ctx, project, projectID)
	rule := LangURLRuleForProject(ctx, project, projectID)
	if err := rule.Validate(langs); err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	out := make([]SiteRouteEntry, 0, len(langs))
	for _, lang := range langs {
		p, err := SitePath(rule, lang, logical)
		if err != nil {
			return nil, err
		}
		if seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, SiteRouteEntry{Lang: lang, Path: p})
	}
	return out, nil
}

// LocaleView 计算 hreflang 互指与语言切换器链接（与 page.localeViewOf 同源）。
//
// published 报告某个访问路径是否真的已发布（审计 I18N-021）。为 nil 时按「全部已发布」
// 处理，行为与接入前逐字一致 —— 未接入校验不该改变产物。
//
// 为什么必须有这道校验：SiteRouteEntries 只是从**启用语言**推导路径，它不知道某个语言
// 的页面到底有没有构建成功。于是一个「已登记但未发布」的语言会出现在切换器里，
// 用户点过去看到的不是「还没翻译」而是站内 404 —— 站点看起来是坏的，
// 而不是「这个语言还没做」。
//
// 只跳过**非当前语言**：当前语言那一项必须保留，否则切换器里没有「你正在看的这一版」。
func LocaleView(ctx context.Context, project projectcontract.ProjectService, projectID, logicalPath, lang string, published func(accessPath string) bool) (alts []builder.Alternate, links []core.LocaleLink) {
	if !i18n.SiteLangURLsSeparated() || strings.TrimSpace(projectID) == "" || strings.TrimSpace(logicalPath) == "" {
		return nil, nil
	}
	entries, err := SiteRouteEntries(ctx, project, projectID, logicalPath)
	if err != nil || len(entries) < 2 {
		return nil, nil
	}
	defaultLang := DefaultLocale(ctx, project, projectID)
	base := strings.TrimSpace(os.Getenv("WP_SITE_BASE_URL"))
	alts = make([]builder.Alternate, 0, len(entries))
	links = make([]core.LocaleLink, 0, len(entries))
	entries = filterPublishedLocales(entries, lang, published)
	if len(entries) < 2 {
		return nil, nil
	}
	for _, e := range entries {
		pubPath := seo.CanonicalPublicPath(e.Path)
		alts = append(alts, builder.Alternate{
			Lang: e.Lang, Href: seo.JoinURL(base, pubPath), Default: e.Lang == defaultLang,
		})
		links = append(links, core.LocaleLink{Lang: e.Lang, Href: pubPath, Current: e.Lang == lang})
	}
	return alts, links
}

// filterPublishedLocales 丢掉「没真的发布」的语言（审计 I18N-021）。
//
// 两条规则，都来自同一条判断：**切换器里不该出现点了 404 的链接**。
//
//  1. published 为 nil（未接入校验）→ 原样返回：产物行为与接入前逐字一致；
//  2. 当前语言的条目**一定保留**，哪怕它自己未发布 —— 切换器里必须有
//     「你正在看的这一版」，否则用户会以为站点只有另一种语言。
//
// 抽成纯函数是为了可测：LocaleView 本身要 project 服务（大接口），
// 而这段判断恰恰是最容易写错、也最该被钉住的一处。
func filterPublishedLocales(entries []SiteRouteEntry, currentLang string, published func(accessPath string) bool) []SiteRouteEntry {
	if published == nil {
		return entries
	}
	out := make([]SiteRouteEntry, 0, len(entries))
	for _, e := range entries {
		if e.Lang != currentLang && !published(e.Path) {
			continue
		}
		out = append(out, e)
	}
	return out
}

// LogicalPathOf 从已发布的访问路径反查逻辑路径（多语言前缀剥离）。
func LogicalPathOf(ctx context.Context, project projectcontract.ProjectService, projectID, accessPath string) string {
	u := strings.TrimSpace(accessPath)
	if u == "" {
		return ""
	}
	rule := LangURLRuleForProject(ctx, project, projectID)
	if _, logical, ok := rule.Locate(u, EnabledLangs(ctx, project, projectID)); ok {
		return logical
	}
	return u
}

// ContentTranslationEnabled 非默认语言且语言非空时接入内容翻译。
func ContentTranslationEnabled(ctx context.Context, project projectcontract.ProjectService, projectID, lang string) bool {
	l := strings.TrimSpace(lang)
	if l == "" {
		return false
	}
	return l != DefaultLocale(ctx, project, projectID)
}
