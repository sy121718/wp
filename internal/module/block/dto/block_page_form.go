package blockdto

// 页面表单只声明普通控件能表达的字段；文档内容走复杂编辑器的明确 JSON 输入。
// 字段名与 blocks.html / 工作台预设对话框的控件一一对应。

type CreateForm struct {
	ProjectID     string `form:"projectId" binding:"required"`
	Name          string `form:"name" binding:"required,max=100"`
	Kind          string `form:"kind"`
	Category      string `form:"category"`
	ReuseMode     string `form:"reuseMode"`
	PresetKey     string `form:"presetKey" binding:"required_with=PresetVersion,max=100"`
	PresetVersion string `form:"presetVersion" binding:"required_with=PresetKey"`
}

type DeleteForm struct {
	ID string `form:"id" binding:"required"`
}

type BulkDeleteForm struct {
	IDs []string `form:"ids"`
}
