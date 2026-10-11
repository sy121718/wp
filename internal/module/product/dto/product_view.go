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
	"net/http"

	projectdto "go_wp/internal/module/project/dto"
	"go_wp/pkg/facing"
	"go_wp/pkg/pagination"
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

// ProductListPageReq 商品列表整页的取数入参。
//
// Page 是**已归一**的页码：控制器用跨页共用的 productPageNumber 归一（`?page=abc` /
// `?page=-3` 已经退回 1），本方法只做下限保护。归一不在这里做，是为了不让同一份规则
// 在 service 与控制器各写一遍（那三个页面的页码归一与这里的必须同口径）。
type ProductListPageReq struct {
	// ProjectID 查询串里的工程 id；空串 = 取工程清单的第一个。
	ProjectID string
	Keyword   string
	Status    string
	Page      int
}

// ProductListPageView 商品列表整页的渲染视图（读模型产出 + 行装配原料 + 控制器补的派生值）。
//
// 它是这一屏**唯一的模板数据源**（`adminui.RenderPage(c, tpl, view)` 没有额外的 args 参数），
// 因此字段分三类，各自有明确的写者：
//   - **service 的读模型写**：Projects / SelectedProject / Filter* / Page / Limit / Total /
//     RowDetails / Categories / Brands / Tags / 三组选项 / Rules / Roundings（取数结果）；
//   - **控制器写**：Title / Menu（页壳键，title 的 i18n key 常量是 http 包的 MsgProductsTitle）、
//     Flat（分类树扁平化）/ Form（改价表单初值）/ Statuses（状态下拉）/ Products（成品行）/
//     Pagination / ListQuery —— 这些的**计算工具还被别的页面共用**（分类标签页 / 详情页回显 /
//     独立定价页），搬进 service 等于让那几页各留一份，理由见 service/product_list_page.go 文件头；
//   - **控制器在失败时写**：LoadError（按当前语言归口；LoadFailed / HTTPStatus 由读模型给）。
type ProductListPageView struct {
	// Title 放 enums 的 i18n key（取词在渲染期，理由同 ProductNewPageView）。
	Title string `view:"title"`
	Menu  string `view:"menu"`

	// Projects 工程清单与本次选中的工程：整页的工程上下文（失败页也带着它）。
	Projects        []projectdto.ProjectResp
	SelectedProject string

	// FilterKeyword / FilterStatus 回显；Statuses 是下拉选项；Page / Limit / Total 是分页
	// **事实**（基地址与翻页链接由 ListQuery + Pagination 承载，本层不拼 URL）。
	FilterKeyword string
	FilterStatus  string
	Statuses      []StatusOptionView
	Page          int
	Limit         int
	Total         int64

	// 行装配的原料：RowDetails 是本页每行的商品详情（列表项不含变体明细，逐行取）；
	// Categories 是**分类树**，Flat 是控制器扁平化后的行（两棵树都留着 —— 行渲染按
	// 父在前子紧随的顺序读 Flat，分类选择器按树读 Categories，见 products.html 的两处）。
	RowDetails []*ProductResp
	Categories []*CategoryResp
	Flat       []*CategoryResp
	Brands     []*BrandResp
	Tags       []*TagResp

	// 页面上三处抽屉/表单要的选项（事实 + 词条 key，取词在渲染期）。
	WarehouseOptions    []WarehouseOptionView
	AttributeOptions    []AttributeOptionView
	WarehouseSKUOptions []WarehouseSKUGroupView

	// 批量改价抽屉的规则清单、尾数处理清单与表单初值。
	Rules     []*PricingRuleTypeResp
	Roundings []*PricingRoundingOptionResp
	Form      PricingForm

	// Products 成品行（`[]map[string]any` 与 gin.H 同型，dto 因此不必 import gin）；
	// Pagination 分页条事实；ListQuery 筛选上下文（表单 action 的 query，写动作失败时
	// 由 pkg/backpath 读回，见 product_jump.go 的 productListBackKeys）。
	Products   []map[string]any
	Pagination pagination.Page
	ListQuery  string

	// LoadFailed 本次请求**没读到**（不是「读到了、只是空的」）；LoadError 由控制器
	// 按当前语言归口后填。
	LoadFailed bool
	LoadError  string

	// HTTPStatus 这一屏的档位：200 读到 / 400 参数不合法 / 500 主数据没读到（导出理由同
	// ProductNewPageView.HTTPStatus）。
	HTTPStatus int
}

