package templates

// pager.go — 分页条的**渲染派生值**，从三件事实（总数 / 当前页 / 每页条数）推出来。
//
// 为什么要这一层：页数、本页覆盖区间、上/下页是否可用都是「显示形状」，不是数据。
// 它们曾经由 pkg/pagination.Facts 在 Go 侧算好放进入数据，于是业务层、控制器与 dto
// 都要带上 Pages / Start / End；模板只是照着抄。现在后端只给 Total / Page / Limit
// 三件事实（外加链接基址与参数名），页码窗口、区间句与按钮态由模板层算。
//
// 口径必须与旧 Facts 逐字一致（limit<1 回落 20、page<1 回落 1、页数向上取整、
// page 超出页数时夹到末页），迁移期的验收判据是「渲染字节相同」。

import "go_wp/pkg/pagination"

// Pager 是分页条的渲染值：事实字段 + 三个派生量。
type Pager struct {
	Total int64
	Page  int
	Limit int
	Pages int
	Start int // 本页第一条的序号（1 起）；没有数据时为 0
	End   int // 本页最后一条的序号；没有数据时为 0

	PageParam string // 页码参数名（默认 page）
	KeepLimit bool   // 链接里是否带 limit（由事实的 DropLimit 取反）
	Base      string // 链接基址：页面路径 + 已知筛选条件（不含 page / limit）
}

// PagerOf 把分页事实补成可渲染的派生值。
func PagerOf(p pagination.Page) Pager {
	limit := p.Limit
	if limit < 1 {
		limit = pagination.DefaultPageSize
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
	pg := Pager{Total: p.Total, Page: page, Limit: limit, Pages: pages,
		PageParam: p.PageParam, KeepLimit: !p.DropLimit, Base: p.Base}
	if pg.PageParam == "" {
		pg.PageParam = "page"
	}
	if p.Total > 0 {
		pg.Start = (page-1)*limit + 1
		pg.End = page * limit
		if int64(pg.End) > p.Total {
			pg.End = int(p.Total)
		}
	}
	return pg
}

// HasPagination 多于一页才有分页条（单页与空集都不渲染）。
func (p Pager) HasPagination() bool { return p.Pages > 1 }

// HasPrev / HasNext 决定上/下一页是链接还是禁用态。
func (p Pager) HasPrev() bool { return p.Page > 1 }
func (p Pager) HasNext() bool { return p.Page < p.Pages }

// PrevPage / NextPage 只在 HasPrev / HasNext 为真时使用。
func (p Pager) PrevPage() int { return p.Page - 1 }
func (p Pager) NextPage() int { return p.Page + 1 }
