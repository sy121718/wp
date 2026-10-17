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

// BulkResultResp 批量用例的结果（四个桶，合计恒等于 Requested）。
type BulkResultResp struct {
	// Requested 请求处理的项数（去空白 / 去重之后）。
	Requested int `json:"requested"`
	// Succeeded 真正完成的项数。
	Succeeded int `json:"succeeded"`
	// Skipped 业务拒绝的项数（不存在 / 有子级 / 被引用 / 跨工程引用）。
	Skipped int `json:"skipped"`
	// Failed 内部失败的项数（数据库错误这类）。只计数，原文进日志、不进响应。
	Failed int `json:"failed"`
	// Items 逐条回执（与三个计数同源：同一批归一后的 id 顺序）。
	// 未成功的那条带 ReasonKey（业务 key，由 inbound 取词成句子）。
	Items []BulkItemResp `json:"items,omitempty"`
}

// BulkItemResp 批量里的一条回执。
//
// Deleted 是**破坏动作**的键：非破坏动作（改价 / 打标签）用 Updated，同一个键不能既表示
// 「成功了」又表示「把它从列表里删掉」（基座按 outcome 读不同的键，见 json_write.js 的
// okKeyOf）。
type BulkItemResp struct {
	ID string `json:"id"`
	// Deleted / Updated 两个键**互斥**，按用例取其一：破坏动作（删除）用 Deleted，
	// 非破坏动作（改价 / 改状态 / 打标签）用 Updated。一个字段同时承担两义会让
	// 「把它从列表里删掉」与「它被改过了」变成同一句话（json_write.js 的 okKeyOf
	// 就是按这个区分摘要句的）。
	Deleted   bool   `json:"deleted,omitempty"`
	Updated   bool   `json:"updated,omitempty"`
	ReasonKey string `json:"reasonKey,omitempty"`
}

// NotDone 未成功的项数（业务跳过 + 内部失败）。
//
// 提示页只有「已删 N / 未成功 M」两个数字，M 由这里给出：让控制器自己把两个桶相加，
// 等于把它不拥有的口径搬到控制器（dd.txt §5.5）。
func (r BulkResultResp) NotDone() int { return r.Skipped + r.Failed }

// BulkPricingResultResp 批量改价的结果（在 BulkResultResp 之上多两个改价专属计数）。
type BulkPricingResultResp struct {
	BulkResultResp
	// ChangedVariants 真正改动的变体数（一次改价覆盖商品的多个变体）。
	ChangedVariants int `json:"changedVariants"`
	// UnchangedProducts 本来就在目标价上的商品数（service 不写空台账，故单独计数）。
	UnchangedProducts int `json:"unchangedProducts"`
	// SkipReasonKey 第一个跳过原因的**业务 key**（enums 常量值）。
	// 存 key 不存成品串：成品串要按请求语言取词，而取词只在 inbound 有（productErrText）。
	SkipReasonKey string `json:"skipReasonKey,omitempty"`
}