// PageStatus 这一屏的 HTTP 档位（adminui.PageResult）；零值 = 200。
func (v ProductListPageView) PageStatus() int { return v.HTTPStatus }

// PageNoStore 失败页不该被缓存：重试必须真的回到服务端（否则拿到的是缓存的失败页）。
func (v ProductListPageView) PageNoStore() bool { return v.LoadFailed }

// StatusOptionView 状态下拉的一项（值 + 词条 key + 是否选中）。
//
// LabelKey / LabelFallback 而不是成品译文的理由同 WarehouseOptionView：取词在渲染期，
// 控制器提前翻好就把这一句锁死在一种语言（英文界面上留中文，不报错、门禁也不红）。
type StatusOptionView struct {
	Value         string
	LabelKey      string
	LabelFallback string
	Selected      bool
}

// ProductDetailPageReq 商品详情整页的取数入参。
type ProductDetailPageReq struct {
	// ProjectID 查询串里的工程 id；空串 = 取工程清单的第一个。
	ProjectID string
	// ProductID 查询串里的商品 id；空串 = 没给参数（与「商品已被删除」同一档：404）。
	ProductID string
}

// ProductDetailPageView 商品详情整页的渲染视图（读模型产出 + 行装配原料 + 控制器补的派生值）。
//
// 字段的写者与 ProductListPageView 同一套约定：
//   - **service 写**：Projects / SelectedProject / ProductID / HasProduct / WarehouseOptions /
//     Detail / Categories / Brands / Tags（取数结果）；
//   - **控制器写**：Title / Menu（页壳）、BackURL（URL 形状属 HTTP）、Flat（扁平行）、
//     Product（成品行）、Bundle / TplPanel（两块面板各自的取数与组装）；
//   - **控制器在失败时写**：LoadError。
type ProductDetailPageView struct {
	Title string `view:"title"`
	Menu  string `view:"menu"`

	Projects        []projectdto.ProjectResp
	SelectedProject string
	ProductID       string
	// HasProduct 为假 = 没读到商品（不存在 / 没给参数）：模板据此走空态分支，
	// 与「读不出来」（LoadFailed）是两档 —— 前者再试也没用，后者可能重试就成功。
	HasProduct bool

	// WarehouseOptions 行数据组装（库存列）要按工程仓库清单补齐未入库的仓 —— 与列表页 /
	// 编辑页共用同一份组装，取数口径不能分叉。
	WarehouseOptions []WarehouseOptionView

	// —— 行装配原料（Product 成品行由控制器的 productRow 组装）——
	// Categories 是**分类树**（扁平行由控制器的 flattenCategoryTree 做，分类页也在用它）。
	Detail     *ProductResp
	Categories []*CategoryResp
	Brands     []*BrandResp
	Tags       []*TagResp
	Flat       []*CategoryResp

	// —— 控制器装配的产物 ——
	// Product 是 productRow 的输出（列表 / 详情 / 编辑三页共用同一份组装）；
	// Bundle 只在 type=bundle 时给（模板据 isset 整块跳过）；TplPanel 是详情模板面板。
	Product  map[string]any
	Bundle   map[string]any
	TplPanel map[string]any
	// BackURL 回列表的链接（带工程上下文，不掉回默认工程）。
	BackURL string

	LoadFailed bool
	LoadError  string
	HTTPStatus int
}

// PageStatus 这一屏的 HTTP 档位（adminui.PageResult）；零值 = 200。
func (v ProductDetailPageView) PageStatus() int { return v.HTTPStatus }

// PageNoStore 非 200 的档位都不该被缓存 —— **404 也一样**：商品可能刚被删，也可能刚被建回来，
// 重试必须真的回到服务端确认。这与「失败页」不是同一件事：404 走的是模板自带的空态分支
// （那是「读到了、库里没有」，不是「没读到」）。
func (v ProductDetailPageView) PageNoStore() bool {
	return v.HTTPStatus != 0 && v.HTTPStatus != http.StatusOK
}

// ProductEditPageReq 商品编辑整页的取数入参。
type ProductEditPageReq struct {
	// ProjectID 查询串 / 表单里的工程 id；空串 = 取工程清单的第一个。
	ProjectID string
	// ProductID 要编辑的商品 id；空串 = 没给参数 → **200 空表单**（不是 404）：
	// 编辑页是写入口，让运营看到一个空表单比看到一个 404 有用（详情页反过来）。
	// 给了 id 但库里没有才是 404（与详情页同一档：空态、不进失败槽）。
	ProductID string
}

