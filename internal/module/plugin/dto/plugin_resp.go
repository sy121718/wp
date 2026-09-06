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
