// product_list.go — 商品列表片段（issue #27）：筛选 / 排序 / 分页的局部刷新出口。
//
// 与 bundleConfigurator 那类「现算结论」的片段不同，本片段返回的是**列表区块 HTML**，
// 而且必须与静态产物里的那份**逐字节同源** —— 所以它不自己拼 HTML，而是把参数还原成
// 组件节点后直接调 builder.RenderNodeHTML（同一组件、同一 BuildView、同一模板）。
//
// 参数分三类：
//
//  1. 实例配置（nodeId / projectId / 字段槽位 / 布局 / 条数）—— 构建期烘进产物的固定值，
//     不进 URL；片段端按白名单校验（字段槽位必须过商品字段白名单）；
//  2. 语义筛选（status / categoryId / brandId / tagIds / onSale / option.<key> ...）——
//     进 URL、可分享；键由集合源契约判定白名单（IsCollectionFilterKey）；
//  3. 语义展示（layout / columns / 排序）—— 同样进 URL。
//
// 安全边界：片段是 anonymous GET，参数来自 URL。工程 id 由产物烘进来（可被篡改），
// 所以这里**强制 status=published** —— 无论 URL 传什么，片段只出已发布商品；
// 商品与列表本来就是公开内容，越权面因此只剩「别的站点的公开商品」。
package runtimefragment

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/CloudyKit/jet/v6"

	builder "go_wp/internal/builder"
	productlist "go_wp/internal/builder/components/productlist"
	"go_wp/internal/builder/core"
	productcontract "go_wp/internal/module/product/contract"
	productenums "go_wp/internal/module/product/enums"
	"go_wp/internal/templates"
)

// 片段依赖（装配期注入）。
var (
	collectionResolver core.CollectionResolver
	productListSet     *jet.Set
	productListSetErr  error
	productListSetOnce sync.Once
)

// productDataSource 商品构建期数据源（issue #35）。
//
// 装配自检（审计 CQ-019）：判为 required-port —— 实现（productSvc）在 routes.go 里
// 恒定可得。nil 分支仅服务单测，表现为组件回退按名路由（取数口径与集合源不一致）。
var productDataSource productcontract.ProductDataSource

// SetProductDataSource 注入商品构建期数据源（issue #35，装配期调用）。
//
// 片段路径渲染商品列表时优先用它（受限接口：只有读集合 / 元数据 / 可筛值）。
func SetProductDataSource(ds productcontract.ProductDataSource) { productDataSource = ds }

// SetCollectionResolver 注入集合解析器（装配期调用）。
func SetCollectionResolver(r core.CollectionResolver) { collectionResolver = r }

func init() {
	Register(Spec{
		Type:   "productList",
		Method: "GET",
		Auth:   AuthAnonymous,
		Render: renderProductList,
	})
}

// 实例配置参数（构建期烘进产物，不进 URL）。
const (
	productListParamNodeID    = "nodeId"
	productListParamProjectID = "projectId"
	productListParamLimit     = "limit"
)

// productListFilterParams 语义筛选参数 → 组件筛选 props。
//
// 键与集合源契约的维度名一致（工作台与 URL 都按这一套写）。
var productListFilterParams = map[string]string{
	productcontract.CollectionFilterStatus:     "filterStatus",
	productcontract.CollectionFilterCategoryID: "filterCategoryId",
	productcontract.CollectionFilterBrandID:    "filterBrandId",
	productcontract.CollectionFilterTagID:      "filterTagId",
	productcontract.CollectionFilterTagIDs:     "filterTagIds",
	productcontract.CollectionFilterTagMode:    "filterTagMode",
	// 最低评分（issue #29）：0~5 的数值，形状由集合源解析期校验。
	productcontract.CollectionFilterMinRating: "filterMinRating",
	// 价格区间（issue #28）：数值形状由集合源解析期校验，片段只做透传。
	productcontract.CollectionFilterMinPrice: "filterMinPrice",
	productcontract.CollectionFilterMaxPrice: "filterMaxPrice",
}

// 分页参数（issue #27 / 审计 PERF-019）。分工刻意不同：page 是**语义参数**
// （进 URL、可分享、访客可控），pageSize 是**实例配置**
// （构建期由 fragmentQuery 焙进产物，访客改不动）。
const (
	productListParamPage     = "page"
	productListParamPageSize = "pageSize"
)

// productListDisplayParams 展示参数 → 组件 props（白名单：只有这里列出的能进 props）。
var productListDisplayParams = map[string]string{
	"layout":            "layout",
	"columns":           "columns",
	"currency":          "currency",
	"titleTag":          "titleTag",
	"emptyText":         "emptyText",
	"linkPrefix":        "linkPrefix",
	"imageField":        "imageField",
	"imageAltField":     "imageAltField",
	"titleField":        "titleField",
	"priceField":        "priceField",
	"comparePriceField": "comparePriceField",
	"tagsField":         "tagsField",
	"linkField":         "linkField",
}

