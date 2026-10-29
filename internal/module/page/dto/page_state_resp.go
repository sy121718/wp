package pagedto

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
