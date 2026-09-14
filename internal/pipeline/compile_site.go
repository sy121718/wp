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
	MediaProbe func(ctx context.Context, url string) []int
}

// SiteCompileParams 单次编译的站点上下文。
type SiteCompileParams struct {
	Ctx         context.Context
	ProjectID   string
	Lang        string
	LogicalPath string // hreflang / 语言切换；空则跳过 alternates
	CurrentPath string // 导航当前项高亮；空则跳过
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
		alts, links := LocaleView(p.Ctx, ports.Project, p.ProjectID, lp, p.Lang)
		if len(alts) > 1 {
			opts = append(opts, builder.WithAlternates(alts))
		}
		if len(links) > 1 {
			opts = append(opts, builder.WithLocaleLinks(links))
		}
	}
	if ports.MediaProbe != nil {
		probe := ports.MediaProbe
		opts = append(opts, builder.WithAssetProbe(func(url string) []int {
			return probe(p.Ctx, url)
		}))
	}
	return opts, nil
}