// ProductEditPageView 商品编辑整页的渲染视图（读模型产出 + 行装配原料 + 控制器补的派生值与回显）。
//
// 与 ProductDetailPageView 的两点不同：
//   - 多一组 AttributeOptions —— 编辑表单的勾选列表用属性组候选，详情页用的是逗号分隔的 id 输入框；
//   - 多两个**控制器侧**的字段：Err（原地重渲染的失败文案）与 EditBlocked（属性组读不到时撤掉
//     保存入口）。两者回答的是「这次提交怎么样」，不是「库里有什么」，本层不参与。
//
// **表单回显（echo）不在本层**：校验失败重渲染时要用**用户刚提交的值**覆盖读回来的值，
// 而提交值在请求体里 —— 那是控制器的事（inbound/http 的 formEchoFrom），
// 也是这一屏与新建页 / 列表页 / 详情页唯一多出来的一层。
type ProductEditPageView struct {
	Title string `view:"title"`
	Menu  string `view:"menu"`

	Projects        []projectdto.ProjectResp
	SelectedProject string
	ProductID       string
	// HasProduct 为假 = 没读到商品：模板据此走空态分支（与详情页同一档）。
	HasProduct bool

	WarehouseOptions []WarehouseOptionView
	AttributeOptions []AttributeOptionView

	// —— 行装配原料（Product 成品行由控制器的 productRow 组装）——
	// Categories 是**分类树**（扁平行由控制器的 flattenCategoryTree 做）。
	Detail     *ProductResp
	Categories []*CategoryResp
	Brands     []*BrandResp
	Tags       []*TagResp
	Flat       []*CategoryResp

	// —— 控制器装配的产物 ——
	Product  map[string]any
	TplPanel map[string]any
	BackURL  string

	// Err 表单校验失败 / 选项读取失败的原地重渲染文案（控制器填）。
	Err string
	// EditBlocked 属性组读不到时撤掉保存入口：选择器缺失与主动取消勾选在 POST 上同形，
	// 读故障时不能装作「用户取消了全部引用」。
	EditBlocked bool
	// AttributeOptionsErr 属性组候选读取失败的原因（模板不读）。
	// 控制器据此把 EditBlocked 置真并按当前语言归口文案 —— 本层不认识 HTTP，只交出事实。
	AttributeOptionsErr error

	// —— 表单字段的回填文本与勾选态（控制器算；echo=true 时用**这次提交的值**覆盖）——
	// Statuses 当前选中档；AttributeChecks 属性组勾选列表；四个 *Text 是图集与数值字段的
	// 文本形态（表单里是 textarea / input，库里是可空数值与切片）。
	Statuses         []StatusOptionView
	AttributeChecks  []AttributeCheckOptionView
	ImagesText       string
	ImageAltsText    string
	WeightText       string
	DefaultPriceText string
	// IsBundle 捆绑容器：构成表在详情页只读展示，编辑入口在独立页 —— 这里只给链接标记
	//（type=variant 时为假，模板据 isset 整块跳过）。
	IsBundle bool

	LoadFailed bool
	LoadError  string
	HTTPStatus int
}

// PageStatus 这一屏的 HTTP 档位（adminui.PageResult）；零值 = 200。
func (v ProductEditPageView) PageStatus() int { return v.HTTPStatus }

// PageNoStore 失败页不该被缓存：重试必须真的回到服务端（否则拿到的是缓存的失败页）。
//
// 判据与详情页一致：**非 200 一律 no-store**。404「商品不存在」不走失败槽（那是「读到了、
// 库里没有」，模板给的是空态），但它同样是一次不该被缓存的结果 —— 商品刚被建回来时，
// 缓存的 404 会把运营挡在门外。
func (v ProductEditPageView) PageNoStore() bool {
	return v.HTTPStatus != 0 && v.HTTPStatus != http.StatusOK
}

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

// ProductCreateFormReq 商品建表单的取数入参。
type ProductCreateFormReq struct {
	// ProjectID 表单里的工程 id；空串 = 没给，候选一律给空列表（表单照常渲染）。
	ProjectID string
}

