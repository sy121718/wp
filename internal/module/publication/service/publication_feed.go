package pubservice

// publication_feed.go — 站点级 feed 的刷新（审计 SEO-012）。
//
// 入口与 sitemap 同一条链：page 发布激活后调 RefreshSiteFiles，这里在写完
// sitemap.xml / robots.txt 之后顺手刷新 feed.xml（见 publication_sitefiles.go）。
//
// 三个输入决定了条目集合：
//
//  1. **已激活路由行**（page_routes，route_kind=active）—— 与 sitemap 同一个真源，
//     feed 里的链接因此与 sitemap 的 loc 逐字同源，不存在「两套路径规则」；
//  2. **归属者是自动发布实例的行**（presentation_id 非空）—— 手工页面不计入
//     「最新内容」，否则 feed 会变成整站改动的流水账；
//  3. **产物里带文章结构化数据**的页面（JSON-LD @type=Article 等）—— 商品详情页
//     同样是实例归属，但不属于「最新内容」。
//
// 语言：多语言站点只收默认语言版本的条目（站点级 feed 一份）。语言归属判定复用
// pipeline.LangURLRule —— 与 sitemap 的分组用的是同一份规则（sitemapEntries 里
// 那句 NewLangURLRule 现在抽成了 siteLangRule，两处共用）。
//
// 时间：条目的 pubDate 取路由行的 update_time（该路径最近一次激活时刻）。产物字节里
// 没有时间（构建期不注入时间戳是有意的确定性约束），因此「最近发布」只能来自路由。
//
// 条目数上限与总开关：见 internal/seo/feed.go 的 FeedEnabled / FeedLimit。

import (
	"context"
	"net/url"
	"sort"
	"strings"

	"go_wp/internal/module/publication/model"
	"go_wp/internal/pipeline"
	"go_wp/internal/seo"
	"go_wp/pkg/i18n"
)

// refreshFeed 生成/刷新站点级 feed（写入激活目录根，与 sitemap.xml 同级）。
func (s *Service) refreshFeed(ctx context.Context, projectID, baseURL, dir string, langs []string, defaultLang string) (err error) {
	if projectID == "" || dir == "" {
		return nil
	}
	if !seo.FeedEnabled() {
		// 开关关闭时删除既有 feed（而不是不写）：激活目录直接对外服务，
		// 不删等于旧 entry 继续可见，指向的可能已是 404（见 seo.RemoveFeed）。
		return seo.RemoveFeed(dir)
	}
	routes, err := s.model.ListActiveRoutes(ctx, projectID)
	if err != nil {
		return err
	}
	items := feedItems(dir, baseURL, routes, langs, defaultLang)
	channel := feedChannel(dir, baseURL, routes, langs, defaultLang)
	return seo.WriteFeed(dir, channel, items, seo.FeedLimit())
}

// siteLangRule 构造站点语言 URL 规则（sitemap 分组与 feed 归属的唯一构造点）。
//
// 第二个返回值为 false 表示「不做语言分组」：单语言站点（langs < 2）或语言路径
// 未分离时，路径本身就是逻辑路径，再分一次组会把条目全过滤掉。
func siteLangRule(langs []string, defaultLang string) (rule pipeline.LangURLRule, ok bool) {
	if !i18n.SiteLangURLsSeparated() || len(langs) < 2 {
		return pipeline.LangURLRule{}, false
	}
	return pipeline.NewLangURLRule(true, i18n.SiteLangURLPrefixDefault(), defaultLang, i18n.URLCodeOverrides()), true
}

