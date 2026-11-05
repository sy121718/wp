package pipeline

import (
	"go_wp/pkg/publish"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"go_wp/pkg/builder"
	"go_wp/pkg/builder/core"
	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/pkg/seo"
	"go_wp/pkg/i18n"
	"go_wp/pkg/logger"
)

type SiteRouteEntry struct {
	Lang string
	Path string
}

func LangURLRuleForProject(ctx context.Context, project projectcontract.ProjectService, projectID string) publish.LangURLRule {
	return LangURLRuleForProjectWithDefault(ctx, project, projectID, DefaultLocale(ctx, project, projectID))
}

func LangURLRuleForProjectWithDefault(ctx context.Context, project projectcontract.ProjectService, projectID, defaultLang string) publish.LangURLRule {
	return LangURLRuleOf(SiteLangURLModeOf(ctx, project, projectID), defaultLang)
}

func SiteLangURLModeOf(ctx context.Context, project projectcontract.ProjectService, projectID string) i18n.SiteLangURLMode {
	if project != nil && strings.TrimSpace(projectID) != "" {
		raw, err := project.SiteLangURLMode(ctx, projectID)
		if err != nil {
			logger.Scene("settings").With("project", projectID).
				Error(err, "读取站点语言 URL 方案失败，回退全局默认方案")
		} else if trimmed := strings.TrimSpace(raw); trimmed != "" {
			mode, perr := i18n.ParseSiteLangURLMode(trimmed)
			if perr != nil {
				logger.Scene("settings").With("project", projectID).With("raw", raw).
					Error(perr, "站点语言 URL 方案取值非法，回退全局默认方案")
			} else {
				return mode
			}
		}
	}
	return i18n.DefaultSiteLangURLMode()
}

func LangURLRuleOf(mode i18n.SiteLangURLMode, defaultLang string) publish.LangURLRule {
	return publish.NewLangURLRule(
		i18n.SiteLangURLsSeparated(mode),
		i18n.SiteLangURLPrefixDefault(mode),
		defaultLang,
		i18n.URLCodeOverrides(),
	)
}

type LangFallback int

const (
	LangFallbackVisible LangFallback = iota

	LangFallbackForbidden
)

var ErrLangTableUnavailable = errors.New("站点语言清单不可读")

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

		if policy == LangFallbackForbidden {
			return nil, fmt.Errorf("%w: 工程 %s 的语言清单为空", ErrLangTableUnavailable, projectID)
		}
		return []string{i18n.GetDefaultLang()}, nil
	}

	if policy == LangFallbackForbidden {
		return nil, fmt.Errorf("%w: 缺少工程上下文（projectId=%q）", ErrLangTableUnavailable, projectID)
	}
	return []string{i18n.GetDefaultLang()}, nil
}

func EnabledLangs(ctx context.Context, project projectcontract.ProjectService, projectID string) []string {
	langs, _ := ResolveSiteLangs(ctx, project, projectID, LangFallbackVisible)
	return langs
}

func DefaultLocale(ctx context.Context, project projectcontract.ProjectService, projectID string) string {
	if project != nil && strings.TrimSpace(projectID) != "" {
		if d, err := project.DefaultLocale(ctx, projectID); err == nil && d != "" {
			return d
		}
	}
	return i18n.GetDefaultLang()
}

type SiteLangInputs struct {
	SiteLangs []string

	DefaultLang string
}

func SiteLangInputsOfPlan(plan *publish.PublicationPlan) (SiteLangInputs, bool) {
	if plan == nil {
		return SiteLangInputs{}, false
	}
	n := plan.Normalize()
	if len(n.SiteLangs) == 0 {
		return SiteLangInputs{}, false
	}
	if n.DefaultLang == "" {

		n.DefaultLang = n.SiteLangs[0]
	}
	return SiteLangInputs{SiteLangs: n.SiteLangs, DefaultLang: n.DefaultLang}, true
}

func PlanOfSiteLangInputs(in SiteLangInputs) publish.PublicationPlan {
	return publish.PublicationPlan{SiteLangs: in.SiteLangs, DefaultLang: in.DefaultLang}.Normalize()
}

func ResolveSiteLangInputs(ctx context.Context, project projectcontract.ProjectService, projectID string, policy LangFallback) (SiteLangInputs, error) {
	langs, err := ResolveSiteLangs(ctx, project, projectID, policy)
	if err != nil {
		return SiteLangInputs{}, err
	}
	return SiteLangInputs{SiteLangs: langs, DefaultLang: DefaultLocale(ctx, project, projectID)}, nil
}

