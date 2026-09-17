package pipeline

// compile_site.go — 站点级 CompileOption 装配（page / presentation 共用，EDT-003）。
//
// 基础层约定：凡「导航 / 槽位 / 当前路径 / hreflang / srcset」一律经
// SiteCompileOptions 注入；page.compileDocument 与 presentation.renderHTML
// 均调用此函数，禁止在模块内再写一份。模块差异项（插件、页眉页脚、实体解析器）
// 在各自路径上追加即可。

import (
	"context"

	"go_wp/internal/builder"
	navigationcontract "go_wp/internal/module/navigation/contract"
	pagecontract "go_wp/internal/module/page/contract"
	projectcontract "go_wp/internal/module/project/contract"
)

// SiteCompilePorts 站点装配可选依赖（nil 表示跳过该项注入）。
type SiteCompilePorts struct {
	Project    projectcontract.ProjectService
	Navigation navigationcontract.NavigationService
	SitePages  pagecontract.SitePageResolver
	// RoutePublished 报告某个访问路径是否真的已发布（审计 I18N-021，可空）。
	//
	// 可空是刻意的：为 nil 时语言切换器按「全部已发布」输出，行为与接入前逐字一致；
	// 实现由装配层提供（已激活产物的访问面本身就是真相），pipeline 不猜它的来源。
	RoutePublished func(accessPath string) bool
	MediaProbe     func(ctx context.Context, url string) []int
}

// SiteCompileParams 单次编译的站点上下文。
type SiteCompileParams struct {
	Ctx         context.Context
	ProjectID   string
	Lang        string
	LogicalPath string // hreflang / 语言切换；空则跳过 alternates
	CurrentPath string // 导航当前项高亮；空则跳过
	// TargetLangs 本批次准备上线的语言集合（SEO-026），由批次发布方传入 —— 必须与
	// 它自己逐语言结案用的那一份是同一个切片，不许各自再读一次语言清单。
	//
	// 非空 = 批次口径：hreflang 互指按这份集合生成，不回读访问面 / 语言账本。
	// 为空 = 访问面口径：沿用 RoutePublished（手工 Page 的逐页发布）。
	TargetLangs []string
}

// SiteCompileOptions 构造 page 与 presentation 共用的站点级 CompileOption。
func SiteCompileOptions(ports SiteCompilePorts, p SiteCompileParams) (opts []builder.CompileOption, err error) {
	opts = append(opts, builder.WithProjectID(p.ProjectID))
	if cp := p.CurrentPath; cp != "" {
		opts = append(opts, builder.WithCurrentPath(cp))
	}
	if ports.Navigation != nil {
		opts = append(opts, builder.WithNavigationResolver(&NavigationAdapter{
			Svc: ports.Navigation, Project: ports.Project, Ctx: p.Ctx,
			ProjectID: p.ProjectID, Lang: p.Lang,
		}))
	}
	if ports.SitePages != nil {
		if sitePages, serr := ports.SitePages.ResolveSitePages(p.Ctx, p.ProjectID, p.Lang); serr != nil {
			return nil, serr
		} else if len(sitePages) > 0 {
			opts = append(opts, builder.WithSitePages(sitePages))
		}
	}
	if lp := p.LogicalPath; lp != "" {
		// 判定依据二选一（SEO-026）：
		//
		//   批次口径：调用方传了 TargetLangs —— 本批次要上线哪些语言是**构建输入**，
		//   构建期就已确定，产物只依赖它；published 整个不参与（自动发布是「先构建
		//   全部语言、再逐语言结案」，账本行要到结案才写，首发布时按它过滤会把本次
		//   正在上线的语言全部剔除，产物缺 hreflang，重建一次才补上）。
		//
		//   访问面口径：没有批次概念的手工 Page —— 行为与接入前逐字一致。
		published := ports.RoutePublished
		if len(p.TargetLangs) == 0 {
			// 本次构建的语言自己也当作「已发布」（审计 I18N-021 的确定性修正）。
			//
			// 不加这一句会破坏「同一输入产出同一 hash」：产物里的语言切换器按访问面过滤，
			// 而 Build 与 Publish 的复构建之间，其它语言可能刚好发布了 —— 于是两次构建
			// 产出不同的切换器，Publish 的 built != staged 校验把发布挡成 ErrRebuildRequired。
			// 表现是「先构建两种语言再逐个发布」这个常规流程永远发布不了第二种语言。
			//
			// 把「自己」并进集合是自洽的：正在构建它，紧接着就要发布它；而对别的语言
			// 仍然只认访问面 —— 没发布的语言不会进切换器，I18N-021 要防的 404 依然防住。
			if published != nil && p.Lang != "" {
				if self, serr := SitePath(LangURLRuleForProject(p.Ctx, ports.Project, p.ProjectID), p.Lang, lp); serr == nil {
					base, selfPath := published, self
					published = func(accessPath string) bool {
						return accessPath == selfPath || base(accessPath)
					}
				}
			}
		}
		alts, links := LocaleView(LocaleViewInput{
			Ctx: p.Ctx, Project: ports.Project, ProjectID: p.ProjectID,
			LogicalPath: lp, Lang: p.Lang,
			TargetLangs: p.TargetLangs, Published: published,
		})
		if len(alts) > 1 {
			opts = append(opts, builder.WithAlternates(alts))
		}
		if len(links) > 1 {
			opts = append(opts, builder.WithLocaleLinks(links))
		}
	}
	// 站内链接本地化（审计 I18N-015）：作者手填的 /shop 在英文站点上要变成 /en/shop，
	// 否则按钮点过去就跳回默认语言 —— 页面自身语言是对的，链接却把人带走。
	//
	// 规则与页面路径**同源**（LangURLRuleForProject → LangURLRule），不另立一套：
	// 两套规则会出现「页面在 /en/shop、按钮指向 /en/shop/」这类只差一个斜杠的坏链。
	//
	// 只被组件的静态链接调用（作者填的逻辑路径）；CMS 绑定值不经过这里 ——
	// 内容里的 URL 语义由内容作者掌握，再前缀一次可能指到不存在的地址。
	if p.Lang != "" {
		rule := LangURLRuleForProject(p.Ctx, ports.Project, p.ProjectID)
		opts = append(opts, builder.WithSiteLinkResolver(func(logical string) string {
			localized, serr := SitePath(rule, p.Lang, logical)
			if serr != nil {
				return logical // 规则解析失败原样输出：缺前缀是可见降级，抛错会让整页构建失败
			}
			return localized
		}))
	}
	if ports.MediaProbe != nil {
		probe := ports.MediaProbe
		opts = append(opts, builder.WithAssetProbe(func(url string) []int {
			return probe(p.Ctx, url)
		}))
	}
	return opts, nil
}