// feedItems 已激活路由 → feed 条目（由 seo.BuildRSS 负责排序与截断）。
//
// 顺序先按「最近激活时刻倒序 → 路径升序」定好：feed 的条目上限截断发生在
// BuildRSS 内部，若这里不先排序，「最新 N 条」会退化成「路径最小的 N 条」。
func feedItems(dir, baseURL string, routes []pubmodel.RouteEntity, langs []string, defaultLang string) []seo.FeedItem {
	rule, separate := siteLangRule(langs, defaultLang)
	// 默认语言未知（调用方没拿到站点语言清单）时不做语言筛选：宁可多收几条，
	// 也不能因为「不知道哪个是默认语言」而把 feed 收空。
	separate = separate && strings.TrimSpace(defaultLang) != ""

	cands := make([]pubmodel.RouteEntity, 0, len(routes))
	for _, rt := range routes {
		if rt.PresentationID == nil || strings.TrimSpace(*rt.PresentationID) == "" {
			continue
		}
		if separate {
			lang, _, ok := rule.Locate(rt.Path, langs)
			if !ok || !strings.EqualFold(lang, defaultLang) {
				continue
			}
		}
		cands = append(cands, rt)
	}
	sort.SliceStable(cands, func(i, j int) bool {
		if !cands[i].UpdatedAt.Equal(cands[j].UpdatedAt) {
			return cands[i].UpdatedAt.After(cands[j].UpdatedAt)
		}
		return cands[i].Path < cands[j].Path
	})

	items := make([]seo.FeedItem, 0, len(cands))
	for _, rt := range cands {
		file, ok := activeHTMLFile(dir, rt.Path)
		if !ok {
			continue // 该路径的产物不在激活目录里（例如刚取消激活）：跳过而不是产出一条死链
		}
		doc, ok := readFeedDoc(file)
		if !ok || !doc.isArticle() {
			continue
		}
		items = append(items, seo.FeedItem{
			Title:       doc.Title,
			Link:        seo.JoinURL(baseURL, rt.Path),
			Description: doc.Description,
			PubDate:     rt.UpdatedAt,
		})
	}
	return items
}

// feedChannel 站点级 feed 的元信息（标题 / 描述 / 语言）。
//
// 标题与描述取默认语言首页产物的 <head>：站点名与站点描述在构建期就烘进了首页产物，
// 而 publication 没有 project 契约可查站点设置（跨模块取数要改装配签名）。取不到时
// 回退 host —— 空 title 的 feed 在阅读器里是没有名字的订阅源（见 seo.BuildRSS）。
func feedChannel(dir, baseURL string, routes []pubmodel.RouteEntity, langs []string, defaultLang string) seo.FeedChannel {
	ch := seo.FeedChannel{
		Link:     seo.JoinURL(baseURL, "/"),
		Language: strings.TrimSpace(defaultLang),
	}
	for _, p := range homeCandidatePaths(langs, defaultLang) {
		if ch.Title != "" && ch.Description != "" {
			break
		}
		file, ok := activeHTMLFile(dir, p)
		if !ok {
			continue
		}
		doc, ok := readFeedDoc(file)
		if !ok {
			continue
		}
		if ch.Title == "" {
			ch.Title = doc.Title
		}
		if ch.Description == "" {
			ch.Description = doc.Description
		}
	}
	if ch.Title == "" {
		ch.Title = feedHost(baseURL)
	}
	return ch
}

// homeCandidatePaths 首页产物的候选路径（默认语言根 → /index → /）。
//
// 三种写法都可能出现：多语言方案下默认语言根是 /index（all_prefix 下是 /{code}/index）、
// 单语言站点是 /index 或 /。全部试一遍比「猜哪种方案」可靠。
func homeCandidatePaths(langs []string, defaultLang string) []string {
	out := make([]string, 0, 3)
	if rule, ok := siteLangRule(langs, defaultLang); ok && strings.TrimSpace(defaultLang) != "" {
		if p, err := rule.Path(defaultLang, "/"); err == nil {
			out = append(out, p)
		}
	}
	out = append(out, "/index", "/")

	// 同一条路径可能被上面的规则与字面量重复给出（如单语言站点两处都是 /index）：
	// 去重后返回，调用方按顺序取第一个能读到的产物。
	seen := make(map[string]bool, len(out))
	uniq := make([]string, 0, len(out))
	for _, p := range out {
		if seen[p] {
			continue
		}
		seen[p] = true
		uniq = append(uniq, p)
	}
	return uniq
}

// feedHost baseURL → host（feed 标题的兜底值）。
func feedHost(baseURL string) string {
	raw := strings.TrimSpace(baseURL)
	if u, err := url.Parse(raw); err == nil && u.Hostname() != "" {
		return u.Hostname()
	}
	return strings.TrimRight(raw, "/")
}