// ProductCreateFormView 商品建表单的渲染视图（本模块第五个读模型，与四个整页同一形态）。
//
// 「表单」不是另一种响应形态：后端给数据、Jet 渲染 HTML —— 这份视图撑的就是**用数据渲染出来的
// 那一段 HTML**（首屏整页里用，提交失败重渲染时也用同一份），两者共用同一个装配助手，
// 不另造一套。
//
// 与四个整页视图的两点不同：
//   - 没有工程清单（不渲染工程下拉）：工程来自表单字段，这里只带 id；
//   - 多一组 FormEcho* —— 那是**请求边界**的产物（本次提交了什么），不是库里的数据，
//     所以由控制器填（同编辑页的表单回显：service 不认识 *gin.Context）。
type ProductCreateFormView struct {
	SelectedProject string

	WarehouseOptions    []WarehouseOptionView
	AttributeOptions    []AttributeOptionView
	WarehouseSKUOptions []WarehouseSKUGroupView

	// SubmitErr 上一次提交的失败文案（当前语言；控制器按出口归口后填）。
	SubmitErr string

	// FormEcho / FormEchoChecked / FormEchoMulti 回填值：控件名 → 本次提交的值。
	// 三份分别服务文本输入、单选、复选框组（同名多值）。
	//
	// 类型是 map[string]any 而不是各自的具体类型：三份的**值**语义不同（串 / 布尔 / 串切片），
	// 由模板判断，与 formEchoData 的产出同型 —— 换的是载体（结构体字段），不是形状。
	//
	// 门控契约（改这里之前先看模板第一行）：模板用 `cfFill := isset(.FormEcho)` 区分
	// 「空表单」与「回填表单」，靠的是**这个键在不在**。所以：
	//   · 本视图必须**恒填**这三键（控制器那边无条件赋，哪怕本次没提交任何字段）；
	//   · 首屏视图（ProductNewPageView）必须**没有**同名字段 —— 它进的是同一份模板。
	// 两边任一被改动，门控就失效，症状是静默的：首屏显示上次提交的值，或失败后回填全丢。
	FormEcho        map[string]any
	FormEchoChecked map[string]any
	FormEchoMulti   map[string]any

	// LoadFailed 取数失败：渲染方据此退回整页提示（这一段 HTML 撑不起来时不留半个表单）。
	// 这里**没有 LoadError** —— 失败不留在这份视图里，驱动原文只走 err 返回值进日志。
	LoadFailed bool
	HTTPStatus int
}

// PageStatus 这一屏的 HTTP 档位（adminui.PageResult）；零值 = 200。
func (v ProductCreateFormView) PageStatus() int { return v.HTTPStatus }

// PageNoStore 取数失败时不该被缓存：重试必须真的回到服务端。
func (v ProductCreateFormView) PageNoStore() bool {
	return v.HTTPStatus != 0 && v.HTTPStatus != http.StatusOK
}

// ProductPricingPageReq 定价工具页的取数入参。
//
// 三个字段都是**写路径的产物**（试算失败要原地重渲染，把用户填的表单与试算结果带回来）：
// 读模型不认识 gin.Context，提交值只能由控制器读出来后经这里传进去 —— 与编辑页的表单回显
// 同一分工（service 只认数据，不认请求）。
type ProductPricingPageReq struct {
	// ProjectID 查询串里的工程 id；空串 = 取工程清单的第一个。
	// Form.ProjectID 非空时以表单为准（试算/应用路径带着它提交）。
	ProjectID string
	// Form 表单回显（试算路径给本次提交值；首次 GET 为 nil = 用默认初值）。
	Form *PricingForm
	// Preview 试算结果（只有试算路径给；nil = 还没试算过）。
	Preview *PricingPreviewResp
	// ErrMsg 试算失败的**成品串**（例外形态：这一句来自 productErrText，已按当前语言翻好，
	// 与「提示页出口」同类 —— 表单要留在眼前，所以不跳转）。
	ErrMsg string
}

