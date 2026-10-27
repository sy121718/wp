package productdto

// product_publish_attempt_resp.go — 发布历史的 JSON 出口形状（迁移 671）。
//
// **只交事实，不交展示形态**：触发源与结果给码值（create / manual / … / success / failed），
// 文案由页面里的 Jet <template> 预置各档文本、按 data-when 显隐 —— 后端不产 HTML，
// 前端不存词表。
//
// 唯一例外是**格式化后的文本**（时间 / 语言串 / 耗时 / 重试次数）：前端控件只会填
// textContent，而时间与浮点格式化在前端没有唯一实现（Jet 的 formatTime 只在渲染期可用）。
// 这类字段带 Text 后缀，与 LastOrderTimeText / RegisteredAtText 同一先例。
//
// 分页就是分页：page / size / hasMore 与全仓分页口径一致，前端点「加载更多」取下一页追加。

// PublishAttemptListResp 一页发布历史。
type PublishAttemptListResp struct {
	// Ready 发布台账端口是否装配：假 → 面板显示「没接上」，不显示「一次都没发过」
	// （装配缺陷要人去查接线，与「读不到」「没记录」是三档）。
	Ready bool                     `json:"ready"`
	Rows  []PublishAttemptRowResp `json:"rows"`
	// Total 该商品的全部尝试条数（面板顶部「共 N 次」）。
	Total int64 `json:"total"`
	// Page 本次是第几页（1 起），Size 每页条数 —— 与全仓分页口径一致，
	// 「加载更多」就是取下一页并追加（倒序列表里下一页 = 更早的行，追加在末尾）。
	Page int `json:"page"`
	Size int `json:"size"`
	// HasMore 还有没有下一页。
	HasMore bool `json:"hasMore"`
}

// PublishAttemptRowResp 一次发布尝试。
type PublishAttemptRowResp struct {
	ID int64 `json:"id"`
	// CreatedAtText 尝试发生的时刻，已按后台显示口径格式化为 "2006-01-02 15:04"。
	//
	// 交已格式化的文本而不是毫秒数：Jet 侧没有日期函数（formatTime 只在模板渲染期可用），
	// 而这条 JSON 由前端控件填进 <template>，前端也没有时间格式化的唯一实现。
	// 与 cLastOrderTimeText / RegisteredAtText 同一先例。
	CreatedAtText string `json:"createdAtText"`
	// Trigger 触发源码值：create / manual / template_switch / update_url / rollback /
	// override / auto_rebuild（与迁移 671 的 CHECK 逐字一致）。
	Trigger string `json:"trigger"`
	// Outcome success / failed。
	Outcome   string `json:"outcome"`
	ErrorText string `json:"errorText"`
	// AttemptsText 重试次数的显示串（空 = 一次成，不显示）。
	AttemptsText string `json:"attemptsText"`
	// DurationText 耗时显示串（"1.2s"）。
	DurationText string `json:"durationText"`
	// Langs 本次涉及的语言（失败时 = 尝试的范围）。
	Langs []string `json:"langs"`
	// LangsText 语言的显示串（"zh-CN, en"）。前端模板只能填文本，列表由服务端拼 ——
	// 与 TimeLabel / LastOrderTimeText 同一先例（显示口径不在前端存第二份）。
	LangsText string `json:"langsText"`
	// Attempts 冲突重试次数（>1 说明有编译结果被丢弃重来）。
	Attempts int `json:"attempts"`
	// ArtifactVersion / ArtifactHash 成功后指向的产物；失败时 0 / 空串。
	ArtifactVersion int64  `json:"artifactVersion"`
	ArtifactHash    string `json:"artifactHash"`
	// OperatorName 操作人快照；空串 = 系统触发（自动重建）。
	OperatorName string `json:"operatorName"`
	DurationMs   int64  `json:"durationMs"`
}

// PublishAttemptListReq 发布历史的查询串。
type PublishAttemptListReq struct {
	ProjectID string `form:"project"`
	// ProductID 商品 id（必填）。
	ProductID string `form:"productId"`
	// Page 第几页（1 起，缺省 1）；Size 每页条数（缺省 10，上限 100）。
	Page int `form:"page"`
	Size int `form:"size"`
}