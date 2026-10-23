package workbenchdto

// inspector_fragment_resp.go — 工作台**片段出口**的渲染视图（htmx / fetch 局部替换）。
//
// 与整页 layout 视图分开：片段的键集只有几个槽位，整页是一屏编辑器状态。
//
// **导出字段名就是模板契约**：internal/templates/fragments/*.html 的 `.Field`。
// 这些片段的取词函数由 adminui.PrepareBare 注入 —— 漏注入时 Jet **静默**输出空串
//（不报错、状态码 200、日志干净），所以空态分支与正常分支走同一个入口，不各写一份。

// InspectorPanelResp 检查器面板片段（fragments/inspector_panel）。
type InspectorPanelResp struct {
	// NodeID 空串 = 空态（没选中任何节点）。
	NodeID   string
	NodeType string
	// Sections 分组数据。**注意它的正文仍是 service 拼串产出的 HTML**
	//（Sections[].HTML / Title 等），那部分的取词留在 service ——
	// 它没有模板槽位可放（见 workbench/service 文件头）。
	Sections any
}

// OutlineTreeResp 结构树片段（fragments/outline_tree）。
//
// 只有一个 HTML 键：树 HTML 整块由 workbenchservice.RenderOutlineHTML 拼串产出
// （取词在 service 侧完成），模板只有一句 {{ .HTML | unsafe }}。
//
// 这个片段今天是**片段出口里最薄的一个** —— 边界测试（workbench_page_error_test.go）
// 里的「片段 / 预览端点不受影响」那条判据就是拿它当样本的。
type OutlineTreeResp struct {
	HTML string
}