// ProductPricingPageView 定价工具页的渲染视图（本模块第六个读模型）。
//
// 这一屏**没有失败档**：三处取数各自降级（工程清单读不到 → 空下拉；规则与尾数处理是
// 注册表快照，本身无 error；留痕台账读不到 → 空台账），页面主体（改价表单）始终撑得起来。
// 但仍实现 adminui.PageResult：装配缺陷那一条 500 要靠它传出去
// （PageStatus 返回 0 时渲染出口按 200 处理，所以正常档不必显式设码）。
type ProductPricingPageView struct {
	Title string `view:"title"`
	Menu  string `view:"menu"`

	Projects        []projectdto.ProjectResp
	SelectedProject string

	// Rules / Roundings 参考表；Form 是表单初值（首次 GET）或本次提交的回显值（试算失败）。
	Rules     []*PricingRuleTypeResp
	Roundings []*PricingRoundingOptionResp
	Form      PricingForm

	// HasPreview 为假 = 还没试算过（模板整块跳过）；摘要与明细都在 service 算好，
	// 模板只做展示（也避免模板解指针）。
	HasPreview     bool
	PreviewSummary map[string]any
	PreviewRows    []map[string]any

	// History 调价留痕台账（批次 + 逐变体明细）；Err 是试算失败的成品串。
	History []map[string]any
	Err     string

	// ListQuery 页面上下文（表单 action 的 query，写动作的提示页由 pkg/backpath 读回）：
	// URL 形状属 HTTP，由控制器填。
	ListQuery string

	// HTTPStatus 这一屏的档位：正常恒 200（本页三处取数各自降级，没有取数失败档）。
	// 唯一的非 200 来源是**装配缺陷**（控制器断言不到读模型）——那时给 500，
	// 让「没接上」与「这一屏本来就没数据」在页面上看得出区别。
	HTTPStatus int
}

// ProductAttributesPageReq 属性库整页的取数入参（四个都是查询串参数）。
//
// Variation 的归一不在这里（`?variation=yes` 会被归一成「全部」，见 service 的读模型）：
// 归一写在控制器时，筛选回显与过滤条件会各拿一份原始串。
type ProductAttributesPageReq struct {
	ProjectID string
	Keyword   string
	Variation string
	// Page 已由控制器归一（productPageNumber 是跨页共用工具）；读模型还会按总数收敛一次。
	Page int
}

// ProductAttributesPageView 属性库整页的渲染视图。
//
// 两类字段分得很清楚：
//   - **取数结果**（AttributeDetails / Total / Projects …）由 service 写；
//   - **装配产物**（Attributes / AttrCreateForm / Pagination / ListQuery / LoadError）
//     由控制器写 —— 行里挂着两个抽屉表单的数据，它们要 csrf 与取词函数（gin.Context 的产物）。
//
// 为什么用 AttributeDetails 而不是让 Attributes 直接装 []*AttributeResp：模板读的
// `.Attributes` 是有 `EditForm` / `ValuesForm` 两键的**成品行**（同 ProductListPageView 的
// RowDetails → Products 分工）。两个键混用会让「原料」与「成品」在模板里看起来是同一件事。
type ProductAttributesPageView struct {
	Title string `view:"title"`
	Menu  string `view:"menu"`

	Projects        []projectdto.ProjectResp
	SelectedProject string

	// FilterKeyword / FilterVariation 回显，Filtered 是空态分档依据（「筛出来是空的」与
	// 「这个工程本来就没有属性组」必须给不同文案）。
	FilterKeyword   string
	FilterVariation string
	Filtered        bool

	// AttributeDetails 本页每行的属性组（含属性值）：行装配的原料。
	AttributeDetails []*AttributeResp
	// Attributes 成品行（模板 `{{range .Attributes}}` 读 ID / Key / Name / IsVariation /
	// Sort / ValueCount / EditForm / ValuesForm）。`map[string]any` 与 gin.H 同型，
	// dto 因此不必 import gin。
	Attributes []map[string]any
	// AttrCreateForm 「新建属性组」抽屉的表单数据（页头与空态两个入口共用同一份）。
	AttrCreateForm map[string]any

	// Total / Page / Limit 分页**事实**；Pagination 由控制器按当前请求补基地址后注入。
	Total      int64
	Page       int
	Limit      int
	Pagination pagination.Page

	// ListQuery 筛选上下文（表单 action 的 query）：写动作失败时由 pkg/backpath 读回，
	// 回跳列表页不丢关键词 / 变体筛选 / 页码。URL 形状属 HTTP，由控制器填。
	ListQuery string

	// LoadFailed 本次请求**没读到**（不是「读到了、只是空的」）；LoadError 由控制器按
	// 当前语言归口后填。HTTPStatus：200 读到 / 400 参数不合法 / 500 主数据没读到。
	LoadFailed bool
	LoadError  string
	HTTPStatus int
}

