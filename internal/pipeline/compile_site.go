package pipeline

import (
	"go_wp/pkg/publish"
	"context"
	"strings"

	"go_wp/pkg/builder"
	"go_wp/pkg/builder/core"
	mediacontract "go_wp/internal/module/media/contract"
	navigationcontract "go_wp/internal/module/navigation/contract"
	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/pkg/seo"
)

type SiteCompilePorts struct {
	Project    projectcontract.ProjectService
	Navigation navigationcontract.NavigationService

	RoutePublished func(accessPath string) bool

	MediaProbe func(ctx context.Context, url string) []mediacontract.VariantRef
}

type SiteCompileParams struct {
	Ctx         context.Context
	ProjectID   string
	Lang        string
	LogicalPath string
	CurrentPath string

	TargetLangs []string

	Plan *publish.PublicationPlan
}

func siteLangInputsFor(ports SiteCompilePorts, p SiteCompileParams) (inputs SiteLangInputs, policy LangFallback, publishFact bool, err error) {
	if in, ok := SiteLangInputsOfPlan(p.Plan); ok {
		return in, LangFallbackForbidden, true, nil
	}
	if publishLangScope(ports, p) {
		policy = LangFallbackForbidden
	}
	in, rerr := ResolveSiteLangInputs(p.Ctx, ports.Project, p.ProjectID, policy)
	if rerr != nil {
		return SiteLangInputs{}, policy, false, rerr
	}
	return in, policy, policy == LangFallbackForbidden, nil
}

func publishLangScope(ports SiteCompilePorts, p SiteCompileParams) bool {
	return ports.RoutePublished != nil &&
		strings.TrimSpace(p.LogicalPath) != "" &&
		strings.TrimSpace(p.ProjectID) != ""
}

func SiteCompileOptions(ports SiteCompilePorts, p SiteCompileParams) (opts []builder.CompileOption, err error) {
	opts = append(opts, builder.WithProjectID(p.ProjectID))
	if cp := p.CurrentPath; cp != "" {
		opts = append(opts, builder.WithCurrentPath(cp))
	}

	inputs, policy, publishFact, lerr := siteLangInputsFor(ports, p)
	if lerr != nil {
		return nil, lerr
	}

	mode := SiteLangURLModeOf(p.Ctx, ports.Project, p.ProjectID)

	rule := LangURLRuleOf(mode, inputs.DefaultLang)
	if ports.Navigation != nil {
		opts = append(opts, builder.WithNavigationResolver(&NavigationAdapter{
			Svc: ports.Navigation, Project: ports.Project, Ctx: p.Ctx,
			ProjectID: p.ProjectID, Lang: p.Lang,

			Rule: &rule, SiteLangs: inputs.SiteLangs,
		}))
	}
	if lp := p.LogicalPath; lp != "" {

		if publishFact {
			frozen := PlanOfSiteLangInputs(inputs)
			if p.Plan != nil {
				frozen.References = p.Plan.References.Clone()
			}
			CompileUsageFromContext(p.Ctx).SetPublicationPlan(frozen)
		}

		published := ports.RoutePublished
		if len(p.TargetLangs) == 0 {

			if published != nil && p.Lang != "" {
				if self, serr := SitePath(rule, p.Lang, lp); serr == nil {
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

			SiteLangs: inputs.SiteLangs, DefaultLang: inputs.DefaultLang, Rule: &rule,
			SiteLangURLMode: string(mode),
			LangFallback:    policy,
		})
		if len(alts) > 1 {
			opts = append(opts, builder.WithAlternates(alts))
		}
		if len(links) > 1 {
			opts = append(opts, builder.WithLocaleLinks(links))
		}
	}

	if p.Lang != "" {
		opts = append(opts, builder.WithSiteLinkResolver(func(logical string) string {
			localized, serr := SitePath(rule, p.Lang, logical)
			if serr != nil {
				return logical
			}

			return AbsoluteSiteURL(seo.CanonicalPublicPath(localized))
		}))
	}
	if ports.MediaProbe != nil {
		probe := ports.MediaProbe
		opts = append(opts, builder.WithAssetProbe(func(url string) []core.AssetVariant {
			refs := probe(p.Ctx, url)
			out := make([]core.AssetVariant, 0, len(refs))
			for _, r := range refs {
				out = append(out, core.AssetVariant{URL: r.URL, Width: r.Width})
			}
			return out
		}))
	}
	return opts, nil
}
