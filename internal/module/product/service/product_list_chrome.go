// product_list_chrome.go — 列表页与编辑页的**派生值**（状态下拉的取值域、筛选基地址）。
//
// 判据：控制器是编排出口，不是算东西的地方。这里两个函数各自把「一屏就这四档」
// 与「翻页不能丢筛选条件」这两条业务结论收在一处 —— 页面模板、编辑页下拉、
// 分页组件都从同一份取值来，改一处不会只改到一半。
package productservice

import (
	"net/url"
	"strings"

	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
)

// 商品列表页的路径（筛选基地址与详情页回跳共用）。
const ProductListPaths = "/admin/products"

// ProductStatusOptions 列表筛选 / 编辑页的状态下拉项（含「全部状态」）。
//
// 槽位收**事实**（Key + 中文兜底）而不是成品串：语言是请求级的，提前翻等于把这一句
// 锁死在一种语言（英文界面上仍是中文，不报错、门禁也不红）。取词由模板的 tr 在渲染期做。
//
// 「值 → (key, 兜底)」的映射在 productenums.StatusLabel：本函数只给**值域与顺序**
// （这一屏就这四档），不再自带一张手抄的三元表。
//
// 状态维度**只有这一种入口**：不另配状态徽章筛选 —— 同一维度两套控件时，
// 用户会怀疑它们是否等价（admin-ui-logic §2）。
func ProductStatusOptions(current string) []productdto.StatusOptionResp {
	values := []string{"", productenums.StatusDraft, productenums.StatusPublished, productenums.StatusArchived}
	out := make([]productdto.StatusOptionResp, 0, len(values))
	cur := strings.TrimSpace(current)
	for _, value := range values {
		key, fallback := productenums.StatusLabel(value)
		out = append(out, productdto.StatusOptionResp{
			Value:         value,
			LabelKey:      key,
			LabelFallback: fallback,
			Selected:      value == cur,
		})
	}
	return out
}

// ProductListFilterURL 列表页的筛选基地址（**不含** page/limit：分页组件自己拼）。
//
// 分页链接由 pkg/pagination.BuildPagination 在这个基地址后追加 page/limit —— 把筛选条件
// 拼进基地址，翻页时关键词与状态才不会丢；反过来说，若基地址里塞了 page，就会出现两个
// page 参数，浏览器取第一个，翻页看起来「点了没反应」。
func ProductListFilterURL(projectID, keyword, status string) string {
	q := url.Values{}
	if p := strings.TrimSpace(projectID); p != "" {
		q.Set("project", p)
	}
	if k := strings.TrimSpace(keyword); k != "" {
		q.Set("keyword", k)
	}
	if s := strings.TrimSpace(status); s != "" {
		q.Set("status", s)
	}
	if enc := q.Encode(); enc != "" {
		return ProductListPaths + "?" + enc
	}
	return ProductListPaths
}
