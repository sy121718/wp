package aienums

// ai_msg_presets.go — 「第三方模型提供商（预设）」与候选弹窗的响应文案 key。
//
// 与 ai_msg.go / ai_session_msg.go 同一口径：service 不硬编码文案，页面侧经 FacingMessages
// 的白名单出口显示。key 单独登记在一个文件里，便于对照本批用例清单（517 迁移 seed）。
const (
	MsgModelsAppended = "ai.msg.modelsAppended"

	ErrNoModelSelected = "ai.err.noModelSelected"
)

// presetFacingMessages 本批中文兜底，由 init 追加进 FacingMessages。
//
// 追加而不是另立一张表：inbound 层只认 FacingText 一个入口，
// 多一张表就多一个「有人只查了一张」的机会。
var presetFacingMessages = map[string]string{
	MsgModelsAppended: "已添加 {n} 个模型。",

	ErrNoModelSelected: "请先勾选要添加的模型。",
}

func init() {
	for k, v := range presetFacingMessages {
		FacingMessages[k] = v
	}
}
