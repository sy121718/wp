package pageservice

// page_seo_patrol.go — 构建期 SEO 合规校验的接入点（审计 SEO-01 前半段）。
//
// 三件事，边界写死在这里：
//  1. seoLangs：把站点的「语言 → 路径前缀」交给校验器（映射点仍是 pipeline.LangURLRule）；
//  2. inspectBuiltArtifact：构建完成后对**刚产出的字节**跑一次确定性校验，命中就记日志；
//  3. SEOPatrol：按激活清单逐份校验，产出可复核的巡检报告（URL / 规则 / 证据 / ArtifactHash）。
//
// 为什么三件事都**不改发布结果**：审计明确反对把内部信号当作发布成功或排名的依据
//（SEO-01 的验收那段），而 SEO 评分（internal/seo/scoring）尤其不得变成闸门。
// 合规校验比评分硬，但它判的仍是「产物内部是否自相矛盾」——确定性事实不足以决定
// 一份内容该不该上线（作者可能正要下线一个 noindex 页面）。因此本文件只产出证据：
// 构建期进日志、巡检进接口，人工据此决定怎么改。
//
// 与访问面抓取校验的分工（报告后半段，本轮不做）：本文件读的是**本机产物存储**与
// **本机路由表**，不发任何网络请求，因此结论也不依赖线上是否可达；「线上 301 是否
// 真的生效、sitemap 是否真的被收录」必须由发布后的抓取与站长平台数据回答。
//
// 与既有「产物 SEO 体检」的分工（internal/module/publication/service/seo_audit.go，
// 审计 SEO-019）—— 两者不是同一件事，刻意没有合并：
//   · 覆盖面不同：体检看「有没有」（缺 title / 缺 canonical / 图片缺 alt / 内链断了 /
//     title 重复），本文件看「自相矛盾」（两条 canonical、canonical 与产物路径不一致、
//     JSON-LD 解析不了或与标题不符、robots 取值不受控、noindex 却进了 sitemap、互指单向）；
//   · 锚点不同：体检的结论只有路径与文案，本文件的每条结论都带 ArtifactHash —— 报告
//     验收要求「可复核」，而同一路径的产物会随重建换掉，没有 hash 就回不到那份字节；
//   · 时机不同：体检读 active 目录（**只有已发布的**），本文件在构建期就能跑（还挂着
//     缓存的那份暂存产物也能查），因此发布前就能看见矛盾。
// 合并的代价是本次不允许改 publication 模块（文件域），且合并会把「按 active 目录直读」
// 与「按产物 hash 直读」两种取数方式混在一处；已在报告里列为后续可复核项。

import (
	"context"
	"fmt"
	"strings"
	"time"

	artifactcontract "go_wp/internal/module/artifact/contract"
	pagedto "go_wp/internal/module/page/dto"
	"go_wp/internal/pipeline"
	"go_wp/internal/seo"
	"go_wp/internal/seo/compliance"
	"go_wp/pkg/logger"
	"go_wp/pkg/utils"
)

// seoLangProbePath 反推语言前缀用的探针路径。
//
// 为什么要探针而不是直接拼 "/" + 语言码：前缀由 pipeline.LangURLRule.Path 单点决定
// （默认语言可能不带前缀、语言码可能映射成短码、首页还会变成 /index），
// 自己拼一份等于把那条规则抄第二遍 —— 抄错的表现是「校验说路径不符，但线上是对的」。
const seoLangProbePath = "/__seo_lang_probe__"

// seoLangs 站点启用语言的 URL 形状（不足两种语言时返回 nil：单语言站点没有前缀概念）。
func (s *Service) seoLangs(ctx context.Context, projectID string) []compliance.LangRule {
	if s == nil || s.project == nil || strings.TrimSpace(projectID) == "" {
		return nil
	}
	langs := pipeline.EnabledLangs(ctx, s.project, projectID)
	if len(langs) < 2 {
		return nil
	}
	return langRulesOf(pipeline.LangURLRuleForProject(ctx, s.project, projectID), langs)
}

// langRulesOf 由语言 URL 规则推导校验器的语言表（纯函数：只依赖规则与语言清单）。
//
// 抽成纯函数是为了可测：语言前缀的推导是这次整改里最容易写错的一处
// （默认语言可能不带前缀、语言码可能映射成短码），而它错了会直接产出错误的
// 「语种 / 路径不一致」结论 —— 那种假警比不校验更糟。
func langRulesOf(rule pipeline.LangURLRule, langs []string) []compliance.LangRule {
	out := make([]compliance.LangRule, 0, len(langs))
	for _, lang := range langs {
		p, err := rule.Path(lang, seoLangProbePath)
		if err != nil {
			// 语言或路径非法：构建期本身就会失败，这里不替它编一个前缀
			//（编错的前缀会让「语种 / 路径一致」这条规则给出错误的结论）。
			continue
		}
		out = append(out, compliance.LangRule{
			Code:   lang,
			Prefix: strings.TrimSuffix(p, seoLangProbePath),
		})
	}
	return out
}