// PageStatus 这一屏的 HTTP 档位（adminui.PageResult）；零值 = 200。
func (v ProductAttributesPageView) PageStatus() int { return v.HTTPStatus }

// PageNoStore 失败页不该被缓存：重试必须真的回到服务端（否则拿到的是缓存的失败页）。
func (v ProductAttributesPageView) PageNoStore() bool { return v.LoadFailed }

// PageStatus 这一屏的 HTTP 档位（adminui.PageResult）；零值 = 200。
func (v ProductPricingPageView) PageStatus() int { return v.HTTPStatus }

// PageNoStore 装配缺陷页不该被缓存：重试必须真的回到服务端。
func (v ProductPricingPageView) PageNoStore() bool {
	return v.HTTPStatus != 0 && v.HTTPStatus != http.StatusOK
}

// ProductDetailTemplatePageReq 「详情页模板」整页的取数入参（两个查询串参数）。
type ProductDetailTemplatePageReq struct {
	// ProjectID 工程 id；空串 = 取工程清单的第一个。
	ProjectID string
	// ProductID 商品 id；空串 = 还没选商品（菜单入口），控制器据此走引导出口。
	ProductID string
}

// DetailTemplateProductView 这一屏展示的商品（只四个字段，不给整份商品）。
type DetailTemplateProductView struct {
	ID   string
	Name string
	Slug string
	// URLPath 发布路径的**表单初值**（按选中工程的 URL 规则派生，用户可改）。
	// 不是已发布路径 —— 后者在 InstanceURL 里。
	URLPath string
}

// DetailTemplateOptionView 模板候选的渲染行。
//
// IsBound / IsDefault 由**本屏**算出（实例实际绑的那一套 / 当前解析到的默认那一套），
// 不复用内容模板模块的 IsDefault：那是 (工程, 类型) 作用域的生效标记，
// 与「这个商品的实例绑在谁身上」不是一回事，两者混用会让「已绑定」被显示成「类型默认」。
type DetailTemplateOptionView struct {
	ID           string
	Name         string
	DraftVersion int64
	UpdatedAt    string
	IsBound      bool
	IsDefault    bool
}

// ProductDetailTemplatePageView 「详情页模板」整页的渲染视图（本模块第八个读模型）。
//
// Ready 与 LoadFailed 是两件事：Ready = 「能不能给这一屏干活」（缺模板能力时为假，
// 走页内提示而不是失败页），LoadFailed = 「取数没读到」（失败槽）。两者混成一个布尔时
// 「未装配」会被显示成「数据读不出来」，而前者要人去查装配、后者重试就好。
type ProductDetailTemplatePageView struct {
	Title string `view:"title"`
	Menu  string `view:"menu"`

	Projects        []projectdto.ProjectResp
	SelectedProject string

	// Ready 主数据齐了（含 Product 与 Templates）；为假时控制器走「未装配」或
	// 「缺商品」两个出口，不渲染模板清单。
	Ready bool

	Product           DetailTemplateProductView
	Templates         []DetailTemplateOptionView
	TemplateCount     int
	BoundTemplateID   string
	DefaultTemplateID string
	InstanceExists    bool
	InstanceStatus    string
	InstanceURL       string

	// NoProduct 没给 product 参数：控制器据此走引导出口（整页提示回列表）而不是渲染本页。
	// 它不是参数非法 —— 本页本来就有「还没选商品」的入口形态。
	NoProduct bool

	LoadFailed bool
	LoadError  string
	HTTPStatus int
}

// PageStatus 这一屏的 HTTP 档位（adminui.PageResult）；零值 = 200。
func (v ProductDetailTemplatePageView) PageStatus() int { return v.HTTPStatus }

// PageNoStore 失败页不该被缓存：重试必须真的回到服务端。
func (v ProductDetailTemplatePageView) PageNoStore() bool {
	return v.HTTPStatus != 0 && v.HTTPStatus != http.StatusOK
}

