package mediadto

// media_page_resp.go — 媒体库页面的渲染视图。
//
// 这一屏的数据与交互全部由 media-admin.js 驱动（复用 /api/media/*），页面几乎只是
// 一具壳 —— 但它仍是**页面 GET 的出口**，所以视图形状要与其它页同形（强类型 + PageSlot），
// 否则「每一屏长什么样」在不同页面之间没有可比性，而那种不一致只会在改动时暴露。
//
// **导出字段名就是模板契约**：internal/templates/admin/media/media.html 里的 `.Field`
// 必须在这里有同名导出字段。
type MediaPageResp struct {
	// Title / Menu 是 layout 的小写壳键（{{.title}} / {{.menu}}）的值。
	// Title 放 enums 的 i18n key，翻译由 adminui.Prepare 做。
	Title string
	Menu  string
	// JSVer 媒体库脚本缓存版本（media-lib.js / media-admin.js 中较新的 mtime）。
	JSVer string
}
