package productdto

// product_detail_template_req.go — 详情页模板的双轨写动作入参（迁移 282）。
//
// 两个动作原先只有页面 POST（/admin/products/reapply-preset、/rollback-document），
// 后台页面改用 ui/json_write.js 直调接口后，页面 POST 与它那套「渲染提示页再跳回」的
// 出口一并删除 —— 结论改成一行回执，刷新由页面自己做。
//
// 入参用**实例 ID** 而不是商品 ID：这两个动作的对象是发布实例（一条商品恰好一个实例），
// 定位依据与 workbench 进入实例模式的入口一致（那里也是 /workbench?instance=…）。
// projectId 照旧显式回传：实例属于哪个工程是请求方的事实，不由服务端猜
//（presentation 侧「工程唯一时取该工程」的兜底在多工程站点会落到另一个站点的实例）。

// DetailTemplateModeReapplyReq 重新套用预设：放弃实例的独立文档、回到跟随模板。
type DetailTemplateModeReapplyReq struct {
	InstanceID string `json:"instanceId" binding:"required"`
	ProjectID  string `json:"projectId"`
	// TemplateID 非空时同时换一套底稿；留空 = 沿用当前绑定。
	TemplateID string `json:"templateId"`
}

// DetailTemplateModeRollbackReq 文档级回滚：取历史快照的文档重新发布（实体数据取最新）。
type DetailTemplateModeRollbackReq struct {
	InstanceID string `json:"instanceId" binding:"required"`
	ProjectID  string `json:"projectId"`
	SnapshotID string `json:"snapshotId" binding:"required"`
}
