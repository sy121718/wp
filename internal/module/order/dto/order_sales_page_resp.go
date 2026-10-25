package orderdto

// order_sales_page_resp.go — 销售概览页的渲染视图（页面 GET 的回执形状）。
//
// 与 order_resp.go 的分工：那一份是接口契约；这一份是页面视图（模板直接读）。
// 理由与 product / masterdata / contenttemplate 一致：读一个类型不必先判断它是进还是出。
//
// **导出字段名就是模板契约**：internal/templates/admin/order/sales_overview.html 里的
// `.Field` 必须在这里有同名导出字段。

import (
	projectcontract "go_wp/internal/module/project/contract"
)

// OrderSalesPageResp 销售概览整页的渲染视图。
type OrderSalesPageResp struct {
	// Title / Menu 是 layout 的小写壳键（{{.title}} / {{.menu}}）的值。
	Title string
	Menu  string

	Projects        []projectcontract.ProjectResp
	SelectedProject string
	// Todo 待办块（库存预警 / 待发货 / 合计）。零值即「都不渲染」。
	Todo SalesTodoResp

	// Overview 取数结果：dto 原样交给模板（卡片与趋势图都由模板从它算出来）。
	//
	// 取数失败时保持 nil —— 模板据此不渲染半张报表。一半真一半 0 的页面比空态危险得多
	// （读的人会以为「这段时间只卖了一点点」）。
	Overview *SalesOverviewResp

	// 生效区间与筛选回显（service 归一化后的值，不是请求原值）。
	FilterFrom   string
	FilterTo     string
	FilterStatus string
	Monthly      int

	// StatusQuickValues 状态快捷项的取值（空值项走「全部」自己的词条）。
	StatusQuickValues []string
	// OrderStatusLabel 状态 → 当前语言标签的取词函数。
	//
	// 这是**语言绑定行为**留在视图上的已知存量（AGENTS.md 的 dto 分层表禁止语言绑定字段）。
	// 本轮迁移只换外层形状、不动这个契约：模板 `{{statusLabel := .OrderStatusLabel}}` 然后
	// 逐行调用它，改成「预先把每行的标签算好」会同时改模板与三处调用点 —— 那是另一件事，
	// 与「页面出口换形态」分开做（混在一起时，出问题分不清是形状还是取词改坏的）。
	OrderStatusLabel func(string) string

	// LoadErr 这一次没读出来的原因（空串 = 正常）。
	// 模板据此把「装载失败」与「这个区间没有销售」分开 —— 两者长得一样会让运营误判。
	LoadErr string
	// ListQuery 快捷项链接要保留当前区间与工程：点一下状态就把区间丢回默认，
	// 是本页最容易让人困惑的交互（「我明明选的是上个月」）。
	ListQuery string
}

// SalesTodoResp 待办块的三个数字（迁移 624 / 623 / 624，W4）。
//
// 取不到的项是 -1，模板据此不渲染那一行（而不是渲染一个「0」假装没有待办）。
// total 是**展示口径**（一个总数），不是业务计算 —— 它由三个数字相加得出，
// 相加这件事属于「这一屏怎么显示」，所以留在装配侧。
type SalesTodoResp struct {
	// 字段名导出（Go 语法要求），模板键用小写 —— view tag 让两者对上，
	// 与 AutomationCanvasPageResp 的 title/jsVer 双出同一手法。
	AlertCount  int `view:"alertCount"`
	PendingShip int `view:"pendingShip"`
	Total       int `view:"total"`
}
