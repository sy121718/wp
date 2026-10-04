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

// 模板集合缓存（首次渲染时惰性装载）。
var (
	productListSet     *jet.Set
	productListSetErr  error
	productListSetOnce sync.Once
)

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
	productcontract.CollectionFilterStatus:       "filterStatus",
	productcontract.CollectionFilterCategoryID:   "filterCategoryId",
	productcontract.CollectionFilterCategoryIDs:  "filterCategoryIds",
	productcontract.CollectionFilterCategoryMode: "filterCategoryMode",
	productcontract.CollectionFilterBrandID:      "filterBrandId",
	productcontract.CollectionFilterBrandIDs:     "filterBrandIds",
	productcontract.CollectionFilterBrandMode:    "filterBrandMode",
	productcontract.CollectionFilterTagID:        "filterTagId",
	productcontract.CollectionFilterTagIDs:       "filterTagIds",
	productcontract.CollectionFilterTagMode:      "filterTagMode",
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
	// 筛选栏 / 工具栏的形态开关与数据。
	//
	// 缺了它们，片段重渲染出来的列表**没有筛选栏**：产物里的初始列表有、
	// 点一下筛选之后就没有了，且此后再也回不来（用户看到「越操作越少」）。
	// 它们与 layout / columns 同性质 —— 是「这个实例怎么渲染」，不是「访客要哪批数据」。
	"filters":       "filters",
	"caretIcon":     "caretIcon",
	"categoryMulti": "categoryMulti",
	"toolbar":       "toolbar",
	"priceRanges":   "priceRanges",
	"priceSlider":   "priceSlider",
	"priceBounds":   "priceBounds",
	"ratingOptions": "ratingOptions",
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
	if deps.CollectionResolver == nil {
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
	// 没有任何**语义参数**（筛选 / 排序 / 翻页）时无需替换目标节点。
	//
	// 这条对应产物里的 hx-trigger="load"：列表容器每次进页面都会请求一次片段。
	// 地址栏带 ?categoryId=x 这类分享链接参数时，这一趟是必要的（静态产物不可能
	// 知道访客的查询）；什么都没有时**不能换** —— 片段请求的实例配置里只有布局类
	// 字段，没有筛选栏 / 排序栏的开关，换上去等于把服务端渲染好的筛选栏删掉
	// （实测：load 之后筛选栏整块消失，用户看到的是一份「越刷新越少」的列表）。
	//
	// **排在 productListProps 之后**：参数白名单校验是安全边界，非法字段必须拿到
	// 400 而不是静默的 204 —— 否则「请求被拒绝」与「请求无事可做」在响应上不可区分，
	// 攻击者可以拿 204 当成白名单探测的成功信号。
	if !productListHasSemanticParam(r) {
		return "", ErrNoChange
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
		// ProjectID 字段：组件把它拼进**实例配置**（productlist/links.go 的
		// fragmentQuery），也就是重渲染出来的容器那条 hx-get。此前只塞进了 Go context
		// （上方 WithBuildProjectID），字段是空的 —— 于是片段刷新后的容器 hx-get 里
		// **没有 projectId**，下一次翻页/筛选请求直接 500「缺少参数 projectId」。
		// 表现极具迷惑性：首屏能看、点一下就废，且只在刷新过一次之后出现。
		ProjectID: projectID,
		// CurrentPath：降级链接（href）要拼绝对地址，而片段期没有「当前页」上下文。
		// 取自请求头 HX-Current-URL（端点解析后放进 Request.CurrentPath）。
		CurrentPath: r.CurrentPath,
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
		Collection: deps.CollectionResolver,
		Product:    deps.ProductDataSource,
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
	// 排序：URL 的 orderBy 是**访客选择**，覆盖作者在 Props 里配的默认排序。
	//
	// 不映射它的表现是「排序栏永远高亮『默认排序』」—— 列表内容确实按新顺序换了，
	// 但控件不反映当前状态，用户无法确认自己点没点上（也无法再点一次取消）。
	// 它属于语义参数，不进实例配置（见 productListInstanceParams）。
	if raw := strings.TrimSpace(r.Params["orderBy"]); raw != "" {
		props["orderBy"] = raw
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
	// 属性维度：一个属性组可以带多个值（逗号分隔，组内并集）。
	//
	// 上面那层的 URL 参数本身就是逗号多值，这里**原样带过**：props 里的 filterOptions
	// 采用 `key:v1,v2` 的形状（值在冒号之后、逗号分隔），与集合源维度
	// `option.<key>=v1,v2` 一一对应 —— 组件解析一次就能同时拿到键与全部值。
	//
	// 参数顺序不稳定（map），先收键再排序，保证同参数同 props（一致性用例据此逐字节比对）。
	options := make([]string, 0, 4)
	optionKeys := make([]string, 0, 4)
	for param := range r.Params {
		if strings.HasPrefix(param, productcontract.CollectionFilterOptionPrefix) {
			optionKeys = append(optionKeys, param)
		}
	}
	sort.Strings(optionKeys)
	for _, param := range optionKeys {
		attrKey := strings.TrimPrefix(param, productcontract.CollectionFilterOptionPrefix)
		values := productListOptionValues(r.Params[param])
		if attrKey == "" || len(values) == 0 {
			continue
		}
		options = append(options, attrKey+":"+strings.Join(values, ","))
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

// productListHasSemanticParam 请求是否带**语义参数**（非实例配置）。
//
// 实例配置 = 组件拼进 hx-get 的那一组固定键（节点 / 工程 / 语言 / 布局 / 字段槽位…），
// 它们表达的是「这是哪个实例、怎么渲染」，不是「访客想要哪一批数据」。
// 判据写成**排除法**而不是白名单：新增语义参数（某个新的筛选维度）时
// 自动被认作语义参数 —— 白名单反过来的话，新维度会被静默当成实例配置、
// 表现为「这个筛选点了没反应」，且只在真实点击时才暴露。
func productListHasSemanticParam(r *Request) bool {
	for key := range r.Params {
		if !productListInstanceParam(key) {
			return true
		}
	}
	return false
}

// productListInstanceParam 该参数是否属于实例配置（不表达「要哪批数据」）。
func productListInstanceParam(key string) bool {
	if key == "" {
		return true
	}
	if _, ok := productListInstanceParams[key]; ok {
		return true
	}
	// option.<attr> 是**语义**参数（属性维度筛选），不能当实例配置。
	return false
}

// productListInstanceParams 实例配置键集合（与 links.go 的 fragmentQuery 同源）。
//
// 新增实例配置键时这里要同步加，否则它会被当成语义参数、导致「每次进页面都白换一次」。
var productListInstanceParams = map[string]bool{
	"nodeId": true, "projectId": true, "lang": true,
	"layout": true, "columns": true, "currency": true, "titleTag": true,
	"emptyText": true, "linkPrefix": true, "limit": true, "pageSize": true,
	"optionKeys": true, "context": true,
	"imageField": true, "imageAltField": true, "titleField": true,
	"priceField": true, "comparePriceField": true, "tagsField": true, "linkField": true,
	// 决定筛选栏 / 工具栏长什么样的开关与数据（见 links.go 的 fragmentQuery）。
	"filters": true, "toolbar": true, "priceRanges": true, "caretIcon": true, "categoryMulti": true,
	"priceSlider": true, "priceBounds": true, "ratingOptions": true,
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
		key, values, ok := strings.Cut(pair, ":")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		values = strings.Join(productListOptionValues(values), ",")
		if key == "" || values == "" {
			continue
		}
		q.Set(productcontract.CollectionFilterOptionPrefix+key, values)
	}
	return q.Encode()
}

// productListOptionValues 属性维度参数值 → 去空去重的值列表（保持首次出现顺序）。
//
// 形状与集合源那一侧完全一致（同样走逗号多值），所以这里不做第二种解读：
// 两处对同一个 URL 参数给出不同语义，会出现「筛得出来但翻页就丢」这类只在
// 组合操作下暴露的缺陷。
func productListOptionValues(raw string) []string {
	out := make([]string, 0, 4)
	seen := map[string]bool{}
	for _, part := range strings.Split(raw, ",") {
		value := strings.TrimSpace(part)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
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
