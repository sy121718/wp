package pagination

// derive.go — 分页条的**派生值**：从三件事实（总数 / 当前页 / 每页条数）推出
// 页数、本页覆盖区间、按钮态、以及可直接渲染的槽位与链接。
//
// 为什么在 pkg 而不是后台视图层：这套派生是「显示形状」，但它**不依赖请求上下文** ——
// 不读 gin.Context、不看当前语言、不查库。而可视化构建器的组件（internal/builder）
// 将来也要分页（商品列表组件、站内搜索片段），那条路径不经过 gin，也 import 不了
// internal/templates。放在这里，两条路径共用一份口径。
//
// 口径与迁移前的后台实现逐字一致（limit<1 回落 DefaultPageSize、page<1 回落 1、
// 页数向上取整、page 超出页数时夹到末页）。迁移期的验收判据是「渲染字节相同」。
//
// 这里**不含取词**：文案由调用方（模板的 t/tf，或构建期的语言参数）决定。
// 槽位里的 URL 也不含 HTML 转义 —— 渲染引擎各转一次，Go 侧预转义会出现 `&amp;amp;`。

// Derived 是分页条的派生值（事实 + 显示形状 + 链接参数）。
//
// 字段全导出：模板引擎（Jet）要直接读它们，构建期组件也要。
type Derived struct {
	Total int64
	Page  int // 已夹紧到 [1, Pages]
	Limit int // 已回落 DefaultPageSize
	Pages int // 总页数（Total=0 时为 0）
	Start int // 本页第一条的序号（1 起）；没有数据时为 0
	End   int // 本页最后一条的序号；没有数据时为 0

	PageParam string // 页码参数名（默认 page）
	KeepLimit bool   // 链接里是否带 limit（由事实的 DropLimit 取反）
	Base      string // 链接基址：页面路径 + 已知筛选条件（不含 page / limit）
}

// Derive 把分页事实补成派生值。
func Derive(p Page) Derived {
	limit := p.Limit
	if limit < 1 {
		limit = DefaultPageSize
	}
	page := p.Page
	if page < 1 {
		page = 1
	}
	pages := 0
	if p.Total > 0 {
		pages = int((p.Total + int64(limit) - 1) / int64(limit))
	}
	if pages > 0 && page > pages {
		page = pages
	}
	d := Derived{
		Total: p.Total, Page: page, Limit: limit, Pages: pages,
		PageParam: p.PageParam, KeepLimit: !p.DropLimit, Base: p.Base,
	}
	if d.PageParam == "" {
		d.PageParam = "page"
	}
	if p.Total > 0 {
		d.Start = (page-1)*limit + 1
		d.End = page * limit
		if int64(d.End) > p.Total {
			d.End = int(p.Total)
		}
	}
	return d
}

// Items 分页条的槽位（含省略号位与已拼好的翻页链接）。
//
// 模板因此只剩三个判断（省略号 / 当前页 / 链接），不再算窗口、不再比较相邻页码。
// 当前页的 URL 留空：渲染层不该拿到一个「指向自己」的链接还能误点。
func (d Derived) Items() []Item {
	win := Window(d.Page, d.Pages)
	if len(win) == 0 {
		return nil
	}
	for i := range win {
		if win[i].Gap || win[i].Page == d.Page {
			continue
		}
		win[i].URL = d.PageURL(win[i].Page)
	}
	return win
}

// PageURL 第 n 页的链接：基址 + 页码 + （按需）limit。
func (d Derived) PageURL(n int) string {
	if d.KeepLimit {
		return QueryURL(d.Base, d.PageParam, n, "limit", d.Limit)
	}
	return QueryURL(d.Base, d.PageParam, n)
}

// PrevURL / NextURL 上/下一页的链接；不可用时为空串（渲染层据此走禁用态）。
func (d Derived) PrevURL() string {
	if !d.HasPrev() {
		return ""
	}
	return d.PageURL(d.Page - 1)
}

func (d Derived) NextURL() string {
	if !d.HasNext() {
		return ""
	}
	return d.PageURL(d.Page + 1)
}

// HasPagination 多于一页才有分页条（单页与空集都不渲染）。
func (d Derived) HasPagination() bool { return d.Pages > 1 }

// HasPrev / HasNext 决定上/下一页是链接还是禁用态。
func (d Derived) HasPrev() bool { return d.Page > 1 }
func (d Derived) HasNext() bool { return d.Page < d.Pages }

// PrevPage / NextPage 只在 HasPrev / HasNext 为真时使用。
func (d Derived) PrevPage() int { return d.Page - 1 }
func (d Derived) NextPage() int { return d.Page + 1 }

// WithBase 换掉链接基址。
//
// 为什么要这一步：Base 可能由渲染层从当前请求串推出来（后端没给的情况），
// 而 Items / PrevURL / NextURL 都要拼在基址上。带上基址的副本交给模板，
// 比让模板把 base 作为参数传给每个方法要干净（Jet 的方法调用带不了具名参数）。
func (d Derived) WithBase(base string) Derived {
	d.Base = base
	return d
}
