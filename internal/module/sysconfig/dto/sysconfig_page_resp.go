package sysconfigdto

// sysconfig_page_resp.go — 系统设置页的渲染视图（页面 GET 的回执形状）。
//
// **导出字段名就是模板契约**：internal/templates/admin/system/settings.html 里的
// `.Field` 必须在这里有同名导出字段（Jet 缺键 = 整页中断）。
//
// 各分组的选项列表用 []any：它们来自不同的契约（语言 / 货币 / 国家 / 模式），
// 模板只读 .Code / .Label 这类既有字段 —— 由视图声明"这一屏要什么"，
// 而不是把四个契约类型全 import 进来（那会让 dto 依赖一堆别人的包）。
type SystemSettingsPageResp struct {
	Title string

	// I18N / Trade 两个设置分组（分组的内部形状由各自服务给出，模板按 .Data 读）。
	I18N  any
	Trade any

	LangOptions     []any
	CurrencyOptions []any
	CountryOptions  []any
	ModeOptions     []any

	// LoadErr 读取失败时的归口提示（空串 = 无错误）。
	LoadErr string
}
