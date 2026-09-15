// search_results.go — 站内搜索结果片段（BIZ-2）。
//
// 一个 capability：searchResults GET anonymous —— 关键词进，结果条目 HTML 出。
// 两个来源：CMS 内容（文章）与商品。工程上下文由构建期烘进 URL（与 productList 同法），
// 片段端不猜、也不跨工程搜。
//
// 三条刻意的口径：
//
//  1. **只出真的能看到的东西**。商品侧「已上架」落在商品域自己的 SQL 条件里；
//     内容侧没有状态列（contents 表只有 data 与 slug），所以「已发布」只能由
//     发布面判定 —— 内容命中必须有**已上线的详情页路径**才输出。两个域的不对称
//     不是疏忽，而是两份数据的形状本来就不同：把发布判定塞进内容模块会让内容域
//     反向依赖发布域，而两处判定的口径迟早分叉。
//  2. **链接必须是真实可用的页面**。路径一律来自发布面（presentation 实例的
//     active 指针），片段不拼、不猜、不回退到 slug 约定 —— 拿不到路径就只输出标题，
//     绝不输出死链。
//  3. **降级不 500**。端口未注入 / 工程上下文缺失 / 关键词为空 / 无结果，
//     四种情况都渲染一句人话，而不是让访客看到一个 500（搜索框旁边出现 500，
//     访客只会认为整站坏了）。
//
// 转义：所有输出都经 fragment 模板的默认 HTML 转义（等价 html.EscapeString）——
// 关键词会回显在「没有找到与「xxx」相关的内容」里，那是**用户输入**，
// 处理器内部绝不手工拼 HTML。
package runtimefragment

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	contentcontract "go_wp/internal/module/content/contract"
	presentationcontract "go_wp/internal/module/presentation/contract"
	productcontract "go_wp/internal/module/product/contract"
	"go_wp/internal/templates"
	"go_wp/pkg/logger"
)

// 检索来源端口（装配期注入；未注入 = 该类来源不可用，走降级文案而不是报错）。
//
// 装配自检（审计 CQ-019）：三条都判为 required-contract —— 提供方必须是内容 / 商品
// 模块里实现了 SearchPort 的那个实现，routes.go 断言失败即 panic。
// （片段层的降级文案保留给单测：未注入时访客看到「搜索功能暂未接入，请稍后再试」。）
var (
	contentSearchProvider  contentcontract.SearchPort
	productSearchProvider  productcontract.SearchPort
	publishedEntityLocator presentationcontract.PublishedEntityLocator
)

// SetContentSearchProvider 注入内容检索端口（装配期调用；传 nil 表示未接入）。
func SetContentSearchProvider(port contentcontract.SearchPort) { contentSearchProvider = port }

// SetProductSearchProvider 注入商品检索端口（装配期调用；传 nil 表示未接入）。
func SetProductSearchProvider(port productcontract.SearchPort) { productSearchProvider = port }

// SetPublishedEntityLocator 注入「实体 → 已上线路径」解析端口（装配期调用；传 nil 表示未接入）。
func SetPublishedEntityLocator(port presentationcontract.PublishedEntityLocator) {
	publishedEntityLocator = port
}

func init() {
	Register(Spec{
		Type:   "searchResults",
		Method: "GET",
		Auth:   AuthAnonymous,
		Render: renderSearchResults,
	})
}

// 参数名与上限。
const (
	searchParamQuery     = "q"
	searchParamProjectID = "projectId"
	searchParamLimit     = "limit"
	// searchQueryMaxRunes 关键词长度上限（按字符算，中文一个字算一个）。
	searchQueryMaxRunes = 64
	// searchExcerptMaxRunes 摘要截断长度（结果条目只是一行提示，不搬全文）。
	searchExcerptMaxRunes = 120
	// searchDefaultLimit 每类结果的条数缺省值与硬上限（片段是 anonymous 请求，
	// 上限必须钉死在服务侧，不能由 URL 决定）。
	searchDefaultLimit = 8
	searchMaxLimit     = 20
)

// searchHit 结果条目。
type searchHit struct {
	Title      string
	HasURL     bool
	URL        string
	Excerpt    string
	HasExcerpt bool
}

// searchResultsView 片段模板数据。
type searchResultsView struct {
	HasMessage bool
	Message    string

	HasContent  bool
	ContentHits []searchHit
	HasProducts bool
	ProductHits []searchHit
}

// renderSearchResults 渲染搜索结果。
func renderSearchResults(ctx context.Context, r *Request) (string, error) {
	if r == nil {
		return "", fmt.Errorf("片段请求为空")
	}
	query := truncateRunes(strings.TrimSpace(r.Params[searchParamQuery]), searchQueryMaxRunes)
	view := searchResultsView{}
	if query == "" {
		// 空关键词不是错误：搜索框还没输入就触发了请求，提示一句即可。
		view.HasMessage, view.Message = true, r.tr("site.fragment.search.empty_query", "请输入搜索关键词")
		return templates.RenderFragment("search_results", view)
	}
	if contentSearchProvider == nil && productSearchProvider == nil {
		view.HasMessage, view.Message = true, r.tr("site.fragment.search.degraded", "搜索功能暂未接入，请稍后再试")
		return templates.RenderFragment("search_results", view)
	}
	projectID := strings.TrimSpace(r.Params[searchParamProjectID])
	lang := searchLang(r)
	limit := searchResultLimit(r.Params[searchParamLimit])

	if contentSearchProvider != nil {
		hits, err := contentSearchProvider.SearchArticles(ctx, query, limit)
		if err != nil {
			return "", err
		}
		view.ContentHits = contentSearchHits(ctx, hits, projectID, lang)
	}
	// 商品检索需要工程上下文（商品表按工程隔离）：没有工程 id 就不搜，
	// 而不是跨工程搜一批别人的商品出来。
	if productSearchProvider != nil && projectID != "" {
		hits, err := productSearchProvider.SearchPublishedProducts(ctx, projectID, query, limit)
		if err != nil {
			return "", err
		}
		view.ProductHits = productSearchHits(ctx, hits, projectID, lang)
	}
	view.HasContent = len(view.ContentHits) > 0
	view.HasProducts = len(view.ProductHits) > 0
	if !view.HasContent && !view.HasProducts {
		view.HasMessage = true
		view.Message = fmt.Sprintf(r.tr("site.fragment.search.no_results", "没有找到与「%s」相关的内容"), query)
	}
	return templates.RenderFragment("search_results", view)
}

