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
	return siteRouteEntriesForLangs(LangURLRuleForProject(ctx, project, projectID), EnabledLangs(ctx, project, projectID), logical)
}

// siteRouteEntriesForLangs 按**给定**语言集合推导逻辑路径的各语言站点路径。
//
// 抽出来是为了让调用方能自带语言集合：批次发布的目标语言在构建前就已确定
// （SEO-026），不必再从站点语言清单推导一次 —— 两份来源各自演进就是漂移的开始。
// langs 的顺序即输出顺序（默认语言在前由调用方保证）。
func siteRouteEntriesForLangs(rule LangURLRule, langs []string, logical string) ([]SiteRouteEntry, error) {
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

// LocaleViewInput LocaleView 的输入。
//
// 聚成结构体而不是继续加位置参数：这里已经同时存在两个「语言集合来源」，
// 位置参数一旦错位，产物只会静默少几条互指链接。
type LocaleViewInput struct {
	Ctx         context.Context
	Project     projectcontract.ProjectService
	ProjectID   string
	LogicalPath string
	Lang        string

	// TargetLangs 本批次准备上线的语言集合（SEO-026）。非空时它是 hreflang 互指的
	// **唯一**判据：这份集合是构建期就已经知道的事实（调用方正逐个构建它们，紧接着
	// 逐个结案），产物于是只依赖构建输入，不依赖「构建完之后才产生的状态」。
	//
	// 契约：当前构建语言必须在集合内（它正在被构建，紧接着就要上线）。
	// 为空表示调用方没有批次概念（手工 Page 的逐页发布），此时退回
	// 「站点启用语言推导 + Published 过滤」—— page 侧口径逐字不变。
	TargetLangs []string

	// Published 报告某个访问路径是否真的已发布（审计 I18N-021）。只在 TargetLangs 为空时使用。
	Published func(accessPath string) bool
}

// LocaleView 计算 hreflang 互指与语言切换器链接（与 page.localeViewOf 同源）。
//
// 两条判据二选一，取哪个由调用方决定（见 LocaleViewInput.TargetLangs）：
//
//  1. 批次口径（TargetLangs 非空，自动发布实例）：互指 = 本批次要上线的语言。
//     这里**不做任何已发布状态过滤** —— 账本行要等逐语言结案才写，首发布构建时
//     它必然为空，按它过滤等于把「本次正在上线的语言」全部剔除（SEO-026：
//     首发布产物缺 hreflang，重建一次才补上）。批次口径的自我约束是：
//     集合不多不少就是本批次逐个激活的那一份，任一语言失败则整批标 stale 并重建，
//     「没上线的语言」是该状态要暴露的问题，不是靠产物静默裁剪来掩盖的。
//
//  2. 访问面口径（TargetLangs 为空，手工 Page）：SiteRouteEntries 只从**启用语言**
//     推导路径，它不知道某个语言的页面到底有没有构建成功。于是一个「已登记但未发布」
//     的语言会出现在切换器里，用户点过去看到的不是「还没翻译」而是站内 404 ——
//     站点看起来是坏的，而不是「这个语言还没做」。所以按 Published 过滤，
//     且只跳过**非当前语言**：当前语言那一项必须保留，否则切换器里没有
//     「你正在看的这一版」。
func LocaleView(in LocaleViewInput) (alts []builder.Alternate, links []core.LocaleLink) {
	if !i18n.SiteLangURLsSeparated() || strings.TrimSpace(in.ProjectID) == "" || strings.TrimSpace(in.LogicalPath) == "" {
		return nil, nil
	}
	// 语言集合的唯一来源：有批次就按批次，没批次才回落到站点启用语言清单。
	langs := in.TargetLangs
	if len(langs) == 0 {
		langs = EnabledLangs(in.Ctx, in.Project, in.ProjectID)
	}
	rule := LangURLRuleForProject(in.Ctx, in.Project, in.ProjectID)
	entries, err := siteRouteEntriesForLangs(rule, langs, in.LogicalPath)
	if err != nil || len(entries) < 2 {
		return nil, nil
	}
	if len(in.TargetLangs) == 0 {
		entries = filterPublishedLocales(entries, in.Lang, in.Published)
		if len(entries) < 2 {
			return nil, nil
		}
	}
	defaultLang := DefaultLocale(in.Ctx, in.Project, in.ProjectID)
	base := strings.TrimSpace(os.Getenv("WP_SITE_BASE_URL"))
	alts = make([]builder.Alternate, 0, len(entries))
	links = make([]core.LocaleLink, 0, len(entries))
	for _, e := range entries {
		pubPath := seo.CanonicalPublicPath(e.Path)
		alts = append(alts, builder.Alternate{
			Lang: e.Lang, Href: seo.JoinURL(base, pubPath), Default: e.Lang == defaultLang,
		})
		links = append(links, core.LocaleLink{Lang: e.Lang, Href: pubPath, Current: e.Lang == in.Lang})
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
