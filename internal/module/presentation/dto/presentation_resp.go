package presentationdto

// Package presentationdto presentation 模块请求/响应结构。

import json "go_wp/pkg/jsonx"

// EnsureArchiveResp 归档页同步结果。
type EnsureArchiveResp struct {
	// Skipped 非空表示本次没有动作及原因（如工程未配置归档模板、实体无 slug）。
	// **这是正常状态而不是错误**：没有配归档模板的站点不该因为「新建了分类」而报错。
	Skipped    string `json:"skipped,omitempty"`
	InstanceID string `json:"instanceId,omitempty"`
	Created    bool   `json:"created"`
}

// 渲染模式取值（迁移 282）。真值在 presentationmodel.RenderMode*；这里为跨模块消费者
// 提供一份可 import 的镜像（模块间只允许依赖 contract 与不可变 DTO，不能 import 对方 model），
// 一致性由 public/test/presentation/unit 的用例钉住。
const (
	// RenderModeTemplate 跟随绑定模板（默认）：模板更新可全局下发。
	RenderModeTemplate = "template"
	// RenderModeDocument 该商品独立文档（override_document）：模板更新不影响它。
	RenderModeDocument = "document"
)

// UpdateURLReq 修改已发布实例的线上路径（改 URL）。
//
// 实例定位二选一：ID，或 EntityType + EntityID（后台页面通常只持有实体，
// 拿不到实例 id）。NewPath 为空或定位信息不全属于参数错误。

// CountByTemplateResp 影响面：template 模式的实例会被模板更新波及，document 模式不会。
type CountByTemplateResp struct {
	TemplateMode int64 `json:"templateMode"`
	DocumentMode int64 `json:"documentMode"`
	Total        int64 `json:"total"`
}

// SnapshotSummary 历史快照投影（只给选择回滚目标需要的字段）。
type SnapshotSummary struct {
	ID                      string `json:"id"`
	SourceTemplateVersionID string `json:"sourceTemplateVersionId"`
	CreatedAt               string `json:"createdAt"`
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
