package artifactdto

import (
	json "go_wp/pkg/jsonx"
)

// RecordReq 归档一条已落盘产物的元数据；闭包对象由服务端从 manifest 提取。
//
// Lang 是产物行唯一键 (pageId, version, lang) 的第三维（多语言 P3）：
// 空 = 全局默认语言（sys_config 的 i18n 组 default_lang），service 落库前统一归一化，绝不留空。
type RecordReq struct {
	ArtifactID       string          `json:"artifactId" binding:"required"`
	PageID           string          `json:"pageId" binding:"required"`
	Version          int64           `json:"version" binding:"required,min=1"`
	Lang             string          `json:"lang"`
	SourceDocument   json.RawMessage `json:"sourceDocument" binding:"required"`
	SchemaVersion    int             `json:"schemaVersion" binding:"required,min=1"`
	SourceHash       string          `json:"sourceHash" binding:"required"`
	BuildInputHash   string          `json:"buildInputHash" binding:"required"`
	ArtifactProvider string          `json:"artifactProvider" binding:"required"`
	ArtifactKey      string          `json:"artifactKey" binding:"required"`
	ArtifactHash     string          `json:"artifactHash" binding:"required"`
	CompilerVersion  string          `json:"compilerVersion" binding:"required"`
	RegistryVersion  string          `json:"registryVersion" binding:"required"`
	Manifest         json.RawMessage `json:"manifest" binding:"required"`
	CreatedBy        string          `json:"createdBy"`
}

// DetailReq 按 pageId + hash 查询产物。
// 不带 lang：产物 hash 覆盖 Manifest（含 lang），同 hash 必同语言。
type DetailReq struct {
	PageID string `form:"pageId" json:"pageId" binding:"required"`
	Hash   string `form:"hash" json:"hash" binding:"required"`
}

// DetailByIDReq 按产物行 ID 查询。
type DetailByIDReq struct {
	ID string `form:"id" json:"id" binding:"required"`
}

// ContentObjectGCReq 共享内容对象（content_objects）孤儿回收请求。
//
// 与产物 GC 同形：DryRun 默认 true，真删必须显式传 false；保留窗口同样按天给。
// Limit 是本批处理上限（标记清除分批做，避免一次干掉几十万行造成长事务）。
type ContentObjectGCReq struct {
	// RetentionDays 保留窗口（天）：早于 now-retentionDays 创建的对象才可能被回收。
	// 留空或 <=0 按默认 30 天。
	RetentionDays int `json:"retentionDays"`
	// DryRun 只统计与列出候选、不实际删除。留空视为 true（安全默认）。
	DryRun *bool `json:"dryRun"`
	// Limit 本批处理行数上限，留空按默认批次。
	Limit int `json:"limit"`
}
