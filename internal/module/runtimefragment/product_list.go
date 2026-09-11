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
	// 价格区间（issue #28）：数值形状由集合源解析期校验，片段只做透传。
	productcontract.CollectionFilterMinPrice: "filterMinPrice",
	productcontract.CollectionFilterMaxPrice: "filterMaxPrice",
}

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
		CSS:        &core.CSSBuckets{},
		Context:    core.WithBuildProjectID(ctx, projectID),
		Collection: collectionResolver,
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
	return props, nil
}

// productListComponentSet 组件模板集（进程内构建一次并复用；失败缓存错误，不反复重试）。
func productListComponentSet() (*jet.Set, error) {
	productListSetOnce.Do(func() {
		productListSet, productListSetErr = templates.NewEmbeddedComponentSet()
	})
	return productListSet, productListSetErr
}