// inspectArtifactSEO 读取产物字节并做确定性校验（只读：不写库、不改产物、不发网络请求）。
//
// sitemapListed 由调用方回答：sitemap 由**已激活路径**生成，因此「这份产物会不会进
// sitemap」= 「它当前是否已激活」（构建期用 pathListedInSitemap 如实判定），
// 而发布路径上它是确定事实（下一步就激活），巡检激活面时恒为 true。
func (s *Service) inspectArtifactSEO(hash, url, lang string, sitemapListed bool, langs []compliance.LangRule) (*compliance.Report, error) {
	if s == nil || s.store == nil {
		return nil, fmt.Errorf("产物存储未就绪")
	}
	if strings.TrimSpace(hash) == "" {
		return nil, fmt.Errorf("产物 hash 为空")
	}
	art, err := s.store.GetArtifact(pipeline.ArtifactLocator(hash))
	if err != nil {
		return nil, err
	}
	return compliance.Inspect(compliance.Artifact{
		URL: url, Lang: lang, ArtifactHash: hash,
		HTML: art.Entries["index.html"], Langs: langs, SitemapListed: sitemapListed,
	}), nil
}

// inspectBuiltArtifact 构建 / 发布路径上的构建期校验：命中就记日志，绝不阻断。
//
// sitemapListed 由调用方按**事实**回答（见 pathListedInSitemap）：sitemap 由激活路径
// 生成，所以「这份产物会不会进 sitemap」= 「这个路径此刻是否已激活」。构建期不猜
// 「发布之后大概会进」—— 猜出来的结论就是假警，而假警会让整份报告失去意义。
//
// 失败一律降级：读不到产物字节时只记一条告警 —— 校验是观测手段，
// 不能因为它自己出问题而让一次本来好的构建失败。
//
// 代价说明：这里会再读一次产物（manifest.json + index.html），而 ensureArtifactRow
// 归档时已经读过一次。多这一次是有意的 —— 校验的证据必须是**落盘之后**的字节
// （内存里那份还没经过 NewArtifact/EncodeManifest 的确定性编码），而构建本身是低频
// 重操作，多一次小文件读入不值得为它把归档的返回值改胖。
func (s *Service) inspectBuiltArtifact(ctx context.Context, projectID, hash, url, lang string, sitemapListed bool) {
	langs := s.seoLangs(ctx, projectID)
	rep, err := s.inspectArtifactSEO(hash, url, lang, sitemapListed, langs)
	if err != nil {
		logger.Scene("build").With("hash", hash).With("url", url).Warn("构建期 SEO 合规校验跳过：产物字节不可读")
		return
	}
	logSEOReport("build", rep)
}

// pathListedInSitemap 判断某个访问路径此刻是否已在站点 sitemap 里。
//
// 判定来源是**访问面本身**（激活目录的符号链接），与 sitemap 的生成口径同源
// （publication.RefreshSiteFiles 由已激活路径写 sitemap）—— 查数据库的路由表会得出
// 「已登记 = 已收录」，而登记与激活是两件事。这与语言切换器的发布态判定是同一实现。
func (s *Service) pathListedInSitemap(path string) bool {
	if s == nil || s.publication == nil || strings.TrimSpace(path) == "" {
		return false
	}
	state, err := s.publication.Inspect(path)
	return err == nil && state != nil && state.Kind != "none"
}

// logSEOReport 把结论逐条写进结构化日志（URL / 规则 / 证据 / ArtifactHash 四要素齐全）。
func logSEOReport(scene string, rep *compliance.Report) {
	if rep == nil {
		return
	}
	for _, f := range rep.Findings {
		entry := logger.Scene(scene).
			With("url", f.URL).With("lang", f.Lang).With("rule", f.Rule).
			With("artifactHash", f.ArtifactHash).With("evidence", f.Evidence)
		if f.Level == compliance.LevelError {
			entry.Warn("构建期 SEO 合规校验：产物自相矛盾")
			continue
		}
		entry.Info("构建期 SEO 合规校验：需人工确认")
	}
}

