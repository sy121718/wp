package pageenums

const (
	DetailDocEmpty = "admin.page.detail.documentEmpty"

	DetailDocSettingsInvalid = "admin.page.detail.settingsInvalid"

	DetailDocNodeDepthExceed = "admin.page.detail.nodeDepthExceed"

	DetailDocNodeInvalid = "admin.page.detail.nodeInvalid"

	DetailDocStructureInvalid = "admin.page.detail.structureInvalid"
)

var DetailKeys = []string{
	DetailDocEmpty,
	DetailDocSettingsInvalid,
	DetailDocNodeDepthExceed,
	DetailDocNodeInvalid,
	DetailDocStructureInvalid,
}

var ErrDetailFallbacks = map[string]string{
	DetailDocEmpty:            "页面文档为空，没有可保存的内容",
	DetailDocSettingsInvalid:  "页面设置不合法",
	DetailDocNodeDepthExceed:  "顶级节点 {index} 的组件树深度 {depth} 超过上限 {max}（嵌套失控，请简化结构）",
	DetailDocNodeInvalid:      "顶级节点 {index} 的配置不合法",
	DetailDocStructureInvalid: "页面文档结构不合法",
}