// contentSearchHits 内容命中 → 结果条目。
//
// 没有已上线路径的命中**直接丢弃**：contents 表没有状态列，「已发布」在这里
// 只能等同于「有线上页面」。这条约束是硬的 —— 搜索结果是任何人可构造、可观察的，
// 把未发布内容的标题漏出去就等于把草稿箱敞开。
func contentSearchHits(ctx context.Context, hits []*contentcontract.ArticleSearchHit, projectID, lang string) []searchHit {
	if len(hits) == 0 {
		return nil
	}
	ids := make([]string, 0, len(hits))
	for _, hit := range hits {
		if hit != nil && strings.TrimSpace(hit.ID) != "" {
			ids = append(ids, hit.ID)
		}
	}
	paths := searchPublishedPaths(ctx, projectID, contentcontract.EntityTypeArticle, lang, ids)
	out := make([]searchHit, 0, len(hits))
	for _, hit := range hits {
		if hit == nil {
			continue
		}
		path := strings.TrimSpace(paths[hit.ID])
		if path == "" {
			continue
		}
		item := searchHit{Title: strings.TrimSpace(hit.Title), HasURL: true, URL: path}
		if item.Title == "" {
			item.Title = strings.TrimSpace(hit.Slug)
		}
		if excerpt := truncateRunes(strings.TrimSpace(hit.Excerpt), searchExcerptMaxRunes); excerpt != "" {
			item.Excerpt, item.HasExcerpt = excerpt, true
		}
		out = append(out, item)
	}
	return out
}

// productSearchHits 商品命中 → 结果条目。
//
// 与内容侧的不对称：商品「已上架」已由商品域的 SQL 条件保证，所以没有线上详情页时
// 仍然输出条目（只是不带链接）—— 宁可让访客看到「有这个商品但点不进去」，
// 也不要假装它不存在。路径拿不到就绝不拼一个 slug 约定出来。
func productSearchHits(ctx context.Context, hits []*productcontract.ProductSearchHit, projectID, lang string) []searchHit {
	if len(hits) == 0 {
		return nil
	}
	ids := make([]string, 0, len(hits))
	for _, hit := range hits {
		if hit != nil && strings.TrimSpace(hit.ID) != "" {
			ids = append(ids, hit.ID)
		}
	}
	paths := searchPublishedPaths(ctx, projectID, productcontract.EntityTypeProduct, lang, ids)
	out := make([]searchHit, 0, len(hits))
	for _, hit := range hits {
		if hit == nil {
			continue
		}
		title := strings.TrimSpace(hit.Name)
		if title == "" {
			title = strings.TrimSpace(hit.Slug)
		}
		if title == "" {
			continue
		}
		item := searchHit{Title: title}
		if path := strings.TrimSpace(paths[hit.ID]); path != "" {
			item.HasURL, item.URL = true, path
		}
		if excerpt := truncateRunes(strings.TrimSpace(hit.Subtitle), searchExcerptMaxRunes); excerpt != "" {
			item.Excerpt, item.HasExcerpt = excerpt, true
		}
		out = append(out, item)
	}
	return out
}

// searchLang 片段请求语言（endpoint 已校验；空时 locator 自行回落默认语言）。
func searchLang(r *Request) string {
	if r == nil {
		return ""
	}
	return strings.TrimSpace(r.Lang)
}

// searchPublishedPaths 解析这批实体的线上路径。
//
// 解析失败只记日志并返回空表：链接是增强，不是搜索能否出结果的前提 ——
// 为它把整个搜索打成 500，损失远大于收益（与槽位解析同一条口径）。
func searchPublishedPaths(ctx context.Context, projectID, entityType, lang string, ids []string) map[string]string {
	if publishedEntityLocator == nil || strings.TrimSpace(projectID) == "" || len(ids) == 0 {
		return nil
	}
	paths, err := publishedEntityLocator.PublishedEntityPaths(ctx, projectID, entityType, lang, ids)
	if err != nil {
		logger.Scene("fragment").Error(err, "站内搜索：解析实体的线上路径失败")
		return nil
	}
	return paths
}

// searchResultLimit 解析每类结果条数（非法值回落到缺省值，不报错 ——
// 参数来自 URL，多余或写错的参数不该让搜索页变得不能用）。
func searchResultLimit(raw string) int {
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || n <= 0 {
		return searchDefaultLimit
	}
	if n > searchMaxLimit {
		return searchMaxLimit
	}
	return n
}

// truncateRunes 按字符（rune）截断，避免把一个汉字切成半个。
func truncateRunes(s string, max int) string {
	if max <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max])
}
