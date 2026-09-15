// publication_receipt.go —— 发布回执台账（TX-009）。
//
// 「先切访问面、再写数据库」的窗口里崩溃，此前没有任何依据能推断出该做什么：
// 线上可能已经生效、也可能没有，只能人工比对符号链接与数据库。回执把这段窗口
// 变成可判定状态 —— 切换**之前**登记 pending，成功之后标记 committed，
// 崩溃后按「pending 回执 + 符号链接实际指向」就能判断是补完成还是回滚。
package pubdto

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
	ToArtifactID   string `json:"toArtifactId" binding:"required"`
	// Lang 构建语言（多语言站点一个页面每个语言一条回执）。
	Lang   string `json:"lang"`
	Action string `json:"action"`
}

// PendingReceiptResp 一条未完成的回执（启动恢复的输入）。
type PendingReceiptResp struct {
	ID             string `json:"id"`
	SourceType     string `json:"sourceType"`
	SourceID       string `json:"sourceId"`
	ProjectID      string `json:"projectId"`
	Path           string `json:"path"`
	Lang           string `json:"lang"`
	FromArtifactID string `json:"fromArtifactId"`
	ToArtifactID   string `json:"toArtifactId"`
	Action         string `json:"action"`
	CreatedAt      string `json:"createdAt"`
}