// ProductBundlePageReq 捆绑配置页的取数入参。
//
// 六个上下文字段 + 一个写路径产物：
//
//	· ProjectID / ProductID 页面的两个选择器（入口有两套字段名，控制器认完再传进来）；
//	· Source / SourceProductID / SourceWarehouseID 成员来源面板的当前上下文
//	  （用 GET 重新渲染，不是前端拼参数 —— 属性值勾选清单与仓库 SKU 候选都由服务端给）；
//	· Submitted 写失败原地重渲染时的提交快照（成员清单 + 配置数值）。
//
// Submitted 为什么由控制器传而不是读模型自己去读：它在**请求体**里，读模型不认识
// gin.Context（与定价页的 Form / 预览结果、编辑页的表单回显同一分工）。
type ProductBundlePageReq struct {
	ProjectID       string
	ProductID       string
	Source          string
	SourceProductID string
	// SourceWarehouseID 选中来源仓（仓库来源面板据此只列那个仓的一屏候选）。
	SourceWarehouseID string
	// Submitted nil = 首屏 GET（成员清单与配置数值都取库里的配置）；
	// 非 nil = 写失败重渲染，**成员清单一律按它渲染**。
	Submitted *BundleSubmittedForm
}

// BundleSubmittedForm 写失败重渲染时带回来的**原始提交**（成员表的并行数组 + 三个配置数值）。
//
// 为什么不是 BundleConfig：配置里没有空白行（保存时 `if vid == "" { continue }` 跳过了它），
// 而渲染必须保住空行占的那个位置 —— 成员表的行与这些数组按 DOM 顺序一一对应，
// 摘掉空行会让它后面的成员整体上移，用户看到的是「我插在中间那一行没了，顺序也变了」。
//
// 数组字段名与表单字段一一对应（VariantIDs ↔ variantId …），控制器从 PostForm 原样取出，
// 不在这里做任何清理：校验与归一在 service 的保存路径上（这里是**渲染**输入）。
type BundleSubmittedForm struct {
	VariantIDs    []string
	Required      []string
	DefaultQty    []string
	MinQty        []string
	MaxQty        []string
	SourceKinds   []string
	WarehouseIDs  []string
	WarehouseSKUs []string
	ExternalSKUs  []string
	// 三个配置数值（与数组同一份提交）：成员留下了、数值却回到 0，用户会以为配置被改过。
	MaxOptions  int
	MinTotalQty int
	MaxTotalQty int
}

// BundleOptionRow 成员表的一行（渲染行，非原始配置）。
//
// 与原始 BundleOption 分开：Available（真源可用量）与 SKULabel 都是这一屏才算得出的，
// 混进配置结构会让「配置里存的字段」与「页面显示的字段」看起来是同一件事。
type BundleOptionRow struct {
	VariantID  string
	SKULabel   string
	Required   bool
	DefaultQty int
	MinQty     int
	MaxQty     int
	// Available 真源可用量；新增的空白行没有可用量可言（还没选 SKU），恒为 0。
	Available int
	// 来源快照（仅作溯源与展示，不是身份）：四个字段原样回填进隐藏域；
	// 标签已拆成事实段（SourceParts，取词在模板）。
	SourceKind   string
	WarehouseID  string
	WarehouseSKU string
	ExternalSKU  string
	SourceParts  []BundleSourcePart
}

// BundleCandidateView 候选 SKU 下拉项（id + 可读标签）。
type BundleCandidateView struct {
	ID    string
	Label string
}

// BundleSourceWarehouseGroupView 来源仓下拉项 + 该仓的仓库 SKU 勾选清单。
type BundleSourceWarehouseGroupView struct {
	WarehouseID string
	Label       string
	IsDefault   bool
	Selected    bool
	Items       []WarehouseSKUItemView
}

