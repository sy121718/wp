package productdto

// product_bulk.go — 批量用例的结果契约与入参（task-156，dd.txt §5.2）。
//
// 为什么另起一套类型、而不是扩展现有的 productenums.Outcome：Outcome 表达的是**单条**操作的
// 结局（成功 / 跳过 / 失败），批量用例需要的是**四个计数的分桶**（请求了 N 个、成了几个、
// 业务跳过几个、内部失败几个）。把计数塞进 Outcome 会让「状态」与「统计」两种语义混在一个
// 类型里，读侧也就无法再按状态分支 —— dd.txt §5.2 的原话：不要扩成万能容器。
//
// 刻意没有「逐项错误明细」（BulkItemError 只是方案里的示意）：现有的提示页协议只展示
// 「已删 N / 未成功 M」这一句，逐项明细没有任何消费方；先造一份没人读的明细，
// 等于给同一件事留第二份真相。

// BulkResult 批量用例的结果（四个桶，合计恒等于 Requested）。
type BulkResult struct {
	// Requested 请求处理的项数（去空白 / 去重之后）。
	Requested int `json:"requested"`
	// Succeeded 真正完成的项数。
	Succeeded int `json:"succeeded"`
	// Skipped 业务拒绝的项数（不存在 / 有子级 / 被引用 / 跨工程引用）。
	Skipped int `json:"skipped"`
	// Failed 内部失败的项数（数据库错误这类）。只计数，原文进日志、不进响应。
	Failed int `json:"failed"`
}

// NotDone 未成功的项数（业务跳过 + 内部失败）。
//
// 提示页只有「已删 N / 未成功 M」两个数字，M 由这里给出：让控制器自己把两个桶相加，
// 等于把它不拥有的口径搬到控制器（dd.txt §5.5）。
func (r BulkResult) NotDone() int { return r.Skipped + r.Failed }

// BulkPricingResult 批量改价的结果（在 BulkResult 之上多两个改价专属计数）。
type BulkPricingResult struct {
	BulkResult
	// ChangedVariants 真正改动的变体数（一次改价覆盖商品的多个变体）。
	ChangedVariants int `json:"changedVariants"`
	// UnchangedProducts 本来就在目标价上的商品数（service 不写空台账，故单独计数）。
	UnchangedProducts int `json:"unchangedProducts"`
	// SkipReasonKey 第一个跳过原因的**业务 key**（enums 常量值）。
	// 存 key 不存成品串：成品串要按请求语言取词，而取词只在 inbound 有（productErrText）。
	SkipReasonKey string `json:"skipReasonKey,omitempty"`
}

// BulkDeleteReq 五个批量删除用例共用的入参。
type BulkDeleteReq struct {
	// IDs 目标 id 清单（inbound 已过 shell.BulkIDs：去空白 / 去重 / 上限 200；
	// service 侧仍会再归一一次，见 normalizeBulkIDs）。
	IDs []string
	// ProjectID 工程作用域；为空时由 service 走「唯一工程」兜底。
	ProjectID string
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