// productListFieldParams 字段槽位参数（值必须过商品字段白名单）。
var productListFieldParams = []string{
	"imageField", "imageAltField", "titleField", "priceField",
	"comparePriceField", "tagsField", "linkField",
}

// renderProductList 渲染列表区块。
func renderProductList(ctx context.Context, r *Request) (string, error) {
	if r == nil {
		return "", fmt.Errorf("片段请求为空")
	}
	if collectionResolver == nil {
		return "", fmt.Errorf("集合解析器未接入（装配缺陷）")
	}
	nodeID := strings.TrimSpace(r.Params[productListParamNodeID])
	if nodeID == "" {
		return "", fmt.Errorf("缺少参数 %s", productListParamNodeID)
	}
	projectID := strings.TrimSpace(r.Params[productListParamProjectID])
	if projectID == "" {
		return "", fmt.Errorf("缺少参数 %s", productListParamProjectID)
	}

	props, perr := productListProps(r)
	if perr != nil {
		return "", perr
	}
	props["collectionSource"] = productcontract.CollectionSourceProduct
	// 强制只出已发布：无论 URL 传什么 status，片段都不越权读取未发布内容。
	props["filterStatus"] = productenums.StatusPublished

	propsJSON, merr := json.Marshal(props)
	if merr != nil {
		return "", merr
	}
	set, serr := productListComponentSet()
	if serr != nil {
		return "", serr
	}
	rctx := &core.RenderContext{
		Context: core.WithBuildProjectID(ctx, projectID),
		// Lang 本次渲染的目标语言（I18N-011）。
		//
		// 片段期必须显式设它：组件用它把 lang 拼进**实例配置**（productlist/links.go 的
		// fragmentQuery），也就是重渲染出来的容器那条 hx-get。不设的后果是「构建期产物
		// 带 lang、片段刷新后的容器不带」—— 同一个语言维度走两条路径，容器再触发一次
		// load 就回落工程默认语言。
		//
		// 值与下方槽位解析取的是同一个 r.Lang（请求解析结果：?lang → 工程默认语言），
		// 不是这里凭空补一个语言 —— 语言仍由 URL 显式表达，只是如实传给渲染层。
		Lang:       r.Lang,
		Collection: collectionResolver,
		Product:    productDataSource,
	}
	// 系统页面槽位（BIZ-2）：列表里的「全部商品」链接按站点槽位取路径，构建期与片段期
	// 必须是同一份解析（各解一次迟早分叉：静态页上的链接能点、片段刷新后变 404）。
	if r.SitePagesOf != nil {
		rctx.SetSitePages(r.SitePagesOf(projectID, r.Lang))
	}
	return builder.RenderNodeHTML(set, &core.Node{ID: nodeID, Type: productlist.Type, Props: propsJSON}, rctx)
}

// productListProps 从请求参数还原组件 props（白名单键 + 形状校验）。
//
// 字段槽位逐个过商品字段白名单：片段参数来自 URL，不能凭它注入任意字段绑定
// （构建期保存校验管不到运行期请求，这一层是运行期的等价防线）。
func productListProps(r *Request) (map[string]any, error) {
	props := map[string]any{}
	for param, key := range productListDisplayParams {
		if v := strings.TrimSpace(r.Params[param]); v != "" {
			props[key] = v
		}
	}
	allowed := map[string]bool{}
	for _, field := range productcontract.FieldWhitelist(productcontract.EntityTypeProduct) {
		allowed[field] = true
	}
	for _, param := range productListFieldParams {
		field, _ := props[param].(string)
		if field == "" {
			continue
		}
		name := field
		if _, tail, ok := strings.Cut(field, "."); ok {
			name = tail
		}
		if !allowed[name] {
			return nil, fmt.Errorf("字段 %q 不在商品字段白名单内", field)
		}
	}
	if raw := strings.TrimSpace(r.Params[productListParamLimit]); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n <= 0 {
			return nil, fmt.Errorf("limit 非法: %q", raw)
		}
		props["collectionLimit"] = n
	}
	// 分页（审计 PERF-019）：page 由 URL 传入（语义参数），pageSize 由产物焙入的
	// 实例配置传入。这两个映射曾经缺失，而分页控件的链接照常输出 ——
	// 表现是「点下一页 URL 变了、列表却一动不动」：组件拿不到 pageSize 就永远按
	// 「不分页」渲染、拿不到 page 就永远停在第 1 页，于是分页下推的 SQL 路径一次也走不到。
	//
	// 边界复用组件导出的常量（两处各写一份数字迟早分叉）；越界**丢弃**而不是报错：
	// 分页控件的产物只可能是合法值，手工改坏 URL 该回到第 1 页，不该把整块列表打成 500。
	if raw := strings.TrimSpace(r.Params[productListParamPageSize]); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n >= 0 && n <= productlist.MaxPageSize {
			props["pageSize"] = n
		}
	}
	if raw := strings.TrimSpace(r.Params[productListParamPage]); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n >= 1 && n <= productlist.MaxPage {
			props["page"] = n
		}
	}
	// 筛选：等值维度直接映射；`option.<属性key>` 前缀维度收成组件认的 `key:value` 列表；
	// 未知维度一律**丢弃**（不是报错）：片段会随页面上多余的 query 参数被请求，
	// 那些参数不归本片段管，报错反而会让页面看起来坏了。
	for param, key := range productListFilterParams {
		if v := strings.TrimSpace(r.Params[param]); v != "" {
			props[key] = v
		}
	}
	if v := strings.TrimSpace(r.Params[productcontract.CollectionFilterOnSale]); v == "true" || v == "1" {
		props["onlyOnSale"] = "on"
	}
	options := make([]string, 0, 4)
	// 参数顺序不稳定（map），先收键再排序，保证同参数同 props（一致性用例据此逐字节比对）。
	optionKeys := make([]string, 0, 4)
	for param := range r.Params {
		if strings.HasPrefix(param, productcontract.CollectionFilterOptionPrefix) {
			optionKeys = append(optionKeys, param)
		}
	}
	sort.Strings(optionKeys)
	for _, param := range optionKeys {
		attrKey := strings.TrimPrefix(param, productcontract.CollectionFilterOptionPrefix)
		value := strings.TrimSpace(r.Params[param])
		if attrKey == "" || value == "" {
			continue
		}
		options = append(options, attrKey+":"+value)
	}
	if len(options) > 0 {
		props["filterOptions"] = strings.Join(options, ",")
	}
	// 当前语义参数（issue #27）：翻页要带上现有筛选、换筛选要回到第 1 页 ——
	// 组件的链接拼装靠这个串。它一直没有被灌进去（PushQuery 原先是
	// json:"-"，没有任何非测试代码能给它赋值），所以“点下一页筛选就没了”一直存在。
	if q := productListSemanticQuery(r, options); q != "" {
		props["pushQuery"] = q
	}
	return props, nil
}

