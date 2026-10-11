// product_view.go — 渲染视图与表单回显形状（dto 里的第三类：既不是入参也不是出参，
// 但同样是**模块内跨层的纯数据形状**）。
//
// 为什么放在 dto 而不是 handle：这些类型是模板按字段读的形状（`{{.Score.OK}}`、`{{r.Label}}`）
// 与表单取值回显的形状，与 handle / 模板 / 测试三处共同对齐，改形状要同时改三处。
// 留在 handle 里时任何想复用或断言它的地方都得 import 控制器包（`inbound/http`），
// 而那个包带着全部 handler；放进 dto 后它与 `XxxReq` / `XxxResp` 同级 —— 模块内的纯数据
// 形状集中一处，谁都不必认识控制器。
//
// 三类分工（本文件是第二类）：
//   · `XxxReq` / `XxxResp` —— 接口与 service 的入参 / 出参；
//   · **本文件** —— 页面与片段的渲染视图、表单回显形状（只有字段，没有方法）；
//   · 留在 handle —— 带装配方法的聚合（`productTranslationsData.templateMap()`，产出 gin.H）、
//     解析与装配选项（`attrGroupFormOpts` / `attrValuesFormOpts` / `formEcho`）、
//     以及 HTTP JSON 响应行（`variantPreviewRow`：dto 行 + 展示用规格串）。
//
// **字段名是模板契约**：改名等于改模板（struct 上取不到字段会让渲染中断，见
// internal/templates/CLAUDE.md）。

package productdto

import (
	projectdto "go_wp/internal/module/project/dto"
	"go_wp/pkg/facing"
)

// ==========================================================================
// 商品后台页的读模型（一屏一个读模型，取数编排与视图装配在 service）。
//
// 这一组形状与 handle 的对应关系：service 的读模型方法产出它，控制器只把它交给
// adminui.RenderPage —— 控制器里不再有 gin.H 装配、取数助手与「每个失败点各写一遍
// 失败页」的编排（internal/module/CLAUDE.md §inbound/http）。
//
// 视图同时承担两件事：
//   · **模板契约** —— 字段名就是模板读的键（`{{.Projects}}` / `{{.LoadError}}`）；
//   · **渲染结论** —— 实现 adminui.PageResult（`PageStatus` / `PageNoStore`），
//     状态码与 no-store 由视图带出来，控制器不再自己判档。
//
// 这两个方法是本文件唯一的例外（其余类型只有字段）：档位是「这一屏的结论」，
// 属于视图而不是控制器，理由见 internal/adminui/render.go 的文件头。
// ==========================================================================

// ProductNewPageReq 商品新建整页的取数入参。
type ProductNewPageReq struct {
	// ProjectID 查询串里的工程 id（控制器只做去空白）；空串 = 取工程清单的第一个。
	ProjectID string
}

// ProductNewPageView 商品新建整页的渲染视图。
type ProductNewPageView struct {
	// Title / Menu 是 layout 的小写壳键（`{{.title}}` / `{{.menu}}`）。
	// Title 放 enums 的 i18n key，翻译由 adminui.Prepare 做（放 key 而不是译文，
	// 是因为译文按请求语言取）。
	Title string `view:"title"`
	Menu  string `view:"menu"`

	// Projects 工程清单与本次选中的工程：整页的工程上下文（失败页也带着它，
	// 模板的失败分支不读，但页壳与返回链接的上下文不因此缺一块）。
	// 形状与工程契约的 List 返回值一致（不为了「视图里用指针」多转一趟）。
	Projects        []projectdto.ProjectResp
	SelectedProject string

	// —— 建表单片段要的三份选项（事实 + 词条 key，取词在渲染期）——
	WarehouseOptions    []WarehouseOptionView
	AttributeOptions    []AttributeOptionView
	WarehouseSKUOptions []WarehouseSKUGroupView

	// —— 右侧模板卡片区 ——
	// TemplatesAvail 为假 = 模板能力未装配（未注入端口）；为真而 Templates 为空 =
	// 「读到了、没有模板」。两档在页面上是两句不同的提示，不能合并。
	Templates         []EntityTemplateOptionView
	DefaultTemplateID string
	TemplatesAvail    bool

	// LoadFailed 本次请求**没读到**（不是「读到了、只是空的」）；LoadError 是归口后的
	// 当前语言文字，非空即这一页失败了。失败优先于空态（docs/rules/template-boundary.md）。
	LoadFailed bool
	LoadError  string

	// HTTPStatus 这一屏的 HTTP 档位：200 读到 / 400 参数不合法 / 500 主数据没读到。
	// 导出是因为写它的 service 在另一个包（dto 的写者只有 service 的读模型）。
	// 它会被 adminui.ViewMap 摊平成一个模板键，但没有任何模板读它 —— 摊平的键
	// 多一个不影响渲染，页面上也不会出现它。
	HTTPStatus int
}

