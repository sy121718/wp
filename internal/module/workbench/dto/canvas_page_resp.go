package workbenchdto

// canvas_page_resp.go — 工作台画布整页（workbench/layout）的渲染视图。
//
// 这一屏的模板键**全是小写**（`{{.title}}` / `{{.jsVer}}` / `{{.isBlock}}` …），
// 与后台其它页的大小写风格不同 —— 它是编辑器壳，模板由自己的工作台 JS 消费。
// 所以这里每个字段都带 `view:"..."` 标签：导出字段名供 Go 侧读，标签供模板读。
//
// 为什么值得强类型：这四种画布模式（页面 / 全局块 / 内容模板 / 实例）各自拼一份
// gin.H，键名靠人肉对齐。少一个键的表现是 Jet **整页中断**（HTTP 200 + 半截 HTML），
// 而四种模式里只有一种被踩到时，另外三种看起来完全正常。
//
// **不是所有字段都必填**：模板用 `{{if .isTemplate}}` 这类判空分流，
// 空串/零值就是"这个模式没有这一项"。所以零值可用是硬要求。

// CanvasA = 自动化实体（画布只读元数据 + 节点图），CanvasD = 运行详情。
// 这两个单字母键是模板里既有的名字（`{{.A}}` / `{{.D}}`），保留原样。

// CanvasPageResp 画布整页的渲染视图（四种模式共用）。
type CanvasPageResp struct {
	Title      string `view:"title"`
	PageID     string `view:"pageId"`
	IsBlock    bool   `view:"isBlock"`
	IsTemplate bool   `view:"isTemplate"`
	// IsStructureTemplate 结构模板（页眉/页脚）：预览不解析字段绑定，
	// 这件事要在编辑器里看得见（否则作者会以为「页眉里该出现的商品名没出现」是渲染坏了）。
	IsStructureTemplate bool   `view:"isStructureTemplate"`
	DraftPath           string `view:"draftPath"`
	Version             int64  `view:"version"`
	// Document / Meta / Schemas 是**已序列化的 JSON 文本**（utils.JsonSafe 包裹）：
	// 模板把它们写进 <script type="application/json">，由前端解析。
	Document string `view:"document"`
	Meta     string `view:"meta"`
	Schemas  string `view:"schemas"`
	// JSVer 静态脚本缓存版本（模板拼 ?v={{.jsVer}}）。
	JSVer string `view:"jsVer"`
	// PreviewQS 模板预览查询串（模板画布用；其它模式为空）。
	PreviewQS string `view:"previewQS"`
}
