package productdto

// product_detail_template_req.go — 详情页模板的双轨写动作入参（迁移 282）。
//
// 两个动作原先只有页面 POST（/admin/products/reapply-preset、/rollback-document），
// 后台页面改用 ui/write-base.js 直调接口后，页面 POST 与它那套「渲染提示页再跳回」的
// 出口一并删除 —— 结论改成一行回执，刷新由页面自己做。
//
// 入参用**实例 ID** 而不是商品 ID：这两个动作的对象是发布实例（一条商品恰好一个实例），
// 定位依据与 workbench 进入实例模式的入口一致（那里也是 /workbench?instance=…）。
// projectId 照旧显式回传：实例属于哪个工程是请求方的事实，不由服务端猜
//（presentation 侧「工程唯一时取该工程」的兜底在多工程站点会落到另一个站点的实例）。

// DetailTemplateModeReapplyReq 重新套用预设：放弃实例的独立文档、回到跟随模板。
type DetailTemplateModeReapplyReq struct {
	InstanceID string `json:"instanceId" form:"instanceId" binding:"required"`
	ProjectID string `json:"projectId" form:"projectId"`
	// TemplateID 非空时同时换一套底稿；留空 = 沿用当前绑定。
	TemplateID string `json:"templateId" form:"templateId"`
}

// DetailTemplateModeRollbackReq 文档级回滚：取历史快照的文档重新发布（实体数据取最新）。
type DetailTemplateModeRollbackReq struct {
	InstanceID string `json:"instanceId" form:"instanceId" binding:"required"`
	ProjectID string `json:"projectId" form:"projectId"`
	SnapshotID string `json:"snapshotId" form:"snapshotId" binding:"required"`
}

// 以下四条是详情页模板页的写动作（新建命名模板 / 首次发布 / 切换模板 / 改 URL）。
//
// 入参用**商品 ID**（不是实例 ID）：发布之前根本没有实例，四个动作的定位依据都是
// 「哪件商品、哪个工程」。projectId 照旧显式回传 —— 工程作用域来自页面上用户选中的
// 那个站点，presentation 侧「工程唯一时取该工程」的兜底在多工程站会落到别的站点。

// DetailTemplateCreateReq 新建一套命名模板（复制指定模板或默认模板的文档，初始 v1）。
type DetailTemplateCreateReq struct {
	ProjectID string `json:"projectId" form:"projectId"`
	ProductID string `json:"productId" form:"productId" binding:"required"`
	Name string `json:"name" form:"name" binding:"required"`
	// CopyFrom 复制来源模板 ID；留空 = 该类型的默认模板。
	CopyFrom string `json:"copyFrom" form:"copyFrom"`
}

// DetailTemplatePublishReq 首次为该商品建立发布实例并发布（URLPath 留空按商品 slug 派生）。
type DetailTemplatePublishReq struct {
	ProjectID string `json:"projectId" form:"projectId"`
	ProductID string `json:"productId" form:"productId" binding:"required"`
	TemplateID string `json:"templateId" form:"templateId"`
	URLPath string `json:"urlPath" form:"urlPath"`
}

// DetailTemplateApplyReq 把商品切换到选中模板并重新发布。
type DetailTemplateApplyReq struct {
	ProjectID string `json:"projectId" form:"projectId"`
	ProductID string `json:"productId" form:"productId" binding:"required"`
	TemplateID string `json:"templateId" form:"templateId"`
}

// DetailTemplateUpdateURLReq 修改线上路径（不动内容与模板绑定）。
type DetailTemplateUpdateURLReq struct {
	ProjectID string `json:"projectId" form:"projectId"`
	ProductID string `json:"productId" form:"productId" binding:"required"`
	NewPath string `json:"newPath" form:"newPath" binding:"required"`
	// WithRedirect 勾选时旧链接 301 到新路径。未勾选时客户端根本不发这个键，
	// 于是零值 false —— 与页面表单「复选框不勾即不提交」同一语义。
	WithRedirect bool `json:"withRedirect" form:"withRedirect"`
}