func SitePath(rule publish.LangURLRule, lang, logical string) (string, error) {
	return rule.Path(lang, logical)
}

func LocalizeMenuURL(ctx context.Context, project projectcontract.ProjectService, projectID, lang, raw string) string {
	return LocalizeMenuURLWith(LangURLRuleForProject(ctx, project, projectID), EnabledLangs(ctx, project, projectID), lang, raw)
}

func LocalizeMenuURLWithRaw(rule publish.LangURLRule, langs []string, lang, raw string) string {
	u := strings.TrimSpace(raw)
	if u == "" || !strings.HasPrefix(u, "/") || strings.HasPrefix(u, "//") {
		return raw
	}
	u = seo.CanonicalPublicPath(u)
	if _, logical, ok := rule.Locate(u, langs); ok {
		u = logical
	}
	p, err := SitePath(rule, lang, u)
	if err != nil {
		return raw
	}
	return seo.CanonicalPublicPath(p)
}

func LocalizeMenuURLWith(rule publish.LangURLRule, langs []string, lang, raw string) string {
	u := strings.TrimSpace(raw)
	if u == "" || !strings.HasPrefix(u, "/") || strings.HasPrefix(u, "//") {
		return raw
	}
	u = seo.CanonicalPublicPath(u)
	if _, logical, ok := rule.Locate(u, langs); ok {
		u = logical
	}
	p, err := SitePath(rule, lang, u)
	if err != nil {
		return raw
	}
	return AbsoluteSiteURL(seo.CanonicalPublicPath(p))
}

func AbsoluteSiteURL(pathOrURL string) string {
	return seo.AbsoluteSiteURL(pathOrURL)
}

func SiteRouteEntries(ctx context.Context, project projectcontract.ProjectService, projectID, logical string) ([]SiteRouteEntry, error) {
	return siteRouteEntriesForLangs(LangURLRuleForProject(ctx, project, projectID), EnabledLangs(ctx, project, projectID), logical)
}

func siteRouteEntriesForLangs(rule publish.LangURLRule, langs []string, logical string) ([]SiteRouteEntry, error) {
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

type LocaleViewInput struct {
	Ctx         context.Context
	Project     projectcontract.ProjectService
	ProjectID   string
	LogicalPath string
	Lang        string

	TargetLangs []string

	Published func(accessPath string) bool

	SiteLangs []string

	DefaultLang string

	Rule *publish.LangURLRule

	SiteLangURLMode string

	LangFallback LangFallback
}

func LocaleView(in LocaleViewInput) (alts []builder.Alternate, links []core.LocaleLink) {

	mode, perr := i18n.ParseSiteLangURLMode(in.SiteLangURLMode)
	if perr != nil || strings.TrimSpace(in.SiteLangURLMode) == "" {
		mode = SiteLangURLModeOf(in.Ctx, in.Project, in.ProjectID)
	}
	if !i18n.SiteLangURLsSeparated(mode) || strings.TrimSpace(in.ProjectID) == "" || strings.TrimSpace(in.LogicalPath) == "" {
		return nil, nil
	}

	langs := in.TargetLangs
	if len(langs) == 0 {
		langs = in.SiteLangs
	}
	if len(langs) == 0 {
		resolved, err := ResolveSiteLangs(in.Ctx, in.Project, in.ProjectID, in.LangFallback)
		if err != nil {

			return nil, nil
		}
		langs = resolved
	}

	defaultLang := strings.TrimSpace(in.DefaultLang)
	if defaultLang == "" {
		defaultLang = DefaultLocale(in.Ctx, in.Project, in.ProjectID)
	}
	rule := LangURLRuleOf(mode, defaultLang)
	if in.Rule != nil {
		rule = *in.Rule
	}
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

func ContentTranslationEnabled(ctx context.Context, project projectcontract.ProjectService, projectID, lang string) bool {
	return ContentTranslationEnabledFor(DefaultLocale(ctx, project, projectID), lang)
}

func ContentTranslationEnabledFor(defaultLang, lang string) bool {
	l := strings.TrimSpace(lang)
	if l == "" {
		return false
	}
	return l != strings.TrimSpace(defaultLang)
}