// SEOPatrol 站点 SEO 合规巡检：按激活清单逐份校验，产出可复核的报告。
//
// 数据来源全部在本机：page_routes 的 active 行（谁在线）+ 本地产物存储（在线的是什么字节）。
// 不发网络请求，因此「线上可抓取性」不在本方法的结论范围内 —— 那是报告后半段（抓取校验
// 与站长平台数据）的事。
//
// 未被纳入的路由（缺产物行、自动发布实例的产物归别的模块）在报告的 Unchecked 里如实列出：
// 巡检报告的价值取决于「它漏了什么」同样可见。
func (s *Service) SEOPatrol(ctx context.Context, req *pagedto.SEOPatrolReq) (res *pagedto.SEOPatrolResp, err error) {
	if req == nil || strings.TrimSpace(req.ProjectID) == "" {
		return nil, ErrInvalidParam
	}
	projectID := strings.TrimSpace(req.ProjectID)
	langs := s.seoLangs(ctx, projectID)
	routes, err := s.model.ListActiveRoutesByProject(ctx, projectID)
	if err != nil {
		return nil, err
	}

	arts := make([]compliance.Artifact, 0, len(routes))
	res = &pagedto.SEOPatrolResp{
		ProjectID: projectID,
		SampledAt: utils.NewJSONTime(time.Now().UTC()),
	}
	unchecked := func(url, reason string) {
		res.Unchecked = append(res.Unchecked, pagedto.SEOUncheckedRoute{URL: url, Reason: reason})
	}
	for _, rt := range routes {
		url := seo.CanonicalPublicPath(rt.Path)
		if rt.PresentationID != nil && strings.TrimSpace(*rt.PresentationID) != "" {
			// 自动发布实例的产物记在 presentation_artifacts：另一张表、另一个模块的属地，
			// page 模块按表隔离不能读它（跨模块直查才是更严重的问题）。
			unchecked(url, "归属自动发布实例：其产物元数据不属于 page 模块（本轮未纳入）")
			continue
		}
		if rt.ArtifactID == nil || strings.TrimSpace(*rt.ArtifactID) == "" {
			unchecked(url, "激活路由没有指向产物")
			continue
		}
		art, derr := s.artifacts.DetailByID(ctx, &artifactcontract.DetailByIDReq{ID: *rt.ArtifactID})
		if derr != nil || art == nil || art.ArtifactHash == "" {
			unchecked(url, "产物元数据读取失败（产物行不存在或不可读）")
			continue
		}
		loaded, lerr := s.store.GetArtifact(pipeline.ArtifactLocator(art.ArtifactHash))
		if lerr != nil {
			unchecked(url, "产物文件缺失或不可读："+art.ArtifactHash)
			continue
		}
		arts = append(arts, compliance.Artifact{
			URL: url, Lang: art.Lang, ArtifactHash: art.ArtifactHash,
			HTML: loaded.Entries["index.html"], Langs: langs,
			SitemapListed: true, // 激活清单即 sitemap 的条目来源
		})
	}

	site := compliance.InspectSite(arts)
	res.ChecksPerArtifact = site.Checks
	res.Checked = site.Checked
	res.Healthy = site.Healthy()
	res.Errors = site.ErrorCount()
	res.Warnings = site.WarnCount()
	res.Artifacts = make([]pagedto.SEOArtifactItem, 0, len(site.Reports))
	for _, r := range site.Reports {
		item := pagedto.SEOArtifactItem{
			URL: r.URL, Lang: r.Lang, ArtifactHash: r.ArtifactHash, Checks: r.Checks,
			Index: r.Index.Index, Follow: r.Index.Follow,
			RobotsExplicit: r.Index.Explicit, Robots: r.Index.Raw,
		}
		for _, f := range r.Findings {
			item.Findings = append(item.Findings, toSEOFindingItem(f))
		}
		res.Artifacts = append(res.Artifacts, item)
	}
	for _, f := range site.Findings {
		res.CrossFindings = append(res.CrossFindings, toSEOFindingItem(f))
	}
	if res.Errors > 0 {
		logger.Scene("page").With("projectId", projectID).
			With("checked", res.Checked).With("errors", res.Errors).With("warnings", res.Warnings).
			Warn("SEO 合规巡检发现产物自相矛盾")
	}
	return res, nil
}

// toSEOFindingItem 结论 → DTO（字段一一对应，四要素不丢）。
func toSEOFindingItem(f compliance.Finding) pagedto.SEOFindingItem {
	return pagedto.SEOFindingItem{
		URL: f.URL, Lang: f.Lang, ArtifactHash: f.ArtifactHash,
		Rule: f.Rule, Level: string(f.Level), Evidence: f.Evidence, Expect: f.Expect,
	}
}
