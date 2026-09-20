package pipeline

// site_lang.go — 站点语言 URL 与 hreflang 装配（page / presentation 共用，EDT-003）。

import (
	"context"
	"errors"
	"fmt"
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

// LangFallback 语言清单读取失败时的等级策略（审计 I18N-02）。
//
// 两种等级对应「错了会怎样」，而不是「谁调用」：
//
//   - 预览 / 后台：作者正在看这份草稿，清单读不到时降级为默认语言一种，页面照常
//     渲染并留下可见告警 —— 报错会让作者以为是自己文档的问题；
//   - 发布：语言表是发布的**输入契约**，读不到就无从判断「这次该上线哪几种语言」。
//     此时降级为「只发默认语言」的后果是其余语言的线上产物停在旧字节（或线上被
//     单语言产物覆盖），而发布回执写的是成功 —— 站点静默降级且无人察觉。
//
// 所以判据是「这次编译的成败是否对线上可感知」，发布口径一律 Forbidden。
type LangFallback int

const (
	// LangFallbackVisible 允许回退：读取失败降级为默认语言一种并记告警。
	// 零值 —— 未表态的调用方行为逐字不变（预览 / 后台 / 草稿路径）。
	LangFallbackVisible LangFallback = iota
	// LangFallbackForbidden 禁止回退：读取失败返回错误，由调用方失败。
	LangFallbackForbidden
)

// ErrLangTableUnavailable 站点语言清单不可读（发布口径下不降级，审计 I18N-02）。
//
// 单独定义成哨兵错误：调用方（发布编排、重试/回执）要能区分「语言表读不到，重试
// 有意义」与「文档本身编译不过，重试无意义」，而不是去嗅探错误文本。
var ErrLangTableUnavailable = errors.New("站点语言清单不可读")

// ResolveSiteLangs 按等级策略解析站点启用语言（默认语言在前）。
//
// 这是「站点有哪几种语言」的唯一读取口：EnabledLangs 是它的 LangFallbackVisible
// 特例，发布路径应显式使用 LangFallbackForbidden。
func ResolveSiteLangs(ctx context.Context, project projectcontract.ProjectService, projectID string, policy LangFallback) ([]string, error) {
	if project != nil && strings.TrimSpace(projectID) != "" {
		langs, err := project.EnabledLangs(ctx, projectID)
		if err == nil && len(langs) > 0 {
			return langs, nil
		}
		if err != nil {
			if policy == LangFallbackForbidden {
				return nil, fmt.Errorf("%w: 工程 %s: %v", ErrLangTableUnavailable, projectID, err)
			}
			logger.Scene("build").With("projectId", projectID).
				Error(err, "启用语言清单读取失败，已降级为默认语言单语言构建")
			return []string{i18n.GetDefaultLang()}, nil
		}
		// 无错误但清单为空：项目服务在「无清单」时已经回退站点默认语言，走到这里
		// 说明清单确实是空的。发布口径下这同样是「不知道该上线哪几种语言」，
		// 与读取失败等价处理 —— 空清单会让发布退化成单语言并覆盖线上其它语言。
		if policy == LangFallbackForbidden {
			return nil, fmt.Errorf("%w: 工程 %s 的语言清单为空", ErrLangTableUnavailable, projectID)
		}
		return []string{i18n.GetDefaultLang()}, nil
	}
	// 无工程上下文（未注入工程服务 / 未指定工程）：发布口径没有语言表可用。
	if policy == LangFallbackForbidden {
		return nil, fmt.Errorf("%w: 缺少工程上下文（projectId=%q）", ErrLangTableUnavailable, projectID)
	}
	return []string{i18n.GetDefaultLang()}, nil
}

// EnabledLangs 站点启用语言（默认语言在前；清单不可读时回退默认语言一种）。
//
// 这是**预览 / 后台口径**的便捷入口（等价 ResolveSiteLangs(..., LangFallbackVisible)）：
// 降级是刻意的，用于草稿、诊断、翻译编辑页这类「读不到清单也不该挡住作者」的场景。
// 发布路径必须用 ResolveSiteLangs(..., LangFallbackForbidden) —— 静默降级到这里
// 正是审计 I18N-02 的成因。
func EnabledLangs(ctx context.Context, project projectcontract.ProjectService, projectID string) []string {
	langs, _ := ResolveSiteLangs(ctx, project, projectID, LangFallbackVisible)
	return langs
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

	// SiteLangs 调用方**已冻结**的站点启用语言（默认语言在前，审计 I18N-02）。
	//
	// 非空时它就是唯一来源，本级不再回调工程服务读一次 —— 发布口径要求「本次发布
	// 依据哪份语言表」只有一个答案，读两次就可能拿到两份（两次读之间清单被改）。
	// 为空表示调用方没有冻结（预览 / 旧调用方），此时按 LangFallback 自行解析。
	SiteLangs []string

	// LangFallback 自行解析语言清单时的等级策略（SiteLangs 为空才用）。
	// 零值 = 允许可见告警回退；发布口径的调用方要么传 SiteLangs、要么传 Forbidden。
	LangFallback LangFallback
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
	// 语言集合的唯一来源：有批次就按批次；否则用调用方冻结的那份；再否则自行解析
	// （按等级策略：预览回退默认语言、发布失败）。
	langs := in.TargetLangs
	if len(langs) == 0 {
		langs = in.SiteLangs
	}
	if len(langs) == 0 {
		resolved, err := ResolveSiteLangs(in.Ctx, in.Project, in.ProjectID, in.LangFallback)
		if err != nil {
			// 发布口径下语言表不可读：**不产出互指**，而不是产出「只指默认语言」的
			// 互指。后者是错的产物且看起来正常（审计 I18N-02 要消灭的正是这种）。
			// 真正的失败已由调用方（SiteCompileOptions）在上游报出。
			return nil, nil
		}
		langs = resolved
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
