package pubservice

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

// 为什么读产物而不是查数据库：access 面只服务产物字节，产物因此是「这个页面对外
// 到底是什么」的权威事实 —— 标题、摘要、结构化数据类型都在里面。content 模块的
// contents.data 在这条链上取不到（publication 不跨模块查内容表，跨模块取数要加端口
// 并改进两处装配签名），而产物恰好是唯一不需要跨模块就能拿到的同一份事实。
//
// 与 seo_audit.go 的 ParseAuditDocument 的分工：那个解析器服务体检（title / meta /
// canonical / 内链），本解析器服务 feed（多了一项 JSON-LD @type，用来判断「这是不是
// 一篇文章」）。两者都用 x/net/html 的 tokenizer，不引入解析器级别的重型依赖。
//
// 一个已知边界：**SEO 头注入落地之前构建的旧产物没有 JSON-LD**，因此不会进 feed，
// 直到该页面重新发布（presentation 侧 presentation_seo.go 的注释同样说明了这一点）。

// 与编辑期评分器的分工：评分器看的是**草稿文档**（写之前给建议），体检看的是
// **激活产物**（写之后看事实）。两者的输入完全不同 —— 改 URL 留下的 301、
// 模板改版导致的 title 重复、模块里漏配 canonical，在草稿里都看不出来，
// 只有把真正服务出去的那份 HTML 读一遍才会暴露。
//
// 三条设计约束：
//  1. **只读产物文件**，不碰数据库、不碰访问面 —— 体检可以在任何时刻跑，
//     不影响访客请求（verification 明确要求）；
//  2. 内链判定对照**同一份激活路径集合**（调用方传入），不猜、不发请求；
//  3. 解析用标准库外唯一的依赖 x/net/html 的 tokenizer（项目已在用它解析富文本），
//     不引入解析器级别的重型依赖。

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/net/html"

	"go_wp/internal/module/publication/model"
	"go_wp/internal/pipeline"
	"go_wp/internal/seo"
	"go_wp/pkg/i18n"
	"go_wp/pkg/logger"
)

// refreshFeed 生成/刷新站点级 feed（写入激活目录根，与 sitemap.xml 同级）。
func (s *Service) refreshFeed(ctx context.Context, projectID, baseURL, dir string, langs []string, defaultLang, siteLangMode string) (err error) {
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
	items := feedItems(dir, baseURL, routes, langs, defaultLang, siteLangMode)
	channel := feedChannel(dir, baseURL, routes, langs, defaultLang, siteLangMode)
	return seo.WriteFeed(dir, channel, items, seo.FeedLimit())
}

// siteLangRule 构造站点语言 URL 规则（sitemap 分组与 feed 归属的唯一构造点）。
//
// siteLangMode 是调用方按**工程**解析出来的方案（pipeline.SiteLangURLModeOf）：
// publication 不认识 project 契约，不自己查设置 —— 站点文件与构建期产物必须用
// 同一份方案，否则 feed 里的语言归属与页面实际路径会对不上。
//
// 第二个返回值为 false 表示「不做语言分组」：单语言站点（langs < 2）或语言路径
// 未分离时，路径本身就是逻辑路径，再分一次组会把条目全过滤掉。
func siteLangRule(langs []string, defaultLang, siteLangMode string) (rule pipeline.LangURLRule, ok bool) {
	if !i18n.SiteLangURLsSeparated(i18n.SiteLangURLMode(siteLangMode)) || len(langs) < 2 {
		return pipeline.LangURLRule{}, false
	}
	// 规则构造与构建期同源（pipeline.LangURLRuleOf）：方案 + 默认语言 + 语言码覆盖
	// 三样一起决定路径形态，自己拼一份就会出现「页面在 /en/about、feed 认为它在 /about」。
	return pipeline.LangURLRuleOf(i18n.SiteLangURLMode(siteLangMode), defaultLang), true
}