// PageStatus 这一屏的 HTTP 档位（adminui.PageResult）；零值 = 200。
func (v ProductNewPageView) PageStatus() int { return v.HTTPStatus }

// PageNoStore 失败页不该被缓存：重试必须真的回到服务端（否则拿到的是缓存的失败页）。
func (v ProductNewPageView) PageNoStore() bool { return v.LoadFailed }

// WarehouseOptionView 仓库下拉 / 勾选项的渲染视图。
//
// Label 只拼**数据**（仓名 + 编码）；「默认仓」后缀是词条，用 DefaultSuffixKey +
// DefaultSuffixFallback 交给模板取词 —— 控制器提前翻好就等于把这一句锁死在一种语言
// （英文界面上留中文，不报错、门禁也不红）。
//
// DefaultSuffixKey 的词条真源在库存模块（enums 与词条都只有一条）：两个页面显示
// 同一个后缀，改词条要两处同步。
type WarehouseOptionView struct {
	ID        string
	Code      string
	Name      string
	Label     string
	IsDefault bool

	DefaultSuffixKey      string
	DefaultSuffixFallback string
}

// WarehouseSKUItemView 「从仓库选」的一个候选货。
type WarehouseSKUItemView struct {
	// SKUCode 一律是**裸码**（仓库侧的名字，不带仓码前缀）：前端把它填进「SKU 编码」框时，
	// 服务端按 (仓, 裸码) 复核并复用那一行 —— 带上前缀会被当成一条新货去建行。
	SKUCode string
	// ExternalSKU 该仓登记的对方编码（可能为空）。
	ExternalSKU string
	// Label 候选的显示串（我们的 SKU，对方编码跟在后面 —— 运营手上可能是其中任意一个）。
	Label string
}

// WarehouseSKUGroupView 一个仓的候选货集合（模板按仓分组渲染 optgroup / datalist）。
type WarehouseSKUGroupView struct {
	WarehouseID string
	// WarehouseCode 仓短码：前端预览「将要生成的主体 SKU」用（服务端才是真源）。
	WarehouseCode string
	Label         string
	IsDefault     bool
	Items         []WarehouseSKUItemView
}

// AttributeOptionView 属性组勾选列表的一项。
type AttributeOptionView struct {
	ID string
	// Label 名字 + 标识 key：商品详情页与属性页都按 key 指代属性组，
	// 只给名字会在同名属性组之间选错。
	Label       string
	IsVariation bool
}

// EntityTemplateOptionView 商品详情模板候选的一项（卡片区只展示名字与「默认」徽标）。
type EntityTemplateOptionView struct {
	ID   string
	Name string
}

// AttributeCheckOptionView 带勾选态的属性组项（商品编辑页的勾选列表）。
//
// 与 AttributeOptionView 是**两个形状**而不是给它加一个 Checked 字段：勾选态是
// 「这一屏 + 这一行数据」的结论（新建页没有它），塞进公共项会让新页的视图里多出一个
// 恒为假的字段，读者要在两处之间确认它到底谁在写。
//
// 内嵌而不是复制字段：模板里 `{{a.ID}}` / `{{a.Label}}` / `{{if a.Checked}}` 的写法不变，
// 「标签口径」仍然只有 AttributeOptionView 一处。Jet 走 reflect.FieldByName，**提升字段
// 照常可读**（已实测；端到端判据是 public/test/product/feature/product_edit_page_test.go
// 的 TestProductEditPageRejectsInvalidNumbersWithoutPartialWrite，它断言渲染结果里出现
// `value="<属性组 id>" checked`）—— 若哪天这条断了，那条用例当场变红。
type AttributeCheckOptionView struct {
	AttributeOptionView
	Checked bool
}

