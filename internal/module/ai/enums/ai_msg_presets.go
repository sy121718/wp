package aienums

const (
	MsgModelsAppended = "ai.msg.modelsAppended"

	ErrNoModelSelected = "ai.err.noModelSelected"
)

var presetFacingMessages = map[string]string{
	MsgModelsAppended: "已添加 {n} 个模型。",

	ErrNoModelSelected: "请先勾选要添加的模型。",
}

func init() {
	for k, v := range presetFacingMessages {
		FacingMessages[k] = v
	}
}