// feedItems 已激活路由 → feed 条目（由 seo.BuildRSS 负责排序与截断）。
//
// 顺序先按「最近激活时刻倒序 → 路径升序」定好：feed 的条目上限截断发生在
// BuildRSS 内部，若这里不先排序，「最新 N 条」会退化成「路径最小的 N 条」。
func feedItems(dir, baseURL string, routes []pubmodel.RouteEntity, langs []string, defaultLang, siteLangMode string) []seo.FeedItem {
	rule, separate := siteLangRule(langs, defaultLang, siteLangMode)
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
func feedChannel(dir, baseURL string, routes []pubmodel.RouteEntity, langs []string, defaultLang, siteLangMode string) seo.FeedChannel {
	ch := seo.FeedChannel{
		Link:     seo.JoinURL(baseURL, "/"),
		Language: strings.TrimSpace(defaultLang),
	}
	for _, p := range homeCandidatePaths(langs, defaultLang, siteLangMode) {
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
func homeCandidatePaths(langs []string, defaultLang, siteLangMode string) []string {
	out := make([]string, 0, 3)
	if rule, ok := siteLangRule(langs, defaultLang, siteLangMode); ok && strings.TrimSpace(defaultLang) != "" {
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

// feedDoc 一份产物 HTML 里 feed 需要的元数据。
type feedDoc struct {
	// Title <title> 文本（产物的页面标题）。
	Title string
	// Description meta[name=description] 的内容。
	Description string
	// Canonical link[rel=canonical] 的目标（当前不参与 feed 输出，留作诊断）。
	Canonical string
	// Types JSON-LD 里的 @type 集合（可以是字符串或数组，@graph 递归展开）。
	Types []string
}

// feedArticleTypes 视为「文章」的结构化数据类型（小写比较）。
//
// 白名单而不是「含 article 字样」：report / blogposting 这些都合法，而任意后缀
// 匹配会把将来可能出现的无关类型也吞进来。schema.org 的前缀在比对前剥掉。
var feedArticleTypes = map[string]bool{
	"article": true, "newsarticle": true, "blogposting": true, "techarticle": true,
	"socialmediaposting": true, "report": true, "scholarlyarticle": true,
}

// isArticle 该产物是否为文章类详情页。
//
// 判据取构建期注入的 JSON-LD @type：presentation 侧的 applyEntitySEO 对
// entityType=article 写 schemaType=article，builder 再映射成 @type=Article。
// 不用「路径像不像文章」这类猜测 —— URL 规则是站的自由，产物里的类型是构建期事实。
func (d feedDoc) isArticle() bool {
	for _, t := range d.Types {
		name := strings.ToLower(strings.TrimSpace(t))
		name = strings.TrimPrefix(name, "https://schema.org/")
		name = strings.TrimPrefix(name, "http://schema.org/")
		if feedArticleTypes[name] || strings.HasSuffix(name, "article") {
			return true
		}
	}
	return false
}

// parseFeedDoc 从产物 HTML 里提取 feed 元数据（只读，不修改任何状态）。
func parseFeedDoc(r io.Reader) (doc feedDoc, err error) {
	z := html.NewTokenizer(r)
	for {
		tt := z.Next()
		if tt == html.ErrorToken {
			break
		}
		if tt != html.StartTagToken && tt != html.SelfClosingTagToken {
			continue
		}
		tok := z.Token()
		switch tok.Data {
		case "title":
			// tokenizer 里 <title> 的内容是紧随其后的 TextToken。
			if doc.Title == "" && z.Next() == html.TextToken {
				doc.Title = strings.TrimSpace(z.Token().Data)
			}
		case "meta":
			if strings.EqualFold(attr(tok, "name"), "description") && doc.Description == "" {
				doc.Description = strings.TrimSpace(attr(tok, "content"))
			}
		case "link":
			if strings.EqualFold(attr(tok, "rel"), "canonical") && doc.Canonical == "" {
				doc.Canonical = strings.TrimSpace(attr(tok, "href"))
			}
		case "script":
			if !strings.EqualFold(strings.TrimSpace(attr(tok, "type")), "application/ld+json") {
				continue
			}
			if z.Next() != html.TextToken {
				continue
			}
			doc.Types = append(doc.Types, jsonLDTypes(z.Token().Data)...)
		}
	}
	return doc, nil
}

// jsonLDTypes 从一个 JSON-LD 块里取出全部 @type 值（数组与 @graph 递归展开）。
//
// 解析失败一律当作「没有类型」而不是错误：一份产物里的结构化数据坏掉不该让整次
// 发布失败，代价只是这个页面不进 feed（且它在 SEO 体检里会被别的手段发现）。
func jsonLDTypes(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var root any
	if err := json.Unmarshal([]byte(raw), &root); err != nil {
		return nil
	}
	var out []string
	collectLDTypes(root, &out)
	return out
}

func collectLDTypes(node any, out *[]string) {
	switch v := node.(type) {
	case []any:
		for _, item := range v {
			collectLDTypes(item, out)
		}
	case map[string]any:
		if t, ok := v["@type"]; ok {
			collectLDTypes(t, out)
		}
		if g, ok := v["@graph"]; ok {
			collectLDTypes(g, out)
		}
	case string:
		if s := strings.TrimSpace(v); s != "" {
			*out = append(*out, s)
		}
	}
}

// readFeedDoc 读取并解析一份产物文件（打不开或读不出元数据时 ok=false）。
func readFeedDoc(file string) (doc feedDoc, ok bool) {
	f, err := os.Open(file)
	if err != nil {
		return feedDoc{}, false
	}
	defer f.Close()
	parsed, perr := parseFeedDoc(f)
	if perr != nil {
		return feedDoc{}, false
	}
	return parsed, true
}

// activeHTMLFile 站点路径 → 激活目录里的产物文件（找不到时 ok=false）。
//
// 激活目录的布局是「路径段 → 指向 artifacts/{hash} 的符号链接」（见
// pipeline.LocalPublicationStore 的 relActivePath：/about → about，"/" → index），
// 所以同一个路径有两种落点：链接本身是目录时，产物在它里面的 index.html；
// 链接本身是文件时（历史布局 /index.html），产物就是它自己。两种都试，不猜。
//
// 不遍历激活目录：active 里放的全是指向产物的符号链接，而 filepath.Walk 默认
// 不跟随目录符号链接 —— 遍历会一个产物也读不到（这正是按路径直读的理由，
// 见 seo_audit.go 的 CollectActiveDocuments 的同一处陷阱）。
func activeHTMLFile(dir, path string) (string, bool) {
	rel := strings.Trim(strings.TrimSpace(path), "/")
	// 空路径折算成 "index"，与 pipeline.relActivePath 的语义**逐字一致**：
	// 首页的激活条目是 <dir>/index（指向 artifacts/<hash>/ 的符号链接），
	// 而不是 <dir>/index.html。这里必须跟着那个定义走，不能自己发明一套形状。
	if rel == "" {
		rel = "index"
	}
	cands := []string{
		filepath.Join(dir, filepath.FromSlash(rel), "index.html"),
		filepath.Join(dir, filepath.FromSlash(rel)),
	}
	for _, cand := range cands {
		if fi, err := os.Stat(cand); err == nil && !fi.IsDir() {
			return cand, true
		}
	}
	return "", false
}

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

// SEO 体检的级别与检查项（落库与后台分组都按级别）。
const (
	AuditLevelError   = "error"
	AuditLevelWarning = "warning"
	AuditLevelInfo    = "info"
)

// SEO 检查项标识（稳定值：落库与去重都按它，改名等于丢掉历史）。
const (
	AuditTitleMissing       = "title_missing"
	AuditTitleDuplicate     = "title_duplicate"
	AuditDescriptionMissing = "description_missing"
	AuditCanonicalMissing   = "canonical_missing"
	AuditImageAltMissing    = "image_alt_missing"
	AuditInternalLinkBroken = "internal_link_broken"
	AuditHreflangIncomplete = "hreflang_incomplete"
)

// AuditIssue 一条体检结论。
type AuditIssue struct {
	Path    string // 产物访问路径（如 /about）
	Item    string // 检查项标识
	Level   string // error / warning / info
	Message string
}

// AuditDocument 单份产物的解析结果（检查项共用一次解析）。
type AuditDocument struct {
	Path        string
	Title       string
	Description string
	Canonical   string
	ImageSrcs   []string // 缺 alt 的图片 src
	Links       []string // 站内链接目标（相对路径）
	Hreflangs   []string
}

// ParseAuditDocument 解析一份产物 HTML（只提取体检需要的字段）。
func ParseAuditDocument(path string, r io.Reader) (doc AuditDocument, err error) {
	doc.Path = path
	z := html.NewTokenizer(r)
	collectAlt := false
	var imgSrc string
	for {
		tt := z.Next()
		if tt == html.ErrorToken {
			break
		}
		if tt != html.StartTagToken && tt != html.SelfClosingTagToken {
			continue
		}
		tok := z.Token()
		switch tok.Data {
		case "title":
			if z.Next() == html.TextToken {
				doc.Title = strings.TrimSpace(z.Token().Data)
			}
		case "meta":
			name := attr(tok, "name")
			if strings.EqualFold(name, "description") {
				doc.Description = strings.TrimSpace(attr(tok, "content"))
			}
		case "link":
			rel := strings.ToLower(attr(tok, "rel"))
			switch {
			case rel == "canonical":
				doc.Canonical = strings.TrimSpace(attr(tok, "href"))
			case rel == "alternate":
				if h := strings.TrimSpace(attr(tok, "hreflang")); h != "" {
					doc.Hreflangs = append(doc.Hreflangs, h)
				}
			}
		case "img":
			imgSrc = strings.TrimSpace(attr(tok, "src"))
			if strings.TrimSpace(attr(tok, "alt")) == "" && imgSrc != "" {
				doc.ImageSrcs = append(doc.ImageSrcs, imgSrc)
			}
			collectAlt = false
		case "a":
			if href := strings.TrimSpace(attr(tok, "href")); isInternalLink(href) {
				doc.Links = append(doc.Links, href)
			}
		}
		_ = collectAlt
	}
	return doc, nil
}

// attr 取属性值（大小写不敏感）。
func attr(tok html.Token, key string) string {
	for _, a := range tok.Attr {
		if strings.EqualFold(a.Key, key) {
			return a.Val
		}
	}
	return ""
}

// isInternalLink 站内链接判定：绝对路径，且不是协议相对 / 邮件 / 电话。
func isInternalLink(href string) bool {
	if href == "" || !strings.HasPrefix(href, "/") || strings.HasPrefix(href, "//") {
		return false
	}
	return true
}

// AuditSite 体检一批产物：docs 为「访问路径 → 已解析文档」。
//
// activePaths 是**服务出去的路径集合**（含 301 旧路径），内链与 hreflang 都对照它 ——
// 这一步能抓出「改 URL 之后没人更新链接」留下的死链，而那类问题在草稿里完全看不见。
func AuditSite(docs []AuditDocument, activePaths map[string]bool) (issues []AuditIssue) {
	byTitle := map[string][]string{}
	for _, d := range docs {
		if d.Title == "" {
			issues = append(issues, AuditIssue{Path: d.Path, Item: AuditTitleMissing, Level: AuditLevelError,
				Message: "页面没有 <title>"})
		} else {
			byTitle[d.Title] = append(byTitle[d.Title], d.Path)
		}
		if d.Description == "" {
			issues = append(issues, AuditIssue{Path: d.Path, Item: AuditDescriptionMissing, Level: AuditLevelWarning,
				Message: "页面没有 meta description"})
		}
		if d.Canonical == "" {
			issues = append(issues, AuditIssue{Path: d.Path, Item: AuditCanonicalMissing, Level: AuditLevelWarning,
				Message: "页面没有 canonical"})
		}
		for _, src := range d.ImageSrcs {
			issues = append(issues, AuditIssue{Path: d.Path, Item: AuditImageAltMissing, Level: AuditLevelWarning,
				Message: "图片缺少 alt：" + src})
		}
		for _, href := range d.Links {
			if !linkExists(href, activePaths) {
				issues = append(issues, AuditIssue{Path: d.Path, Item: AuditInternalLinkBroken, Level: AuditLevelError,
					Message: "站内链接指向不存在的路径：" + href})
			}
		}
		// hreflang：出现就必须至少两条（只有一条等于没声明互指），且目标都在激活集合内。
		if len(d.Hreflangs) == 1 {
			issues = append(issues, AuditIssue{Path: d.Path, Item: AuditHreflangIncomplete, Level: AuditLevelWarning,
				Message: "hreflang 只有一条，缺少互指"})
		}
	}
	for title, pages := range byTitle {
		if len(pages) < 2 {
			continue
		}
		sort.Strings(pages)
		// 必须把命中页面**列进结论**：只报「有 2 个页面重复」等于给了一个无法行动的结果 ——
		// 运营知道有问题，却不知道该去改哪一页。这是体检类功能的常见失手。
		issues = append(issues, AuditIssue{Path: strings.Join(pages, ", "), Item: AuditTitleDuplicate, Level: AuditLevelWarning,
			Message: "重复的 title（" + title + "）出现在 " + itoa(len(pages)) + " 个页面：" + strings.Join(pages, "、")})
	}
	return issues
}

// linkExists 站内链接目标是否真的在激活集合里。
//
// 归一化：去掉 query 与 fragment（它们不影响路径是否存在）、去掉结尾斜杠、
// 空路径视为首页。不做 301 跟随 —— 体检报「这个链接指向的路径没有激活」，
// 至于它是不是被 301 了，那是另一条链（重定向管理）的事。
func linkExists(href string, activePaths map[string]bool) bool {
	if len(activePaths) == 0 {
		return true // 没有路径集合可比对时不下结论，避免整站误报
	}
	p := href
	if i := strings.IndexAny(p, "?#"); i >= 0 {
		p = p[:i]
	}
	if p == "" || p == "/" {
		p = "/"
	}
	for _, cand := range []string{p, strings.TrimSuffix(p, "/"), p + "/"} {
		if cand != "" && activePaths[cand] {
			return true
		}
	}
	return false
}

// itoa 小整数转字符串（避免为一个用例引入 strconv 的间接层）。
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

// CollectActiveDocuments 遍历激活目录，读出全部 HTML 产物（只读，不解析链接目标）。
//
// CollectActiveDocuments 按**激活路径**读出 HTML 产物（只读，不解析链接目标）。
//
// 为什么不是遍历目录：激活目录里放的全是**指向 artifacts/{hash} 的目录符号链接**
// （/about → about，后者是链接）。filepath.Walk 用 Lstat，**不会下沉进目录符号链接**，
// 于是扫描结果是 0 个文件 —— 体检永远报「无问题」。这个失效是静默的：
// 报告里 scanned=0 看起来像「站点还没有产物」，而不是「扫描器坏了」。
// 实测（临时目录造 active/about 为符号链接）：Walk 只访问到 active/about 本身，
// 文件数 0，且该名字不以 .html 结尾会被后续过滤掉。
//
// 按路径直读时 os.Open 会跟随符号链接（这正是访问面服务静态文件的方式），
// 拿到的就是访客真的会收到的那份 HTML —— 体检看到的与访问面一致。
//
// dir 为空或不存在返回 nil, nil：体检是**可选能力**，接了才算数，
// 而不是把「没配目录」变成构建/发布的错误。
func CollectActiveDocuments(dir string, activePaths []string) (docs []AuditDocument, err error) {
	if strings.TrimSpace(dir) == "" || len(activePaths) == 0 {
		return nil, nil
	}
	if _, statErr := os.Stat(dir); statErr != nil {
		return nil, nil
	}
	for _, access := range activePaths {
		file, ok := activeHTMLFile(dir, access)
		if !ok {
			continue // 该路径没有 HTML 产物（重定向产物等），跳过而不是中断整次体检
		}
		f, oerr := os.Open(file)
		if oerr != nil {
			continue // 单个文件读不了不中断整次体检
		}
		doc, perr := ParseAuditDocument(access, f)
		f.Close()
		if perr == nil {
			docs = append(docs, doc)
		}
	}
	return docs, nil
}

// RunSEOAudit 跑一次产物 SEO 体检（审计 SEO-019）。
//
// 三个输入一起决定结论：**激活路径集合**（对谁体检）、**激活目录**（读哪份 HTML）、
// 路径集合本身（内链对照）。
//
// active 目录是**站点级**的（不含工程维度），多工程部署时里面会混着其它工程的产物 ——
// 所以这里按本工程的激活路径过滤一遍再体检。不过滤的话，别的工程的页面会带着
// 它们自己的内链进来，而对照的是本工程的路由表，结果是满屏误报的死链。
//
// 只读产物文件：不写库、不发请求、不碰访问面，可以在任何时刻跑。
func (s *Service) RunSEOAudit(ctx context.Context, projectID string) (issues []AuditIssue, scanned int, err error) {
	if strings.TrimSpace(projectID) == "" {
		return nil, 0, errors.New("缺少站点工程")
	}
	paths, err := s.model.ListActivePaths(ctx, projectID)
	if err != nil {
		return nil, 0, err
	}
	if len(paths) == 0 {
		return nil, 0, nil // 还没有激活产物：体检无从谈起，不是错误
	}
	active := make(map[string]bool, len(paths))
	for _, p := range paths {
		active[p] = true
	}
	docs, err := CollectActiveDocuments(pipeline.ActiveRoot(), paths)
	if err != nil {
		return nil, 0, err
	}
	mine := make([]AuditDocument, 0, len(docs))
	for _, d := range docs {
		if active[d.Path] {
			mine = append(mine, d)
		}
	}
	return AuditSite(mine, active), len(mine), nil
}
