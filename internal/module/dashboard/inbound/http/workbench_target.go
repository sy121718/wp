package dashboardhttp

import "sort"

// workbench_target.go — 工作台编辑目标描述符（审计 EDT-017）。
//
// 工作台此前靠 meta.saveBase 字符串分派（page / block / template 各写一段 if）：
// 接入一种新文档类型要在保存、预览、校验、历史四处各加一个分支，而这些分派散在
// 前端 JS 里 —— 漏改一处不会编译失败，只会在用户点保存时表现为「什么都没发生」。
//
// 描述符把「这个目标有哪些端点、支持哪些动作、保存体长什么样」变成**数据**：
// 前端只按描述符走，新增目标类型 = 在这里注册一条，前端零改动。

// 编辑目标类型。
const (
	EditTargetPage     = "page"
	EditTargetBlock    = "block"
	EditTargetTemplate = "template"
)

// TargetEndpoint 目标的一个端点（method 省略即 POST：项目的写操作一律 POST）。
type TargetEndpoint struct {
	Path string `json:"path"`
}

// TargetSaveBody 保存请求体的构造方式。
//
// 三种目标的键名各不相同（page 用 draftDocument + expectedVersion + draftPath，
// block 用 document 且额外带 name，template 用 draftDocument 无版本），
// 键名放数据里，前端就不必认识任何一种目标。
type TargetSaveBody struct {
	// IDKey 目标 id 的键名。
	IDKey string `json:"idKey"`
	// DocumentKey 文档的键名。
	DocumentKey string `json:"documentKey"`
	// VersionKey 乐观锁版本键（空 = 该目标不用版本）。
	VersionKey string `json:"versionKey,omitempty"`
	// PathKey 路径键（空 = 该目标没有 URL 概念）。
	PathKey string `json:"pathKey,omitempty"`
	// Extras 附加字段：请求体键 → meta 里的来源键（如 block 的 name ← blockName）。
	Extras map[string]string `json:"extras,omitempty"`
}

// TargetCapabilities 能力开关：前端据此显示或隐藏按钮。
//
// 没有这层的话，「块没有发布链」这种事只能靠前端记：按钮照常显示，点了报错，
// 或者更糟 —— 点了之后静默失败。
type TargetCapabilities struct {
	Publish  bool `json:"publish"`
	Build    bool `json:"build"`
	Rollback bool `json:"rollback"`
	History  bool `json:"history"`
	URL      bool `json:"url"`
}

// EditTarget 一种可编辑文档类型的完整描述。
type EditTarget struct {
	Type     string             `json:"type"`
	Save     TargetEndpoint     `json:"save"`
	SaveBody TargetSaveBody     `json:"saveBody"`
	Caps     TargetCapabilities `json:"caps"`
}

// workbenchTargets 目标注册表 —— **新增编辑目标只需要在这里加一条**。
var workbenchTargets = map[string]EditTarget{
	EditTargetPage: {
		Type: EditTargetPage,
		Save: TargetEndpoint{Path: "/api/page/draft/save"},
		SaveBody: TargetSaveBody{
			IDKey: "id", DocumentKey: "draftDocument",
			VersionKey: "expectedVersion", PathKey: "draftPath",
		},
		// 手工页面是唯一有完整发布链的目标（构建 / 发布 / 回滚 / 历史 / 改 URL）。
		Caps: TargetCapabilities{Publish: true, Build: true, Rollback: true, History: true, URL: true},
	},
	EditTargetBlock: {
		Type: EditTargetBlock,
		Save: TargetEndpoint{Path: "/admin/blocks/save-content"},
		SaveBody: TargetSaveBody{
			IDKey: "id", DocumentKey: "document",
			Extras: map[string]string{"name": "blockName"},
		},
		// 全局块没有 URL、没有独立产物：它是被引用展开进别人的产物的。
		Caps: TargetCapabilities{},
	},
	EditTargetTemplate: {
		Type:     EditTargetTemplate,
		Save:     TargetEndpoint{Path: "/api/contenttemplate/update"},
		SaveBody: TargetSaveBody{IDKey: "id", DocumentKey: "draftDocument"},
		// 模板保存即产生新版本（无独立发布动作），也没有自己的 URL。
		Caps: TargetCapabilities{},
	},
}

// workbenchTargetOf 取目标描述符；类型未注册时 panic。
//
// panic 而不是返回空描述符：三个注入点写的都是常量类型，未注册只可能是
// 「加了新目标常量却忘了在注册表里加一条」—— 这种错误要在首次渲染就炸出来，
// 而不是把一个空描述符发给前端（前端会静默退回默认行为，问题看不见）。
func workbenchTargetOf(targetType string) EditTarget {
	target, ok := EditTargetFor(targetType)
	if !ok {
		panic("工作台编辑目标未注册: " + targetType)
	}
	return target
}

// EditTargetFor 按类型取描述符；未知类型返回 false（调用方据此明确报错，不静默降级）。
func EditTargetFor(targetType string) (EditTarget, bool) {
	t, ok := workbenchTargets[targetType]
	return t, ok
}

// WorkbenchTargets 返回全部目标的确定性列表（测试与前端契约共用）。
func WorkbenchTargets() []EditTarget {
	types := make([]string, 0, len(workbenchTargets))
	for t := range workbenchTargets {
		types = append(types, t)
	}
	sort.Strings(types)
	out := make([]EditTarget, 0, len(types))
	for _, t := range types {
		out = append(out, workbenchTargets[t])
	}
	return out
}
