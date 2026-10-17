package productdto

// product_bulk_req.go / product_bulk_resp.go — 批量用例的入参（Req）与回执（Resp）。
//
// 文件按「入参 / 返回」成对出现：一个域只有这两份，类型名以 Req / Resp 结尾。
// 为什么批量另起一套类型、而不是扩展现有的 productenums.Outcome：Outcome 表达的是**单条**
// 操作的结局（成功 / 跳过 / 失败），批量用例需要的是**四个计数的分桶**（请求了 N 个、成了几个、
// 业务跳过几个、内部失败几个）。把计数塞进 Outcome 会让「状态」与「统计」两种语义混在一个类型里。
//
// 逐项明细是后加的：提示页协议当初只展示一句计数，改走 ui/json_write.js 之后消费方出现了 ——
// 基座逐条渲染失败原因，只有计数时操作者看到「跳过 3 条」而无从下手。判定口径仍归 service：
// 明细里存的是**业务 key**，成品句由 inbound 取词，不在 service 拼中文。

// BulkDeleteReq 五个批量删除用例共用的入参。
type BulkDeleteReq struct {
	// IDs 目标 id 清单（inbound 已过 shell.BulkIDs / 批量 JSON 端点自行归一：去空白 /
	// 去重 / 上限 200；service 侧仍会再归一一次，见 normalizeBulkIDs）。
	IDs []string `json:"ids"`
	// ProjectID 工程作用域；为空时由 service 走「唯一工程」兜底。
	// 多工程部署下必须带：空着只剩「唯一工程」兜底一条路，否则 ErrInvalidParam。
	ProjectID string `json:"projectId"`
}

// BulkPricingReq 批量改价的入参。
type BulkPricingReq struct {
	// IDs 目标商品 id 清单（语义同 BulkDeleteReq.IDs）。
	IDs []string
	// ProjectID 工程作用域；为空时由 service 走「唯一工程」兜底。
	ProjectID string
	// Note 本次调价的备注（写进每一条留痕台账）。
	Note string
	// OperatorID 操作人（inbound 从会话覆盖写入，客户端传入的值被忽略 —— 调价留痕的操作人不可伪造）。
	OperatorID string
	// Rule 定价规则。Scope 与 TargetID 由 service 逐商品覆盖（批量入口恒为
	// 「单个商品的全部变体」，TargetID = 当前商品 id）。
	Rule PricingRuleReq
}
