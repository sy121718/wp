package contenttemplatedto

// contenttemplate_page_resp.go — 内容模板列表整页的渲染视图（页面 GET 的回执形状）。
//
// 与 contenttemplate_resp.go 的分工：那一份是**接口契约**（JSON API 的响应，逐字段原始值）；
// 这一份是**页面视图**（已取好词、已算好展示文本，模板直接读）。理由与 product /
// masterdata 的两份一样：读一个类型不必先判断它是进还是出。
//
// **导出字段名就是模板契约**：internal/templates/admin/contenttemplate/
// content_templates.html 里的 `.Field` 必须在这里有同名导出字段（模板键名清单见文件末尾）。
// 改名要同步改模板，`content_template_page_render_test.go` 用反射 + 模板源码双向对账。
//
// 为什么从 gin.H 换成强类型：这个页面用 gin.H 拼了外层 + 行，键名拼错的表现是
// 「HTTP 200 + 之后整块 HTML 消失」（Jet 对缺键不报错，只是不再往下渲染），
// 而编译器对 map 键一无所知。换成结构体后改名即编译错误。

import (
	enums "go_wp/internal/module/contenttemplate/enums"
	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/pkg/utils"
)

// TemplatePageRefResp 引用某模板的一处**页面**（删前决策依据：是哪几张页面）。
type TemplatePageRefResp struct {
	Label string
	Path  string
	Slots string
	URL   string
}

// TemplateInstanceRefResp 引用某模板的一处**发布实例**。
type TemplateInstanceRefResp struct {
	Label  string
	Entity string
	Path   string
	Slots  string
}

// TemplateListRowResp 列表里的一行模板。
//
// 结构模板（页眉/页脚）与内容实体模板在这张表里是两类东西：前者没有实体来源、也不接受
// 字段绑定，两者的可编辑性与删除后果都不同 —— 页面上必须一眼分得开（IsStructure/Type）。
type TemplateListRowResp struct {
	ID           string
	Name         string
	EntityType   string
	DraftVersion int64
	// UpdatedAt 用 utils.JSONTime（与接口契约同型）：模板按自己的格式化函数渲染。
	UpdatedAt utils.JSONTime

	// IsStructure 结构模板（页眉/页脚）。
	IsStructure bool
	// Type / Role 是**词条事实**（key + 中文兜底），由模板用 tr(key, fallback)
	// 取成当前语言文本 —— 不在 dto 里存成品句。
	//
	// 为什么不在这里存译文：dto 带成品文案会被 internal/architecture 的
	// 「dto 不带展示字段」门禁判为违规（Type 就在那张名单里），而那条判据是对的 ——
	// 成品句一旦进 dto，换语言就要重算一遍整份视图，且「谁取的词」变得不可追。
	// 商品的页面视图也是这个形态：只放 key / 原始值，取词交给模板。
	Type      enums.LabelPair
	Role      enums.LabelPair
	IsDefault bool

	// EditURL 进入工作台编辑的地址；SampleErr 样例实体取不到时的归口提示（**模板数据**，
	// 依赖错误只能出归口文案，原文进日志）。
	EditURL   string
	SampleErr string

	// 引用明细（页面 / 实例）：删除与切换生效前必须看得见的东西，所以给「是哪几张页面、
	// 哪个实例」而不是一个计数 —— 计数只够做提示，不够做决策。
	RefPages         []TemplatePageRefResp
	RefInstances     []TemplateInstanceRefResp
	RefCount         int
	RefPageCount     int
	RefInstanceCount int
}

// ContentTemplatesPageResp 内容模板列表整页的渲染视图。
type ContentTemplatesPageResp struct {
	// Title / Menu 是 layout 的小写壳键（{{.title}} / {{.menu}}）的值。
	// Title 放 enums 的 i18n key，翻译由 adminui.Prepare 做。
	Title string
	Menu  string

	// 工程上下文（失败页也带着它：页壳与返回链接不因取数失败缺一块）。
	Projects        []projectcontract.ProjectResp
	SelectedProject string
	// EntityType 当前筛选的实体类型（空 = 全部）。
	EntityType string

	// Err 列表取数失败时的归口提示（空串 = 无错误）。
	Err string
	// Ready 模板写入能力是否装配（false = 未注入端口，页面显示「能力未装配」而非「没有模板」）。
	Ready bool

	Templates     []TemplateListRowResp
	TemplateCount int

	// ImpactAvailable / ImpactNote 影响面能力的三种状态各说各话：
	// 可用 / 未装配 / 有文档解析不了（影响面可能不完整）。
	// 未装配与「没有引用」必须长得不一样，否则运营会拿一个空白引用列表当证据去删模板。
	ImpactAvailable bool
	ImpactNote      string
}