// ProductBundlePageView 捆绑配置页的渲染视图（本模块第九个读模型）。
//
// 取数原料与装配产物分开命名：Detail / Skus / Warehouses / WarehouseGroups / Products
// 是读回来的，Rows / CandidateSKUs / Sources / SourceProducts / SourceWarehouses 是装出来的。
// 混在一个字段名下会让人以为模板里看到的就是库里的东西。
type ProductBundlePageView struct {
	Title string `view:"title"`
	Menu  string `view:"menu"`

	Projects        []projectdto.ProjectResp
	SelectedProject string
	Products        []*ProductResp
	SelectedProduct string
	// Detail 库里的捆绑配置（写失败重渲染时 Config 已被 Submitted 覆盖）。
	Detail *BundleConfigResp
	// Skus 本工程的候选 SKU（成员标签与候选下拉都从它来）。
	Skus []*BundleSKUResp

	// Rows 成员表行；CandidateSKUs 候选下拉；SkuCount 候选条数（模板提示用）。
	Rows          []BundleOptionRow
	CandidateSKUs []BundleCandidateView
	SkuCount      int
	// MaxOptions 成员数上限（表单约束 + 空行补齐的判据）。
	MaxOptions int

	// 成员来源面板：三种来源 + 各自的候选数据。槽位收事实（Key + 中文兜底 / 数据名），取词在模板。
	Sources        []map[string]any
	Source         string
	SourceProducts []map[string]any
	// SourceAttributes 与 SourceWarehouses 的选择依据分别是「选中来源商品」与「选中来源仓」。
	SourceAttributes  []*AttributeResp
	SourceWarehouses  []BundleSourceWarehouseGroupView
	SourceWarehouseID string

	// Cfg* 配置数值的回填源（提交快照优先，其次才是库里的配置）。
	CfgMaxOptions  int
	CfgMinTotalQty int
	CfgMaxTotalQty int

	// Submitted 这一屏有「用户刚提交的快照」（写失败重渲染）。
	// SaveBlocked 页面重建失败时禁止保存：配置读不出来就无法保证这次提交的是完整配置，
	// 宁可让用户先「重新载入」，也不要缺配置误写。
	Submitted   bool
	SaveBlocked bool

	// LoadError 由控制器按当前语言归口后填（与前八屏同一分工：文案是 HTTP 侧的取词）。
	LoadFailed bool
	LoadError  string
	// HTTPStatus 这一屏的档位；正常恒 200。
	HTTPStatus int
}

// PageStatus 这一屏的 HTTP 档位（adminui.PageResult）；零值 = 200。
func (v ProductBundlePageView) PageStatus() int { return v.HTTPStatus }

// PageNoStore 失败页不该被缓存：重试必须真的回到服务端。
func (v ProductBundlePageView) PageNoStore() bool {
	return v.HTTPStatus != 0 && v.HTTPStatus != http.StatusOK
}

// ProductTranslationsPageReq 商品译文工作台的取数入参（工程 / 商品 / 目标语言，三者都可空）。
type ProductTranslationsPageReq struct {
	ProjectID string
	ProductID string
	Lang      string
}

// ProductTranslationsPageView 商品译文工作台的一屏。
//
// 取数结果（ProjectOptions / Langs / SourceLang / Groups）与保存结论（Saved / Written /
// LangInvalid / Errors）合在一份视图里：保存路径**先重建这一屏再校验**，两者本来就是
// 同一次取数的产物 —— 拆成两份会让「保存失败后重渲的那一页」与「GET 出来的那一页」
// 慢慢长成两个形状。
type ProductTranslationsPageView struct {
	Title string `view:"title"`
	Menu  string `view:"menu"`
	// ProjectID / ProductID / Lang 实际生效的三个上下文（空工程已回退到第一个工程、
	// 空语言已落到第一个目标语言）—— 表单回填与保存校验都用它们，不用请求里那个原始值。
	ProjectID string
	ProductID string
	Lang      string
	Langs     []TranslationLangOption
	// SourceLang 站点源语言（= 站点默认语言）；判定依据见 service 的 translationSourceLang。
	SourceLang string
	// NoTargetLang 没有任何「非源语言」的启用语言：没有可翻译的目标，页面给可行动空态。
	NoTargetLang   bool
	Groups         []ProductTranslationGroup
	RowCount       int
	Done           int
	Total          int
	ProjectOptions []ProjectTranslationOption

	// Errors 逐行 / 页面级错误（校验或写入失败时重渲本页）。元素是**事实**
	//（哪一行 + 词条 key + 中文兜底），取词留给页面的 tf。
	Errors []TranslationRowError
	// LangInvalid 目标语言不在站点启用清单里（保存被拒，页面重渲并列出结论）。
	LangInvalid bool
	// Saved 这次保存已经落库（或确认无变化）：控制器据此走提示页出口。
	Saved bool
	// Written 本次实际写入条数（0 = 没有需要写入的变化）。
	Written int
}

// SaveProductTranslationsReq 保存提交：三组并行数组按行一一对应（与表单字段同形）。
//
// 数组原样带过来，长度对齐与逐行校验都在读模型侧做 —— 控制器只负责把表单读成数组。
type SaveProductTranslationsReq struct {
	ProjectID string
	ProductID string
	Lang      string
	Contexts  []string
	Hashes    []string
	Targets   []string
}
