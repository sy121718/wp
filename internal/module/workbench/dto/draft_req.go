package workbenchdto

// draft_req.go — 工作台画布与片段的表单入参。
//
// 这些都是**片段 / 画布**端点：请求体是前端编辑器的 DOM 快照（draftDocument 整棵树），
// 响应是 text/html 片段或预览文档，所以失败出口是 c.String 而不是 JSON ——
// 控制器用 binding.ParseForm 拿 error 自己决定怎么回。
//
// 字段一律是文本：它们来自表单编码，数字与 JSON 只在 service 里解析。

// InspectorPanelReq 渲染选中节点的检查器面板片段。
type InspectorPanelReq struct {
	NodeID    string `form:"nodeId"`
	Document  string `form:"document"`
	Tab       string `form:"tab"`
	ProjectID string `form:"projectId"`
}

// OutlineTreeReq 渲染结构树片段。
type OutlineTreeReq struct {
	Document   string `form:"document"`
	SelectedID string `form:"selectedId"`
	Filter     string `form:"filter"`
}

// TemplatePreviewDraftReq 基于未保存 AST 的模板预览（画布刷新）。
type TemplatePreviewDraftReq struct {
	ID            string `form:"id"`
	DraftDocument string `form:"draftDocument"`
	ProjectID     string `form:"projectId"`
	EntityType    string `form:"entityType"`
	EntityID      string `form:"entityId"`
}

// NavigationCreateReq 检查器里新建菜单项（导航选择器的内联新建）。
type NavigationCreateReq struct {
	ProjectID string `form:"projectId"`
	Title     string `form:"title"`
	Path      string `form:"path"`
	Kind      string `form:"kind"`
}

// SEOScoreReq 草稿文档的 SEO 评分入参。
type SEOScoreReq struct {
	DraftDocument string `form:"draftDocument"`
	URL           string `form:"url"`
}

// PreviewDraftReq 基于未保存 AST 的页面预览（带乐观锁版本号）。
//
// ExpectedVersion 收**原文**而不是 int64：非法版本号在这里是「草稿解不开」这一档
// 业务失败（回 400 + ErrDraftDecodeFailed 文案），不是绑定错误 —— 用 int64 会让
// gin 在绑定期就写一个 JSON 400，把这个端点唯一的出口形状破坏掉。
type PreviewDraftReq struct {
	ID              string `form:"id"`
	DraftDocument   string `form:"draftDocument"`
	ExpectedVersion string `form:"expectedVersion"`
}
