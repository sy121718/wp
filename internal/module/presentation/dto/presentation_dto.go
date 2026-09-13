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
}

// RebuildReq 实体数据更新后重建。
type RebuildReq struct {
	EntityID string `json:"entityId" binding:"required"`
	// TemplateID 切换实例绑定的模板后重建（issue #14）；可空 = 沿用实例当前绑定。
	TemplateID string `json:"templateId"`
}

// UpdateURLReq 修改已发布实例的线上路径（改 URL）。
//
// 实例定位二选一：ID，或 EntityType + EntityID（后台页面通常只持有实体，
// 拿不到实例 id）。NewPath 为空或定位信息不全属于参数错误。
type UpdateURLReq struct {
	ID         string `json:"id" form:"id"`
	EntityType string `json:"entityType" form:"entityType"`
	EntityID   string `json:"entityId" form:"entityId"`
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
}

// DeleteReq 删除实例。
type DeleteReq struct {
	ID string `form:"id" binding:"required"`
}

// ListReq 按类型列表。
type ListReq struct {
	EntityType string `form:"entityType"`
}

// InstanceResp 实例响应。
//
// Status 由 active_artifact_id 指针推导（active / draft），不是表列。
type InstanceResp struct {
	ID           string          `json:"id"`
	ProjectID    string          `json:"projectId"`
	EntityType   string          `json:"entityType"`
	EntityID     string          `json:"entityId"`
	URLPath      string          `json:"urlPath"`
	TemplateID   string          `json:"templateId"`
	Status       string          `json:"status"`
	Stale        bool            `json:"stale"`
	ArtifactID   string          `json:"artifactId,omitempty"`
	ArtifactHash string          `json:"artifactHash,omitempty"`
	SnapshotID   string          `json:"snapshotId,omitempty"`
	Document     json.RawMessage `json:"document,omitempty"`
	UpdatedAt    string          `json:"updatedAt"`
}
