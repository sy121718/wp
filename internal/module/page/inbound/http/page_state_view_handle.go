package pagehttp

// page_state_view_handle.go — 页面列表页「全站待重建」区块与提示词的视图类型。
//
// 这三个类型只被页面出口消费（page_stale.go / pages_handle.go / page_langs_handle.go /
// page_jump.go / page_translations_handle.go），没有一个走 JSON 出口；
// 原先住在 dto 包里，与接口 DTO 混居 —— 那是「这是给模板的还是给 API 的」要靠读消费点回答的来源。

import "go_wp/pkg/utils"

// MsgPair 词条事实（key + 中文兜底）。取词交给模板。
type MsgPair struct {
	Key      string
	Fallback string
	// Args 词条的 %s 实参（空 = 该词条没有占位符）。
	// 模板用 tf(key, fallback, args) 取词 —— 与 adminui.Msg 同形，页壳那条链路认这个形状。
	Args []string
}

// Empty 两项都空 = 没有这条提示。
func (m MsgPair) Empty() bool { return m.Key == "" && m.Fallback == "" }

// StaleItemResp 「全站待重建」清单的一行。
//
// 跨工程清单必须带工程名：每个工程都可能有一个一模一样的 /about，只给路径分不清是哪一个
// （页面列表页本身有「所属工程」上下文，这一块没有）。
type StaleItemResp struct {
	ID          string
	Path        string
	ProjectName string
	// Published 是否已上线（有活跃产物路径）：用来区分「已发布但有更新未发布」与「从未上线」——
	// 后者的处置方式不同（重建也还不会出现在访问面，要先发布）。
	Published bool
	// FailedStageKey / FailedStageFallback 最近一次自动重建失败的**阶段事实**
	// （两个都空 = 没有失败痕迹）。非空即「这页不是还没轮到，而是重建失败过」——
	// 与 stale 徽标合起来才能回答「为什么它还在这儿」。
	//
	// 行上不放拼好的句子：那一句由「前缀词条 + 阶段词条 + 时刻」三段组成，
	// 控制器拼的那一刻就是它替渲染期定语言的时刻（语言是请求级的）。
	FailedStageKey      string
	FailedStageFallback string
	// FailedAt 失败时刻（事实；格式由模板的 siteDateTime 决定）。
	FailedAt *utils.JSONTime
}

// StaleOverviewResp 「全站待重建」区块的渲染数据（页面列表页的页头区块）。
//
// 降级语义：失败 → Available=false（显示「读不到」），**绝不**渲染成「0 个待重建」
// —— 那会把一次读取失败伪装成一切正常。
//
// 唯一的例外是「一个站点工程都没有」：契约按语义返回 ErrProjectRequired，
// 而那时全站确实没有任何页面 —— 那是确定的事实，按空态处理。
//
// Limit 在空态下也给**真实口径**（模板不读它，但零值会让「清单上限是多少」
// 在两个分支里出现两种答案）。
type StaleOverviewResp struct {
	Available bool
	Total     int
	Pages     []StaleItemResp
	Limit     int
	Truncated bool
}
