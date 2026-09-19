// Package presentationdto presentation 模块请求/响应结构。
package presentationdto

import "encoding/json"

// CreateInstanceReq 创建自动发布实例。
type CreateInstanceReq struct {
	EntityType string `json:"entityType" binding:"required"`
	EntityID   string `json:"entityId" binding:"required"`
	URLPath    string `json:"urlPath" binding:"required"`
	// ProjectID 实例所属站点工程（presentation_instances.project_id 为 NOT NULL 外键）。
	// 可空：缺省时经 project 契约解析（工程唯一时取该工程），否则报参数错误。
	ProjectID string `json:"projectId"`
	// TemplateID 显式指定使用哪套模板（issue #14：同一实体类型下可有多套命名模板）。
	// 可空：缺省时按实体类型取默认模板（既有行为不变）。
	TemplateID string `json:"templateId"`
	// InstanceRole 要建哪一类实例（审计 EDT-004）：空 = detail（实体详情页，既有行为）。
	// archive = 归档列表页（如「某分类下的商品列表」），与详情页共存于同一实体。
	InstanceRole string `json:"instanceRole"`
}

// EnsureArchiveReq 确保某实体的归档页存在（审计 EDT-004）。
//
// 由实体侧（分类 / 标签 / 品牌）在增删改时调用：新建实体 → 补建归档页，
// 改名 → 更新路径，删除 → 下线。
type EnsureArchiveReq struct {
	ProjectID string `json:"projectId"`
	// EntityType 归档主体的实体类型（category / tag / brand）。
	EntityType string `json:"entityType" required:"true"`
	EntityID   string `json:"entityId" required:"true"`
	// Slug 用于按规则生成访问路径（/{entityType}/{slug}）。
	Slug string `json:"slug"`
}

// EnsureArchiveResp 归档页同步结果。
type EnsureArchiveResp struct {
	// Skipped 非空表示本次没有动作及原因（如工程未配置归档模板、实体无 slug）。
	// **这是正常状态而不是错误**：没有配归档模板的站点不该因为「新建了分类」而报错。
	Skipped    string `json:"skipped,omitempty"`
	InstanceID string `json:"instanceId,omitempty"`
	Created    bool   `json:"created"`
}

// RebuildReq 实体数据更新后重建。
type RebuildReq struct {
	EntityID string `json:"entityId" binding:"required"`
	// ProjectID 实例所属工程（DB-009 第二批：签名里没有工程 id 的读写路径要补工程作用域）。
	// 可空：缺省时经 project 契约解析（工程唯一时取该工程），多工程时必须显式指定。
	ProjectID string `json:"projectId" form:"projectId"`
	// TemplateID 切换实例绑定的模板后重建（issue #14）；可空 = 沿用实例当前绑定。
	TemplateID string `json:"templateId"`
}

// SaveOverrideReq 保存实例级文档覆盖（迁移 281，docs/04-C-instance-override.md）：
// workbench 实例模式的保存通道。Document 必须是合法的 Page Document JSON。
type SaveOverrideReq struct {
	InstanceID string `json:"instanceId" binding:"required"`
	// ProjectID 实例所属工程（DB-009 第二批）；可空时经 project 契约解析。
	ProjectID string `json:"projectId"`
	// Document 覆盖后的文档（含 binding 节点，编译期照常解析实体数据）。
	Document json.RawMessage `json:"document" binding:"required"`
}

// ClearOverrideReq 清除实例级文档覆盖（放弃自定义，按模板重建）。
// TemplateID 非空 = 同时切换绑定的模板（换底稿）；可空 = 沿用当前绑定。
type ClearOverrideReq struct {
	InstanceID string `json:"instanceId" binding:"required"`
	ProjectID  string `json:"projectId"`
	TemplateID string `json:"templateId"`
}

// UpdateURLReq 修改已发布实例的线上路径（改 URL）。
//
// 实例定位二选一：ID，或 EntityType + EntityID（后台页面通常只持有实体，
// 拿不到实例 id）。NewPath 为空或定位信息不全属于参数错误。

// ReapplyPresetReq 重新套用预设：放弃该商品独立文档，回到跟随模板（可反悔的另一半）。
type ReapplyPresetReq struct {
	InstanceID string `json:"instanceId" binding:"required"`
	ProjectID  string `json:"projectId"`
	// TemplateID 可选：同时切换到另一套模板（换底稿 = 放弃独立文档）。
	TemplateID string `json:"templateId"`
}

// RollbackArtifactReq 产物指针回滚（秒级，不重新编译；要求历史产物文件仍在磁盘上）。
type RollbackArtifactReq struct {
	InstanceID string `json:"instanceId" binding:"required"`
	ProjectID  string `json:"projectId"`
	// TargetHash 目标产物的内容哈希（presentation_artifacts.artifact_hash）。
	TargetHash string `json:"targetHash" binding:"required"`
}

// RollbackDocumentReq 快照级文档回滚：取历史快照的文档重新发布（重新编译，数据取最新）。
type RollbackDocumentReq struct {
	InstanceID string `json:"instanceId" binding:"required"`
	ProjectID  string `json:"projectId"`
	SnapshotID string `json:"snapshotId" binding:"required"`
}

// ListSnapshotsReq 实例历史快照清单（回滚目标选择）。
type ListSnapshotsReq struct {
	InstanceID string `form:"instanceId" json:"instanceId" binding:"required"`
	ProjectID  string `form:"projectId" json:"projectId"`
	Limit      int    `form:"limit" json:"limit"`
}