// productListSemanticQuery 合成当前语义查询串（翻页 / 换筛选时由组件的链接拼装消费）。
//
// **键名取自 URL 而不是 props**：两者的键并不一一对应（URL 是 categoryId，
// props 是 filterCategoryId；价格区间、属性维度同理），从 props 反推会拼出组件不认的参数。
// 但也**不是原样回传**：逐键过语义白名单，实例配置（nodeId / projectId /
// 字段槽位 / 布局 / 条数 / 每页条数）一律不进串 —— 组件会拿这个串拼片段请求并
// **覆盖实例配置**，掺进去等于让访客用 query 换掉自己请求的工程与节点。
//
// 顺序由 url.Values.Encode 固定，保证同参数同 props —— 一致性用例逐字节比对。
func productListSemanticQuery(r *Request, options []string) string {
	q := url.Values{}
	for param, value := range r.Params {
		// 属性维度统一由下面的 options 还原（它们在 props 层已经收成了 key:value）。
		if strings.HasPrefix(param, productcontract.CollectionFilterOptionPrefix) {
			continue
		}
		if !productListSemanticParam(param) {
			continue
		}
		if v := strings.TrimSpace(value); v != "" {
			q.Set(param, v)
		}
	}
	for _, pair := range options {
		key, value, ok := strings.Cut(pair, ":")
		if !ok {
			continue
		}
		if key = strings.TrimSpace(key); key == "" {
			continue
		}
		if value = strings.TrimSpace(value); value == "" {
			continue
		}
		q.Set(productcontract.CollectionFilterOptionPrefix+key, value)
	}
	return q.Encode()
}

// productListSemanticParam 该 URL 参数是否属于语义参数（可进 pushQuery）。
//
// 白名单直接复用筛选参数表 + 分页 + onSale + option 前缀，而不另立一份：
// 两份清单分叉的后果是“某个筛选维度能筛但一翻页就丢”，而那正是难查的一类。
//
// lang 也在这份白名单里（I18N-011）：它不是筛选维度，而是**视图维度** ——
// 组件的翻页/换筛选链接由 pushQuery 拼装，串里没有 lang 就只会带上实例配置，
// 于是「英文站点翻到第 2 页」的请求不再带语言，片段回落工程默认语言，
// 页面看起来是「翻页之后列表自己变回了中文」。与 orders 的
// orderListFragmentURL / orderDetailURL 同一形状（它们早就带上了 lang）。
// 只在请求**确实带了** lang 时才进串：语言由 URL 显式表达，不从请求头猜。
func productListSemanticParam(param string) bool {
	if param == productListParamPage || param == productcontract.CollectionFilterOnSale || param == fragmentLangParam {
		return true
	}
	_, ok := productListFilterParams[param]
	return ok
}

// productListComponentSet 组件模板集（进程内构建一次并复用；失败缓存错误，不反复重试）。
func productListComponentSet() (*jet.Set, error) {
	productListSetOnce.Do(func() {
		productListSet, productListSetErr = templates.NewEmbeddedComponentSet()
	})
	return productListSet, productListSetErr
}
