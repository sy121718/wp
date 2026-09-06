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
