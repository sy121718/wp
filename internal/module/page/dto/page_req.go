package pagedto

import json "go_wp/pkg/jsonx"

// CreateReq 创建手工 Page。
type CreateReq struct {
	ProjectID         string  `json:"projectId" form:"projectId" binding:"required"`
	Kind              string  `json:"kind" form:"kind" binding:"required"`
	ContentTargetType string  `json:"contentTargetType" form:"contentTargetType"`
	ContentTargetID   *string `json:"contentTargetId" form:"contentTargetId"`
	DraftPath         string  `json:"draftPath" form:"draftPath" binding:"required"`
	// DraftDocument 初始文档。与 BlueprintID 二选一：给了蓝图就以蓝图为准
	//（蓝图是「用完即弃」的初始化输入，AST 会被完整复制并重生成节点 ID）。
	// 因此这里不再是 required —— 从蓝图建页时前端不需要先造一份空文档。
	DraftDocument json.RawMessage `json:"draftDocument" form:"draftDocument"`
	// BlueprintID 从哪份蓝图初始化（可选，审计 VIS-010）。
	BlueprintID string `json:"blueprintId" form:"blueprintId"`
}

// SaveDraftReq 保存 Page 草稿，使用 draftVersion 做乐观锁。
type SaveDraftReq struct {
	ID              string          `json:"id" form:"id" binding:"required"`
	ExpectedVersion int64           `json:"expectedVersion" form:"expectedVersion"`
	DraftPath       string          `json:"draftPath" form:"draftPath" binding:"required"`
	DraftDocument   json.RawMessage `json:"draftDocument" form:"draftDocument" binding:"required"`
}

// ListReq 列出 Page 摘要（必须带工程 scope，防跨工程越权）。
//
// Lang / LangState 是**语言维度收窄**（可选，空 = 不收窄）：后台页面列表要能回答
// 「哪些页面有 en-US 的线上页面」—— 这件事的事实分散在两处（pages.excluded_langs 与
// page_publications），只有服务端聚合一次才不重复算。
//
// 取值域与派生判据同 pageenums.LangStatusOf：LangState 空 = 「这一语言有页面」
// （已激活产物），excluded = 本页排除了它，published = 有产物，draft = 既没产物也没排除。
type ListReq struct {
	ProjectID string `form:"projectId" json:"projectId" binding:"required"`
	ThemeID   string `form:"themeId" json:"themeId"`
	// Lang 目标语言（空 = 不按语言收窄）。
	Lang string `form:"lang" json:"lang"`
	// LangState 该语言的产出状态（空 = 已发布；判据见 pageenums.LangStatusOf）。
	LangState string `form:"state" json:"state"`
}

// PageListQueryReq 后台页面列表的查询串（筛选栏的三项）。
//
// 独立成一条查询 DTO 而不是让控制器读各自读 c.Query：键名与归一只有一份，
// 且「读查询串」这条路径统一走 pkg/binding（控制器形状门禁的第 2 条）。
//
// State 不做 required / oneof 校验：手改 URL 的未知状态应回退成「不过滤」，
// 而不是把一张列表变成 400 —— 一条过期的链接不该让整页不可用（与 focusProjectID 同口径）。
type PageListQueryReq struct {
	Project string `form:"project"`
	Lang    string `form:"lang"`
	State   string `form:"state"`
}

// DetailReq 查询 Page。
type DetailReq struct {
	ProjectID string `form:"projectId" json:"projectId" binding:"required"`
	ID        string `form:"id" json:"id" binding:"required"`
}

// RevisionReq 查询 Page 修订历史。
type RevisionReq struct {
	PageID string `form:"pageId" json:"pageId" binding:"required"`
}

// DeleteReq 软删 Page（释放其路径占用，同路径可被新页面重新占用）。
type DeleteReq struct {
	ID string `json:"id" form:"id" binding:"required"`
}