// SnapshotSummary 历史快照投影（只给选择回滚目标需要的字段）。
type SnapshotSummary struct {
	ID                      string `json:"id"`
	SourceTemplateVersionID string `json:"sourceTemplateVersionId"`
	CreatedAt               string `json:"createdAt"`
}
type UpdateURLReq struct {
	ID         string `json:"id" form:"id"`
	EntityType string `json:"entityType" form:"entityType"`
	EntityID   string `json:"entityId" form:"entityId"`
	// ProjectID 实例所属工程（DB-009 第二批）：定位实例与占用预检都在工程作用域内，
	// 缺省时经 project 契约解析（工程唯一时取该工程）。
	ProjectID string `json:"projectId" form:"projectId"`
	// NewPath 新的线上路径（站内绝对路径，如 /shop/phone-x）。
	NewPath string `json:"newPath" form:"newPath"`
	// WithRedirect 旧路径登记 301 永久重定向；false = 直接取消旧路径激活。
	// 与手工页面改 URL 的 WithRedirect 同一语义（page_publish.go §UpdateURL）。
	WithRedirect bool `json:"withRedirect" form:"withRedirect"`
}

// GetByEntityReq 按内容实体查询实例（后台「详情页模板」页读当前绑定）。
type GetByEntityReq struct {
	EntityType string `form:"entityType" binding:"required"`
	EntityID   string `form:"entityId" binding:"required"`
	// ProjectID 工程作用域（DB-009 第二批）；可空，缺省时取唯一工程。
	ProjectID string `form:"projectId" json:"projectId"`
}

// PreviewInstanceReq 发布前预览模板渲染效果（issue #14）。
//
// 只读渲染：不写快照/产物/指针，不激活 URL —— 预览不得改变线上状态。
type PreviewInstanceReq struct {
	EntityType string `json:"entityType" form:"entityType" binding:"required"`
	EntityID   string `json:"entityId" form:"entityId" binding:"required"`
	// TemplateID 预览哪套模板；可空 = 按实体类型取默认模板。
	TemplateID string `json:"templateId" form:"templateId"`
	// ProjectID 构建上下文所属工程（集合源按工程取数）；可空时经 project 契约解析。
	ProjectID string `json:"projectId" form:"projectId"`
	// DraftDocument 工作台未保存草稿（EDT-001）：非空且合法 JSON 时覆盖模板当前
	// draft_document，使画布预览与即将保存的排版一致；不写库。
	DraftDocument json.RawMessage `json:"draftDocument" form:"draftDocument"`
}

// PreviewInstanceResp 预览响应：渲染结果 + 实际使用的模板与版本。
type PreviewInstanceResp struct {
	HTML              string `json:"html"`
	EntityType        string `json:"entityType"`
	EntityID          string `json:"entityId"`
	TemplateID        string `json:"templateId"`
	TemplateName      string `json:"templateName"`
	TemplateVersionID string `json:"templateVersionId"`
	TemplateVersion   int64  `json:"templateVersion"`
}

// GetReq 按 ID 查询。
type GetReq struct {
	ID string `form:"id" binding:"required"`
	// ProjectID 工程作用域（DB-009 第二批）；可空，缺省时取唯一工程。
	ProjectID string `form:"projectId" json:"projectId"`
}

// DeleteReq 删除实例。
type DeleteReq struct {
	ID string `form:"id" binding:"required"`
	// ProjectID 工程作用域（DB-009 第二批）；可空，缺省时取唯一工程。
	ProjectID string `form:"projectId" json:"projectId"`
}

// ListReq 按类型列表。
type ListReq struct {
	EntityType string `form:"entityType"`
	// ProjectID 工程作用域（DB-009 第二批）：列表只返回本工程的实例，
	// 可空时取唯一工程；多工程时必须显式指定。
	ProjectID string `form:"projectId" json:"projectId"`
}

// InstanceResp 实例响应。
//
// Status 由 active_artifact_id 指针推导（active / draft），不是表列。
type InstanceResp struct {
	ID         string `json:"id"`
	ProjectID  string `json:"projectId"`
	EntityType string `json:"entityType"`
	EntityID   string `json:"entityId"`
	// InstanceRole 实例角色（审计 EDT-004）：detail = 实体详情页，archive = 归档列表页。
	// 同一个分类可以同时有这两张页面；空值按 detail 处理（既有行为不变）。
	InstanceRole string `json:"instanceRole,omitempty"`
	URLPath      string `json:"urlPath"`
	TemplateID   string `json:"templateId"`
	// RenderMode 渲染模式（迁移 282）：template=跟随模板 | document=该商品独立文档。
	RenderMode string `json:"renderMode,omitempty"`
	// SourceTemplateVersionID 当前快照依据的模板版本：与模板最新版对比即可提示
	// 「预设有新版本」（document 模式不会自动跟随，只能靠它提示用户）。
	SourceTemplateVersionID string          `json:"sourceTemplateVersionId,omitempty"`
	Status                  string          `json:"status"`
	Stale                   bool            `json:"stale"`
	ArtifactID              string          `json:"artifactId,omitempty"`
	ArtifactHash            string          `json:"artifactHash,omitempty"`
	SnapshotID              string          `json:"snapshotId,omitempty"`
	Document                json.RawMessage `json:"document,omitempty"`
	UpdatedAt               string          `json:"updatedAt"`
}