// ScoreView SEO 评分视图（服务端渲染，客户端只处理「点击建议跳转」）。
type ScoreView struct {
	OK       bool
	Total    int
	Grade    string
	Sections []ScoreSectionView
	// ProfileType / ProfileReason 本次评分所用的页型与调权理由（审计 SEO-016）。
	// 文档要求调权在结果里回显（docs/02-E1 §5）：不显示的话，编辑者看到同一份内容
	// 在商品页比文章页高几分时无从解释。空 = 用默认权重。
	ProfileType   string
	ProfileReason string
	// Duplicates 与本页标题重复的其它页面（审计 SEO-018 编辑期轻量版）。
	// **列出冲突页面本身**而不是只报数量：只报「有重复」运营不知道该去改哪一页。
	Duplicates    []string
	DuplicateNote string
	// Empty 空态文案（读不到实体时给一句可读的话，而不是让面板整体不可用）。
	Empty     string
	SerpTitle string
	SerpURL   string
	SerpDesc  string
}

// ScoreSectionView 单个评分维度。
type ScoreSectionView struct {
	Label      string
	Color      string
	ColorLabel string
	Score      int
	Max        int
	Issues     []ScoreIssueView
}

// ScoreIssueView 单条未达标检查（Target 非空表示可点击定位）。
type ScoreIssueView struct {
	Text   string
	Target string
}

// TranslationLangOption 工作台语言下拉项。
type TranslationLangOption struct {
	Code   string
	Label  string
	Active bool
}

// ProductTranslationRow 工作台一行（一个可翻译取值）。
type ProductTranslationRow struct {
	Context    string
	Field      string
	FieldLabel string
	Source     string
	SourceHash string
	Target     string
	Engine     string
	Translated bool
	// EngineTone 译文来源的徽标分档（i18n.ContentEngineTone）：模板据此选 class，
	// 不在模板里写 engine == "ai"（字符串字面量只该出现在常量定义处）。
	EngineTone string
	// Rich 富文本 / 多段文本字段（描述、属性值数组）：译文需与原文同形。
	Rich bool
}

// ProductTranslationGroup 按实体分组的行集合。
type ProductTranslationGroup struct {
	Key string
	// EntityType / EntityID 该组对应的实体（译文保存后据此精确标记自动发布实例）。
	EntityType string
	EntityID   string
	// Title 该组的**实体名**（商品名 / 分类名…）：它是数据，不取词。
	Title string
	// TitleKey / TitleFallback 分组标题里实体类型名的事实（「商品分类」「商品品牌」…）。
	// 页面用 `{{tr(g.TitleKey, g.TitleFallback)}} · {{g.Title}}` 拼 —— 实体类型是词条、
	// 实体名是数据，两样不能提前拼成一串（拼了就锁死在一种语言）。
	TitleKey      string
	TitleFallback string
	Subtitle      string
	Rows          []ProductTranslationRow
}

// ProjectTranslationOption 工程下拉项。
type ProjectTranslationOption struct {
	ID       string
	Name     string
	Selected bool
}

// TranslationRowError 译文工作台的一行错误：Label 是「哪一行」（语境名，可空 = 页面级），
// Msg 是结论事实。
//
// 不把 Label 拼进 Msg.Fallback：拼了就成成品串，模板再取词也救不回来
// （中文永远粘在英文词条前面）。也不把 Label 塞进 Msg.Args —— 那个位置留给词条自身的 `%s`。
type TranslationRowError struct {
	Label string
	Msg   facing.Msg
}

// AttrValueFormRow 留存失败回显所需的原始索引与控件值；写入解析仍保持原有的空行过滤。
type AttrValueFormRow struct {
	Index   int
	ID      string
	Key     string
	Label   string
	Sort    string
	Enabled bool
}

// BundleSourcePart 成员来源快照的**一段**（词条 + 可选的数据值）。
//
// 拆成段而不是在 Go 侧拼成一句话：那句话里的「从仓库选」「仓库 SKU」是词条，
// 拼好就等于把语言锁死在一种（英文界面上留中文）。段交给模板，分隔符「·」与取值
// 都在渲染期决定 —— 数据（SKU 编码）原样带上，它翻不了也不需要翻。
type BundleSourcePart struct {
	LabelKey      string
	LabelFallback string
	// Value 数据（仓库 SKU 编码 / 外部编码）；纯标签段留空 —— 空值段不渲染。
	Value string
}

// PricingForm 定价表单的取值（试算后要原样回填，用户接着点应用）。
type PricingForm struct {
	ProjectID  string
	RuleType   string
	Multiplier string
	Amount     string
	Margin     string
	Rounding   string
	Scope      string
	TargetID   string
	Status     string
	Keyword    string
	CategoryID string
	BrandID    string
	TagID      string
	Note       string
}
