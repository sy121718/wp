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

	// 以下三项只有**实例模式**给（layout.html 据 isset(.instanceMode) 渲染双轨状态条）：
	// 实例编辑器改的是「这一条实体对模板的覆盖」，与页面 / 块 / 模板三种模式的状态条不同。
	InstanceMode    bool   `view:"instanceMode"`
	RenderMode      string `view:"renderMode"`
	RenderModeLabel string `view:"renderModeLabel"`
}

// DashboardPageResp 后台概览页（/admin/dashboard）的视图。
//
// 三个装配档共用一张模板：依赖缺失 / 数据源读不到 / 正常。三档的差别只在
// 「有没有 Err」与「有没有 Overview」，键集本身恒定。
type DashboardPageResp struct {
	// Title 是 i18n **key**（adminui.Prepare → injectI18n 按当前语言翻译它）。
	Title string `view:"title"`
	Menu  string

	// Err 概览数据读不到的**归口文案**（不是驱动原文）。
	//
	// 从前这里塞的是 err.Error()：模板直接渲染 {{.Err}}，页面上就是
	// `pq: relation "projects" does not exist`。概览是登录后的第一跳，
	// 原文漏在这里等于把库结构写给每个登录用户看。
	Err string

	// Overview 概览数据（依赖缺失与读不到时为零值：模板据此退回空态）。
	//
	// 类型是 any 而不是具体结构：概览快照（withPresets 的结果）是 inbound/http 层的
	// 纯装配中间态，不为它造一份 dto 镜像 —— 它是这一页专属的、一屏即用即弃。
	Overview any
}
