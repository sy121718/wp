package projectdto

import json "go_wp/pkg/jsonx"

// SettingsPanelReq 是编辑器「设置」面板的显式 JSON 输入。
//
// 面板交的是整份编辑器文档，属于复杂编辑器输入：按 AGENTS.md 用明确 JSON body，
// 不把它 stringify 后塞进普通表单字段。输出仍是 HTML 片段，输入与输出协议解耦。
type SettingsPanelReq struct {
	Document       json.RawMessage `json:"document"`
	AccessPassword string          `json:"accessPassword"`
}

// SeoScorePanelReq 是评分面板的显式 JSON 输入：同一份文档 + 当前草稿地址。
type SeoScorePanelReq struct {
	Document json.RawMessage `json:"document"`
	URL      string          `json:"url"`
}

// GlobalPanelReq 是全局主题面板的显式 JSON 输入：主题标识 + 该主题的设置对象。
type GlobalPanelReq struct {
	ThemeID  string          `json:"themeId"`
	Settings json.RawMessage `json:"settings"`
}
