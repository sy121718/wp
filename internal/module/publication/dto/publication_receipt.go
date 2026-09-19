// publication_receipt.go —— 发布回执台账（TX-009）。
//
// 「先切访问面、再写数据库」的窗口里崩溃，此前没有任何依据能推断出该做什么：
// 线上可能已经生效、也可能没有，只能人工比对符号链接与数据库。回执把这段窗口
// 变成可判定状态 —— 切换**之前**登记 pending，成功之后标记 committed，
// 崩溃后按「pending 回执 + 符号链接实际指向」就能判断是补完成还是回滚。
package pubdto

// 访问面切换回执的三种动作名。
//
// 恢复流程按它分派补齐例程：三种动作「已切换访问面、数据库没跟上」时要补的
// 数据库步骤各不相同（发布补活跃指针、改 URL 补路径迁移与路由、回滚补活跃指针
// 与旧路径下线），用一个动作名会让恢复猜错方向。
//
// 与此并存的是路由回执（Activate 的 "activate" / Redirect 的 "redirect"）：
// 它们由各自的事务内结案与补偿处理，不在启动恢复的分派范围内。
const (
	// ReceiptActionSwitchActive 发布：路径上的符号链接从旧产物切到暂存产物。
	ReceiptActionSwitchActive = "switch_active"
	// ReceiptActionUpdateURL 改 URL：新路径激活了新产物，旧路径按 301 / 下线处置。
	ReceiptActionUpdateURL = "update_url"
	// ReceiptActionRollback 回滚：路径上的符号链接切回历史产物。
	ReceiptActionRollback = "rollback"
)

// 回执归属（publication_receipts.source_type，与 page_routes 的 CHECK 同源）。
//
// 收敛例程按它筛出「自己该管的那一批」：手工页面（page）与自动发布实例（presentation）
// 的活跃指针在不同表上，用同一套补齐逻辑会写错地方。字面量收在这里而不是各调用方
// 各写一遍 —— 两处各写一遍迟早分叉，而分叉的表现是「回执留在 pending 但没人认领」。
const (
	// ReceiptSourcePage 手工页面发布（page 模块的发布链）。
	ReceiptSourcePage = "page"
	// ReceiptSourcePresentation 自动发布实例（presentation 模块的自动发布）。
	ReceiptSourcePresentation = "presentation"
)

// BeginPublishReceiptReq 登记一条「即将切换访问面」的发布回执。
//
// FromArtifactID 是本次发布前该语言的活跃产物（首次发布为空）；ToArtifactID 是本次
// 要激活的产物。恢复流程靠它们判断「符号链接现在指着谁」，因此两者都要如实填写。
type BeginPublishReceiptReq struct {
	ProjectID string `json:"projectId" binding:"required"`
	Path      string `json:"path" binding:"required"`
	// PageID / PresentationID 二选一：手工页面发布走前者，自动发布实例走后者。
	PageID         string `json:"pageId"`
	PresentationID string `json:"presentationId"`
	FromArtifactID string `json:"fromArtifactId"`
	// ToArtifactID 本次要激活的产物行 id。
	//
	// 例外：Action 为 update_url 时允许为空 —— 改 URL 的产物是**按新路径**现编译的
	// （canonicalPath 进 Manifest 并参与 hash），登记回执时（切换之前）拿不到它的
	// 产物行 id。该形态的恢复改用「新路径上的产物 canonicalPath 是否等于新路径」判定。
	ToArtifactID string `json:"toArtifactId"`
	// Lang 构建语言（多语言站点一个页面每个语言一条回执）。
	Lang   string `json:"lang"`
	Action string `json:"action"`
	// OldPath 切换前该语言的线上路径（改 URL 的旧路径 / 回滚前的路径）。
	//
	// 放进回执是因为恢复要用它处置旧路径（301 或下线）：从新路径反推不出来，
	// 而那一刻数据库里的激活记录还是旧值也会随时间被后续操作改掉。
	OldPath string `json:"oldPath"`
	// Redirect 旧路径是否应登记为 301 重定向（update_url 专用）。
	Redirect bool `json:"redirect"`
}

// PendingReceiptResp 一条未完成的回执（启动恢复的输入）。
type PendingReceiptResp struct {
	ID         string `json:"id"`
	SourceType string `json:"sourceType"`
	SourceID   string `json:"sourceId"`
	ProjectID  string `json:"projectId"`
	Path       string `json:"path"`
	Lang       string `json:"lang"`
	// FromArtifactID / ToArtifactID 切换前后的产物行 id（update_url 的 to 为空，见请求字段注释）。
	FromArtifactID string `json:"fromArtifactId"`
	ToArtifactID   string `json:"toArtifactId"`
	Action         string `json:"action"`
	// OldPath / Redirect 切换前线上路径与它的处置方式（见请求字段注释）。
	OldPath   string `json:"oldPath"`
	Redirect  bool   `json:"redirect"`
	CreatedAt string `json:"createdAt"`
}

// ReceiptsQueryReq 领取 / 统计未结案回执的过滤条件（收敛例程与可观测接口共用同一口径）。
//
// 过滤条件为什么由调用方传：同一张表上叠着多套恢复职责 —— 手工页面（page）与自动发布
// 实例（presentation）各自的补齐例程认得的动作不同，路由回执（activate / redirect）
// 又由各自的事务内结案处理。领到不属于自己的回执只会被反复领取再跳过，还会把真正
// 待办的行挤出单批上限（饥饿）—— 所以「领取口径」必须与「谁能收敛」严格一致。
type ReceiptsQueryReq struct {
	// SourceType 回执归属（'page' / 'presentation'）；空 = 不限。
	SourceType string `json:"sourceType"`
	// Actions 认得的动作名；空 = 不限（不筛就领到全部，谨慎使用）。
	Actions []string `json:"actions"`
	// Limit 单批上限（领取用；统计忽略）。<= 0 视为「不领任何行」。
	Limit int `json:"limit"`
}
