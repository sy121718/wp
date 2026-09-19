package plugindto

import "encoding/json"

// PluginResp 插件列表/详情行。
type PluginResp struct {
	ID             string          `json:"id"`
	Name           string          `json:"name"`
	Version        string          `json:"version"`
	SchemaVersion  int             `json:"schemaVersion"`
	Enabled        bool            `json:"enabled"`
	ComponentCount int             `json:"componentCount"`
	InstalledAt    string          `json:"installedAt"`
	UpdatedAt      string          `json:"updatedAt"`
	Manifest       json.RawMessage `json:"manifest,omitempty"`
}

// SchemaInfo 一个插件 L1 schema 的巡检条目。
type SchemaInfo struct {
	Name       string `json:"name"`
	TableCount int    `json:"tableCount"`
}

// PatrolResp 插件「三处产物」与注册表的对账结果（只读，见 docs/06 §8.2）。
//
// 一次卸载要清三处互相独立的存储：L1 schema（PG）、注册行（PG）、存储目录（文件系统）。
// 后者的清理不参与事务，所以每一步都可能单独失败。四类不一致分开表达，
// 因为**处置方式完全不同** —— 合成一个「异常数」会让人不知道该做什么：
//
//	· OrphanSchemas —— schema 还在、注册行没了。人工确认表里有没有要留的数据，再决定 DROP；
//	· MissingSchemas —— 注册行说 schema_version > 0 但 schema 不在。装配在建表/查表时炸，
//	  处置是重装插件（Install 会先清旧 schema 再跑迁移）；
//	· OrphanStorage —— 目录还在、注册行没了（卸载时 RemoveAll 未成功）。确认资产是否还要，
//	  再决定删目录；
//	· MissingStorage —— 注册行在、目录不在。装配会**静默跳过**该插件（组件库里少一组组件
//	  而没有任何提示），处置是重装插件。
type PatrolResp struct {
	OrphanSchemas  []SchemaInfo `json:"orphanSchemas"`
	MissingSchemas []string     `json:"missingSchemas"`
	OrphanStorage  []string     `json:"orphanStorage"`
	MissingStorage []string     `json:"missingStorage"`
	// StorageRoot 存储根路径（OrphanStorage 里的名字要拼上它才是可操作的路径）。
	// 报路径而不是只报 ID：让人能直接把 DROP / rm 的对象定位到磁盘位置。
	StorageRoot string `json:"storageRoot"`
}

// ComponentSummary 工作台组件库注入的插件组件摘要。
type ComponentSummary struct {
	Type  string         `json:"type"`
	Label string         `json:"label"`
	Hint  string         `json:"hint"`
	Props map[string]any `json:"props"`
}

// PresetSummary 工作台区块预设摘要（docs/06 §5.2）。
// 预设不是单个组件，而是一段预组合 AST 片段：Document 为 Node 数组 JSON，
// 前端插入时深拷贝 + 递归重写节点 ID 后顶级平铺进文档根。
type PresetSummary struct {
	ID        string          `json:"id"`
	Label     string          `json:"label"`
	Category  string          `json:"category,omitempty"`
	Thumbnail string          `json:"thumbnail,omitempty"`
	Document  json.RawMessage `json:"document"`
}
